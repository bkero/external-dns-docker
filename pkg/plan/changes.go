// Package plan holds the diff engine and the Changes type it produces.
package plan

import "github.com/bkero/external-dns-docker/pkg/endpoint"

// Changes holds the sets of DNS record operations to apply in a single
// reconciliation cycle.
type Changes struct {
	// Create contains endpoints that should be created.
	Create []*endpoint.Endpoint
	// UpdateOld contains the current (old) state of endpoints to be updated.
	UpdateOld []*endpoint.Endpoint
	// UpdateNew contains the desired (new) state of endpoints to be updated.
	// Parallel slice with UpdateOld: UpdateOld[i] is replaced by UpdateNew[i].
	UpdateNew []*endpoint.Endpoint
	// Delete contains endpoints that should be deleted.
	Delete []*endpoint.Endpoint
}

// IsEmpty reports whether the change set has no operations.
func (c *Changes) IsEmpty() bool {
	return len(c.Create) == 0 &&
		len(c.UpdateOld) == 0 &&
		len(c.UpdateNew) == 0 &&
		len(c.Delete) == 0
}
