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
	if v != 1 {
		t.Fatalf("want user_version 1 after migrate, got %d", v)
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
