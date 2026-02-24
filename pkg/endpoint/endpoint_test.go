package endpoint

import (
	"strings"
	"testing"
)

func TestInferRecordType(t *testing.T) {
	tests := []struct {
		target string
		want   string
	}{
		{"192.168.1.5", RecordTypeA},
		{"10.0.0.1", RecordTypeA},
		{"203.0.113.10", RecordTypeA},
		{"0.0.0.0", RecordTypeA},
		{"255.255.255.255", RecordTypeA},
		{"fe80::1", RecordTypeAAAA},
		{"2001:db8::1", RecordTypeAAAA},
		{"::1", RecordTypeAAAA},
		{"my-service.internal", RecordTypeCNAME},
		{"backend.example.com", RecordTypeCNAME},
		{"localhost", RecordTypeCNAME},
		{"", RecordTypeCNAME},
		{"not-an-ip", RecordTypeCNAME},
	}

	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			got := InferRecordType(tt.target)
			if got != tt.want {
				t.Errorf("InferRecordType(%q) = %q, want %q", tt.target, got, tt.want)
			}
		})
	}
}

func TestNew(t *testing.T) {
	t.Run("basic A record with explicit TTL", func(t *testing.T) {
		ep := New("web.example.com", []string{"203.0.113.10"}, RecordTypeA, 600, nil)
		if ep.DNSName != "web.example.com" {
			t.Errorf("DNSName = %q, want %q", ep.DNSName, "web.example.com")
		}
		if len(ep.Targets) != 1 || ep.Targets[0] != "203.0.113.10" {
			t.Errorf("Targets = %v, want [203.0.113.10]", ep.Targets)
		}
		if ep.RecordType != RecordTypeA {
			t.Errorf("RecordType = %q, want A", ep.RecordType)
		}
		if ep.TTL != 600 {
			t.Errorf("TTL = %d, want 600", ep.TTL)
		}
	})

	t.Run("zero TTL defaults to DefaultTTL", func(t *testing.T) {
		ep := New("app.example.com", []string{"1.2.3.4"}, RecordTypeA, 0, nil)
		if ep.TTL != DefaultTTL {
			t.Errorf("TTL = %d, want %d", ep.TTL, DefaultTTL)
		}
	})

	t.Run("nil labels initialised to empty map", func(t *testing.T) {
		ep := New("app.example.com", []string{"1.2.3.4"}, RecordTypeA, 300, nil)
		if ep.Labels == nil {
			t.Error("Labels should not be nil")
		}
	})

	t.Run("provided labels preserved", func(t *testing.T) {
		labels := map[string]string{"owner": "test"}
		ep := New("app.example.com", []string{"1.2.3.4"}, RecordTypeA, 300, labels)
		if ep.Labels["owner"] != "test" {
			t.Errorf("Labels[owner] = %q, want %q", ep.Labels["owner"], "test")
		}
	})
}

func TestString(t *testing.T) {
	ep := New("app.example.com", []string{"1.2.3.4"}, RecordTypeA, 300, nil)
	s := ep.String()
	if !strings.Contains(s, "app.example.com") {
		t.Errorf("String() %q missing DNS name", s)
	}
	if !strings.Contains(s, "1.2.3.4") {
		t.Errorf("String() %q missing target", s)
	}
	if !strings.Contains(s, RecordTypeA) {
		t.Errorf("String() %q missing record type", s)
	}
}

func TestRecordTypeScenarios(t *testing.T) {
	// Scenario: Basic A record endpoint (IPv4 target)
	t.Run("IPv4 produces A record", func(t *testing.T) {
		rt := InferRecordType("203.0.113.10")
		if rt != RecordTypeA {
			t.Errorf("got %q, want A", rt)
		}
	})

	// Scenario: AAAA record endpoint (IPv6 target)
	t.Run("IPv6 produces AAAA record", func(t *testing.T) {
		rt := InferRecordType("2001:db8::1")
		if rt != RecordTypeAAAA {
			t.Errorf("got %q, want AAAA", rt)
		}
	})

	// Scenario: Hostname target produces CNAME
	t.Run("Hostname produces CNAME", func(t *testing.T) {
		rt := InferRecordType("backend.internal.example.com")
		if rt != RecordTypeCNAME {
			t.Errorf("got %q, want CNAME", rt)
		}
	})
}
