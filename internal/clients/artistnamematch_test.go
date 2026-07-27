package clients

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// topNameServer stands in for Deezer's artist search and top-tracks endpoints.
// tops maps "/artist/{id}/top" to its JSON, so each candidate gets its own
// catalogue, and the returned slice records which ones were actually fetched.
func topNameServer(t *testing.T, searchJSON string, tops map[string]string) *[]string {
	t.Helper()
	var fetched []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/search/artist":
			w.Write([]byte(searchJSON))
		case strings.HasSuffix(r.URL.Path, "/top"):
			fetched = append(fetched, r.URL.Path)
			if body, ok := tops[r.URL.Path]; ok {
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

// The reported bug, exactly as it rendered on RED's artist page: Deezer's top
// hit for "Red" was an unrelated artist named 肯定, whose single track was
// published as RED's top tracks. Two faults compounded — the search returned
// IDs only, so no name could be checked, and the function ended by returning
// the most-relevant match anyway when nothing corroborated. A candidate that
// isn't named "Red" must yield nothing, and must not even cost a top-tracks
// call.
func TestTopPreviewsForRejectsDifferentlyNamedArtist(t *testing.T) {
	fetched := topNameServer(t, `{"data":[
		{"id":1,"name":"肯定"},
		{"id":2,"name":"Redbone"}
	]}`, map[string]string{
		"/artist/1/top": `{"data":[{"title":"Lovers Gone By (Gospel Song)","preview":"http://x/1.mp3","duration":200,"album":{"title":"Gone"}}]}`,
	})

	tracks, err := NewDeezer().TopPreviewsFor(context.Background(), "Red",
		[]string{"Gone", "Of Beauty and Rage"}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 0 {
		t.Fatalf("an artist not named Red must not supply RED's top tracks, got %+v", tracks)
	}
	if len(*fetched) != 0 {
		t.Fatalf("the name check should reject before any top-tracks call, made %v", *fetched)
	}
}

// 肯定 and 確定 both normalize to "" under normAlbum, which keeps only ASCII
// alphanumerics — so a plain normalized comparison makes every non-Latin name
// equal to every other. sameName has to keep them apart.
func TestTopPreviewsForNonLatinNamesDoNotCollide(t *testing.T) {
	fetched := topNameServer(t, `{"data":[
		{"id":1,"name":"肯定"}
	]}`, map[string]string{
		"/artist/1/top": `{"data":[{"title":"wrong","preview":"http://x/1.mp3","duration":200,"album":{"title":"Gone"}}]}`,
	})

	tracks, err := NewDeezer().TopPreviewsFor(context.Background(), "確定", []string{"Gone"}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 0 {
		t.Fatalf("distinct non-Latin names must not match, got %+v (fetched %v)", tracks, *fetched)
	}
}

// ...but a non-Latin name still matches itself. The empty-normalization
// fallback must not reject every CJK artist outright.
func TestTopPreviewsForNonLatinNameMatchesItself(t *testing.T) {
	topNameServer(t, `{"data":[
		{"id":1,"name":"肯定"}
	]}`, map[string]string{
		"/artist/1/top": `{"data":[{"title":"right","preview":"http://x/1.mp3","duration":200,"album":{"title":"Gone"}}]}`,
	})

	tracks, err := NewDeezer().TopPreviewsFor(context.Background(), "肯定", []string{"Gone"}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 || tracks[0].Title != "right" {
		t.Fatalf("a name should match itself, got %+v", tracks)
	}
}

// Two real bands called Red: the one whose top tracks sit on albums from the
// MusicBrainz discography wins, even though Deezer ranks it second.
func TestTopPreviewsForPicksDiscographyMatch(t *testing.T) {
	topNameServer(t, `{"data":[
		{"id":1,"name":"Red"},
		{"id":2,"name":"RED"}
	]}`, map[string]string{
		"/artist/1/top": `{"data":[{"title":"Not Ours","preview":"http://x/a.mp3","duration":200,"album":{"title":"Some Other Record"}}]}`,
		"/artist/2/top": `{"data":[{"title":"Death of Me","preview":"http://x/b.mp3","duration":210,"album":{"title":"Of Beauty and Rage"}}]}`,
	})

	tracks, err := NewDeezer().TopPreviewsFor(context.Background(), "Red",
		[]string{"Gone", "Of Beauty and Rage", "Release the Panic"}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 || tracks[0].Title != "Death of Me" {
		t.Fatalf("want the discography-matching Red's tracks, got %+v", tracks)
	}
}

// Right name on every candidate, but nothing either is known to have
// released: fail closed rather than pick one.
func TestTopPreviewsForSameNameNoOverlapReturnsNothing(t *testing.T) {
	topNameServer(t, `{"data":[
		{"id":1,"name":"Red"},
		{"id":2,"name":"Red"}
	]}`, map[string]string{
		"/artist/1/top": `{"data":[{"title":"x","preview":"http://x/a.mp3","duration":200,"album":{"title":"Mystery One"}}]}`,
		"/artist/2/top": `{"data":[{"title":"y","preview":"http://x/b.mp3","duration":200,"album":{"title":"Mystery Two"}}]}`,
	})

	tracks, err := NewDeezer().TopPreviewsFor(context.Background(), "Red",
		[]string{"Gone", "Of Beauty and Rage"}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 0 {
		t.Fatalf("unverifiable namesakes must yield no tracks, got %+v", tracks)
	}
}

// With no discography there is genuinely nothing to verify against, so the
// name-matched artist is used as-is — but the near-miss above it is still
// skipped.
func TestTopPreviewsForNoDiscographySkipsNearMiss(t *testing.T) {
	topNameServer(t, `{"data":[
		{"id":1,"name":"Redbone"},
		{"id":2,"name":"Red"}
	]}`, map[string]string{
		"/artist/1/top": `{"data":[{"title":"Come and Get Your Love","preview":"http://x/a.mp3","duration":200,"album":{"title":"Wovoka"}}]}`,
		"/artist/2/top": `{"data":[{"title":"Death of Me","preview":"http://x/b.mp3","duration":210,"album":{"title":"Of Beauty and Rage"}}]}`,
	})

	tracks, err := NewDeezer().TopPreviewsFor(context.Background(), "Red", nil, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 || tracks[0].Title != "Death of Me" {
		t.Fatalf("want the name-matched artist's tracks, got %+v", tracks)
	}
}

// TopPreviews takes no discography, so the name check is its only guard —
// it was the unguarded sibling that let the wrong artist through.
func TestTopPreviewsSkipsNameMismatches(t *testing.T) {
	fetched := topNameServer(t, `{"data":[
		{"id":1,"name":"肯定"},
		{"id":2,"name":"Red"}
	]}`, map[string]string{
		"/artist/1/top": `{"data":[{"title":"wrong","preview":"http://x/a.mp3","duration":200,"album":{"title":"Gone"}}]}`,
		"/artist/2/top": `{"data":[{"title":"right","preview":"http://x/b.mp3","duration":200,"album":{"title":"Gone"}}]}`,
	})

	tracks, err := NewDeezer().TopPreviews(context.Background(), "Red", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 || tracks[0].Title != "right" {
		t.Fatalf("want only the name-matched artist, got %+v", tracks)
	}
	for _, p := range *fetched {
		if p == "/artist/1/top" {
			t.Fatalf("must not fetch a differently-named artist's tracks, made %v", *fetched)
		}
	}
}

// No name matches at all, so there is nothing to return and nothing to fetch.
func TestTopPreviewsNoNameMatchReturnsNothing(t *testing.T) {
	fetched := topNameServer(t, `{"data":[
		{"id":1,"name":"Redbone"},
		{"id":2,"name":"Red Hot Chili Peppers"}
	]}`, nil)

	tracks, err := NewDeezer().TopPreviews(context.Background(), "Red", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 0 {
		t.Fatalf("no exact name match must yield no tracks, got %+v", tracks)
	}
	if len(*fetched) != 0 {
		t.Fatalf("must not fetch any candidate's tracks, made %v", *fetched)
	}
}

// The same name-collision reached the album endpoint too: an artist whose
// name normalizes to "" must not satisfy a query for a differently-named one.
func TestAlbumPreviewsNonLatinArtistDoesNotCollide(t *testing.T) {
	_, fetched := albumSearchServer(t, `{"data":[
		{"id":5,"title":"Gone","artist":{"name":"肯定"}}
	]}`)

	tracks, err := NewDeezer().AlbumPreviews(context.Background(), "確定", "Gone", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 0 || len(*fetched) != 0 {
		t.Fatalf("distinct non-Latin names must not match, got %+v / %v", tracks, *fetched)
	}
}

// And the artist-photo endpoint: 肯定's picture must not become 確定's.
func TestArtistImageNonLatinNamesDoNotCollide(t *testing.T) {
	artistImageServer(t, `{"data":[
		{"id":1,"name":"肯定","picture_xl":"http://x/wrong.jpg"}
	]}`, nil)

	got, err := NewDeezer().ArtistImageFor(context.Background(), "確定", []string{"Gone"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("distinct non-Latin names must not share a photo, got %q", got)
	}
}
