package clients

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strconv"
)

// ListenBrainz — keyless, open-data discovery backend (MusicBrainz's sister
// project). Used automatically when no Last.fm API key is configured.
//
//   - Trending:  sitewide artist stats  (api.listenbrainz.org)
//   - Similar:   Labs similar-artists   (labs.api.listenbrainz.org), keyed by
//     MusicBrainz IDs — which our library sync already has, since Lidarr's
//     foreignArtistId IS an MBID.
type ListenBrainz struct {
	apiBase  string
	labsBase string
	lim      *limiter
	labsLim  *limiter
}

// The Labs API requires an explicit algorithm parameter; this is the
// production algorithm the ListenBrainz site itself uses.
const lbSimilarAlgorithm = "session_based_days_7500_session_300_contribution_5_threshold_10_limit_100_filter_True_skip_30"

func NewListenBrainz() *ListenBrainz {
	api := os.Getenv("MUSICSEER_LB_API_BASE") // test hook; defaults to production
	if api == "" {
		api = "https://api.listenbrainz.org"
	}
	labs := os.Getenv("MUSICSEER_LB_LABS_BASE")
	if labs == "" {
		labs = "https://labs.api.listenbrainz.org"
	}
	return &ListenBrainz{apiBase: api, labsBase: labs, lim: newLimiter(3), labsLim: newLimiter(2)}
}

type LBArtist struct {
	Name        string
	MBID        string
	ListenCount int64
	Score       float64 // similarity only; normalized 0..1 by the caller
}

// TopArtists returns the sitewide chart for the given range ("week", "month").
func (l *ListenBrainz) TopArtists(ctx context.Context, rng string, count int) ([]LBArtist, error) {
	var resp struct {
		Payload struct {
			Artists []struct {
				ArtistName  string          `json:"artist_name"`
				ArtistMBID  json.RawMessage `json:"artist_mbid"` // string, array, or null across API versions
				ListenCount int64           `json:"listen_count"`
			} `json:"artists"`
		} `json:"payload"`
	}
	u := l.apiBase + "/1/stats/sitewide/artists?range=" + url.QueryEscape(rng) + "&count=" + strconv.Itoa(count)
	if err := getJSON(ctx, l.lim, u, nil, &resp); err != nil {
		return nil, err
	}
	out := make([]LBArtist, 0, len(resp.Payload.Artists))
	for _, a := range resp.Payload.Artists {
		if a.ArtistName == "" {
			continue
		}
		out = append(out, LBArtist{Name: a.ArtistName, MBID: firstMBID(a.ArtistMBID), ListenCount: a.ListenCount})
	}
	return out, nil
}

// SimilarArtists returns artists similar to the given MBID with raw scores
// normalized to 0..1 against the strongest match.
func (l *ListenBrainz) SimilarArtists(ctx context.Context, mbid string, limit int) ([]LBArtist, error) {
	var raw []map[string]any
	u := l.labsBase + "/similar-artists/json?artist_mbids=" + url.QueryEscape(mbid) +
		"&algorithm=" + url.QueryEscape(lbSimilarAlgorithm)
	if err := getJSON(ctx, l.labsLim, u, nil, &raw); err != nil {
		return nil, err
	}
	var out []LBArtist
	var maxScore float64
	for _, m := range raw {
		a := LBArtist{
			Name:  str(m["name"], str(m["artist_name"], "")),
			MBID:  str(m["artist_mbid"], ""),
			Score: num(m["score"]),
		}
		if a.Name == "" || a.MBID == mbid {
			continue
		}
		if a.Score > maxScore {
			maxScore = a.Score
		}
		out = append(out, a)
	}
	if maxScore > 0 {
		for i := range out {
			out[i].Score = out[i].Score / maxScore
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ---------- tolerant JSON helpers ----------

func firstMBID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var arr []string
	if json.Unmarshal(raw, &arr) == nil && len(arr) > 0 {
		return arr[0]
	}
	return ""
}

func str(v any, def string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}

func num(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return 0
}
