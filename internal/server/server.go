// Package server is the HTTP layer: session auth, JSON API and the embedded
// single-page frontend. Interactive handlers only ever read SQLite — every
// external call lives in the engine package.
package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"musicseer/internal/config"
	"musicseer/internal/engine"
	"musicseer/internal/secrets"
	"musicseer/internal/store"
	"musicseer/web"
)

const sessionCookie = "musicseer_session"

type Server struct {
	cfg config.Config
	st  *store.Store
	box *secrets.Box
	eng *engine.Engine
	log *slog.Logger

	loginMu    sync.Mutex
	loginFails map[string][]time.Time // simple per-IP login throttle

	trustedProxies []*net.IPNet // parsed cfg.TrustedProxies; XFF trusted only from these
}

// maxLoginBuckets caps the login-throttle map so forged, per-request client IPs
// can't grow it without bound (memory-exhaustion guard).
const maxLoginBuckets = 4096

func New(cfg config.Config, st *store.Store, box *secrets.Box, eng *engine.Engine, log *slog.Logger) *Server {
	s := &Server{cfg: cfg, st: st, box: box, eng: eng, log: log, loginFails: map[string][]time.Time{}}
	for _, p := range cfg.TrustedProxies {
		if !strings.Contains(p, "/") {
			if strings.Contains(p, ":") {
				p += "/128" // IPv6 host
			} else {
				p += "/32" // IPv4 host
			}
		}
		if _, ipnet, err := net.ParseCIDR(p); err == nil {
			s.trustedProxies = append(s.trustedProxies, ipnet)
		} else {
			log.Warn("ignoring invalid MUSICSEER_TRUSTED_PROXIES entry", "value", p, "err", err)
		}
	}
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Auth
	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("POST /api/auth/setup", s.handleSetup)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.requireUser(s.handleLogout))
	mux.HandleFunc("GET /api/auth/me", s.requireUser(s.handleMe))

	// Discovery (DB reads only)
	mux.HandleFunc("GET /api/discovery/trending", s.requireUser(s.handleTrending))
	mux.HandleFunc("GET /api/discovery/recommendations", s.requireUser(s.handleRecommendations))
	mux.HandleFunc("GET /api/search", s.requireUser(s.handleSearch))
	mux.HandleFunc("GET /api/artist", s.requireUser(s.handleArtistDetail))
	mux.HandleFunc("GET /api/discovery/genres", s.requireUser(s.handleGenres))
	mux.HandleFunc("GET /api/discovery/genre", s.requireUser(s.handleGenreArtists))
	mux.HandleFunc("GET /api/preview", s.requireUser(s.handlePreview))
	mux.HandleFunc("GET /api/album-tracks", s.requireUser(s.handleAlbumTracks))

	// Plex sign-in
	mux.HandleFunc("POST /api/auth/plex/start", s.handlePlexStart)
	mux.HandleFunc("POST /api/auth/plex/poll", s.handlePlexPoll)
	mux.HandleFunc("GET /api/admin/plex", s.requireAdmin(s.handlePlexConfigGet))
	mux.HandleFunc("POST /api/admin/plex", s.requireAdmin(s.handlePlexConfigSet))
	mux.HandleFunc("GET /api/admin/lastfm", s.requireAdmin(s.handleLastfmGet))
	mux.HandleFunc("POST /api/admin/lastfm", s.requireAdmin(s.handleLastfmSet))
	mux.HandleFunc("GET /api/admin/update", s.requireAdmin(s.handleUpdateCheck))
	mux.HandleFunc("POST /api/admin/update", s.requireAdmin(s.handleUpdateApply))

	// Requests
	mux.HandleFunc("GET /api/requests", s.requireUser(s.handleRequestsList))
	mux.HandleFunc("POST /api/requests", s.requireUser(s.handleRequestCreate))
	mux.HandleFunc("POST /api/requests/batch", s.requireUser(s.handleRequestBatch))
	mux.HandleFunc("POST /api/requests/{id}/approve", s.requireAdmin(s.handleRequestApprove))
	mux.HandleFunc("POST /api/requests/{id}/reject", s.requireAdmin(s.handleRequestReject))
	mux.HandleFunc("POST /api/requests/{id}/retry", s.requireAdmin(s.handleRequestRetry))
	mux.HandleFunc("DELETE /api/requests/{id}", s.requireUser(s.handleRequestDelete))

	// Admin
	mux.HandleFunc("GET /api/users", s.requireAdmin(s.handleUsersList))
	mux.HandleFunc("POST /api/users", s.requireAdmin(s.handleUserCreate))
	mux.HandleFunc("PUT /api/users/{id}", s.requireAdmin(s.handleUserUpdate))
	mux.HandleFunc("DELETE /api/users/{id}", s.requireAdmin(s.handleUserDelete))
	mux.HandleFunc("GET /api/instances", s.requireAdmin(s.handleInstancesList))
	mux.HandleFunc("POST /api/instances", s.requireAdmin(s.handleInstanceCreate))
	mux.HandleFunc("PUT /api/instances/{id}", s.requireAdmin(s.handleInstanceUpdate))
	mux.HandleFunc("DELETE /api/instances/{id}", s.requireAdmin(s.handleInstanceDelete))
	mux.HandleFunc("POST /api/instances/test", s.requireAdmin(s.handleInstanceTest))
	mux.HandleFunc("GET /api/instances/{id}/lidarr-options", s.requireAdmin(s.handleLidarrOptions))
	mux.HandleFunc("GET /api/admin/stats", s.requireAdmin(s.handleStats))
	mux.HandleFunc("POST /api/admin/sync/{job}", s.requireAdmin(s.handleSyncNow))

	// Embedded SPA
	mux.Handle("/", spaHandler())

	return securityHeaders(mux)
}

// ---------- middleware ----------

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) currentUser(r *http.Request) *store.User {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	u, err := s.st.SessionUser(c.Value)
	if err != nil {
		return nil
	}
	return u
}

type userHandler func(w http.ResponseWriter, r *http.Request, u *store.User)

func (s *Server) requireUser(h userHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := s.currentUser(r)
		if u == nil {
			jsonError(w, http.StatusUnauthorized, "not authenticated")
			return
		}
		h(w, r, u)
	}
}

func (s *Server) requireAdmin(h userHandler) http.HandlerFunc {
	return s.requireUser(func(w http.ResponseWriter, r *http.Request, u *store.User) {
		if u.Role != "admin" {
			jsonError(w, http.StatusForbidden, "admin only")
			return
		}
		h(w, r, u)
	})
}

// ---------- helpers ----------

func jsonWrite(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, status int, msg string) {
	jsonWrite(w, status, map[string]string{"error": msg})
}

func decodeBody(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

func queryLimit(r *http.Request, def, max int) int {
	n, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

func newToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is catastrophic — never emit a weak/predictable
		// token (session or Plex client id). Fail loudly instead.
		panic("crypto/rand unavailable: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https"),
	})
}

// clientIP resolves the real client address for throttling. X-Forwarded-For is
// honored ONLY when the immediate peer (r.RemoteAddr) is a configured trusted
// proxy, and then the rightmost non-proxy hop is used — a client cannot forge
// the header to escape the login throttle. With no trusted proxies configured
// (the default), the direct peer address is always used.
func (s *Server) clientIP(r *http.Request) string {
	host := hostOnly(r.RemoteAddr)
	if !s.isTrustedProxy(host) {
		return host
	}
	if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
		parts := strings.Split(xf, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			ip := strings.TrimSpace(parts[i])
			if ip != "" && !s.isTrustedProxy(ip) {
				return ip
			}
		}
	}
	return host
}

func (s *Server) isTrustedProxy(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, n := range s.trustedProxies {
		if n.Contains(parsed) {
			return true
		}
	}
	return false
}

func hostOnly(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}

// loginAllowed implements a small sliding-window throttle: 10 failures / 15 min.
func (s *Server) loginAllowed(ip string) bool {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	cutoff := time.Now().Add(-15 * time.Minute)
	var kept []time.Time
	for _, t := range s.loginFails[ip] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(s.loginFails, ip) // don't retain empty buckets
	} else {
		s.loginFails[ip] = kept
	}
	return len(kept) < 10
}

func (s *Server) recordLoginFailure(ip string) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	// Guard against unbounded growth from many distinct source IPs: once the map
	// is saturated, sweep buckets that have aged out of the window.
	if len(s.loginFails) >= maxLoginBuckets {
		cutoff := time.Now().Add(-15 * time.Minute)
		for k, times := range s.loginFails {
			if len(times) == 0 || times[len(times)-1].Before(cutoff) {
				delete(s.loginFails, k)
			}
		}
	}
	s.loginFails[ip] = append(s.loginFails[ip], time.Now())
}

// ---------- SPA ----------

func spaHandler() http.Handler {
	dist, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if f, err := dist.Open(path); err == nil {
				f.Close()
				if strings.HasPrefix(path, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		// SPA fallback: serve index.html for client-side routes.
		r.URL.Path = "/"
		w.Header().Set("Cache-Control", "no-cache")
		fileServer.ServeHTTP(w, r)
	})
}

var errNotFound = errors.New("not found")
