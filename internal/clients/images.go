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

func deezerBase() string {
	if b := os.Getenv("MUSICSEER_DEEZER_BASE"); b != "" { // test hook
		return b
	}
	return "https://api.deezer.com"
}

// DeezerChartArtist is one entry from Deezer's public streaming charts.
type DeezerChartArtist struct {
	Name    string `json:"name"`
	Picture string `json:"picture_xl"`
}

// ChartArtists returns Deezer's global top artists — a mainstream chart from
// tens of millions of listeners, keyless. Used as the trending source when
// no Last.fm key is configured (ListenBrainz's sitewide chart is heavily
// skewed by its small, fan-campaign-prone user base).
func (d *Deezer) ChartArtists(ctx context.Context, limit int) ([]DeezerChartArtist, error) {
	var resp struct {
		Data []DeezerChartArtist `json:"data"`
	}
	if err := getJSON(ctx, d.lim, deezerBase()+"/chart/0/artists?limit="+fmtInt(limit), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// DeezerTrackHit is one track from a keyword search: enough to show a rich
// result row (cover, artist, album, 30-second preview) and to navigate to the
// artist page. Deezer has no MBID concept, so the artist is matched by name.
type DeezerTrackHit struct {
	Title    string
	Artist   string
	Album    string
	CoverURL string
	Preview  string
	Duration int
}

// SearchTracks does a keyword track search (keyless). Deezer's /search is the
// best keyless option here: it returns the artist name, album title, cover art
// and a 30-second preview URL in one call.
func (d *Deezer) SearchTracks(ctx context.Context, query string, limit int) ([]DeezerTrackHit, error) {
	var resp struct {
		Data []struct {
			Title    string `json:"title"`
			Preview  string `json:"preview"`
			Duration int    `json:"duration"`
			Artist   struct {
				Name string `json:"name"`
			} `json:"artist"`
			Album struct {
				Title string `json:"title"`
				Cover string `json:"cover_medium"`
			} `json:"album"`
		} `json:"data"`
	}
	if err := getJSON(ctx, d.lim, deezerBase()+"/search?limit="+fmtInt(limit)+"&q="+url.QueryEscape(query), nil, &resp); err != nil {
		return nil, err
	}
	out := make([]DeezerTrackHit, 0, len(resp.Data))
	for _, t := range resp.Data {
		out = append(out, DeezerTrackHit{
			Title: t.Title, Artist: t.Artist.Name, Album: t.Album.Title,
			CoverURL: t.Album.Cover, Preview: t.Preview, Duration: t.Duration,
		})
	}
	return out, nil
}

// AlbumPreviews finds an album by artist+title (a much more precise match
// than artist name alone) and returns its tracks' 30-second samples.
func (d *Deezer) AlbumPreviews(ctx context.Context, artist, album string, limit int) ([]DeezerTrack, error) {
	q := `artist:"` + artist + `" album:"` + album + `"`
	var search struct {
		Data []struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	if err := getJSON(ctx, d.lim, deezerBase()+"/search/album?limit=1&q="+url.QueryEscape(q), nil, &search); err != nil {
		return nil, err
	}
	if len(search.Data) == 0 {
		return nil, nil
	}
	var tracks struct {
		Data []struct {
			Title    string `json:"title"`
			Preview  string `json:"preview"`
			Duration int    `json:"duration"`
		} `json:"data"`
	}
	if err := getJSON(ctx, d.lim,
		deezerBase()+"/album/"+strconv.FormatInt(search.Data[0].ID, 10)+"/tracks?limit="+fmtInt(limit), nil, &tracks); err != nil {
		return nil, err
	}
	out := make([]DeezerTrack, 0, len(tracks.Data))
	for _, t := range tracks.Data {
		out = append(out, DeezerTrack{Title: t.Title, Preview: t.Preview, Duration: t.Duration})
	}
	return out, nil
}

// DeezerTrack is one preview-able track. Album is the release the track
// appears on; it is populated only for artist top tracks (TopPreviews) and
// left empty for album track lists, where the album is already known.
type DeezerTrack struct {
	Title    string `json:"title"`
	Preview  string `json:"preview"` // 30s MP3 sample URL
	Duration int    `json:"duration"`
	Album    string `json:"album,omitempty"`
}

// TopPreviews returns an artist's top tracks with 30-second sample URLs.
func (d *Deezer) TopPreviews(ctx context.Context, name string, limit int) ([]DeezerTrack, error) {
	var search struct {
		Data []struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	if err := getJSON(ctx, d.lim, deezerBase()+"/search/artist?limit=1&q="+url.QueryEscape(name), nil, &search); err != nil {
		return nil, err
	}
	if len(search.Data) == 0 {
		return nil, nil
	}
	// Deezer's /top track objects carry the album they appear on; capture the
	// title so the UI can label each top track without another API call.
	var top struct {
		Data []struct {
			Title    string `json:"title"`
			Preview  string `json:"preview"`
			Duration int    `json:"duration"`
			Album    struct {
				Title string `json:"title"`
			} `json:"album"`
		} `json:"data"`
	}
	if err := getJSON(ctx, d.lim,
		deezerBase()+"/artist/"+strconv.FormatInt(search.Data[0].ID, 10)+"/top?limit="+fmtInt(limit), nil, &top); err != nil {
		return nil, err
	}
	out := make([]DeezerTrack, 0, len(top.Data))
	for _, t := range top.Data {
		if t.Preview != "" {
			out = append(out, DeezerTrack{Title: t.Title, Preview: t.Preview, Duration: t.Duration, Album: t.Album.Title})
		}
	}
	return out, nil
}

func (d *Deezer) ArtistImage(ctx context.Context, name string) (string, error) {
	var resp struct {
		Data []struct {
			Name      string `json:"name"`
			PictureXL string `json:"picture_xl"`
			PictureBg string `json:"picture_big"`
			PictureMd string `json:"picture_medium"`
		} `json:"data"`
	}
	err := getJSON(ctx, d.lim, deezerBase()+"/search/artist?limit=1&q="+url.QueryEscape(name), nil, &resp)
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
	return &MusicBrainz{lim: newLimiter(0.9), userAgent: "MusicSeerEnhanced/2 (" + contact + ")"}
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

// MBTrack is one track from a MusicBrainz release.
type MBTrack struct {
	Position int    `json:"position"`
	Title    string `json:"title"`
	LengthMs int    `json:"lengthMs,omitempty"`
}

// ReleaseGroupTracks returns the track list of a release group's primary
// release. Two rate-limited calls; callers cache the result.
func (m *MusicBrainz) ReleaseGroupTracks(ctx context.Context, rgMBID string) ([]MBTrack, error) {
	var rg struct {
		Releases []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"releases"`
	}
	if err := getJSON(ctx, m.lim,
		mbBase()+"/ws/2/release-group/"+url.PathEscape(rgMBID)+"?inc=releases&fmt=json",
		map[string]string{"User-Agent": m.userAgent}, &rg); err != nil {
		return nil, err
	}
	if len(rg.Releases) == 0 {
		return nil, nil
	}
	releaseID := rg.Releases[0].ID
	for _, r := range rg.Releases {
		if r.Status == "Official" {
			releaseID = r.ID
			break
		}
	}
	var rel struct {
		Media []struct {
			Tracks []struct {
				Position int    `json:"position"`
				Title    string `json:"title"`
				Length   int    `json:"length"`
			} `json:"tracks"`
		} `json:"media"`
	}
	if err := getJSON(ctx, m.lim,
		mbBase()+"/ws/2/release/"+url.PathEscape(releaseID)+"?inc=recordings&fmt=json",
		map[string]string{"User-Agent": m.userAgent}, &rel); err != nil {
		return nil, err
	}
	var out []MBTrack
	pos := 0
	for _, med := range rel.Media {
		for _, t := range med.Tracks {
			pos++
			out = append(out, MBTrack{Position: pos, Title: t.Title, LengthMs: t.Length})
		}
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
