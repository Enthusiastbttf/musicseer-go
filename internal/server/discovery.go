package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"musicseer/internal/clients"
	"musicseer/internal/engine"
	"musicseer/internal/store"
)

// interactiveSearchDeadline bounds a live search against Last.fm/MusicBrainz so
// a slow upstream fails fast instead of hanging the request.
const interactiveSearchDeadline = 8 * time.Second

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
	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		jsonError(w, http.StatusInternalServerError, "corrupt recommendation payload")
		return
	}
	if len(items) > limit {
		items = items[:limit]
	}

	// The payload snapshots artwork at compute time, but the image worker
	// keeps resolving in the background — join the latest images in live so
	// cards fill in on refresh instead of waiting for the next recompute.
	var missing []string
	for _, it := range items {
		if s, _ := it["imageUrl"].(string); s == "" {
			if name, _ := it["name"].(string); name != "" {
				missing = append(missing, name)
			}
		}
	}
	if len(missing) > 0 {
		if meta, err := s.st.ArtistsByNames(missing); err == nil {
			for _, it := range items {
				if img, _ := it["imageUrl"].(string); img == "" {
					if name, _ := it["name"].(string); name != "" {
						if m := meta[strings.ToLower(name)]; m != nil && m.ImageURL != "" {
							it["imageUrl"] = m.ImageURL
						}
					}
				}
			}
		}
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

// handlePreview returns 30-second sample tracks for an artist (Deezer,
// keyless, cached 12h in memory). Interactive-on-demand like search.
func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request, _ *store.User) {
	artist := strings.TrimSpace(r.URL.Query().Get("artist"))
	if artist == "" {
		jsonError(w, http.StatusBadRequest, "artist required")
		return
	}
	tracks, err := s.eng.Previews(r.Context(), artist, strings.TrimSpace(r.URL.Query().Get("album")))
	if err != nil {
		jsonError(w, http.StatusBadGateway, "preview lookup failed: "+err.Error())
		return
	}
	if tracks == nil {
		tracks = []clients.DeezerTrack{}
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	jsonWrite(w, http.StatusOK, map[string]any{"tracks": tracks})
}

// handleAlbumTracks returns an album's track list (Deezer with samples,
// MusicBrainz fallback without). Interactive-on-demand, cached.
func (s *Server) handleAlbumTracks(w http.ResponseWriter, r *http.Request, _ *store.User) {
	q := r.URL.Query()
	artist, album, mbid := strings.TrimSpace(q.Get("artist")), strings.TrimSpace(q.Get("album")), strings.TrimSpace(q.Get("mbid"))
	if artist == "" || album == "" {
		jsonError(w, http.StatusBadRequest, "artist and album required")
		return
	}
	tracks, source, err := s.eng.AlbumTrackList(r.Context(), artist, album, mbid)
	if err != nil {
		jsonError(w, http.StatusBadGateway, "track list lookup failed: "+err.Error())
		return
	}
	if tracks == nil {
		tracks = []engine.AlbumTrack{}
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	jsonWrite(w, http.StatusOK, map[string]any{"tracks": tracks, "source": source})
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
	ctx, cancel := context.WithTimeout(r.Context(), interactiveSearchDeadline)
	defer cancel()

	type hit struct {
		name, mbid, disamb string
		listeners          int64
	}
	var results []hit
	if s.eng.UsingLastFM() {
		found, err := s.eng.LastFM.SearchArtists(ctx, q, 24)
		if err != nil {
			jsonError(w, http.StatusBadGateway, "search failed: "+err.Error())
			return
		}
		for _, a := range found {
			listeners, _ := strconv.ParseInt(a.Listeners, 10, 64)
			results = append(results, hit{a.Name, a.MBID, "", listeners})
		}
		// Last.fm's artist index is scrobble-derived and full of phantom
		// entries from mistagged files. Real artists have real audiences —
		// rank by listeners so junk sinks, and the counts label the rest.
		sort.SliceStable(results, func(i, j int) bool { return results[i].listeners > results[j].listeners })
	} else {
		found, err := s.eng.MB.SearchArtists(ctx, q, 24)
		if err != nil {
			jsonError(w, http.StatusBadGateway, "search failed: "+err.Error())
			return
		}
		for _, a := range found {
			// "US · Group · Christian rock" — everything MusicBrainz knows
			// that tells identically-named artists apart.
			var parts []string
			if a.Country != "" {
				parts = append(parts, a.Country)
			}
			if a.Type != "" {
				parts = append(parts, a.Type)
			}
			if a.Disambiguation != "" {
				parts = append(parts, a.Disambiguation)
			}
			results = append(results, hit{a.Name, a.MBID, strings.Join(parts, " · "), 0})
		}
	}

	libM, libN, _ := s.st.LibraryIndex()
	reqM, reqN, _ := s.st.RequestedIndex()
	lib, req := membership{libM, libN}, membership{reqM, reqN}
	names := make([]string, len(results))
	nameCount := map[string]int{}
	for i, a := range results {
		names[i] = a.name
		nameCount[strings.ToLower(a.name)]++
	}
	meta, _ := s.st.ArtistsByNames(names)

	type entry struct {
		Name           string `json:"name"`
		MBID           string `json:"mbid,omitempty"`
		Disambiguation string `json:"disambiguation,omitempty"`
		Listeners      int64  `json:"listeners"`
		ImageURL       string `json:"imageUrl,omitempty"`
		InLibrary      bool   `json:"inLibrary"`
		Requested      bool   `json:"requested"`
	}
	out := make([]entry, 0, len(results))
	for _, a := range results {
		key := strings.ToLower(a.name)
		ambiguous := nameCount[key] > 1
		e := entry{Name: a.name, MBID: a.mbid, Disambiguation: a.disamb,
			InLibrary: lib.has(a.name, a.mbid), Requested: req.has(a.name, a.mbid)}
		if m := meta[key]; m != nil {
			if e.Listeners == 0 {
				e.Listeners = m.Listeners
			}
			// The image cache is keyed by NAME. Among identically-named
			// artists, only show the cached image when its stored MBID
			// proves it belongs to THIS one.
			if !ambiguous || (m.MBID != "" && strings.EqualFold(m.MBID, a.mbid)) {
				e.ImageURL = m.ImageURL
			}
			if e.MBID == "" {
				e.MBID = m.MBID
			}
		}
		if e.ImageURL == "" && !ambiguous {
			// Backfill artwork in the background so results have images on
			// the next visit (and often within seconds on this one).
			s.eng.EnqueueImage(a.name, a.mbid)
		}
		out = append(out, e)
	}
	jsonWrite(w, http.StatusOK, out)
}
