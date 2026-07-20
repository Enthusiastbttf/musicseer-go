package clients

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// TopPreviews should carry each top track's album title through, and still
// drop tracks that have no playable preview.
func TestDeezerTopPreviewsAlbum(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/artist":
			w.Write([]byte(`{"data":[{"id":42}]}`))
		case "/artist/42/top":
			w.Write([]byte(`{"data":[
				{"title":"Bring Me to Life","preview":"http://x/bmtl.mp3","duration":237,"album":{"title":"Fallen"}},
				{"title":"No Sample","preview":"","duration":100,"album":{"title":"Fallen"}},
				{"title":"What You Want","preview":"http://x/wyw.mp3","duration":218,"album":{"title":"Evanescence"}}
			]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	os.Setenv("MUSICSEER_DEEZER_BASE", srv.URL)
	defer os.Unsetenv("MUSICSEER_DEEZER_BASE")

	tracks, err := NewDeezer().TopPreviews(context.Background(), "Evanescence", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 2 { // the empty-preview track is dropped
		t.Fatalf("want 2 tracks, got %d: %+v", len(tracks), tracks)
	}
	if tracks[0].Title != "Bring Me to Life" || tracks[0].Album != "Fallen" || tracks[0].Preview != "http://x/bmtl.mp3" {
		t.Fatalf("first track parsed wrong: %+v", tracks[0])
	}
	if tracks[1].Title != "What You Want" || tracks[1].Album != "Evanescence" {
		t.Fatalf("second track parsed wrong: %+v", tracks[1])
	}
}

// TopPreviewsFor should pick the same-named Deezer artist whose top-track
// albums overlap the supplied discography, not Deezer's most-relevant match.
func TestDeezerTopPreviewsForDisambiguates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/artist":
			w.Write([]byte(`{"data":[{"id":1},{"id":2}]}`)) // id 1 = most relevant (wrong band)
		case "/artist/1/top":
			w.Write([]byte(`{"data":[{"title":"Blaspheming Prophets","preview":"http://x/bp.mp3","duration":200,"album":{"title":"Serpent Temptation"}}]}`))
		case "/artist/2/top":
			w.Write([]byte(`{"data":[{"title":"Drive","preview":"http://x/drive.mp3","duration":230,"album":{"title":"Make Yourself"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	os.Setenv("MUSICSEER_DEEZER_BASE", srv.URL)
	defer os.Unsetenv("MUSICSEER_DEEZER_BASE")

	known := []string{"Make Yourself", "Morning View", "S.C.I.E.N.C.E."}
	tracks, err := NewDeezer().TopPreviewsFor(context.Background(), "Incubus", known, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 || tracks[0].Title != "Drive" || tracks[0].Album != "Make Yourself" {
		t.Fatalf("expected the discography-matching artist (Drive/Make Yourself), got %+v", tracks)
	}
}

// With no discography to match against, TopPreviewsFor falls back to Deezer's
// most-relevant match (preserving the old behavior).
func TestDeezerTopPreviewsForFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/artist":
			w.Write([]byte(`{"data":[{"id":1},{"id":2}]}`))
		case "/artist/1/top":
			w.Write([]byte(`{"data":[{"title":"Blaspheming Prophets","preview":"http://x/bp.mp3","duration":200,"album":{"title":"Serpent Temptation"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	os.Setenv("MUSICSEER_DEEZER_BASE", srv.URL)
	defer os.Unsetenv("MUSICSEER_DEEZER_BASE")

	tracks, err := NewDeezer().TopPreviewsFor(context.Background(), "Incubus", nil, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 1 || tracks[0].Title != "Blaspheming Prophets" {
		t.Fatalf("expected fallback to most-relevant match, got %+v", tracks)
	}
}
