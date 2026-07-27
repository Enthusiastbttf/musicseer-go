package server

import (
	"testing"

	"musicseer/internal/store"
)

// The exact URL the running v2.13.5 instance served for Coldplay, Elton John
// and Eric Clapton on the Discover page.
const dzPlaceholder = "https://cdn-images.dzcdn.net/images/artist/d41d8cd98f00b204e9800998ecf8427e/1000x1000-000000-80-0-0.jpg"

func noLookup(names []string) map[string]*store.Artist { return nil }

// The reported bug: v2.13.5 cleared the artists table but "Similar to Your
// Library" is served from a precomputed JSON payload with artwork baked in,
// and the live join only touched entries whose image was empty. A silhouette
// snapshotted before the fix therefore survived it. It must be erased even
// when nothing is available to replace it — the frontend's own placeholder is
// the honest rendering.
func TestRefreshItemImagesDropsSnapshottedPlaceholder(t *testing.T) {
	items := []map[string]any{
		{"name": "Coldplay", "imageUrl": dzPlaceholder},
	}
	refreshItemImages(items, noLookup)

	if got := items[0]["imageUrl"]; got != "" {
		t.Fatalf("snapshotted placeholder must be erased, got %v", got)
	}
}

// A real photo in the payload is left exactly as it is.
func TestRefreshItemImagesKeepsRealArtwork(t *testing.T) {
	const real = "https://e-cdns-images.dzcdn.net/images/artist/abc123/500x500-000000-80-0-0.jpg"
	items := []map[string]any{
		{"name": "Bon Jovi", "imageUrl": real},
	}
	refreshItemImages(items, func(names []string) map[string]*store.Artist {
		t.Fatalf("a resolved photo must not trigger a store lookup, asked for %v", names)
		return nil
	})
	if got := items[0]["imageUrl"]; got != real {
		t.Fatalf("real artwork must survive, got %v", got)
	}
}

// A placeholder is replaced by whatever the artists table has resolved since
// the payload was computed.
func TestRefreshItemImagesJoinsResolvedPhoto(t *testing.T) {
	const resolved = "https://www.theaudiodb.com/images/media/artist/thumb/coldplay.jpg"
	items := []map[string]any{
		{"name": "Coldplay", "imageUrl": dzPlaceholder},
		{"name": "Elton John", "imageUrl": ""},
	}
	var asked []string
	refreshItemImages(items, func(names []string) map[string]*store.Artist {
		asked = names
		return map[string]*store.Artist{"coldplay": {Name: "Coldplay", ImageURL: resolved}}
	})

	if len(asked) != 2 {
		t.Fatalf("both the placeholder and the empty entry need a lookup, asked %v", asked)
	}
	if got := items[0]["imageUrl"]; got != resolved {
		t.Fatalf("want the freshly resolved photo, got %v", got)
	}
	if got := items[1]["imageUrl"]; got != "" {
		t.Fatalf("an unresolved artist stays blank, got %v", got)
	}
}

// The store itself may still hold a placeholder on an instance whose migration
// has not run yet. Joining that in would reintroduce the silhouette.
func TestRefreshItemImagesRejectsPlaceholderFromStore(t *testing.T) {
	items := []map[string]any{
		{"name": "Elton John", "imageUrl": ""},
	}
	refreshItemImages(items, func(names []string) map[string]*store.Artist {
		return map[string]*store.Artist{"elton john": {Name: "Elton John", ImageURL: dzPlaceholder}}
	})
	if got := items[0]["imageUrl"]; got != "" {
		t.Fatalf("a placeholder from the store must not be joined in, got %v", got)
	}
}
