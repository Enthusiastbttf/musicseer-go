package server

import "testing"

func TestVersionLess(t *testing.T) {
	cases := []struct {
		cur, lat string
		want     bool
	}{
		{"2.9.2", "2.10.0", true},
		{"2.10.0", "2.9.2", false},
		{"2.10.0", "2.10.0", false},
		{"2.9.2", "2.9.10", true},
		{"dev", "2.9.2", true},  // dev build -> any release is newer
		{"2.9.2", "dev", false}, // never "downgrade" to a non-version
		{"v2.9.1", "v2.9.2", true},
		{"2.9.2-rc1", "2.9.2", false}, // suffix stripped -> equal
	}
	for _, c := range cases {
		if got := versionLess(c.cur, c.lat); got != c.want {
			t.Errorf("versionLess(%q,%q)=%v want %v", c.cur, c.lat, got, c.want)
		}
	}
}
