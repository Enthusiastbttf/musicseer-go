package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func cspDirectives(t *testing.T, policy string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, d := range strings.Split(policy, ";") {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		name, rest, _ := strings.Cut(d, " ")
		out[name] = strings.TrimSpace(rest)
	}
	return out
}

// Security review item 14 (the CSP half). The header has to actually be
// emitted, alongside the ones that were already there.
func TestSecurityHeadersIncludeCSP(t *testing.T) {
	h := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	for name, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	} {
		if got := rec.Header().Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("no Content-Security-Policy header")
	}
}

// A CSP that permits inline or remote script buys nothing — that clause is the
// entire reason for the header. The frontend is a Vite bundle served from the
// embedded dist, so 'self' alone is sufficient and anything looser is a
// regression, not a convenience.
func TestCSPScriptSrcIsLockedDown(t *testing.T) {
	d := cspDirectives(t, contentSecurityPolicy)

	script, ok := d["script-src"]
	if !ok {
		t.Fatal("no script-src directive; default-src alone is too easy to widen by accident")
	}
	if script != "'self'" {
		t.Fatalf("script-src = %q, want exactly 'self'", script)
	}
	for _, bad := range []string{"'unsafe-inline'", "'unsafe-eval'", "*", "http:", "https:"} {
		if strings.Contains(script, bad) {
			t.Errorf("script-src must not contain %s", bad)
		}
	}
}

// The clickjacking and injection-base directives that have no downside here.
func TestCSPClosesTheCheapHoles(t *testing.T) {
	d := cspDirectives(t, contentSecurityPolicy)
	for name, want := range map[string]string{
		"object-src":      "'none'",
		"frame-ancestors": "'none'",
		"base-uri":        "'self'",
		"form-action":     "'self'",
		"default-src":     "'self'",
	} {
		if got := d[name]; got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

// Artwork and previews are deliberately allowed from any https origin, because
// the provider CDN hostnames move. Documented as intentional so a future reader
// does not "tighten" it and silently break every album cover.
func TestCSPAllowsRemoteArtworkAndPreviews(t *testing.T) {
	d := cspDirectives(t, contentSecurityPolicy)
	if !strings.Contains(d["img-src"], "https:") {
		t.Errorf("img-src = %q, but artwork comes from Deezer/TheAudioDB/CAA/Last.fm CDNs", d["img-src"])
	}
	if !strings.Contains(d["media-src"], "https:") {
		t.Errorf("media-src = %q, but 30s previews stream from Deezer", d["media-src"])
	}
}
