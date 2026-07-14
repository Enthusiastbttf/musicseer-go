package server

import (
	"math/rand"
	"net/http"
	"strings"

	"musicseer/internal/store"
)

// curatedGenres are the always-available browse tiles (the "explore" pills on
// top of them are personal, derived from the user's library tags).
var curatedGenres = []string{
	"rock", "pop", "hip hop", "electronic", "jazz", "classical",
	"r&b", "country", "metal", "blues", "reggae", "soul",
	"punk", "indie rock", "folk", "gospel", "christian rock", "ambient",
	"dance", "alternative rock",
}

// handleGenres returns the personal "genres to explore" pills (from the
// library's MusicBrainz tags, shuffled for variety) plus the curated browse
// list. Pure SQLite — no external calls.
func (s *Server) handleGenres(w http.ResponseWriter, _ *http.Request, _ *store.User) {
	libGenres, err := s.st.LibraryGenres(24)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Shuffle so the pills rotate between visits, like the old app.
	rand.Shuffle(len(libGenres), func(i, j int) { libGenres[i], libGenres[j] = libGenres[j], libGenres[i] })
	if len(libGenres) > 8 {
		libGenres = libGenres[:8]
	}
	if libGenres == nil {
		libGenres = []string{}
	}
	jsonWrite(w, http.StatusOK, map[string]any{
		"explore": libGenres,
		"browse":  curatedGenres,
	})
}

// handleGenreArtists powers a genre page: artists tagged with the genre on
// MusicBrainz (cache-first), annotated with library/request state.
func (s *Server) handleGenreArtists(w http.ResponseWriter, r *http.Request, _ *store.User) {
	genre := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("name")))
	if genre == "" || len(genre) > 60 {
		jsonError(w, http.StatusBadRequest, "genre name required")
		return
	}
	artists, err := s.eng.GetGenreArtists(r.Context(), genre)
	if err != nil {
		jsonError(w, http.StatusBadGateway, "could not load genre: "+err.Error())
		return
	}

	libNames, _ := s.st.LibraryNames()
	reqNames, _ := s.st.RequestedNames()
	names := make([]string, len(artists))
	for i, a := range artists {
		names[i] = a.Name
	}
	meta, _ := s.st.ArtistsByNames(names)

	type entry struct {
		Name           string   `json:"name"`
		MBID           string   `json:"mbid,omitempty"`
		Disambiguation string   `json:"disambiguation,omitempty"`
		Genres         []string `json:"genres,omitempty"`
		ImageURL       string   `json:"imageUrl,omitempty"`
		Listeners      int64    `json:"listeners"`
		InLibrary      bool     `json:"inLibrary"`
		Requested      bool     `json:"requested"`
	}
	out := make([]entry, 0, len(artists))
	for _, a := range artists {
		key := strings.ToLower(a.Name)
		e := entry{Name: a.Name, MBID: a.MBID, Disambiguation: a.Disambiguation,
			InLibrary: libNames[key], Requested: reqNames[key]}
		if m := meta[key]; m != nil {
			e.ImageURL, e.Listeners, e.Genres = m.ImageURL, m.Listeners, m.Genres
		}
		out = append(out, e)
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	jsonWrite(w, http.StatusOK, out)
}
