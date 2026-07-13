package clients

import (
	"context"
	"net/url"
	"strconv"
)

// LastFM client. Last.fm asks for no more than 5 req/s averaged.
type LastFM struct {
	APIKey string
	lim    *limiter
}

func NewLastFM(apiKey string) *LastFM {
	return &LastFM{APIKey: apiKey, lim: newLimiter(4)}
}

const lastfmBase = "https://ws.audioscrobbler.com/2.0/"

type LFArtist struct {
	Name      string `json:"name"`
	MBID      string `json:"mbid"`
	Match     string `json:"match"`
	Listeners string `json:"listeners"`
	Playcount string `json:"playcount"`
}

func (l *LastFM) call(ctx context.Context, params url.Values, out any) error {
	params.Set("api_key", l.APIKey)
	params.Set("format", "json")
	return getJSON(ctx, l.lim, lastfmBase+"?"+params.Encode(), nil, out)
}

func (l *LastFM) TopArtists(ctx context.Context, limit int) ([]LFArtist, error) {
	var resp struct {
		Artists struct {
			Artist []LFArtist `json:"artist"`
		} `json:"artists"`
	}
	err := l.call(ctx, url.Values{
		"method": {"chart.gettopartists"},
		"limit":  {strconv.Itoa(limit)},
	}, &resp)
	return resp.Artists.Artist, err
}

func (l *LastFM) SimilarArtists(ctx context.Context, artist, mbid string, limit int) ([]LFArtist, error) {
	params := url.Values{
		"method":      {"artist.getsimilar"},
		"limit":       {strconv.Itoa(limit)},
		"autocorrect": {"1"},
	}
	if mbid != "" {
		params.Set("mbid", mbid)
	} else {
		params.Set("artist", artist)
	}
	var resp struct {
		SimilarArtists struct {
			Artist []LFArtist `json:"artist"`
		} `json:"similarartists"`
	}
	err := l.call(ctx, params, &resp)
	return resp.SimilarArtists.Artist, err
}

type LFArtistInfo struct {
	Name  string `json:"name"`
	MBID  string `json:"mbid"`
	Stats struct {
		Listeners string `json:"listeners"`
		Playcount string `json:"playcount"`
	} `json:"stats"`
	Tags struct {
		Tag []struct {
			Name string `json:"name"`
		} `json:"tag"`
	} `json:"tags"`
}

func (l *LastFM) ArtistInfo(ctx context.Context, artist, mbid string) (*LFArtistInfo, error) {
	params := url.Values{"method": {"artist.getinfo"}, "autocorrect": {"1"}}
	if mbid != "" {
		params.Set("mbid", mbid)
	} else {
		params.Set("artist", artist)
	}
	var resp struct {
		Artist *LFArtistInfo `json:"artist"`
	}
	err := l.call(ctx, params, &resp)
	return resp.Artist, err
}

func (l *LastFM) SearchArtists(ctx context.Context, query string, limit int) ([]LFArtist, error) {
	var resp struct {
		Results struct {
			ArtistMatches struct {
				Artist []LFArtist `json:"artist"`
			} `json:"artistmatches"`
		} `json:"results"`
	}
	err := l.call(ctx, url.Values{
		"method": {"artist.search"},
		"artist": {query},
		"limit":  {strconv.Itoa(limit)},
	}, &resp)
	return resp.Results.ArtistMatches.Artist, err
}
