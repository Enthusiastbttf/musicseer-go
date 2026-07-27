package server

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Security review item 13. Handlers used to pass err.Error() straight to the
// client, so a caller saw SQLite column names, the database path, or decrypt
// failure text. Harmless while the only account was the operator; not once
// family members have Plex logins.
func TestServerErrorDoesNotLeakInternals(t *testing.T) {
	var logbuf bytes.Buffer
	s := &Server{log: slog.New(slog.NewTextHandler(&logbuf, nil))}

	const secret = "no such column: users.plex_token; database is /var/lib/musicseer/musicseer.db"
	rec := httptest.NewRecorder()
	s.serverError(rec, httptest.NewRequest(http.MethodGet, "/api/requests", nil), "requests list", errors.New(secret))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "no such column") || strings.Contains(body, "musicseer.db") {
		t.Fatalf("internal error text reached the client: %s", body)
	}
	if !strings.Contains(body, "internal error") {
		t.Fatalf("client should still get a generic error, got: %s", body)
	}
}

// Suppressing the detail is only acceptable because it is still recorded
// server-side — otherwise this trades an information leak for an undebuggable
// box, which on a headless LXC is the worse deal.
func TestServerErrorStillLogsTheDetail(t *testing.T) {
	var logbuf bytes.Buffer
	s := &Server{log: slog.New(slog.NewTextHandler(&logbuf, nil))}

	const secret = "disk I/O error"
	s.serverError(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/api/requests", nil), "requests list", errors.New(secret))

	logged := logbuf.String()
	for _, want := range []string{secret, "requests list", "/api/requests"} {
		if !strings.Contains(logged, want) {
			t.Errorf("server log is missing %q; got: %s", want, logged)
		}
	}
}
