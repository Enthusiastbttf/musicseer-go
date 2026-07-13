package server

import (
	"context"
	"net/http"
)

// contextWithoutCancel detaches background work from the request lifecycle.
func contextWithoutCancel(r *http.Request) context.Context {
	return context.WithoutCancel(r.Context())
}
