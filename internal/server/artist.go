package server

import (
	"errors"
	"net/http"
	"strings"

	"musicseer/internal/engine"
	"musicseer/internal/store"
)

// handleArtistDetail powers the artist page: cached bio + discography from
// the engine, overlaid live with library ownership (from Lidarr) and open
// request state (from SQLite).
func (s *Server) handleArtistDetail(w http.ResponseWriter, r *http.Request, _ *store.User) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	mbid := strings.TrimSpace(r.URL.Query().Get("mbid"))
	if name == "" && mbid == "" {
		jsonError(w, http.StatusBadRequest, "name or mbid required")
		return
	}

	detail, err := s.eng.GetArtistDetail(r.Context(), name, mbid)
	if err != nil {
		if errors.Is(err, engine.ErrNoMBID) {
			jsonError(w, http.StatusNotFound, err.Error())
			return
		}
		jsonError(w, http.StatusBadGateway, "could not load artist details: "+err.Error())
		return
	}

	type albumOut struct {
		engine.AlbumDetail
		Owned     bool    `json:"owned"`
		Percent   float64 `json:"percent,omitempty"`
		Requested bool    `json:"requested"`
	}
	type artistOut struct {
		*engine.ArtistDetail
		ImageURL  string     `json:"imageUrl,omitempty"`
		Genres    []string   `json:"genres,omitempty"`
		Listeners int64      `json:"listeners"`
		InLibrary bool       `json:"inLibrary"`
		Requested bool       `json:"requested"`
		Albums    []albumOut `json:"albums"`
	}

	out := artistOut{ArtistDetail: detail}

	// Metadata overlay from the artists cache (image, genres, listeners).
	if meta, err := s.st.ArtistsByNames([]string{detail.Name}); err == nil {
		if m := meta[strings.ToLower(detail.Name)]; m != nil {
			out.ImageURL, out.Genres, out.Listeners = m.ImageURL, m.Genres, m.Listeners
		}
	}
	libNames, _ := s.st.LibraryNames()
	out.InLibrary = libNames[strings.ToLower(detail.Name)]
	reqNames, _ := s.st.RequestedNames()
	out.Requested = reqNames[strings.ToLower(detail.Name)]
	albumReqs, _ := s.st.OpenAlbumRequests(detail.MBID)

	// Live ownership overlay from Lidarr (LAN call, fast; skipped gracefully
	// if the artist isn't in Lidarr or no instance is configured).
	owned := map[string]struct {
		percent float64
		files   int
	}{}
	if out.InLibrary {
		if inst, err := s.st.FirstActiveInstance("lidarr"); err == nil {
			if apiKey, err := s.box.Decrypt(inst.APIKeyEnc); err == nil {
				if artistID, err := s.eng.Lidarr.FindArtistID(r.Context(), inst.BaseURL, apiKey, detail.MBID); err == nil && artistID != 0 {
					if albums, err := s.eng.Lidarr.Albums(r.Context(), inst.BaseURL, apiKey, artistID); err == nil {
						for _, a := range albums {
							if a.Statistics.TrackFileCount > 0 {
								owned[strings.ToLower(a.ForeignAlbumID)] = struct {
									percent float64
									files   int
								}{a.Statistics.PercentOfTracks, a.Statistics.TrackFileCount}
							}
						}
					}
				}
			}
		}
	}

	out.Albums = make([]albumOut, 0, len(detail.Albums))
	for _, a := range detail.Albums {
		ao := albumOut{AlbumDetail: a, Requested: albumReqs[a.MBID]}
		if o, ok := owned[strings.ToLower(a.MBID)]; ok {
			ao.Owned, ao.Percent = true, o.percent
		}
		out.Albums = append(out.Albums, ao)
	}

	w.Header().Set("Cache-Control", "private, max-age=60")
	jsonWrite(w, http.StatusOK, out)
}
