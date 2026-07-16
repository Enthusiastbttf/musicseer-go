package clients

import (
	"strings"
	"testing"
)

func TestSanitizeURL(t *testing.T) {
	cases := []struct{ in, mustNotContain, mustContain string }{
		{"https://ws.audioscrobbler.com/2.0/?method=chart&api_key=SECRETKEY123&format=json", "SECRETKEY123", "REDACTED"},
		{"https://navidrome.local/rest/ping?u=admin&t=deadbeef&s=abc123&v=1.16.1", "deadbeef", "REDACTED"},
		{"https://api.deezer.com/search?q=radiohead", "REDACTED", "radiohead"},
	}
	for _, c := range cases {
		got := sanitizeURL(c.in)
		if strings.Contains(got, c.mustNotContain) {
			t.Errorf("sanitizeURL(%q)=%q still leaks %q", c.in, got, c.mustNotContain)
		}
		if !strings.Contains(got, c.mustContain) {
			t.Errorf("sanitizeURL(%q)=%q missing %q", c.in, got, c.mustContain)
		}
	}
}

func TestReadCappedTruncation(t *testing.T) {
	_, err := readCapped(strings.NewReader(strings.Repeat("x", 100)), 50)
	if err == nil {
		t.Fatal("expected error when body exceeds cap, got nil")
	}
	data, err := readCapped(strings.NewReader("hello"), 50)
	if err != nil || string(data) != "hello" {
		t.Fatalf("small body: got %q err %v", data, err)
	}
}
