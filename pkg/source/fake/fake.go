// Package fake provides an in-memory Source implementation for testing.
package fake

import (
	"context"
	"sync"

	"github.com/bkero/external-dns-docker/pkg/endpoint"
)

// Source is a fake implementation of source.Source that returns a fixed list
// of endpoints and supports manually triggering event handlers.
type Source struct {
	mu        sync.Mutex
	endpoints []*endpoint.Endpoint
	handlers  []func()
}

// New returns a fake Source pre-loaded with the given endpoints.
func New(endpoints []*endpoint.Endpoint) *Source {
	return &Source{endpoints: endpoints}
}

// Endpoints returns the configured endpoint list.
func (s *Source) Endpoints(_ context.Context) ([]*endpoint.Endpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*endpoint.Endpoint, len(s.endpoints))
	copy(out, s.endpoints)
	return out, nil
}

// AddEventHandler registers a handler to be called by TriggerEvent.
func (s *Source) AddEventHandler(_ context.Context, handler func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers = append(s.handlers, handler)
}

// TriggerEvent calls all registered event handlers, simulating a Docker event.
func (s *Source) TriggerEvent() {
	s.mu.Lock()
	handlers := make([]func(), len(s.handlers))
	copy(handlers, s.handlers)
	s.mu.Unlock()

	for _, h := range handlers {
		h()
	}
}

// SetEndpoints replaces the endpoint list returned by Endpoints.
func (s *Source) SetEndpoints(endpoints []*endpoint.Endpoint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.endpoints = endpoints
}
