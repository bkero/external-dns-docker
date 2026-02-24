// Package provider defines the Provider interface for DNS backends.
package provider

import (
	"context"

	"github.com/bkero/external-dns-docker/pkg/endpoint"
	"github.com/bkero/external-dns-docker/pkg/plan"
)

// Provider is implemented by every DNS backend.
type Provider interface {
	// Records returns the current set of DNS endpoints in the managed zone.
	Records(ctx context.Context) ([]*endpoint.Endpoint, error)

	// ApplyChanges applies the given set of create, update, and delete
	// operations to the DNS backend.
	ApplyChanges(ctx context.Context, changes *plan.Changes) error
}
