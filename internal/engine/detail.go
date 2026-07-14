package engine

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// AlbumDetail is one discography entry on the artist page.
type AlbumDetail struct {
	MBID           string   `json:"mbid"`
	Title          string   `json:"title"`
	Type           string   `json:"type"` // Album | EP | Single
	SecondaryTypes []string `json:"secondaryTypes,omitempty"`
	Year           string   `json:"year,omitempty"`
	CoverURL       string   `json:"coverUrl,omitempty"`
}

// ArtistDetail is the cached portion of an artist page (bio + discography).
// Library/request overlays are joined live by the HTTP handler.
type ArtistDetail struct {
	Name   string        `json:"name"`
	MBID   string        `json:"mbid"`
	Bio    string        `json:"bio,omitempty"`
	Formed string        `json:"formed,omitempty"`
	Albums []AlbumDetail `json:"albums"`
}

// GetArtistDetail returns bio + discography for an artist, cache-first
// (7-day TTL). A cold fetch costs one MusicBrainz call and one TheAudioDB
// call — this is the second interactive endpoint (after search) allowed to
// call out, because a first visit to an artist page is inherently on-demand.
func (e *Engine) GetArtistDetail(ctx context.Context, name, mbid string) (*ArtistDetail, error) {
	if mbid == "" {
		var err error
		mbid, err = e.MB.SearchArtistMBID(ctx, name)
		if err != nil || mbid == "" {
			return nil, ErrNoMBID
		}
	}

	if raw, ok := e.st.ArtistDetailCached(mbid, 7*24*time.Hour); ok {
		var d ArtistDetail
		if json.Unmarshal(raw, &d) == nil {
			return &d, nil
		}
	}

	groups, err := e.MB.ReleaseGroups(ctx, mbid)
	if err != nil {
		// A stale cache beats an error page.
		if raw, ok := e.st.ArtistDetailCached(mbid, 365*24*time.Hour); ok {
			var d ArtistDetail
			if json.Unmarshal(raw, &d) == nil {
				return &d, nil
			}
		}
		return nil, err
	}

	detail := &ArtistDetail{Name: name, MBID: mbid, Albums: make([]AlbumDetail, 0, len(groups))}
	for _, g := range groups {
		year := g.FirstRelease
		if len(year) > 4 {
			year = year[:4]
		}
		detail.Albums = append(detail.Albums, AlbumDetail{
			MBID: g.MBID, Title: g.Title, Type: g.PrimaryType,
			SecondaryTypes: g.SecondaryTypes, Year: year,
			// Cover Art Archive serves release-group front covers directly;
			// the browser fetches these itself (404s are hidden client-side).
			CoverURL: "https://coverartarchive.org/release-group/" + g.MBID + "/front-250",
		})
	}
	// Newest first within the page; the UI groups by type.
	sort.SliceStable(detail.Albums, func(i, j int) bool { return detail.Albums[i].Year > detail.Albums[j].Year })

	if bio, formed, err := e.AudioDB.ArtistBio(ctx, name, mbid); err == nil {
		detail.Bio, detail.Formed = strings.TrimSpace(bio), formed
	}

	e.st.SaveArtistDetail(mbid, name, detail)
	return detail, nil
}

var ErrNoMBID = jsonErr("could not resolve a MusicBrainz ID for this artist")

type jsonErr string

func (e jsonErr) Error() string { return string(e) }
