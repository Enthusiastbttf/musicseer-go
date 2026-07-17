package clients

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestDeezerSearchTracks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"data":[
			{"title":"Karma Police","preview":"http://x/kp.mp3","duration":261,
			 "artist":{"name":"Radiohead"},"album":{"title":"OK Computer","cover_medium":"http://x/okc.jpg"}},
			{"title":"Flood","preview":"","duration":240,
			 "artist":{"name":"Jars of Clay"},"album":{"title":"Jars of Clay","cover_medium":"http://x/joc.jpg"}}
		]}`))
	}))
	defer srv.Close()
	os.Setenv("MUSICSEER_DEEZER_BASE", srv.URL)
	defer os.Unsetenv("MUSICSEER_DEEZER_BASE")

	hits, err := NewDeezer().SearchTracks(context.Background(), "karma police", 30)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("want 2 hits, got %d", len(hits))
	}
	if hits[0].Title != "Karma Police" || hits[0].Artist != "Radiohead" ||
		hits[0].Album != "OK Computer" || hits[0].CoverURL != "http://x/okc.jpg" || hits[0].Preview != "http://x/kp.mp3" {
		t.Fatalf("first hit parsed wrong: %+v", hits[0])
	}
	if hits[1].Preview != "" {
		t.Fatalf("expected empty preview for Flood, got %q", hits[1].Preview)
	}
}
