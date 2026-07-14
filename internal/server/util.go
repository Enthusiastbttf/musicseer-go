package server

import (
	"context"
	"net/http"
	"strings"
)

// membership evaluates identity-aware "in library"/"requested" checks:
// MBID match when the candidate has one, name match only against entries
// that never had an MBID to match on.
type membership struct {
	mbids, namesNoMbid map[string]bool
}

func (m membership) has(name, mbid string) bool {
	if mbid != "" && m.mbids[strings.ToLower(mbid)] {
		return true
	}
	return m.namesNoMbid[strings.ToLower(name)]
}

// contextWithoutCancel detaches background work from the request lifecycle.
func contextWithoutCancel(r *http.Request) context.Context {
	return context.WithoutCancel(r.Context())
}
