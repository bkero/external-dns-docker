package rfc2136

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"testing"

	"github.com/miekg/dns"

	"github.com/bkero/external-dns-docker/pkg/endpoint"
	"github.com/bkero/external-dns-docker/pkg/plan"
)

// --- helpers for multi-zone tests ---

// newMultiWithDeps builds a MultiProvider whose sub-providers use injected
// mock transferrer/exchanger, bypassing real DNS.
func newMultiWithDeps(configs []ZoneConfig, t dnsTransferer, e dnsExchanger) *MultiProvider {
	entries := make([]zoneEntry, 0, len(configs))
	for _, zc := range configs {
		cfg := Config{
			Host:          zc.Host,
			Port:          zc.Port,
			Zone:          zc.Zone,
			TSIGKeyName:   zc.TSIGKey,
			TSIGSecret:    zc.TSIGSecret,
			TSIGSecretAlg: zc.TSIGAlg,
			MinTTL:        zc.MinTTL,
			Timeout:       zc.Timeout,
		}
		entries = append(entries, zoneEntry{
			zone: dns.Fqdn(zc.Zone),
			prov: newWithDeps(cfg, nil, t, e),
		})
	}
	return &MultiProvider{zones: entries, log: slog.Default()}
}

func twoZoneConfigs() []ZoneConfig {
	return []ZoneConfig{
		{Host: "ns1.example.com", Port: 53, Zone: "example.com", TSIGKey: "k1", TSIGSecret: "s1", TSIGAlg: "hmac-sha256"},
		{Host: "ns2.bke.ro", Port: 53, Zone: "bke.ro", TSIGKey: "k2", TSIGSecret: "s2", TSIGAlg: "hmac-sha256"},
	}
}

// --- zoneFor tests ---

func TestZoneFor_ExactMatch(t *testing.T) {
	m := newMultiWithDeps(twoZoneConfigs(), nil, nil)
	ze := m.zoneFor("example.com")
	if ze == nil {
		t.Fatal("expected match for exact zone name, got nil")
	}
	if ze.zone != "example.com." {
		t.Errorf("zone = %q, want example.com.", ze.zone)
	}
}

func TestZoneFor_SubdomainMatch(t *testing.T) {
	m := newMultiWithDeps(twoZoneConfigs(), nil, nil)
	ze := m.zoneFor("app.example.com")
	if ze == nil {
		t.Fatal("expected match for subdomain, got nil")
	}
	if ze.zone != "example.com." {
		t.Errorf("zone = %q, want example.com.", ze.zone)
	}
}

func TestZoneFor_LongestSuffixWins(t *testing.T) {
	// Add a sub-zone to ensure longest match wins.
	configs := []ZoneConfig{
		{Host: "ns1.example.com", Zone: "example.com"},
		{Host: "ns2.sub.example.com", Zone: "sub.example.com"},
	}
	m := newMultiWithDeps(configs, nil, nil)
	ze := m.zoneFor("app.sub.example.com")
	if ze == nil {
		t.Fatal("expected match, got nil")
	}
	if ze.zone != "sub.example.com." {
		t.Errorf("zone = %q, want sub.example.com. (longest match)", ze.zone)
	}
}

func TestZoneFor_NoMatch_ReturnsNil(t *testing.T) {
	m := newMultiWithDeps(twoZoneConfigs(), nil, nil)
	if ze := m.zoneFor("unknown.other.tld"); ze != nil {
		t.Errorf("expected nil for unmatched name, got zone %q", ze.zone)
	}
}

func TestZoneFor_TrailingDotHandled(t *testing.T) {
	m := newMultiWithDeps(twoZoneConfigs(), nil, nil)
	// DNSName with trailing dot should still match.
	ze := m.zoneFor("app.example.com.")
	if ze == nil {
		t.Fatal("expected match for name with trailing dot, got nil")
	}
	if ze.zone != "example.com." {
		t.Errorf("zone = %q, want example.com.", ze.zone)
	}
}

func TestZoneFor_SecondZoneMatches(t *testing.T) {
	m := newMultiWithDeps(twoZoneConfigs(), nil, nil)
	ze := m.zoneFor("api.bke.ro")
	if ze == nil {
		t.Fatal("expected match for bke.ro subdomain, got nil")
	}
	if ze.zone != "bke.ro." {
		t.Errorf("zone = %q, want bke.ro.", ze.zone)
	}
}

// --- Records tests ---

func TestMultiRecords_MergesResultsFromTwoZones(t *testing.T) {
	rrA := &dns.A{
		Hdr: dns.RR_Header{Name: "app.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
		A:   net.ParseIP("1.2.3.4"),
	}
	rrB := &dns.A{
		Hdr: dns.RR_Header{Name: "api.bke.ro.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
		A:   net.ParseIP("5.6.7.8"),
	}
	// Both zones use the same mock transferrer (returns both RRs regardless of zone).
	// We use two separate mockTransferer instances to simulate per-zone responses.
	mt := &mockTransferer{envelopes: []*dns.Envelope{{RR: []dns.RR{rrA, rrB}}}}
	m := newMultiWithDeps(twoZoneConfigs(), mt, nil)

	eps, err := m.Records(context.Background())
	if err != nil {
		t.Fatalf("Records() error = %v", err)
	}
	if len(eps) != 4 { // 2 records × 2 zones (same mock)
		t.Fatalf("got %d endpoints, want 4 (2 per zone)", len(eps))
	}
}

func TestMultiRecords_OneZoneError_ReturnsError(t *testing.T) {
	mt := &mockTransferer{err: fmt.Errorf("axfr failure")}
	m := newMultiWithDeps(twoZoneConfigs(), mt, nil)

	_, err := m.Records(context.Background())
	if err == nil {
		t.Error("expected error when a zone AXFR fails, got nil")
	}
}

func TestMultiRecords_EmptyZones_ReturnsNil(t *testing.T) {
	mt := &mockTransferer{envelopes: []*dns.Envelope{}}
	m := newMultiWithDeps(twoZoneConfigs(), mt, nil)

	eps, err := m.Records(context.Background())
	if err != nil {
		t.Fatalf("Records() error = %v", err)
	}
	if len(eps) != 0 {
		t.Errorf("got %d endpoints, want 0", len(eps))
	}
}

// --- ApplyChanges tests ---

func TestMultiApplyChanges_CreateRoutedToCorrectZone(t *testing.T) {
	// Track which provider was called using separate exchangers per zone.
	exA := &mockExchanger{resp: successResp()}
	exB := &mockExchanger{resp: successResp()}

	configs := twoZoneConfigs()
	m := &MultiProvider{log: slog.Default()}
	m.zones = []zoneEntry{
		{zone: dns.Fqdn(configs[0].Zone), prov: newWithDeps(Config{Host: configs[0].Host, Port: configs[0].Port, Zone: configs[0].Zone, TSIGKeyName: configs[0].TSIGKey, TSIGSecret: configs[0].TSIGSecret}, nil, nil, exA)},
		{zone: dns.Fqdn(configs[1].Zone), prov: newWithDeps(Config{Host: configs[1].Host, Port: configs[1].Port, Zone: configs[1].Zone, TSIGKeyName: configs[1].TSIGKey, TSIGSecret: configs[1].TSIGSecret}, nil, nil, exB)},
	}

	err := m.ApplyChanges(context.Background(), &plan.Changes{
		Create: []*endpoint.Endpoint{
			endpoint.New("app.example.com", []string{"1.2.3.4"}, endpoint.RecordTypeA, 300, nil),
		},
	})
	if err != nil {
		t.Fatalf("ApplyChanges() error = %v", err)
	}
	if exA.sent == nil {
		t.Error("expected example.com provider to be called")
	}
	if exB.sent != nil {
		t.Error("expected bke.ro provider NOT to be called for example.com endpoint")
	}
}

func TestMultiApplyChanges_UnmatchedEndpointSkipped(t *testing.T) {
	exA := &mockExchanger{resp: successResp()}
	exB := &mockExchanger{resp: successResp()}

	configs := twoZoneConfigs()
	m := &MultiProvider{log: slog.Default()}
	m.zones = []zoneEntry{
		{zone: dns.Fqdn(configs[0].Zone), prov: newWithDeps(Config{Host: configs[0].Host, Zone: configs[0].Zone}, nil, nil, exA)},
		{zone: dns.Fqdn(configs[1].Zone), prov: newWithDeps(Config{Host: configs[1].Host, Zone: configs[1].Zone}, nil, nil, exB)},
	}

	// Endpoint in an unmanaged zone — should be skipped, not cause an error.
	err := m.ApplyChanges(context.Background(), &plan.Changes{
		Create: []*endpoint.Endpoint{
			endpoint.New("app.unknown.tld", []string{"1.2.3.4"}, endpoint.RecordTypeA, 300, nil),
		},
	})
	if err != nil {
		t.Fatalf("ApplyChanges() error = %v (unmatched endpoint should be skipped)", err)
	}
	if exA.sent != nil || exB.sent != nil {
		t.Error("expected neither provider to be called for unmatched endpoint")
	}
}

func TestMultiApplyChanges_ZoneWithNoChangesNotCalled(t *testing.T) {
	exA := &mockExchanger{resp: successResp()}
	exB := &mockExchanger{resp: successResp()}

	configs := twoZoneConfigs()
	m := &MultiProvider{log: slog.Default()}
	m.zones = []zoneEntry{
		{zone: dns.Fqdn(configs[0].Zone), prov: newWithDeps(Config{Host: configs[0].Host, Zone: configs[0].Zone}, nil, nil, exA)},
		{zone: dns.Fqdn(configs[1].Zone), prov: newWithDeps(Config{Host: configs[1].Host, Zone: configs[1].Zone}, nil, nil, exB)},
	}

	// Only example.com gets a change.
	err := m.ApplyChanges(context.Background(), &plan.Changes{
		Create: []*endpoint.Endpoint{
			endpoint.New("app.example.com", []string{"1.2.3.4"}, endpoint.RecordTypeA, 300, nil),
		},
	})
	if err != nil {
		t.Fatalf("ApplyChanges() error = %v", err)
	}
	if exA.sent == nil {
		t.Error("expected example.com provider to be called")
	}
	if exB.sent != nil {
		t.Error("expected bke.ro provider NOT to be called when it has no changes")
	}
}

func TestMultiApplyChanges_DeleteRoutedToCorrectZone(t *testing.T) {
	exA := &mockExchanger{resp: successResp()}
	exB := &mockExchanger{resp: successResp()}

	configs := twoZoneConfigs()
	m := &MultiProvider{log: slog.Default()}
	m.zones = []zoneEntry{
		{zone: dns.Fqdn(configs[0].Zone), prov: newWithDeps(Config{Host: configs[0].Host, Zone: configs[0].Zone, TSIGKeyName: configs[0].TSIGKey, TSIGSecret: configs[0].TSIGSecret}, nil, nil, exA)},
		{zone: dns.Fqdn(configs[1].Zone), prov: newWithDeps(Config{Host: configs[1].Host, Zone: configs[1].Zone, TSIGKeyName: configs[1].TSIGKey, TSIGSecret: configs[1].TSIGSecret}, nil, nil, exB)},
	}

	err := m.ApplyChanges(context.Background(), &plan.Changes{
		Delete: []*endpoint.Endpoint{
			endpoint.New("old.bke.ro", []string{"9.9.9.9"}, endpoint.RecordTypeA, 300, nil),
		},
	})
	if err != nil {
		t.Fatalf("ApplyChanges() error = %v", err)
	}
	if exB.sent == nil {
		t.Error("expected bke.ro provider to be called for bke.ro endpoint")
	}
	if exA.sent != nil {
		t.Error("expected example.com provider NOT to be called for bke.ro delete")
	}
}

// --- Preflight tests ---

func TestMultiPreflight_AllSuccess(t *testing.T) {
	me := &mockExchanger{resp: successResp()}
	m := newMultiWithDeps(twoZoneConfigs(), nil, me)

	if err := m.Preflight(context.Background()); err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
}

func TestMultiPreflight_FirstZoneFailure_ReturnsError(t *testing.T) {
	me := &mockExchanger{err: fmt.Errorf("connection refused")}
	m := newMultiWithDeps(twoZoneConfigs(), nil, me)

	err := m.Preflight(context.Background())
	if err == nil {
		t.Error("expected error from first-zone preflight failure, got nil")
	}
}

func TestMultiPreflight_NonSuccessRcode_ReturnsError(t *testing.T) {
	resp := new(dns.Msg)
	resp.Rcode = dns.RcodeNotAuth
	me := &mockExchanger{resp: resp}
	m := newMultiWithDeps(twoZoneConfigs(), nil, me)

	err := m.Preflight(context.Background())
	if err == nil {
		t.Error("expected error for NOTAUTH preflight rcode, got nil")
	}
}

// --- NewMulti construction tests ---

func TestNewMulti_NilLog_UsesDefault(t *testing.T) {
	m := NewMulti(twoZoneConfigs(), nil)
	if m.log == nil {
		t.Error("expected non-nil logger")
	}
}

func TestNewMulti_ZonesNormalised(t *testing.T) {
	// Zone without trailing dot should be normalised.
	configs := []ZoneConfig{{Host: "ns1.example.com", Zone: "example.com"}}
	m := NewMulti(configs, nil)
	if len(m.zones) != 1 {
		t.Fatalf("got %d zones, want 1", len(m.zones))
	}
	if m.zones[0].zone != "example.com." {
		t.Errorf("zone = %q, want example.com. (trailing dot added)", m.zones[0].zone)
	}
}
