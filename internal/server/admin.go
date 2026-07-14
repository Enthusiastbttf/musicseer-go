package server

import (
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"musicseer/internal/clients"
	"musicseer/internal/store"
)

// ---------- users ----------

func (s *Server) handleUsersList(w http.ResponseWriter, _ *http.Request, _ *store.User) {
	users, err := s.st.Users()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonWrite(w, http.StatusOK, users)
}

func (s *Server) handleUserCreate(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var body struct {
		Username       string `json:"username"`
		Email          string `json:"email"`
		Password       string `json:"password"` // empty => Navidrome-auth user
		Role           string `json:"role"`
		CanAutoApprove bool   `json:"canAutoApprove"`
	}
	if err := decodeBody(r, &body); err != nil || strings.TrimSpace(body.Username) == "" {
		jsonError(w, http.StatusBadRequest, "username required")
		return
	}
	if body.Role != "admin" {
		body.Role = "user"
	}
	var hash string
	if body.Password != "" {
		if len(body.Password) < 8 {
			jsonError(w, http.StatusBadRequest, "password must be at least 8 characters")
			return
		}
		h, err := bcrypt.GenerateFromPassword([]byte(body.Password), 12)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		hash = string(h)
	}
	id, err := s.st.CreateUser(strings.TrimSpace(body.Username), body.Email, hash, body.Role, body.CanAutoApprove)
	if err != nil {
		jsonError(w, http.StatusConflict, err.Error())
		return
	}
	u, _ := s.st.UserByID(id)
	jsonWrite(w, http.StatusCreated, u)
}

func (s *Server) handleUserUpdate(w http.ResponseWriter, r *http.Request, admin *store.User) {
	id, err := pathID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "bad id")
		return
	}
	var body struct {
		Role           *string `json:"role"`
		CanAutoApprove *bool   `json:"canAutoApprove"`
		Password       *string `json:"password"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Role != nil && *body.Role != "admin" && *body.Role != "user" {
		jsonError(w, http.StatusBadRequest, "role must be admin or user")
		return
	}
	if body.Role != nil && id == admin.ID && *body.Role != "admin" {
		jsonError(w, http.StatusBadRequest, "cannot demote yourself")
		return
	}
	var hash *string
	if body.Password != nil && *body.Password != "" {
		if len(*body.Password) < 8 {
			jsonError(w, http.StatusBadRequest, "password must be at least 8 characters")
			return
		}
		h, err := bcrypt.GenerateFromPassword([]byte(*body.Password), 12)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		hs := string(h)
		hash = &hs
	}
	if err := s.st.UpdateUser(id, body.Role, body.CanAutoApprove, hash); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	u, _ := s.st.UserByID(id)
	jsonWrite(w, http.StatusOK, u)
}

func (s *Server) handleUserDelete(w http.ResponseWriter, r *http.Request, admin *store.User) {
	id, err := pathID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "bad id")
		return
	}
	if id == admin.ID {
		jsonError(w, http.StatusBadRequest, "cannot delete yourself")
		return
	}
	if err := s.st.DeleteUser(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonWrite(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ---------- instances ----------

type instanceBody struct {
	Name              string `json:"name"`
	Type              string `json:"type"`
	BaseURL           string `json:"baseUrl"`
	Username          string `json:"username"`
	APIKey            string `json:"apiKey"` // plaintext in, encrypted at rest
	IsActive          *bool  `json:"isActive"`
	IsAuthSource      bool   `json:"isAuthSource"`
	QualityProfileID  int64  `json:"qualityProfileId"`
	MetadataProfileID int64  `json:"metadataProfileId"`
	RootFolder        string `json:"rootFolder"`
}

func (s *Server) handleInstancesList(w http.ResponseWriter, _ *http.Request, _ *store.User) {
	instances, err := s.st.Instances("")
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if instances == nil {
		instances = []store.Instance{}
	}
	jsonWrite(w, http.StatusOK, instances)
}

func (s *Server) handleInstanceCreate(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var body instanceBody
	if err := decodeBody(r, &body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid body")
		return
	}
	body.BaseURL = strings.TrimRight(strings.TrimSpace(body.BaseURL), "/")
	if body.Name == "" || body.BaseURL == "" || body.APIKey == "" ||
		(body.Type != "navidrome" && body.Type != "lidarr") {
		jsonError(w, http.StatusBadRequest, "name, type (navidrome|lidarr), baseUrl and apiKey are required")
		return
	}
	enc, err := s.box.Encrypt(body.APIKey)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	active := true
	if body.IsActive != nil {
		active = *body.IsActive
	}
	inst := &store.Instance{
		Name: body.Name, Type: body.Type, BaseURL: body.BaseURL, Username: body.Username,
		APIKeyEnc: enc, IsActive: active, IsAuthSource: body.IsAuthSource,
		QualityProfileID: body.QualityProfileID, MetadataProfileID: body.MetadataProfileID,
		RootFolder: body.RootFolder,
	}
	id, err := s.st.CreateInstance(inst)
	if err != nil {
		jsonError(w, http.StatusConflict, err.Error())
		return
	}
	created, _ := s.st.InstanceByID(id)
	jsonWrite(w, http.StatusCreated, created)
}

func (s *Server) handleInstanceUpdate(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, err := pathID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "bad id")
		return
	}
	existing, err := s.st.InstanceByID(id)
	if err != nil {
		jsonError(w, http.StatusNotFound, "instance not found")
		return
	}
	var body instanceBody
	if err := decodeBody(r, &body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Name != "" {
		existing.Name = body.Name
	}
	if body.BaseURL != "" {
		existing.BaseURL = strings.TrimRight(strings.TrimSpace(body.BaseURL), "/")
	}
	existing.Username = body.Username
	if body.APIKey != "" { // only rotate the key when a new one is supplied
		enc, err := s.box.Encrypt(body.APIKey)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		existing.APIKeyEnc = enc
	}
	if body.IsActive != nil {
		existing.IsActive = *body.IsActive
	}
	existing.IsAuthSource = body.IsAuthSource
	existing.QualityProfileID = body.QualityProfileID
	existing.MetadataProfileID = body.MetadataProfileID
	existing.RootFolder = body.RootFolder
	if err := s.st.UpdateInstance(existing); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonWrite(w, http.StatusOK, existing)
}

func (s *Server) handleInstanceDelete(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, err := pathID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "bad id")
		return
	}
	if err := s.st.DeleteInstance(id); err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonWrite(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleInstanceTest validates credentials before saving. For Navidrome the
// apiKey field carries the account password (Subsonic token auth).
func (s *Server) handleInstanceTest(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var body instanceBody
	if err := decodeBody(r, &body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid body")
		return
	}
	body.BaseURL = strings.TrimRight(strings.TrimSpace(body.BaseURL), "/")
	// Allow testing a saved instance without re-entering the secret.
	if body.APIKey == "" && r.URL.Query().Get("id") != "" {
		jsonError(w, http.StatusBadRequest, "apiKey required for test")
		return
	}
	switch body.Type {
	case "navidrome":
		if err := s.eng.Sub.Ping(r.Context(), body.BaseURL, body.Username, body.APIKey); err != nil {
			jsonError(w, http.StatusBadGateway, err.Error())
			return
		}
		jsonWrite(w, http.StatusOK, map[string]any{"ok": true})
	case "lidarr":
		version, err := s.eng.Lidarr.Ping(r.Context(), body.BaseURL, body.APIKey)
		if err != nil {
			jsonError(w, http.StatusBadGateway, err.Error())
			return
		}
		jsonWrite(w, http.StatusOK, map[string]any{"ok": true, "version": version})
	default:
		jsonError(w, http.StatusBadRequest, "type must be navidrome or lidarr")
	}
}

// handleLidarrOptions returns profiles + root folders for the admin dropdowns.
func (s *Server) handleLidarrOptions(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, err := pathID(r)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "bad id")
		return
	}
	inst, err := s.st.InstanceByID(id)
	if err != nil || inst.Type != "lidarr" {
		jsonError(w, http.StatusNotFound, "lidarr instance not found")
		return
	}
	apiKey, err := s.box.Decrypt(inst.APIKeyEnc)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "cannot decrypt API key")
		return
	}
	quality, metadata, roots, err := s.eng.Lidarr.Options(r.Context(), inst.BaseURL, apiKey)
	if err != nil {
		jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonWrite(w, http.StatusOK, map[string]any{
		"qualityProfiles":  quality,
		"metadataProfiles": metadata,
		"rootFolders":      roots,
	})
}

// ---------- Last.fm ----------

// handleLastfmGet reports whether/where a Last.fm key is configured.
func (s *Server) handleLastfmGet(w http.ResponseWriter, _ *http.Request, _ *store.User) {
	source := "none"
	if s.st.Setting("lastfm_api_key") != "" {
		source = "admin"
	} else if s.eng.LastFMKey() != "" {
		source = "env"
	}
	jsonWrite(w, http.StatusOK, map[string]any{
		"configured": s.eng.UsingLastFM(),
		"source":     source,
	})
}

// handleLastfmSet validates and stores (encrypted) a Last.fm API key at
// runtime — no restart needed. Empty key removes the admin-configured one.
// On any change the similarity cache is cleared (Last.fm and ListenBrainz
// scores are not comparable) and background refreshes are kicked off.
func (s *Server) handleLastfmSet(w http.ResponseWriter, r *http.Request, u *store.User) {
	var body struct {
		APIKey string `json:"apiKey"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid body")
		return
	}
	body.APIKey = strings.TrimSpace(body.APIKey)

	if body.APIKey == "" {
		s.st.SetSetting("lastfm_api_key", "")
	} else {
		// Validate against Last.fm before saving.
		probe := clients.NewLastFM(func() string { return body.APIKey })
		if _, err := probe.TopArtists(r.Context(), 1); err != nil {
			jsonError(w, http.StatusBadRequest, "Last.fm rejected this key: "+err.Error())
			return
		}
		enc, err := s.box.Encrypt(body.APIKey)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.st.SetSetting("lastfm_api_key", enc)
	}

	// Backend switched: stale similarity scores are meaningless now.
	s.st.ClearSimilar()
	ctx := contextWithoutCancel(r)
	go s.eng.SyncTrending(ctx)
	s.eng.RefreshUserAsync(u.ID)

	s.log.Info("lastfm key updated", "configured", s.eng.UsingLastFM())
	s.handleLastfmGet(w, r, u)
}

// ---------- stats & manual sync ----------

func (s *Server) handleStats(w http.ResponseWriter, _ *http.Request, _ *store.User) {
	users, _ := s.st.UserCount()
	artists, _ := s.st.ArtistCount()
	library, _ := s.st.LibraryCount()
	reqCounts, _ := s.st.RequestCounts()
	jsonWrite(w, http.StatusOK, map[string]any{
		"users":          users,
		"cachedArtists":  artists,
		"libraryArtists": library,
		"requests":       reqCounts,
		"jobs":           s.eng.Status(),
		"discovery":      map[bool]string{true: "lastfm", false: "listenbrainz"}[s.eng.UsingLastFM()],
		"version":        Version,
	})
}

func (s *Server) handleSyncNow(w http.ResponseWriter, r *http.Request, u *store.User) {
	job := r.PathValue("job")
	switch job {
	case "trending":
		go s.eng.SyncTrending(contextWithoutCancel(r))
	case "library":
		go s.eng.SyncLibraries(contextWithoutCancel(r))
	case "recommendations":
		s.eng.RefreshUserAsync(u.ID)
	default:
		jsonError(w, http.StatusBadRequest, "unknown job: "+job)
		return
	}
	jsonWrite(w, http.StatusAccepted, map[string]string{"status": "started", "job": job})
}
