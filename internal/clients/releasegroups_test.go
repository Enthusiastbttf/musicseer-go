package clients

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
)

// A prolific artist has more than one page of release groups. The old
// single-page fetch capped the discography at 100, which silently hid releases
// (e.g. Imagine Dragons' "Bones" single) and broke top-track album matching.
func TestReleaseGroupsPaginates(t *testing.T) {
	const total = 237
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ws/2/release-group" {
			http.NotFound(w, r)
			return
		}
		calls++
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		n := total - offset
		if n > 100 {
			n = 100
		}
		if n < 0 {
			n = 0
		}
		items := make([]string, 0, n)
		for i := 0; i < n; i++ {
			items = append(items, fmt.Sprintf(
				`{"id":"rg-%d","title":"Release %d","primary-type":"Single","first-release-date":"2022-01-01"}`,
				offset+i, offset+i))
		}
		fmt.Fprintf(w, `{"release-group-count":%d,"release-groups":[%s]}`, total, strings.Join(items, ","))
	}))
	defer srv.Close()
	os.Setenv("MUSICSEER_MB_BASE", srv.URL)
	defer os.Unsetenv("MUSICSEER_MB_BASE")

	groups, err := NewMusicBrainz("test@example.com").ReleaseGroups(context.Background(), "some-mbid")
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != total {
		t.Fatalf("want all %d release groups, got %d", total, len(groups))
	}
	if calls != 3 {
		t.Fatalf("want 3 paged calls, got %d", calls)
	}
	if groups[0].Title != "Release 0" || groups[total-1].Title != fmt.Sprintf("Release %d", total-1) {
		t.Fatalf("pagination lost ordering: first=%q last=%q", groups[0].Title, groups[total-1].Title)
	}
}

// A single short page must not trigger a second request.
func TestReleaseGroupsSinglePage(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{"release-group-count":2,"release-groups":[
			{"id":"a","title":"Night Visions","primary-type":"Album","first-release-date":"2012-09-04"},
			{"id":"b","title":"Continued Silence","primary-type":"EP","first-release-date":"2012-02-14"},
			{"id":"c","title":"Untyped","first-release-date":"2012-02-14"}
		]}`))
	}))
	defer srv.Close()
	os.Setenv("MUSICSEER_MB_BASE", srv.URL)
	defer os.Unsetenv("MUSICSEER_MB_BASE")

	groups, err := NewMusicBrainz("test@example.com").ReleaseGroups(context.Background(), "mbid")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("want 1 call, got %d", calls)
	}
	if len(groups) != 2 { // the entry with no primary type is skipped
		t.Fatalf("want 2 typed release groups, got %d: %+v", len(groups), groups)
	}
}

// A failure partway through returns what was collected instead of nothing.
func TestReleaseGroupsPartialOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("offset") != "0" {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		items := make([]string, 0, 100)
		for i := 0; i < 100; i++ {
			items = append(items, fmt.Sprintf(`{"id":"rg-%d","title":"R%d","primary-type":"Single"}`, i, i))
		}
		fmt.Fprintf(w, `{"release-group-count":200,"release-groups":[%s]}`, strings.Join(items, ","))
	}))
	defer srv.Close()
	os.Setenv("MUSICSEER_MB_BASE", srv.URL)
	defer os.Unsetenv("MUSICSEER_MB_BASE")

	groups, err := NewMusicBrainz("test@example.com").ReleaseGroups(context.Background(), "mbid")
	if err != nil {
		t.Fatalf("a mid-way failure should not fail the whole fetch: %v", err)
	}
	if len(groups) != 100 {
		t.Fatalf("want the first page preserved, got %d", len(groups))
	}
}
