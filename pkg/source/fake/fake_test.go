package fake

import (
	"context"
	"testing"

	"github.com/bkero/external-dns-docker/pkg/endpoint"
)

func TestFakeSource_Endpoints(t *testing.T) {
	eps := []*endpoint.Endpoint{
		endpoint.New("a.example.com", []string{"1.2.3.4"}, endpoint.RecordTypeA, 300, nil),
		endpoint.New("b.example.com", []string{"5.6.7.8"}, endpoint.RecordTypeA, 300, nil),
	}
	s := New(eps)

	got, err := s.Endpoints(context.Background())
	if err != nil {
		t.Fatalf("Endpoints() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Endpoints() returned %d endpoints, want 2", len(got))
	}
	if got[0].DNSName != "a.example.com" {
		t.Errorf("got[0].DNSName = %q, want %q", got[0].DNSName, "a.example.com")
	}
	if got[1].DNSName != "b.example.com" {
		t.Errorf("got[1].DNSName = %q, want %q", got[1].DNSName, "b.example.com")
	}
}

func TestFakeSource_EmptyEndpoints(t *testing.T) {
	s := New(nil)
	got, err := s.Endpoints(context.Background())
	if err != nil {
		t.Fatalf("Endpoints() error = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Endpoints() returned %d endpoints, want 0", len(got))
	}
}

func TestFakeSource_TriggerEvent(t *testing.T) {
	s := New(nil)

	called := 0
	s.AddEventHandler(context.Background(), func() { called++ })
	s.AddEventHandler(context.Background(), func() { called++ })

	s.TriggerEvent()

	if called != 2 {
		t.Errorf("TriggerEvent() called handlers %d times, want 2", called)
	}
}

func TestFakeSource_SetEndpoints(t *testing.T) {
	s := New(nil)

	got, _ := s.Endpoints(context.Background())
	if len(got) != 0 {
		t.Errorf("initial Endpoints() returned %d, want 0", len(got))
	}

	eps := []*endpoint.Endpoint{
		endpoint.New("new.example.com", []string{"9.9.9.9"}, endpoint.RecordTypeA, 300, nil),
	}
	s.SetEndpoints(eps)

	got, _ = s.Endpoints(context.Background())
	if len(got) != 1 || got[0].DNSName != "new.example.com" {
		t.Errorf("after SetEndpoints, got %v, want [new.example.com]", got)
	}
}

func TestFakeSource_EndpointsReturnsCopy(t *testing.T) {
	eps := []*endpoint.Endpoint{
		endpoint.New("a.example.com", []string{"1.2.3.4"}, endpoint.RecordTypeA, 300, nil),
	}
	s := New(eps)

	got, _ := s.Endpoints(context.Background())
	// Mutating the returned slice should not affect subsequent calls.
	got[0] = endpoint.New("mutated.example.com", []string{"0.0.0.0"}, endpoint.RecordTypeA, 300, nil)

	got2, _ := s.Endpoints(context.Background())
	if got2[0].DNSName != "a.example.com" {
		t.Errorf("mutation leaked: got2[0].DNSName = %q, want a.example.com", got2[0].DNSName)
	}
}
