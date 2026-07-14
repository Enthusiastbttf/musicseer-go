package clients

import (
	"context"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Deezer: free, no key, best artist images. Public limit is 50 req / 5 s;
// we stay well under it.
type Deezer struct{ lim *limiter }

func NewDeezer() *Deezer { return &Deezer{lim: newLimiter(5)} }

func (d *Deezer) ArtistImage(ctx context.Context, name string) (string, error) {
	var resp struct {
		Data []struct {
			Name      string `json:"name"`
			PictureXL string `json:"picture_xl"`
			PictureBg string `json:"picture_big"`
			PictureMd string `json:"picture_medium"`
		} `json:"data"`
	}
	err := getJSON(ctx, d.lim, "https://api.deezer.com/search/artist?limit=1&q="+url.QueryEscape(name), nil, &resp)
	if err != nil || len(resp.Data) == 0 {
		return "", err
	}
	a := resp.Data[0]
	for _, u := range []string{a.PictureXL, a.PictureBg, a.PictureMd} {
		if u != "" {
			return u, nil
		}
	}
	return "", nil
}

// TheAudioDB: fallback image source (free tier key "2").
type AudioDB struct{ lim *limiter }

func NewAudioDB() *AudioDB { return &AudioDB{lim: newLimiter(1)} }

// ArtistBio returns a biography and formation year from TheAudioDB (keyless).
func (a *AudioDB) ArtistBio(ctx context.Context, name, mbid string) (bio, formed string, err error) {
	u := "https://www.theaudiodb.com/api/v1/json/2/search.php?s=" + url.QueryEscape(name)
	if mbid != "" {
		u = "https://www.theaudiodb.com/api/v1/json/2/artist-mb.php?i=" + url.QueryEscape(mbid)
	}
	if b := os.Getenv("MUSICSEER_ADB_BASE"); b != "" { // test hook
		u = b + u[strings.Index(u, "/api/"):]
	}
	var resp struct {
		Artists []struct {
			Bio    string `json:"strBiographyEN"`
			Formed string `json:"intFormedYear"`
		} `json:"artists"`
	}
	if err := getJSON(ctx, a.lim, u, nil, &resp); err != nil || len(resp.Artists) == 0 {
		return "", "", err
	}
	return resp.Artists[0].Bio, resp.Artists[0].Formed, nil
}

func (a *AudioDB) ArtistImage(ctx context.Context, name, mbid string) (string, error) {
	u := "https://www.theaudiodb.com/api/v1/json/2/search.php?s=" + url.QueryEscape(name)
	if mbid != "" {
		u = "https://www.theaudiodb.com/api/v1/json/2/artist-mb.php?i=" + url.QueryEscape(mbid)
	}
	var resp struct {
		Artists []struct {
			Thumb  string `json:"strArtistThumb"`
			Fanart string `json:"strArtistFanart"`
		} `json:"artists"`
	}
	if err := getJSON(ctx, a.lim, u, nil, &resp); err != nil || len(resp.Artists) == 0 {
		return "", err
	}
	if t := resp.Artists[0].Thumb; t != "" {
		return t, nil
	}
	return resp.Artists[0].Fanart, nil
}

// MusicBrainz: genre tags + MBID search. Hard limit 1 req/s — enforced here.
type MusicBrainz struct {
	lim       *limiter
	userAgent string
}

func NewMusicBrainz(contact string) *MusicBrainz {
	return &MusicBrainz{lim: newLimiter(0.9), userAgent: "MusicSeer/2.0 (" + contact + ")"}
}

// mbBase allows tests to point at a mock server; defaults to production.
func mbBase() string {
	if b := os.Getenv("MUSICSEER_MB_BASE"); b != "" {
		return b
	}
	return "https://musicbrainz.org"
}

func fmtInt(n int) string { return strconv.Itoa(n) }

func (m *MusicBrainz) ArtistTags(ctx context.Context, mbid string) ([]string, error) {
	var resp struct {
		Tags []struct {
			Name  string `json:"name"`
			Count int    `json:"count"`
		} `json:"tags"`
	}
	err := getJSON(ctx, m.lim, mbBase()+"/ws/2/artist/"+url.PathEscape(mbid)+"?inc=tags&fmt=json",
		map[string]string{"User-Agent": m.userAgent}, &resp)
	if err != nil {
		return nil, err
	}
	var tags []string
	for _, t := range resp.Tags {
		if t.Count > 0 {
			tags = append(tags, t.Name)
		}
	}
	if len(tags) > 6 {
		tags = tags[:6]
	}
	return tags, nil
}

// MBSearchResult is one hit from MusicBrainz artist search.
type MBSearchResult struct {
	Name           string
	MBID           string
	Disambiguation string
	Country        string
	Type           string // Person | Group | ...
	Score          int
}

// SearchArtists is the keyless search backend (used when no Last.fm key is
// configured). One rate-limited call per user search.
func (m *MusicBrainz) SearchArtists(ctx context.Context, query string, limit int) ([]MBSearchResult, error) {
	var resp struct {
		Artists []struct {
			ID             string `json:"id"`
			Name           string `json:"name"`
			Score          int    `json:"score"`
			Disambiguation string `json:"disambiguation"`
			Country        string `json:"country"`
			Type           string `json:"type"`
		} `json:"artists"`
	}
	base := mbBase()
	err := getJSON(ctx, m.lim,
		base+"/ws/2/artist?limit="+url.QueryEscape(fmtInt(limit))+"&fmt=json&query="+url.QueryEscape(query),
		map[string]string{"User-Agent": m.userAgent}, &resp)
	if err != nil {
		return nil, err
	}
	out := make([]MBSearchResult, 0, len(resp.Artists))
	for _, a := range resp.Artists {
		out = append(out, MBSearchResult{Name: a.Name, MBID: a.ID, Score: a.Score,
			Disambiguation: a.Disambiguation, Country: a.Country, Type: a.Type})
	}
	return out, nil
}

// MBReleaseGroup is one release group (album/EP/single) from MusicBrainz.
type MBReleaseGroup struct {
	MBID           string   `json:"mbid"`
	Title          string   `json:"title"`
	PrimaryType    string   `json:"type"` // Album | EP | Single
	SecondaryTypes []string `json:"secondaryTypes,omitempty"`
	FirstRelease   string   `json:"firstRelease,omitempty"` // YYYY or YYYY-MM-DD
}

// ReleaseGroups returns an artist's discography (albums, EPs, singles).
// One rate-limited call; cached by the caller for a week.
func (m *MusicBrainz) ReleaseGroups(ctx context.Context, artistMBID string) ([]MBReleaseGroup, error) {
	var resp struct {
		ReleaseGroups []struct {
			ID             string   `json:"id"`
			Title          string   `json:"title"`
			PrimaryType    string   `json:"primary-type"`
			SecondaryTypes []string `json:"secondary-types"`
			FirstRelease   string   `json:"first-release-date"`
		} `json:"release-groups"`
	}
	u := mbBase() + "/ws/2/artist/" + url.PathEscape(artistMBID) +
		"?inc=release-groups&type=album%7Cep%7Csingle&limit=100&fmt=json"
	if err := getJSON(ctx, m.lim, u, map[string]string{"User-Agent": m.userAgent}, &resp); err != nil {
		return nil, err
	}
	out := make([]MBReleaseGroup, 0, len(resp.ReleaseGroups))
	for _, rg := range resp.ReleaseGroups {
		if rg.PrimaryType == "" {
			continue
		}
		out = append(out, MBReleaseGroup{
			MBID: rg.ID, Title: rg.Title, PrimaryType: rg.PrimaryType,
			SecondaryTypes: rg.SecondaryTypes, FirstRelease: rg.FirstRelease,
		})
	}
	return out, nil
}

// ArtistsByTag returns artists carrying a MusicBrainz tag/genre, by relevance.
func (m *MusicBrainz) ArtistsByTag(ctx context.Context, tag string, limit int) ([]MBSearchResult, error) {
	var resp struct {
		Artists []struct {
			ID             string `json:"id"`
			Name           string `json:"name"`
			Score          int    `json:"score"`
			Disambiguation string `json:"disambiguation"`
			Country        string `json:"country"`
			Type           string `json:"type"`
		} `json:"artists"`
	}
	err := getJSON(ctx, m.lim,
		mbBase()+"/ws/2/artist?limit="+fmtInt(limit)+`&fmt=json&query=tag:`+url.QueryEscape(`"`+tag+`"`),
		map[string]string{"User-Agent": m.userAgent}, &resp)
	if err != nil {
		return nil, err
	}
	out := make([]MBSearchResult, 0, len(resp.Artists))
	for _, a := range resp.Artists {
		out = append(out, MBSearchResult{Name: a.Name, MBID: a.ID, Score: a.Score,
			Disambiguation: a.Disambiguation, Country: a.Country, Type: a.Type})
	}
	return out, nil
}

// SearchArtistMBID finds the best-match MBID for an artist name.
func (m *MusicBrainz) SearchArtistMBID(ctx context.Context, name string) (string, error) {
	var resp struct {
		Artists []struct {
			ID    string `json:"id"`
			Score int    `json:"score"`
		} `json:"artists"`
	}
	err := getJSON(ctx, m.lim,
		mbBase()+"/ws/2/artist?limit=1&fmt=json&query=artist:"+url.QueryEscape(`"`+name+`"`),
		map[string]string{"User-Agent": m.userAgent}, &resp)
	if err != nil || len(resp.Artists) == 0 || resp.Artists[0].Score < 90 {
		return "", err
	}
	return resp.Artists[0].ID, nil
}
