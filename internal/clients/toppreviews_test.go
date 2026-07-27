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
			w.Write([]byte(`{"data":[{"id":42,"name":"Evanescence"}]}`))
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
			w.Write([]byte(`{"data":[{"id":1,"name":"Incubus"},{"id":2,"name":"Incubus"}]}`)) // id 1 = most relevant (wrong band)
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
			w.Write([]byte(`{"data":[{"id":1,"name":"Incubus"},{"id":2,"name":"Incubus"}]}`))
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

// The reported bug: Deezer's top tracks for Imagine Dragons included "Bones",
// filed under an album not in the artist's discography, which rendered with no
// Album/EP/Single badge. Mismatched tracks must not be listed at all.
func TestDeezerTopPreviewsForDropsMismatchedAlbums(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/artist":
			w.Write([]byte(`{"data":[{"id":7,"name":"Imagine Dragons"}]}`))
		case "/artist/7/top":
			w.Write([]byte(`{"data":[
				{"title":"Demons","preview":"http://x/1.mp3","duration":175,"album":{"title":"Night Visions"}},
				{"title":"Natural","preview":"http://x/2.mp3","duration":189,"album":{"title":"Origins (Deluxe)"}},
				{"title":"Bones","preview":"http://x/3.mp3","duration":165,"album":{"title":"Bones"}},
				{"title":"Whatever It Takes","preview":"http://x/4.mp3","duration":201,"album":{"title":"Evolve"}},
				{"title":"No Album","preview":"http://x/5.mp3","duration":100,"album":{"title":""}}
			]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	os.Setenv("MUSICSEER_DEEZER_BASE", srv.URL)
	defer os.Unsetenv("MUSICSEER_DEEZER_BASE")

	known := []string{"Night Visions", "Origins", "Evolve", "Mercury - Act 1"}
	tracks, err := NewDeezer().TopPreviewsFor(context.Background(), "Imagine Dragons", known, 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, tr := range tracks {
		if tr.Title == "Bones" {
			t.Fatalf("mismatched album track was listed: %+v", tracks)
		}
		if tr.Album == "" {
			t.Fatalf("unverifiable track (no album) was listed: %+v", tracks)
		}
	}
	if len(tracks) != 3 {
		t.Fatalf("want the 3 discography-backed tracks, got %d: %+v", len(tracks), tracks)
	}
	// "Origins (Deluxe)" must still match the discography's "Origins".
	if tracks[1].Title != "Natural" {
		t.Fatalf("deluxe-suffixed album should still match: %+v", tracks)
	}
}

// If the discography is present but nothing overlaps, return nothing.
//
// This test used to assert the opposite — "fall back to the unfiltered list
// rather than showing an empty Top Tracks section" — and that fallback is
// what shipped an unrelated artist's single track as RED's top tracks. It
// defeated the filter in precisely the case the filter existed for. An empty
// section is the honest answer; preview.go can then fall through to data
// keyed by MBID, which cannot name-collide.
func TestDeezerTopPreviewsForNoOverlapReturnsNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/artist":
			w.Write([]byte(`{"data":[{"id":9,"name":"Obscure Band"}]}`))
		case "/artist/9/top":
			w.Write([]byte(`{"data":[{"title":"Some Song","preview":"http://x/s.mp3","duration":200,"album":{"title":"Unlisted Release"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	os.Setenv("MUSICSEER_DEEZER_BASE", srv.URL)
	defer os.Unsetenv("MUSICSEER_DEEZER_BASE")

	tracks, err := NewDeezer().TopPreviewsFor(context.Background(), "Obscure Band", []string{"Something Else"}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 0 {
		t.Fatalf("uncorroborated tracks must not be published, got %+v", tracks)
	}
}

// The limit must be honoured after filtering, not before (we over-fetch so the
// list stays full once mismatches are dropped).
func TestDeezerTopPreviewsForRespectsLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/artist":
			w.Write([]byte(`{"data":[{"id":3,"name":"Band"}]}`))
		case "/artist/3/top":
			if got := r.URL.Query().Get("limit"); got != "20" {
				t.Errorf("expected an over-fetch of 20, got limit=%s", got)
			}
			w.Write([]byte(`{"data":[
				{"title":"A","preview":"http://x/a.mp3","album":{"title":"LP"}},
				{"title":"B","preview":"http://x/b.mp3","album":{"title":"Nope"}},
				{"title":"C","preview":"http://x/c.mp3","album":{"title":"LP"}},
				{"title":"D","preview":"http://x/d.mp3","album":{"title":"LP"}}
			]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	os.Setenv("MUSICSEER_DEEZER_BASE", srv.URL)
	defer os.Unsetenv("MUSICSEER_DEEZER_BASE")

	tracks, err := NewDeezer().TopPreviewsFor(context.Background(), "Band", []string{"LP"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracks) != 2 || tracks[0].Title != "A" || tracks[1].Title != "C" {
		t.Fatalf("want the first 2 matching tracks (A, C), got %+v", tracks)
	}
}
