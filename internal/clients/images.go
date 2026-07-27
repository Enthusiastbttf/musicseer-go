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

// AlbumPreviews finds an album by artist+title and returns its tracks'
// 30-second samples.
//
// Deezer's advanced search does NOT fail closed: when no album matches the
// artist+album pair it degrades to loose keyword relevance rather than
// returning nothing, so the top hit can be a completely unrelated release.
// Common single words are the worst case — artist "Red" + album "Gone"
// returned another artist's gospel track, which was then rendered as RED's
// discography. So candidates are verified here rather than trusted: the hit
// is used only when BOTH its artist name and its title match what was asked
// for. Same failure mode TopPreviewsFor guards against, one endpoint over.
//
// Returning nil on no verified match is deliberate — AlbumTrackList then
// falls through to the MusicBrainz track list, which is authoritative but
// carries no samples. A silent-but-correct list beats a playable wrong one.
func (d *Deezer) AlbumPreviews(ctx context.Context, artist, album string, limit int) ([]DeezerTrack, error) {
	q := `artist:"` + artist + `" album:"` + album + `"`
	var search struct {
		Data []struct {
			ID     int64  `json:"id"`
			Title  string `json:"title"`
			Artist struct {
				Name string `json:"name"`
			} `json:"artist"`
		} `json:"data"`
	}
	// Over-fetch: the exact release is often ranked below a more "popular"
	// near-miss, so the right answer can sit at position 2 or 3.
	if err := getJSON(ctx, d.lim, deezerBase()+"/search/album?limit=5&q="+url.QueryEscape(q), nil, &search); err != nil {
		return nil, err
	}
	wantArtist, wantAlbum := normAlbum(artist), normAlbum(album)
	var id int64
	for _, c := range search.Data {
		// normAlbum strips trailing "(Deluxe Edition)"-style qualifiers, so a
		// Deezer deluxe still matches MusicBrainz's standard-edition title.
		if normAlbum(c.Artist.Name) == wantArtist && normAlbum(c.Title) == wantAlbum {
			id = c.ID
			break
		}
	}
	if id == 0 {
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
		deezerBase()+"/album/"+strconv.FormatInt(id, 10)+"/tracks?limit="+fmtInt(limit), nil, &tracks); err != nil {
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

// searchArtistIDs returns up to `limit` Deezer artist IDs for a name query,
// in Deezer's relevance order (most popular first).
func (d *Deezer) searchArtistIDs(ctx context.Context, name string, limit int) ([]int64, error) {
	var search struct {
		Data []struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	if err := getJSON(ctx, d.lim, deezerBase()+"/search/artist?limit="+fmtInt(limit)+"&q="+url.QueryEscape(name), nil, &search); err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(search.Data))
	for _, a := range search.Data {
		ids = append(ids, a.ID)
	}
	return ids, nil
}

// artistTop fetches one Deezer artist's top tracks with the album each appears
// on, dropping tracks that have no playable preview.
func (d *Deezer) artistTop(ctx context.Context, id int64, limit int) ([]DeezerTrack, error) {
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
		deezerBase()+"/artist/"+strconv.FormatInt(id, 10)+"/top?limit="+fmtInt(limit), nil, &top); err != nil {
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

// TopPreviews returns the most-relevant same-named Deezer artist's top tracks.
// Use TopPreviewsFor when a discography is available to disambiguate.
func (d *Deezer) TopPreviews(ctx context.Context, name string, limit int) ([]DeezerTrack, error) {
	ids, err := d.searchArtistIDs(ctx, name, 1)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	return d.artistTop(ctx, ids[0], limit)
}

// TopPreviewsFor picks, among same-named Deezer artists, the one whose top
// tracks' albums best overlap knownAlbums (the artist's MusicBrainz
// discography), and returns ONLY that artist's tracks whose album is in the
// discography. This does two jobs: it stops Deezer's name-only lookup from
// returning a different band that shares the name (e.g. the 1980s metal
// "Incubus" vs the rock one), and it drops individual stray tracks — features,
// compilation appearances, or another artist's release that Deezer filed under
// this name — that don't belong to this artist at all.
//
// Falls back to the most-relevant unfiltered match when no discography is
// supplied or nothing overlaps anywhere, so a thin/missing MusicBrainz
// discography degrades to the old behaviour instead of an empty section.
func (d *Deezer) TopPreviewsFor(ctx context.Context, name string, knownAlbums []string, limit int) ([]DeezerTrack, error) {
	ids, err := d.searchArtistIDs(ctx, name, 5)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	known := make(map[string]struct{}, len(knownAlbums))
	for _, a := range knownAlbums {
		if k := normAlbum(a); k != "" {
			known[k] = struct{}{}
		}
	}
	// Over-fetch so that dropping mismatched tracks still leaves a full list.
	fetch := limit * 4
	if fetch < 20 {
		fetch = 20
	}

	var firstNonEmpty, best []DeezerTrack
	for i, id := range ids {
		tracks, err := d.artistTop(ctx, id, fetch)
		if err != nil || len(tracks) == 0 {
			continue
		}
		if firstNonEmpty == nil {
			firstNonEmpty = tracks
		}
		if len(known) == 0 {
			return capTracks(tracks, limit), nil // nothing to disambiguate against
		}
		kept := keepKnownAlbums(tracks, known)
		if len(kept) > len(best) {
			best = kept
		}
		if i == 0 && len(kept) > 0 {
			return capTracks(kept, limit), nil // Deezer's top match already fits — skip the rest
		}
	}
	if len(best) > 0 {
		return capTracks(best, limit), nil
	}
	return capTracks(firstNonEmpty, limit), nil // no overlap anywhere; best-effort = most relevant
}

// keepKnownAlbums returns the tracks whose album appears in the artist's known
// discography, preserving Deezer's popularity order.
func keepKnownAlbums(tracks []DeezerTrack, known map[string]struct{}) []DeezerTrack {
	out := make([]DeezerTrack, 0, len(tracks))
	for _, t := range tracks {
		if t.Album == "" {
			continue // no album to verify against — can't confirm it's this artist's
		}
		if _, ok := known[normAlbum(t.Album)]; ok {
			out = append(out, t)
		}
	}
	return out
}

func capTracks(tracks []DeezerTrack, limit int) []DeezerTrack {
	if limit > 0 && len(tracks) > limit {
		return tracks[:limit]
	}
	return tracks
}

// normAlbum normalizes an album title for loose overlap matching: lowercase,
// drop trailing "(...)"/"[...]" qualifiers such as "(Deluxe)" or "[Remastered]",
// then keep only ASCII alphanumerics. Deliberately mirrors normTitle() in
// web/app/src/pages/Artist.tsx so the server's filter and the client's
// Album/EP/Single badge agree on what counts as a match.
func normAlbum(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	for {
		t := strings.TrimRight(s, " ")
		n := len(t)
		if n == 0 {
			break
		}
		open := byte(0)
		switch t[n-1] {
		case ')':
			open = '('
		case ']':
			open = '['
		}
		if open == 0 {
			break
		}
		i := strings.LastIndexByte(t, open)
		if i <= 0 {
			break
		}
		s = t[:i] // strictly shorter, so this loop always terminates
	}
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteByte(byte(r))
		}
	}
	return b.String()
}

// deezerArtistHit is one /search/artist result, with the picture sizes we
// fall back through.
type deezerArtistHit struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	PictureXL string `json:"picture_xl"`
	PictureBg string `json:"picture_big"`
	PictureMd string `json:"picture_medium"`
}

// picture returns the largest available image, or "" when Deezer has none.
func (h deezerArtistHit) picture() string {
	for _, u := range []string{h.PictureXL, h.PictureBg, h.PictureMd} {
		if u != "" {
			return u
		}
	}
	return ""
}

func (d *Deezer) searchArtists(ctx context.Context, name string, limit int) ([]deezerArtistHit, error) {
	var resp struct {
		Data []deezerArtistHit `json:"data"`
	}
	if err := getJSON(ctx, d.lim, deezerBase()+"/search/artist?limit="+fmtInt(limit)+"&q="+url.QueryEscape(name), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// artistAlbumTitles returns the titles of one Deezer artist's releases, used
// to tell same-named artists apart.
func (d *Deezer) artistAlbumTitles(ctx context.Context, id int64, limit int) ([]string, error) {
	var resp struct {
		Data []struct {
			Title string `json:"title"`
		} `json:"data"`
	}
	if err := getJSON(ctx, d.lim,
		deezerBase()+"/artist/"+strconv.FormatInt(id, 10)+"/albums?limit="+fmtInt(limit), nil, &resp); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(resp.Data))
	for _, a := range resp.Data {
		out = append(out, a.Title)
	}
	return out, nil
}

// ArtistImage looks up an artist photo by name alone. Prefer ArtistImageFor,
// which can tell same-named artists apart.
func (d *Deezer) ArtistImage(ctx context.Context, name string) (string, error) {
	return d.ArtistImageFor(ctx, name, nil)
}

// ArtistImageFor returns an artist photo, using knownAlbums (the artist's
// MusicBrainz discography) to pick between artists that share a name.
//
// Deezer ranks /search/artist by popularity, not by string match, and the
// previous code took the first hit unverified — so a search for "Red" could
// return Redbone's photo, and even an exact hit could be a different band
// called Red. Two guards, cheapest first:
//
//  1. Drop hits whose name isn't the one asked for. This alone kills the
//     fuzzy near-misses and costs nothing extra.
//  2. If several real artists genuinely share the name, fetch each one's
//     releases and keep the one overlapping the known discography. Only
//     ambiguous names pay for this, so the common case stays a single call.
//
// Returns "" rather than guessing when nothing verifies. That is deliberate:
// the caller falls back to TheAudioDB keyed by MBID, which cannot pick the
// wrong artist, and a missing photo degrades to a placeholder while a wrong
// one silently misinforms.
func (d *Deezer) ArtistImageFor(ctx context.Context, name string, knownAlbums []string) (string, error) {
	hits, err := d.searchArtists(ctx, name, 5)
	if err != nil || len(hits) == 0 {
		return "", err
	}

	wantName := normAlbum(name) // same normalizer: lowercase, alphanumerics only
	var exact []deezerArtistHit
	for _, h := range hits {
		if normAlbum(h.Name) == wantName && h.picture() != "" {
			exact = append(exact, h)
		}
	}
	if len(exact) == 0 {
		return "", nil
	}
	if len(exact) == 1 || len(knownAlbums) == 0 {
		// Unambiguous, or no discography to disambiguate with. Deezer's own
		// relevance order is the best signal left.
		return exact[0].picture(), nil
	}

	known := make(map[string]struct{}, len(knownAlbums))
	for _, a := range knownAlbums {
		if k := normAlbum(a); k != "" {
			known[k] = struct{}{}
		}
	}
	for _, h := range exact {
		titles, err := d.artistAlbumTitles(ctx, h.ID, 50)
		if err != nil {
			continue
		}
		for _, t := range titles {
			if _, ok := known[normAlbum(t)]; ok {
				return h.picture(), nil
			}
		}
	}
	return "", nil // several same-named artists, none provably this one
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

// MusicBrainz caps a page at 100 results. Prolific artists (Imagine Dragons
// have well over 100 release groups once singles are counted) were silently
// truncated by the old single-page fetch, which both hid releases from the
// discography and made top-track album matching fail for anything missing.
const (
	mbPageLimit = 100
	mbMaxPages  = 4 // 400 release groups; bounds the worst case at ~4 rate-limited calls
)

// ReleaseGroups returns an artist's discography (albums, EPs, singles), paging
// through MusicBrainz until the set is complete (or mbMaxPages is reached).
// Cached by the caller for a week, so the extra calls are a cold-fetch cost
// only. A mid-way error returns what has been collected rather than failing.
func (m *MusicBrainz) ReleaseGroups(ctx context.Context, artistMBID string) ([]MBReleaseGroup, error) {
	var out []MBReleaseGroup
	fetched := 0
	for page := 0; page < mbMaxPages; page++ {
		var resp struct {
			Count         int `json:"release-group-count"`
			ReleaseGroups []struct {
				ID             string   `json:"id"`
				Title          string   `json:"title"`
				PrimaryType    string   `json:"primary-type"`
				SecondaryTypes []string `json:"secondary-types"`
				FirstRelease   string   `json:"first-release-date"`
			} `json:"release-groups"`
		}
		// The browse endpoint (unlike the artist lookup) reports the total count,
		// so we only pay for extra pages when there are extra pages.
		u := mbBase() + "/ws/2/release-group?artist=" + url.QueryEscape(artistMBID) +
			"&type=album%7Cep%7Csingle&limit=" + fmtInt(mbPageLimit) +
			"&offset=" + fmtInt(page*mbPageLimit) + "&fmt=json"
		if err := getJSON(ctx, m.lim, u, map[string]string{"User-Agent": m.userAgent}, &resp); err != nil {
			if page == 0 {
				return nil, err
			}
			break // partial discography beats none
		}
		if len(resp.ReleaseGroups) == 0 {
			break
		}
		fetched += len(resp.ReleaseGroups)
		for _, rg := range resp.ReleaseGroups {
			if rg.PrimaryType == "" {
				continue
			}
			out = append(out, MBReleaseGroup{
				MBID: rg.ID, Title: rg.Title, PrimaryType: rg.PrimaryType,
				SecondaryTypes: rg.SecondaryTypes, FirstRelease: rg.FirstRelease,
			})
		}
		if len(resp.ReleaseGroups) < mbPageLimit || (resp.Count > 0 && fetched >= resp.Count) {
			break
		}
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
