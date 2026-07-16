package server

import (
	"net/http"

	"musicseer/internal/store"
)

// Plex sign-in (plex.tv PIN flow, Overseerr-style).
//
// Admin side: connect their own Plex account once to pick which server's
// members are allowed in; stored in settings (plex_machine_id / plex_enabled).
// User side: "Sign in with Plex" creates a PIN, the browser opens plex.tv to
// approve it, the login page polls until a token appears, then we verify the
// account can access the configured server and create/link a local user.

func (s *Server) plexClientID() string {
	id := s.st.Setting("plex_client_id")
	if id == "" {
		id = "musicseer-" + newToken()[:16]
		s.st.SetSetting("plex_client_id", id)
	}
	return id
}

func (s *Server) plexEnabled() bool {
	return s.st.Setting("plex_enabled") == "1" && s.st.Setting("plex_machine_id") != ""
}

type plexStartResponse struct {
	PinID   int64  `json:"pinId"`
	Code    string `json:"code"`
	AuthURL string `json:"authUrl"`
}

// handlePlexStart begins a PIN flow. Public when Plex login is enabled;
// admins may always use it (to do the initial server setup).
func (s *Server) handlePlexStart(w http.ResponseWriter, r *http.Request) {
	u := s.currentUser(r)
	if !s.plexEnabled() && (u == nil || u.Role != "admin") {
		jsonError(w, http.StatusForbidden, "Plex sign-in is not enabled")
		return
	}
	pin, authURL, err := s.eng.Plex.CreatePin(r.Context(), s.plexClientID())
	if err != nil {
		jsonError(w, http.StatusBadGateway, "plex.tv unreachable: "+err.Error())
		return
	}
	jsonWrite(w, http.StatusOK, plexStartResponse{PinID: pin.ID, Code: pin.Code, AuthURL: authURL})
}

// handlePlexPoll checks a PIN once and, when approved, signs the user in.
func (s *Server) handlePlexPoll(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PinID int64 `json:"pinId"`
	}
	if err := decodeBody(r, &body); err != nil || body.PinID == 0 {
		jsonError(w, http.StatusBadRequest, "pinId required")
		return
	}
	token, err := s.eng.Plex.CheckPin(r.Context(), s.plexClientID(), body.PinID)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	if token == "" {
		jsonWrite(w, http.StatusOK, map[string]bool{"pending": true})
		return
	}

	// Admin doing initial setup: return their servers instead of logging in.
	if u := s.currentUser(r); u != nil && u.Role == "admin" && r.URL.Query().Get("setup") == "1" {
		servers, err := s.eng.Plex.Servers(r.Context(), s.plexClientID(), token)
		if err != nil {
			jsonError(w, http.StatusBadGateway, err.Error())
			return
		}
		jsonWrite(w, http.StatusOK, map[string]any{"servers": servers})
		return
	}

	if !s.plexEnabled() {
		jsonError(w, http.StatusForbidden, "Plex sign-in is not enabled")
		return
	}

	plexUser, err := s.eng.Plex.User(r.Context(), s.plexClientID(), token)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	ok, err := s.eng.Plex.HasServer(r.Context(), s.plexClientID(), token, s.st.Setting("plex_machine_id"))
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	if !ok {
		jsonError(w, http.StatusForbidden, "this Plex account does not have access to the configured Plex server")
		return
	}

	user := s.plexUserFromAccount(plexUser.ID, plexUser.Username, plexUser.Email)
	if user == nil {
		jsonError(w, http.StatusForbidden, "could not sign you in with Plex; an unlinked local account may already use this name — contact the administrator")
		return
	}
	s.startSession(w, r, user.ID)
	jsonWrite(w, http.StatusOK, map[string]any{"user": user})
	if _, computedAt, err := s.st.Recommendations(user.ID, "similar"); err != nil || computedAt.IsZero() {
		s.eng.RefreshUserAsync(user.ID)
	}
}

// plexUserFromAccount maps a plex.tv account onto a local user, keyed ONLY on
// the immutable numeric Plex account id. It deliberately does NOT fall back to
// matching by Plex username/email: those are freely editable by the account
// owner, so matching on them let any Plex-server member sign in as a local
// account whose name/email they copied — including the admin (account takeover).
// Linking an existing local account to Plex must be done from an already
// authenticated session, never inferred during sign-in.
func (s *Server) plexUserFromAccount(id int64, username, email string) *store.User {
	if id == 0 {
		s.log.Warn("plex account has no numeric id; refusing sign-in", "username", username)
		return nil
	}
	plexID := itoa64(id)
	if u, err := s.st.UserByPlexID(plexID); err == nil {
		return u
	}
	uid, err := s.st.CreateUser(username, email, "", "user", false)
	if err != nil {
		// A local account with this username/email already exists but is NOT
		// linked to this Plex id. Refuse rather than adopt it. The operator can
		// link the accounts deliberately from an authenticated session.
		s.log.Warn("plex sign-in refused: unlinked local account name collision or create error",
			"username", username, "err", err)
		return nil
	}
	s.st.LinkPlex(uid, plexID)
	u, _ := s.st.UserByID(uid)
	s.log.Info("plex user signed up", "username", username, "plex_id", plexID)
	return u
}

// ---------- admin config ----------

func (s *Server) handlePlexConfigGet(w http.ResponseWriter, _ *http.Request, _ *store.User) {
	jsonWrite(w, http.StatusOK, map[string]any{
		"enabled":    s.st.Setting("plex_enabled") == "1",
		"machineId":  s.st.Setting("plex_machine_id"),
		"serverName": s.st.Setting("plex_server_name"),
	})
}

func (s *Server) handlePlexConfigSet(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var body struct {
		Enabled    bool   `json:"enabled"`
		MachineID  string `json:"machineId"`
		ServerName string `json:"serverName"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Enabled && body.MachineID == "" {
		jsonError(w, http.StatusBadRequest, "connect a Plex server before enabling")
		return
	}
	s.st.SetSetting("plex_machine_id", body.MachineID)
	s.st.SetSetting("plex_server_name", body.ServerName)
	if body.Enabled {
		s.st.SetSetting("plex_enabled", "1")
	} else {
		s.st.SetSetting("plex_enabled", "0")
	}
	s.handlePlexConfigGet(w, r, nil)
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
