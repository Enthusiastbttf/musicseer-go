package store

import "testing"

// v2.12.3 purges cached artist detail once, because discographies fetched by
// earlier versions were truncated at 100 release groups. It must run exactly
// once — not on every restart, which would re-fetch every artist page forever.
func TestArtistDetailPurgeRunsOnce(t *testing.T) {
	dir := t.TempDir()

	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	var v int
	if err := s.DB.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != schemaVersion {
		t.Fatalf("want user_version %d after migrate, got %d", schemaVersion, v)
	}

	// Simulate a truncated cache entry written by the new version, then reopen:
	// it must survive, because the purge has already been recorded.
	if err := s.SaveArtistDetail("mbid-1", "Imagine Dragons", map[string]any{"albums": []any{}}); err != nil {
		t.Fatal(err)
	}
	s.DB.Close()

	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.DB.Close()
	var n int
	if err := s2.DB.QueryRow("SELECT COUNT(*) FROM artist_detail").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("cache should survive a restart once purged, got %d rows", n)
	}
}

// A database from an older version (user_version 0) with stale, truncated
// discographies gets them cleared on upgrade.
func TestArtistDetailPurgeOnUpgrade(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveArtistDetail("mbid-1", "Imagine Dragons", map[string]any{"albums": []any{}}); err != nil {
		t.Fatal(err)
	}
	// Roll the marker back to look like a pre-2.12.3 database.
	if _, err := s.DB.Exec("PRAGMA user_version = 0"); err != nil {
		t.Fatal(err)
	}
	s.DB.Close()

	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.DB.Close()
	var n int
	if err := s2.DB.QueryRow("SELECT COUNT(*) FROM artist_detail").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("stale truncated discography should have been purged, got %d rows", n)
	}
}

// v2.13.3: photos resolved before v2.13.2 came from an unverified Deezer name
// search, so a row could hold a different band's face. The backfill only
// selects artists with no image AND no check timestamp, so both columns have
// to be cleared — clearing image_url alone would leave the row permanently
// ineligible, turning a wrong photo into a permanent blank.
func TestArtistImagePurgeOnUpgrade(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetArtistImage("Red", "http://deezer/wrong-band.jpg"); err != nil {
		t.Fatal(err)
	}
	// Look like a pre-2.13.3 database that has already taken the v1 purge.
	if _, err := s.DB.Exec("PRAGMA user_version = 1"); err != nil {
		t.Fatal(err)
	}
	s.DB.Close()

	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.DB.Close()

	var url, checked any
	if err := s2.DB.QueryRow("SELECT image_url, image_checked_at FROM artists WHERE name='Red'").Scan(&url, &checked); err != nil {
		t.Fatal(err)
	}
	if url != nil || checked != nil {
		t.Fatalf("stale photo should be cleared for re-fetch, got url=%v checked=%v", url, checked)
	}

	// The row must now be visible to the backfill worker again.
	missing, err := s2.ArtistsMissingImages(10)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, a := range missing {
		if a.Name == "Red" {
			found = true
		}
	}
	if !found {
		t.Fatalf("purged artist must be eligible for re-fetch, got %+v", missing)
	}
}

// The image purge must not re-run on every restart, or every artist photo
// would be dropped and re-fetched forever.
func TestArtistImagePurgeRunsOnce(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetArtistImage("Red", "http://verified/red.jpg"); err != nil {
		t.Fatal(err)
	}
	s.DB.Close()

	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.DB.Close()
	var url string
	if err := s2.DB.QueryRow("SELECT COALESCE(image_url,'') FROM artists WHERE name='Red'").Scan(&url); err != nil {
		t.Fatal(err)
	}
	if url != "http://verified/red.jpg" {
		t.Fatalf("a photo written after the purge must survive a restart, got %q", url)
	}
}

// v2.13.4: Deezer's grey-silhouette default was stored as a real photo, so
// the artist never fell through to TheAudioDB. Those rows are cleared, and
// only those — a wholesale re-purge would restart the v2 backfill.
func TestDeezerPlaceholderPhotoPurge(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	const placeholder = "https://e-cdns-images.dzcdn.net/images/artist//1000x1000-000000-80-0-0.jpg"
	if err := s.SetArtistImage("Coldplay", placeholder); err != nil {
		t.Fatal(err)
	}
	// v2.13.5: the shape the running instance actually held. The v3 purge
	// above cannot match it — there is no empty path segment.
	const emptyHash = "https://cdn-images.dzcdn.net/images/artist/d41d8cd98f00b204e9800998ecf8427e/1000x1000-000000-80-0-0.jpg"
	if err := s.SetArtistImage("Elton John", emptyHash); err != nil {
		t.Fatal(err)
	}
	if err := s.SetArtistImage("RED", "https://e-cdns-images.dzcdn.net/images/artist/real/500x500.jpg"); err != nil {
		t.Fatal(err)
	}
	// Look like a database that has taken the v2 purge but not this one.
	if _, err := s.DB.Exec("PRAGMA user_version = 2"); err != nil {
		t.Fatal(err)
	}
	s.DB.Close()

	s2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.DB.Close()

	for _, name := range []string{"Coldplay", "Elton John"} {
		var url, checked any
		if err := s2.DB.QueryRow("SELECT image_url, image_checked_at FROM artists WHERE name=?", name).Scan(&url, &checked); err != nil {
			t.Fatal(err)
		}
		if url != nil || checked != nil {
			t.Fatalf("%s silhouette row should be cleared, got url=%v checked=%v", name, url, checked)
		}
		// Clearing image_url alone would leave the row invisible to the
		// backfill, turning a silhouette into a permanent blank.
		missing, err := s2.ArtistsMissingImages(10)
		if err != nil {
			t.Fatal(err)
		}
		var found bool
		for _, a := range missing {
			if a.Name == name {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s must be eligible for re-fetch after the purge", name)
		}
	}

	// A real photo alongside it must survive: this purge is targeted.
	var kept string
	if err := s2.DB.QueryRow("SELECT COALESCE(image_url,'') FROM artists WHERE name='RED'").Scan(&kept); err != nil {
		t.Fatal(err)
	}
	if kept != "https://e-cdns-images.dzcdn.net/images/artist/real/500x500.jpg" {
		t.Fatalf("real photo must not be purged, got %q", kept)
	}
}
