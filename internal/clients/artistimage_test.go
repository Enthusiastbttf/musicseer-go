package clients

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// artistImageServer stands in for Deezer. albums maps an artist id path
// ("/artist/1/albums") to the JSON to return, so a test can give each
// same-named candidate its own discography.
func artistImageServer(t *testing.T, searchJSON string, albums map[string]string) *[]string {
	t.Helper()
	var fetched []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/search/artist":
			w.Write([]byte(searchJSON))
		case strings.HasSuffix(r.URL.Path, "/albums"):
			fetched = append(fetched, r.URL.Path)
			if body, ok := albums[r.URL.Path]; ok {
				w.Write([]byte(body))
				return
			}
			w.Write([]byte(`{"data":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	os.Setenv("MUSICSEER_DEEZER_BASE", srv.URL)
	t.Cleanup(func() {
		srv.Close()
		os.Unsetenv("MUSICSEER_DEEZER_BASE")
	})
	return &fetched
}

// Deezer ranks by popularity, not string match, so a search for "Red" returns
// bands merely starting with it. Those must not supply the photo, and the
// name check must be cheap — no per-candidate discography calls.
func TestArtistImageRejectsNameNearMisses(t *testing.T) {
	fetched := artistImageServer(t, `{"data":[
		{"id":1,"name":"Redbone","picture_xl":"http://x/redbone.jpg"},
		{"id":2,"name":"Red Hot Chili Peppers","picture_xl":"http://x/rhcp.jpg"}
	]}`, nil)

	got, err := NewDeezer().ArtistImageFor(context.Background(), "Red", []string{"Gone"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("near-miss name must not supply a photo, got %q", got)
	}
	if len(*fetched) != 0 {
		t.Fatalf("name check should not cost album calls, made %v", *fetched)
	}
}

// One artist by that name: use it directly, without paying for disambiguation.
func TestArtistImageSingleExactMatchSkipsDisambiguation(t *testing.T) {
	fetched := artistImageServer(t, `{"data":[
		{"id":1,"name":"Evanescence","picture_xl":"http://x/ev.jpg"},
		{"id":2,"name":"Evanescence Tribute","picture_xl":"http://x/trib.jpg"}
	]}`, nil)

	got, err := NewDeezer().ArtistImageFor(context.Background(), "Evanescence", []string{"Fallen"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://x/ev.jpg" {
		t.Fatalf("want the exact-name artist's photo, got %q", got)
	}
	if len(*fetched) != 0 {
		t.Fatalf("unambiguous name should cost no album calls, made %v", *fetched)
	}
}

// The reported ambiguity: several real bands are called "Red". The one whose
// releases overlap the MusicBrainz discography wins, even when Deezer ranks
// it below a more popular namesake.
func TestArtistImageDisambiguatesSameNameByDiscography(t *testing.T) {
	artistImageServer(t, `{"data":[
		{"id":1,"name":"Red","picture_xl":"http://x/wrong-red.jpg"},
		{"id":2,"name":"RED","picture_xl":"http://x/right-red.jpg"}
	]}`, map[string]string{
		"/artist/1/albums": `{"data":[{"title":"Lovers Gone By"},{"title":"Gospel Roots"}]}`,
		"/artist/2/albums": `{"data":[{"title":"Gone (Deluxe Edition)"},{"title":"Of Beauty and Rage"}]}`,
	})

	got, err := NewDeezer().ArtistImageFor(context.Background(), "Red",
		[]string{"Gone", "Of Beauty and Rage", "Release the Panic"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://x/right-red.jpg" {
		t.Fatalf("want the discography-matching Red, got %q", got)
	}
}

// Same name, no overlap anywhere: return nothing so the caller falls through
// to the MBID-keyed source rather than showing a plausible wrong face.
func TestArtistImageNoOverlapReturnsNothing(t *testing.T) {
	artistImageServer(t, `{"data":[
		{"id":1,"name":"Red","picture_xl":"http://x/a.jpg"},
		{"id":2,"name":"Red","picture_xl":"http://x/b.jpg"}
	]}`, map[string]string{
		"/artist/1/albums": `{"data":[{"title":"Something Else"}]}`,
		"/artist/2/albums": `{"data":[{"title":"Another Thing"}]}`,
	})

	got, err := NewDeezer().ArtistImageFor(context.Background(), "Red", []string{"Gone"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("unverifiable namesakes must yield no photo, got %q", got)
	}
}

// Regression test on the original ArtistImage signature: it used to take
// search.Data[0] unverified, so a query for "Red" returned Redbone's photo
// and pinned it to RED's artist page. Even with no discography to
// disambiguate with, a name that doesn't match must yield nothing.
func TestArtistImageDoesNotReturnADifferentArtistsPhoto(t *testing.T) {
	artistImageServer(t, `{"data":[
		{"id":1,"name":"Redbone","picture_xl":"http://x/redbone.jpg"}
	]}`, nil)

	got, err := NewDeezer().ArtistImage(context.Background(), "Red")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("must not return a different artist's photo, got %q", got)
	}
}

// Picture sizes degrade largest-first, and a hit with no picture at all is
// not a usable candidate.
func TestArtistImagePictureFallback(t *testing.T) {
	artistImageServer(t, `{"data":[
		{"id":1,"name":"Red","picture_xl":"","picture_big":"","picture_medium":""},
		{"id":2,"name":"Red","picture_xl":"","picture_big":"http://x/big.jpg"}
	]}`, nil)

	// Only id 2 has any picture, so it is the single usable candidate and
	// disambiguation is skipped.
	got, err := NewDeezer().ArtistImageFor(context.Background(), "Red", []string{"Gone"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://x/big.jpg" {
		t.Fatalf("want the big-size fallback, got %q", got)
	}
}
