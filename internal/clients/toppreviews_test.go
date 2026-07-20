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
