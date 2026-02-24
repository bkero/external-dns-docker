package rfc2136

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/bkero/external-dns-docker/pkg/endpoint"
	"github.com/bkero/external-dns-docker/pkg/plan"
)

// --- Mock helpers ---

type mockTransferer struct {
	envelopes []*dns.Envelope
	err       error // returned from In()
}

func (m *mockTransferer) In(_ *dns.Msg, _ string) (chan *dns.Envelope, error) {
	if m.err != nil {
		return nil, m.err
	}
	ch := make(chan *dns.Envelope, len(m.envelopes))
	for _, e := range m.envelopes {
		ch <- e
	}
	close(ch)
	return ch, nil
}

type mockExchanger struct {
	resp *dns.Msg
	err  error
	// Records the most-recently sent message for inspection.
	sent *dns.Msg
}

func (m *mockExchanger) Exchange(msg *dns.Msg, _ string) (*dns.Msg, time.Duration, error) {
	m.sent = msg
	if m.err != nil {
		return nil, 0, m.err
	}
	return m.resp, 0, nil
}

func successResp() *dns.Msg {
	r := new(dns.Msg)
	r.Rcode = dns.RcodeSuccess
	return r
}

func testProvider(t *mockTransferer, e *mockExchanger) *Provider {
	return newWithDeps(Config{
		Host:          "ns1.example.com",
		Port:          53,
		Zone:          "example.com",
		TSIGKeyName:   "testkey",
		TSIGSecret:    "c2VjcmV0",
		TSIGSecretAlg: "hmac-sha256",
	}, nil, t, e)
}

// --- Records / AXFR tests ---

func TestRecords_ReturnsARecord(t *testing.T) {
	rr := &dns.A{
		Hdr: dns.RR_Header{Name: "app.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
		A:   net.ParseIP("1.2.3.4"),
	}
	mt := &mockTransferer{envelopes: []*dns.Envelope{{RR: []dns.RR{rr}}}}
	p := testProvider(mt, nil)

	eps, err := p.Records(context.Background())
	if err != nil {
		t.Fatalf("Records() error = %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("got %d endpoints, want 1", len(eps))
	}
	if eps[0].DNSName != "app.example.com" {
		t.Errorf("DNSName = %q, want app.example.com", eps[0].DNSName)
	}
	if eps[0].RecordType != endpoint.RecordTypeA {
		t.Errorf("RecordType = %q, want A", eps[0].RecordType)
	}
	if eps[0].Targets[0] != "1.2.3.4" {
		t.Errorf("Target = %q, want 1.2.3.4", eps[0].Targets[0])
	}
	if eps[0].TTL != 300 {
		t.Errorf("TTL = %d, want 300", eps[0].TTL)
	}
}

func TestRecords_ReturnsAAAARecord(t *testing.T) {
	rr := &dns.AAAA{
		Hdr:  dns.RR_Header{Name: "app.example.com.", Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: 60},
		AAAA: net.ParseIP("2001:db8::1"),
	}
	mt := &mockTransferer{envelopes: []*dns.Envelope{{RR: []dns.RR{rr}}}}
	eps, err := testProvider(mt, nil).Records(context.Background())
	if err != nil || len(eps) != 1 {
		t.Fatalf("got err=%v, len=%d", err, len(eps))
	}
	if eps[0].RecordType != endpoint.RecordTypeAAAA {
		t.Errorf("RecordType = %q, want AAAA", eps[0].RecordType)
	}
}

func TestRecords_ReturnsCNAMERecord(t *testing.T) {
	rr := &dns.CNAME{
		Hdr:    dns.RR_Header{Name: "www.example.com.", Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 300},
		Target: "app.example.com.",
	}
	mt := &mockTransferer{envelopes: []*dns.Envelope{{RR: []dns.RR{rr}}}}
	eps, err := testProvider(mt, nil).Records(context.Background())
	if err != nil || len(eps) != 1 {
		t.Fatalf("got err=%v, len=%d", err, len(eps))
	}
	if eps[0].RecordType != endpoint.RecordTypeCNAME {
		t.Errorf("RecordType = %q, want CNAME", eps[0].RecordType)
	}
	if eps[0].Targets[0] != "app.example.com" {
		t.Errorf("CNAME target = %q, want app.example.com (no trailing dot)", eps[0].Targets[0])
	}
}

func TestRecords_ReturnsTXTRecord(t *testing.T) {
	rr := &dns.TXT{
		Hdr: dns.RR_Header{Name: "owner.example.com.", Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: 300},
		Txt: []string{"heritage=external-dns-docker"},
	}
	mt := &mockTransferer{envelopes: []*dns.Envelope{{RR: []dns.RR{rr}}}}
	eps, err := testProvider(mt, nil).Records(context.Background())
	if err != nil || len(eps) != 1 {
		t.Fatalf("got err=%v, len=%d", err, len(eps))
	}
	if eps[0].RecordType != endpoint.RecordTypeTXT {
		t.Errorf("RecordType = %q, want TXT", eps[0].RecordType)
	}
}

func TestRecords_IgnoresSOA(t *testing.T) {
	soa := &dns.SOA{
		Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: 3600},
	}
	a := &dns.A{
		Hdr: dns.RR_Header{Name: "app.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300},
		A:   net.ParseIP("1.2.3.4"),
	}
	mt := &mockTransferer{envelopes: []*dns.Envelope{{RR: []dns.RR{soa, a}}}}
	eps, err := testProvider(mt, nil).Records(context.Background())
	if err != nil {
		t.Fatalf("Records() error = %v", err)
	}
	if len(eps) != 1 {
		t.Errorf("got %d endpoints, want 1 (SOA should be ignored)", len(eps))
	}
}

func TestRecords_MultipleEnvelopes(t *testing.T) {
	rr1 := &dns.A{Hdr: dns.RR_Header{Name: "a.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300}, A: net.ParseIP("1.1.1.1")}
	rr2 := &dns.A{Hdr: dns.RR_Header{Name: "b.example.com.", Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 300}, A: net.ParseIP("2.2.2.2")}
	mt := &mockTransferer{envelopes: []*dns.Envelope{
		{RR: []dns.RR{rr1}},
		{RR: []dns.RR{rr2}},
	}}
	eps, err := testProvider(mt, nil).Records(context.Background())
	if err != nil || len(eps) != 2 {
		t.Fatalf("got err=%v len=%d, want 2", err, len(eps))
	}
}

func TestRecords_TransferError(t *testing.T) {
	mt := &mockTransferer{err: fmt.Errorf("connection refused")}
	_, err := testProvider(mt, nil).Records(context.Background())
	if err == nil {
		t.Error("expected error from transfer failure, got nil")
	}
}

func TestRecords_EnvelopeError(t *testing.T) {
	mt := &mockTransferer{envelopes: []*dns.Envelope{{Error: fmt.Errorf("xfr error")}}}
	_, err := testProvider(mt, nil).Records(context.Background())
	if err == nil {
		t.Error("expected error from envelope error, got nil")
	}
}

// --- ApplyChanges tests ---

func TestApplyChanges_Create(t *testing.T) {
	me := &mockExchanger{resp: successResp()}
	p := testProvider(nil, me)

	err := p.ApplyChanges(context.Background(), &plan.Changes{
		Create: []*endpoint.Endpoint{
			endpoint.New("app.example.com", []string{"1.2.3.4"}, endpoint.RecordTypeA, 300, nil),
		},
	})
	if err != nil {
		t.Fatalf("ApplyChanges() error = %v", err)
	}
	if me.sent == nil {
		t.Fatal("expected Exchange to be called")
	}
	// Verify an INSERT is present in the authority section.
	if len(me.sent.Ns) == 0 {
		t.Error("expected at least one RR in the update authority section")
	}
}

func TestApplyChanges_Delete(t *testing.T) {
	me := &mockExchanger{resp: successResp()}
	p := testProvider(nil, me)

	err := p.ApplyChanges(context.Background(), &plan.Changes{
		Delete: []*endpoint.Endpoint{
			endpoint.New("old.example.com", []string{"9.9.9.9"}, endpoint.RecordTypeA, 300, nil),
		},
	})
	if err != nil {
		t.Fatalf("ApplyChanges() error = %v", err)
	}
	if len(me.sent.Ns) == 0 {
		t.Error("expected at least one RR in the update authority section")
	}
}

func TestApplyChanges_Update(t *testing.T) {
	me := &mockExchanger{resp: successResp()}
	p := testProvider(nil, me)

	old := endpoint.New("app.example.com", []string{"1.2.3.4"}, endpoint.RecordTypeA, 300, nil)
	newEp := endpoint.New("app.example.com", []string{"5.6.7.8"}, endpoint.RecordTypeA, 300, nil)

	err := p.ApplyChanges(context.Background(), &plan.Changes{
		UpdateOld: []*endpoint.Endpoint{old},
		UpdateNew: []*endpoint.Endpoint{newEp},
	})
	if err != nil {
		t.Fatalf("ApplyChanges() error = %v", err)
	}
	// Remove + Insert = 2 RRs.
	if len(me.sent.Ns) < 2 {
		t.Errorf("expected at least 2 RRs in authority (remove+insert), got %d", len(me.sent.Ns))
	}
}

func TestApplyChanges_Empty_NoExchange(t *testing.T) {
	me := &mockExchanger{resp: successResp()}
	p := testProvider(nil, me)

	err := p.ApplyChanges(context.Background(), &plan.Changes{})
	if err != nil {
		t.Fatalf("ApplyChanges() error = %v", err)
	}
	if me.sent != nil {
		t.Error("expected no Exchange call for empty changes")
	}
}

func TestApplyChanges_NonSuccessRcode(t *testing.T) {
	resp := new(dns.Msg)
	resp.Rcode = dns.RcodeRefused
	me := &mockExchanger{resp: resp}
	p := testProvider(nil, me)

	err := p.ApplyChanges(context.Background(), &plan.Changes{
		Create: []*endpoint.Endpoint{
			endpoint.New("app.example.com", []string{"1.2.3.4"}, endpoint.RecordTypeA, 300, nil),
		},
	})
	if err == nil {
		t.Error("expected error for non-success rcode, got nil")
	}
}

func TestApplyChanges_ExchangeError(t *testing.T) {
	me := &mockExchanger{err: fmt.Errorf("network error")}
	p := testProvider(nil, me)

	err := p.ApplyChanges(context.Background(), &plan.Changes{
		Create: []*endpoint.Endpoint{
			endpoint.New("app.example.com", []string{"1.2.3.4"}, endpoint.RecordTypeA, 300, nil),
		},
	})
	if err == nil {
		t.Error("expected error from exchange failure, got nil")
	}
}

func TestApplyChanges_CNAME(t *testing.T) {
	me := &mockExchanger{resp: successResp()}
	p := testProvider(nil, me)

	err := p.ApplyChanges(context.Background(), &plan.Changes{
		Create: []*endpoint.Endpoint{
			endpoint.New("www.example.com", []string{"app.example.com"}, endpoint.RecordTypeCNAME, 300, nil),
		},
	})
	if err != nil {
		t.Fatalf("ApplyChanges() error = %v", err)
	}
}

// --- min-TTL tests ---

func TestEffectiveTTL_BelowMin(t *testing.T) {
	p := newWithDeps(Config{Host: "ns1", Zone: "example.com", MinTTL: 300}, nil, nil, nil)
	if got := p.effectiveTTL(60); got != 300 {
		t.Errorf("effectiveTTL(60) = %d, want 300", got)
	}
}

func TestEffectiveTTL_AboveMin(t *testing.T) {
	p := newWithDeps(Config{Host: "ns1", Zone: "example.com", MinTTL: 300}, nil, nil, nil)
	if got := p.effectiveTTL(3600); got != 3600 {
		t.Errorf("effectiveTTL(3600) = %d, want 3600", got)
	}
}

func TestEffectiveTTL_NoMin(t *testing.T) {
	p := newWithDeps(Config{Host: "ns1", Zone: "example.com", MinTTL: 0}, nil, nil, nil)
	if got := p.effectiveTTL(60); got != 60 {
		t.Errorf("effectiveTTL(60) with no min = %d, want 60", got)
	}
}

func TestApplyChanges_MinTTLEnforced(t *testing.T) {
	me := &mockExchanger{resp: successResp()}
	p := newWithDeps(Config{
		Host: "ns1.example.com", Port: 53, Zone: "example.com",
		TSIGKeyName: "k", TSIGSecret: "s", TSIGSecretAlg: "hmac-sha256",
		MinTTL: 300,
	}, nil, nil, me)

	err := p.ApplyChanges(context.Background(), &plan.Changes{
		Create: []*endpoint.Endpoint{
			endpoint.New("app.example.com", []string{"1.2.3.4"}, endpoint.RecordTypeA, 60, nil),
		},
	})
	if err != nil {
		t.Fatalf("ApplyChanges() error = %v", err)
	}
	// Verify the RR in the sent message has TTL=300 not 60.
	if len(me.sent.Ns) == 0 {
		t.Fatal("no RRs in update message")
	}
	if me.sent.Ns[0].Header().Ttl != 300 {
		t.Errorf("RR TTL = %d, want 300 (min-ttl enforced)", me.sent.Ns[0].Header().Ttl)
	}
}

// --- New constructor tests ---

func TestNew_DefaultPort(t *testing.T) {
	p := New(Config{Host: "ns1.example.com", Zone: "example.com"}, nil)
	if p.cfg.Port != 53 {
		t.Errorf("Port = %d, want 53 (default)", p.cfg.Port)
	}
	if p.server != "ns1.example.com:53" {
		t.Errorf("server = %q, want ns1.example.com:53", p.server)
	}
}

func TestNew_NilLog_UsesDefault(t *testing.T) {
	p := New(Config{Host: "ns1.example.com", Zone: "example.com"}, nil)
	if p.log == nil {
		t.Error("expected non-nil logger")
	}
}

func TestNew_ExplicitPort(t *testing.T) {
	p := New(Config{Host: "ns1.example.com", Port: 5353, Zone: "example.com"}, nil)
	if p.cfg.Port != 5353 {
		t.Errorf("Port = %d, want 5353", p.cfg.Port)
	}
}

func TestNew_TransfererFactory_ReturnsNonNil(t *testing.T) {
	p := New(Config{Host: "ns1.example.com", Zone: "example.com"}, nil)
	if p.newTransferer == nil {
		t.Fatal("newTransferer factory is nil")
	}
	if p.newTransferer() == nil {
		t.Error("newTransferer() returned nil")
	}
}

// --- normaliseTSIGAlg tests ---

func TestNormaliseTSIGAlg(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"hmac-sha256", "hmac-sha256."},
		{"hmac-sha256.", "hmac-sha256."},
		{"HMAC-SHA256", "hmac-sha256."},
		{"", dns.HmacSHA256},
	}
	for _, tt := range tests {
		got := normaliseTSIGAlg(tt.in)
		if got != tt.want {
			t.Errorf("normaliseTSIGAlg(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// --- endpointToRRs unit tests ---

func TestEndpointToRRs_A(t *testing.T) {
	p := newWithDeps(Config{Host: "ns1", Zone: "example.com"}, nil, nil, nil)
	rrs, err := p.endpointToRRs(endpoint.New("app.example.com", []string{"1.2.3.4"}, endpoint.RecordTypeA, 300, nil))
	if err != nil || len(rrs) != 1 {
		t.Fatalf("err=%v len=%d", err, len(rrs))
	}
	if _, ok := rrs[0].(*dns.A); !ok {
		t.Errorf("expected *dns.A, got %T", rrs[0])
	}
}

func TestEndpointToRRs_AAAA(t *testing.T) {
	p := newWithDeps(Config{Host: "ns1", Zone: "example.com"}, nil, nil, nil)
	rrs, err := p.endpointToRRs(endpoint.New("app.example.com", []string{"2001:db8::1"}, endpoint.RecordTypeAAAA, 300, nil))
	if err != nil || len(rrs) != 1 {
		t.Fatalf("err=%v len=%d", err, len(rrs))
	}
	if _, ok := rrs[0].(*dns.AAAA); !ok {
		t.Errorf("expected *dns.AAAA, got %T", rrs[0])
	}
}

func TestEndpointToRRs_CNAME(t *testing.T) {
	p := newWithDeps(Config{Host: "ns1", Zone: "example.com"}, nil, nil, nil)
	rrs, err := p.endpointToRRs(endpoint.New("www.example.com", []string{"app.example.com"}, endpoint.RecordTypeCNAME, 300, nil))
	if err != nil || len(rrs) != 1 {
		t.Fatalf("err=%v len=%d", err, len(rrs))
	}
	if _, ok := rrs[0].(*dns.CNAME); !ok {
		t.Errorf("expected *dns.CNAME, got %T", rrs[0])
	}
}

func TestEndpointToRRs_TXT(t *testing.T) {
	p := newWithDeps(Config{Host: "ns1", Zone: "example.com"}, nil, nil, nil)
	rrs, err := p.endpointToRRs(endpoint.New("owner.example.com", []string{"heritage=x"}, endpoint.RecordTypeTXT, 300, nil))
	if err != nil || len(rrs) != 1 {
		t.Fatalf("err=%v len=%d", err, len(rrs))
	}
	if _, ok := rrs[0].(*dns.TXT); !ok {
		t.Errorf("expected *dns.TXT, got %T", rrs[0])
	}
}

func TestEndpointToRRs_InvalidAIP(t *testing.T) {
	p := newWithDeps(Config{Host: "ns1", Zone: "example.com"}, nil, nil, nil)
	_, err := p.endpointToRRs(endpoint.New("app.example.com", []string{"not-an-ip"}, endpoint.RecordTypeA, 300, nil))
	if err == nil {
		t.Error("expected error for invalid A record IP, got nil")
	}
}

func TestEndpointToRRs_InvalidAAAAIP(t *testing.T) {
	p := newWithDeps(Config{Host: "ns1", Zone: "example.com"}, nil, nil, nil)
	_, err := p.endpointToRRs(endpoint.New("app.example.com", []string{"not-an-ip"}, endpoint.RecordTypeAAAA, 300, nil))
	if err == nil {
		t.Error("expected error for invalid AAAA record IP, got nil")
	}
}

func TestEndpointToRRs_UnsupportedType(t *testing.T) {
	p := newWithDeps(Config{Host: "ns1", Zone: "example.com"}, nil, nil, nil)
	_, err := p.endpointToRRs(endpoint.New("app.example.com", []string{"1.2.3.4"}, "MX", 300, nil))
	if err == nil {
		t.Error("expected error for unsupported record type, got nil")
	}
}

// --- ApplyChanges: invalid endpoint warning paths ---

func TestApplyChanges_InvalidCreateEndpoint_Skipped(t *testing.T) {
	me := &mockExchanger{resp: successResp()}
	p := testProvider(nil, me)

	// A record with an invalid IP — endpointToRRs will fail, should be skipped with warning.
	err := p.ApplyChanges(context.Background(), &plan.Changes{
		Create: []*endpoint.Endpoint{
			endpoint.New("app.example.com", []string{"not-an-ip"}, endpoint.RecordTypeA, 300, nil),
		},
	})
	if err != nil {
		t.Fatalf("ApplyChanges() error = %v (invalid endpoint should be skipped, not fail)", err)
	}
}

func TestApplyChanges_InvalidDeleteEndpoint_Skipped(t *testing.T) {
	me := &mockExchanger{resp: successResp()}
	p := testProvider(nil, me)

	err := p.ApplyChanges(context.Background(), &plan.Changes{
		Delete: []*endpoint.Endpoint{
			endpoint.New("app.example.com", []string{"not-an-ip"}, endpoint.RecordTypeA, 300, nil),
		},
	})
	if err != nil {
		t.Fatalf("ApplyChanges() error = %v (invalid delete endpoint should be skipped)", err)
	}
}

func TestApplyChanges_InvalidUpdateEndpoints_Skipped(t *testing.T) {
	me := &mockExchanger{resp: successResp()}
	p := testProvider(nil, me)

	err := p.ApplyChanges(context.Background(), &plan.Changes{
		UpdateOld: []*endpoint.Endpoint{
			endpoint.New("app.example.com", []string{"bad"}, endpoint.RecordTypeA, 300, nil),
		},
		UpdateNew: []*endpoint.Endpoint{
			endpoint.New("app.example.com", []string{"also-bad"}, endpoint.RecordTypeA, 300, nil),
		},
	})
	if err != nil {
		t.Fatalf("ApplyChanges() error = %v (invalid update endpoints should be skipped)", err)
	}
}

func TestApplyChanges_InvalidUpdateNew_Skipped(t *testing.T) {
	// UpdateOld is valid (remove succeeds) but UpdateNew has a bad IP so the
	// insert is skipped with a warning — covers the UpdateNew error branch.
	me := &mockExchanger{resp: successResp()}
	p := testProvider(nil, me)

	err := p.ApplyChanges(context.Background(), &plan.Changes{
		UpdateOld: []*endpoint.Endpoint{
			endpoint.New("app.example.com", []string{"1.2.3.4"}, endpoint.RecordTypeA, 300, nil),
		},
		UpdateNew: []*endpoint.Endpoint{
			endpoint.New("app.example.com", []string{"not-an-ip"}, endpoint.RecordTypeA, 300, nil),
		},
	})
	if err != nil {
		t.Fatalf("ApplyChanges() error = %v (invalid UpdateNew should be skipped, not fail)", err)
	}
}

// --- rrType unit tests ---

func TestRRType(t *testing.T) {
	tests := []struct{ in, wantType string }{
		{endpoint.RecordTypeA, "A"},
		{endpoint.RecordTypeAAAA, "AAAA"},
		{endpoint.RecordTypeCNAME, "CNAME"},
		{endpoint.RecordTypeTXT, "TXT"},
	}
	for _, tt := range tests {
		got := rrType(tt.in)
		if dns.TypeToString[got] != tt.wantType {
			t.Errorf("rrType(%q) = %d (%s), want %s", tt.in, got, dns.TypeToString[got], tt.wantType)
		}
	}
}

func TestRRType_Unknown(t *testing.T) {
	if got := rrType("MX"); got != dns.TypeNone {
		t.Errorf("rrType(MX) = %d, want TypeNone (%d)", got, dns.TypeNone)
	}
}

// --- rrToEndpoint unit tests ---

func TestRRToEndpoint_UnsupportedType_ReturnsNil(t *testing.T) {
	soa := &dns.SOA{Hdr: dns.RR_Header{Name: "example.com.", Rrtype: dns.TypeSOA, Ttl: 3600}}
	if ep := rrToEndpoint(soa); ep != nil {
		t.Errorf("expected nil for SOA, got %v", ep)
	}
}

func TestRRToEndpoint_TrailingDotStripped(t *testing.T) {
	rr := &dns.A{
		Hdr: dns.RR_Header{Name: "app.example.com.", Rrtype: dns.TypeA, Ttl: 300},
		A:   net.ParseIP("1.2.3.4"),
	}
	ep := rrToEndpoint(rr)
	if ep == nil {
		t.Fatal("expected endpoint, got nil")
	}
	if ep.DNSName != "app.example.com" {
		t.Errorf("DNSName = %q, want no trailing dot", ep.DNSName)
	}
}
