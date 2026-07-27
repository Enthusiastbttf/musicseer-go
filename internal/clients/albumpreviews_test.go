package clients

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// albumSearchServer stands in for Deezer: /search/album returns whatever
// candidates the test supplies, and /album/{id}/tracks returns a single track
// named after the id so a test can tell WHICH album was fetched.
func albumSearchServer(t *testing.T, searchJSON string) (*httptest.Server, *[]string) {
	t.Helper()
	var fetched []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/search/album":
			w.Write([]byte(searchJSON))
		case len(r.URL.Path) > 7 && r.URL.Path[:7] == "/album/":
			fetched = append(fetched, r.URL.Path)
			w.Write([]byte(`{"data":[{"title":"track from ` + r.URL.Path + `","preview":"http://x/p.mp3","duration":180}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	os.Setenv("MUSICSEER_DEEZER_BASE", srv.URL)
	t.Cleanup(func() {
		srv.Close()
		os.Unsetenv("MUSICSEER_DEEZER_BASE")
	})
	return srv, &fetched
}

// The reported bug: artist "Red" + album "Gone" is a real RED (Christian rock)
// release, but Deezer's advanced search degrades to keyword relevance and
// returned an unrelated gospel single, which was rendered as RED's track list.
// A candidate whose artist name doesn't match must be rejected outright, and
// no /album/{id}/tracks call should be made at all.
func TestAlbumPreviewsRejectsWrongArtist(t *testing.T) {
	_, fetched := albumSearchServer(t, `{"data":[
		{"id":999,"title":"Gone","artist":{"name":"Sister Mary Gospel Choir"}}
	]}`)

	tracks, err := NewDeezer().AlbumPreviews(context.Background(), "Red", "Gone", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 0 {
		t.Fatalf("wrong-artist album must be rejected, got %d tracks: %+v", len(tracks), tracks)
	}
	if len(*fetched) != 0 {
		t.Fatalf("must not fetch tracks for an unverified album, fetched %v", *fetched)
	}
}

// A title that matches but under the wrong artist, alongside the right album
// further down the list, must select the right one — Deezer ranks by
// popularity, so the correct release is often not first.
func TestAlbumPreviewsPicksVerifiedCandidateNotFirst(t *testing.T) {
	_, fetched := albumSearchServer(t, `{"data":[
		{"id":111,"title":"Gone","artist":{"name":"Some Pop Act"}},
		{"id":222,"title":"Gone Away","artist":{"name":"Red"}},
		{"id":333,"title":"Gone (Deluxe Edition)","artist":{"name":"RED"}}
	]}`)

	tracks, err := NewDeezer().AlbumPreviews(context.Background(), "Red", "Gone", 50)
	if err != nil {
		t.Fatal(err)
	}
	// id 333: casing differs and it carries a "(Deluxe Edition)" qualifier,
	// both of which normAlbum is expected to absorb. id 222 is a different
	// album by the right artist and must not win.
	if len(tracks) != 1 || tracks[0].Title != "track from /album/333/tracks" {
		t.Fatalf("wrong album selected: %+v (fetched %v)", tracks, *fetched)
	}
}

// No candidates at all is not an error — it falls through to the MusicBrainz
// track list one layer up.
func TestAlbumPreviewsNoCandidates(t *testing.T) {
	_, fetched := albumSearchServer(t, `{"data":[]}`)

	tracks, err := NewDeezer().AlbumPreviews(context.Background(), "Red", "Gone", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 0 || len(*fetched) != 0 {
		t.Fatalf("want no tracks and no fetch, got %+v / %v", tracks, *fetched)
	}
}

// The happy path still works: exact artist and title match is used.
func TestAlbumPreviewsExactMatch(t *testing.T) {
	_, _ = albumSearchServer(t, `{"data":[
		{"id":77,"title":"Of Beauty and Rage","artist":{"name":"Red"}}
	]}`)

	tracks, err := NewDeezer().AlbumPreviews(context.Background(), "Red", "Of Beauty and Rage", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 || tracks[0].Preview != "http://x/p.mp3" || tracks[0].Duration != 180 {
		t.Fatalf("exact match should be used: %+v", tracks)
	}
}
