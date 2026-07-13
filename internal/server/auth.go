package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"musicseer/internal/store"
)

// handleStatus tells the frontend whether first-run setup is needed and who
// (if anyone) is logged in. Unauthenticated by design.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	count, err := s.st.UserCount()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp := map[string]any{"setupComplete": count > 0, "version": Version}
	if u := s.currentUser(r); u != nil {
		resp["user"] = u
	}
	jsonWrite(w, http.StatusOK, resp)
}

// handleSetup creates the first admin account. Only works when no users exist —
// unlike the old app, there is no permanently-open /auth/register endpoint.
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	count, err := s.st.UserCount()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if count > 0 {
		jsonError(w, http.StatusForbidden, "setup already completed")
		return
	}
	var body struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeBody(r, &body); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid body")
		return
	}
	body.Username = strings.TrimSpace(body.Username)
	if body.Username == "" || len(body.Password) < 8 {
		jsonError(w, http.StatusBadRequest, "username required; password must be at least 8 characters")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), 12)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	id, err := s.st.CreateUser(body.Username, body.Email, string(hash), "admin", true)
	if err != nil {
		jsonError(w, http.StatusConflict, err.Error())
		return
	}
	s.startSession(w, r, id)
	u, _ := s.st.UserByID(id)
	jsonWrite(w, http.StatusCreated, map[string]any{"user": u})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.loginAllowed(ip) {
		jsonError(w, http.StatusTooManyRequests, "too many failed attempts, try again later")
		return
	}
	var body struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if err := decodeBody(r, &body); err != nil || body.Login == "" || body.Password == "" {
		jsonError(w, http.StatusBadRequest, "login and password required")
		return
	}

	user, err := s.st.UserByLogin(strings.TrimSpace(body.Login))
	authenticated := false
	if err == nil && user.PasswordHash != "" {
		authenticated = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(body.Password)) == nil
	}

	// Navidrome fallback: users who exist here without a local password can
	// sign in with their Navidrome credentials (against the auth-source instance).
	if !authenticated && err == nil && user.PasswordHash == "" {
		authenticated = s.tryNavidromeAuth(r.Context(), user.Username, body.Password)
	}

	if !authenticated {
		s.recordLoginFailure(ip)
		// Constant response regardless of whether the user exists.
		jsonError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	s.startSession(w, r, user.ID)
	jsonWrite(w, http.StatusOK, map[string]any{"user": user})
	// Warm this user's recommendations if they are stale.
	if _, computedAt, err := s.st.Recommendations(user.ID, "similar"); err != nil || time.Since(computedAt) > s.cfg.RecsTTL {
		s.eng.RefreshUserAsync(user.ID)
	}
}

func (s *Server) tryNavidromeAuth(ctx context.Context, username, password string) bool {
	inst, err := s.st.FirstActiveInstance("navidrome")
	if err != nil {
		return false
	}
	return s.eng.Sub.Ping(ctx, inst.BaseURL, username, password) == nil
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID int64) {
	token := newToken()
	if err := s.st.CreateSession(token, userID, s.cfg.SessionTTL); err != nil {
		s.log.Error("create session", "err", err)
	}
	s.setSessionCookie(w, r, token, s.cfg.SessionTTL)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request, _ *store.User) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.st.DeleteSession(c.Value)
	}
	s.setSessionCookie(w, r, "", -1)
	jsonWrite(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleMe(w http.ResponseWriter, _ *http.Request, u *store.User) {
	jsonWrite(w, http.StatusOK, map[string]any{"user": u})
}

// Version is stamped at build time via -ldflags.
var Version = "dev"
