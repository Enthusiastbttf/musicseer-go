package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"musicseer/internal/store"
)

// handleTrending serves the trending chart straight from SQLite — a handful
// of rows plus one batched metadata lookup. No external calls, ever.
func (s *Server) handleTrending(w http.ResponseWriter, r *http.Request, _ *store.User) {
	limit := queryLimit(r, 50, 100)
	trending, _, err := s.st.Trending("global", limit)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	names := make([]string, len(trending))
	for i, t := range trending {
		names[i] = t.Name
	}
	meta, err := s.st.ArtistsByNames(names)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	type entry struct {
		Rank      int      `json:"rank"`
		Name      string   `json:"name"`
		MBID      string   `json:"mbid,omitempty"`
		Listeners int64    `json:"listeners"`
		ImageURL  string   `json:"imageUrl,omitempty"`
		Genres    []string `json:"genres,omitempty"`
	}
	out := make([]entry, 0, len(trending))
	for _, t := range trending {
		e := entry{Rank: t.Rank, Name: t.Name, MBID: t.MBID}
		if m := meta[strings.ToLower(t.Name)]; m != nil {
			e.Listeners, e.ImageURL, e.Genres = m.Listeners, m.ImageURL, m.Genres
			if e.MBID == "" {
				e.MBID = m.MBID
			}
		}
		out = append(out, e)
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	jsonWrite(w, http.StatusOK, out)
}

// serveRecs returns precomputed recommendations (stale-while-revalidate).
func (s *Server) serveRecs(w http.ResponseWriter, r *http.Request, u *store.User, kind string) {
	limit := queryLimit(r, 20, 60)
	data, computedAt, err := s.st.Recommendations(u.ID, kind)
	if err != nil {
		// Nothing computed yet: kick off a build and tell the client to retry.
		s.eng.RefreshUserAsync(u.ID)
		jsonWrite(w, http.StatusOK, map[string]any{"items": []any{}, "computing": true})
		return
	}
	if time.Since(computedAt) > s.cfg.RecsTTL {
		s.eng.RefreshUserAsync(u.ID) // serve stale now, refresh in background
	}
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil {
		jsonError(w, http.StatusInternalServerError, "corrupt recommendation payload")
		return
	}
	if len(items) > limit {
		items = items[:limit]
	}
	jsonWrite(w, http.StatusOK, map[string]any{
		"items":      items,
		"computedAt": computedAt.UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleRecommendations(w http.ResponseWriter, r *http.Request, u *store.User) {
	s.serveRecs(w, r, u, "similar")
}

func (s *Server) handleHiddenGems(w http.ResponseWriter, r *http.Request, u *store.User) {
	s.serveRecs(w, r, u, "gems")
}

// handleSearch is the one endpoint allowed to call out (a single Last.fm or
// MusicBrainz search) because it is inherently interactive. Results are
// annotated with library/request state from SQLite.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request, _ *store.User) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		jsonWrite(w, http.StatusOK, []any{})
		return
	}

	type hit struct{ name, mbid string }
	var results []hit
	if s.eng.UsingLastFM() {
		found, err := s.eng.LastFM.SearchArtists(r.Context(), q, 24)
		if err != nil {
			jsonError(w, http.StatusBadGateway, "search failed: "+err.Error())
			return
		}
		for _, a := range found {
			results = append(results, hit{a.Name, a.MBID})
		}
	} else {
		found, err := s.eng.MB.SearchArtists(r.Context(), q, 24)
		if err != nil {
			jsonError(w, http.StatusBadGateway, "search failed: "+err.Error())
			return
		}
		for _, a := range found {
			results = append(results, hit{a.Name, a.MBID})
		}
	}

	libNames, _ := s.st.LibraryNames()
	reqNames, _ := s.st.RequestedNames()
	names := make([]string, len(results))
	for i, a := range results {
		names[i] = a.name
	}
	meta, _ := s.st.ArtistsByNames(names)

	type entry struct {
		Name      string `json:"name"`
		MBID      string `json:"mbid,omitempty"`
		Listeners int64  `json:"listeners"`
		ImageURL  string `json:"imageUrl,omitempty"`
		InLibrary bool   `json:"inLibrary"`
		Requested bool   `json:"requested"`
	}
	out := make([]entry, 0, len(results))
	for _, a := range results {
		key := strings.ToLower(a.name)
		e := entry{Name: a.name, MBID: a.mbid, InLibrary: libNames[key], Requested: reqNames[key]}
		if m := meta[key]; m != nil {
			e.ImageURL, e.Listeners = m.ImageURL, m.Listeners
			if e.MBID == "" {
				e.MBID = m.MBID
			}
		}
		out = append(out, e)
	}
	jsonWrite(w, http.StatusOK, out)
}
