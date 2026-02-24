// Package fake provides an in-memory Provider implementation for testing.
package fake

import (
	"context"
	"sync"

	"github.com/bkero/external-dns-docker/pkg/endpoint"
	"github.com/bkero/external-dns-docker/pkg/plan"
)

// ChangeRecord is a snapshot of a single ApplyChanges call, kept for
// test assertions.
type ChangeRecord struct {
	Create    []*endpoint.Endpoint
	UpdateOld []*endpoint.Endpoint
	UpdateNew []*endpoint.Endpoint
	Delete    []*endpoint.Endpoint
}

// Provider is an in-memory DNS provider for testing.
type Provider struct {
	mu      sync.Mutex
	records map[string]*endpoint.Endpoint // keyed by DNSName+RecordType
	history []ChangeRecord
}

// New returns a Provider pre-loaded with the given endpoints.
func New(initial []*endpoint.Endpoint) *Provider {
	p := &Provider{records: make(map[string]*endpoint.Endpoint)}
	for _, ep := range initial {
		p.records[key(ep)] = ep
	}
	return p
}

// Records returns all currently stored endpoints.
func (p *Provider) Records(_ context.Context) ([]*endpoint.Endpoint, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*endpoint.Endpoint, 0, len(p.records))
	for _, ep := range p.records {
		out = append(out, ep)
	}
	return out, nil
}

// ApplyChanges applies creates, updates, and deletes to the in-memory store
// and appends a ChangeRecord to the history for later inspection.
func (p *Provider) ApplyChanges(_ context.Context, changes *plan.Changes) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, ep := range changes.Create {
		p.records[key(ep)] = ep
	}
	for i, old := range changes.UpdateOld {
		delete(p.records, key(old))
		if i < len(changes.UpdateNew) {
			p.records[key(changes.UpdateNew[i])] = changes.UpdateNew[i]
		}
	}
	for _, ep := range changes.Delete {
		delete(p.records, key(ep))
	}

	p.history = append(p.history, ChangeRecord{
		Create:    changes.Create,
		UpdateOld: changes.UpdateOld,
		UpdateNew: changes.UpdateNew,
		Delete:    changes.Delete,
	})
	return nil
}

// History returns all ApplyChanges calls made so far, oldest first.
func (p *Provider) History() []ChangeRecord {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]ChangeRecord, len(p.history))
	copy(out, p.history)
	return out
}

// RecordCount returns the number of endpoints currently in the store.
func (p *Provider) RecordCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.records)
}

func key(ep *endpoint.Endpoint) string {
	return ep.DNSName + "|" + ep.RecordType
}
