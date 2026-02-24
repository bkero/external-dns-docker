// Package source defines the Source interface for discovering desired DNS endpoints.
package source

import (
	"context"

	"github.com/bkero/external-dns-docker/pkg/endpoint"
)

// Source discovers desired DNS endpoints from an external system (e.g. Docker).
type Source interface {
	// Endpoints returns the current set of desired DNS endpoints.
	Endpoints(ctx context.Context) ([]*endpoint.Endpoint, error)

	// AddEventHandler registers a function to be called when the source detects
	// a change (e.g. a container start or stop). The handler should trigger a
	// reconciliation. It may be called from a background goroutine.
	AddEventHandler(ctx context.Context, handler func())
}
