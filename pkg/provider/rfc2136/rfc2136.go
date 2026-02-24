// Package rfc2136 implements a DNS provider using RFC2136 dynamic updates.
package rfc2136

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"

	"github.com/bkero/external-dns-docker/pkg/endpoint"
	"github.com/bkero/external-dns-docker/pkg/plan"
)

// dnsTransferer abstracts dns.Transfer.In for testability.
type dnsTransferer interface {
	In(m *dns.Msg, addr string) (chan *dns.Envelope, error)
}

// dnsExchanger abstracts dns.Client.ExchangeContext for testability.
type dnsExchanger interface {
	ExchangeContext(ctx context.Context, m *dns.Msg, addr string) (*dns.Msg, time.Duration, error)
}

// defaultTimeout is the DNS operation timeout applied when none is configured.
const defaultTimeout = 10 * time.Second

// Config holds all RFC2136 provider configuration.
type Config struct {
	Host          string
	Port          int
	Zone          string
	TSIGKeyName   string
	TSIGSecret    string
	TSIGSecretAlg string // e.g. "hmac-sha256" (trailing dot optional)
	MinTTL        int64
	Timeout       time.Duration // DNS operation timeout; 0 uses defaultTimeout (10s)
}

// Provider implements provider.Provider against an RFC2136-capable DNS server.
type Provider struct {
	cfg           Config
	server        string // "host:port"
	tsigAlg       string // normalised algorithm name (with trailing dot)
	log           *slog.Logger
	newTransferer func() dnsTransferer // factory: creates a fresh transferrer per Records() call
	exchanger     dnsExchanger
}

// New returns a configured RFC2136 Provider.
func New(cfg Config, log *slog.Logger) *Provider {
	if cfg.Port == 0 {
		cfg.Port = 53
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	if log == nil {
		log = slog.Default()
	}
	alg := normaliseTSIGAlg(cfg.TSIGSecretAlg)
	server := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	tsigSecret := map[string]string{
		dns.Fqdn(cfg.TSIGKeyName): cfg.TSIGSecret,
	}

	return &Provider{
		cfg:     cfg,
		server:  server,
		tsigAlg: alg,
		log:     log,
		newTransferer: func() dnsTransferer {
			return &dns.Transfer{TsigSecret: tsigSecret}
		},
		exchanger: &dns.Client{
			Net:        "tcp",
			TsigSecret: tsigSecret,
			Timeout:    cfg.Timeout,
		},
	}
}

// newWithDeps constructs a Provider with injected transport dependencies for testing.
func newWithDeps(cfg Config, log *slog.Logger, t dnsTransferer, e dnsExchanger) *Provider {
	if cfg.Port == 0 {
		cfg.Port = 53
	}
	if log == nil {
		log = slog.Default()
	}
	return &Provider{
		cfg:           cfg,
		server:        fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		tsigAlg:       normaliseTSIGAlg(cfg.TSIGSecretAlg),
		log:           log,
		newTransferer: func() dnsTransferer { return t },
		exchanger:     e,
	}
}

// Preflight validates connectivity and TSIG credentials by sending a SOA query
// to the configured DNS server. Returns an error if the server is unreachable
// or responds with a non-success rcode (e.g. NOTAUTH on bad TSIG).
func (p *Provider) Preflight(ctx context.Context) error {
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(p.cfg.Zone), dns.TypeSOA)
	if p.cfg.TSIGKeyName != "" {
		m.SetTsig(dns.Fqdn(p.cfg.TSIGKeyName), p.tsigAlg, 300, time.Now().Unix())
	}
	r, _, err := p.exchanger.ExchangeContext(ctx, m, p.server)
	if err != nil {
		return fmt.Errorf("preflight SOA query to %s failed: %w", p.server, err)
	}
	if r.Rcode != dns.RcodeSuccess {
		return fmt.Errorf("preflight SOA query failed: rcode %s (%d) — check --rfc2136-host and TSIG credentials",
			dns.RcodeToString[r.Rcode], r.Rcode)
	}
	return nil
}

// Records fetches the current zone contents via AXFR and returns them as Endpoints.
func (p *Provider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	m := new(dns.Msg)
	m.SetAxfr(dns.Fqdn(p.cfg.Zone))
	if p.cfg.TSIGKeyName != "" {
		m.SetTsig(dns.Fqdn(p.cfg.TSIGKeyName), p.tsigAlg, 300, time.Now().Unix())
	}

	env, err := p.newTransferer().In(m, p.server)
	if err != nil {
		return nil, fmt.Errorf("axfr %s: %w", p.cfg.Zone, err)
	}

	var endpoints []*endpoint.Endpoint
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case e, ok := <-env:
			if !ok {
				return endpoints, nil
			}
			if e.Error != nil {
				return nil, fmt.Errorf("axfr %s: %w", p.cfg.Zone, e.Error)
			}
			for _, rr := range e.RR {
				ep := rrToEndpoint(rr)
				if ep != nil {
					endpoints = append(endpoints, ep)
				}
			}
		}
	}
}

// ApplyChanges sends RFC2136 UPDATE messages to create, update, and delete records.
func (p *Provider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	if changes.IsEmpty() {
		return nil
	}

	// Collect all RRs into a single UPDATE message for atomicity.
	m := new(dns.Msg)
	m.SetUpdate(dns.Fqdn(p.cfg.Zone))

	// Deletes: remove exact RR (RFC2136 §2.5.4).
	for _, ep := range changes.Delete {
		rrs, err := p.endpointToRRs(ep)
		if err != nil {
			p.log.Warn("skipping delete: cannot convert endpoint to RR",
				"endpoint", ep.DNSName, "err", err)
			continue
		}
		m.Remove(rrs)
	}

	// Updates: remove old, insert new.
	for i, old := range changes.UpdateOld {
		rrs, err := p.endpointToRRs(old)
		if err != nil {
			p.log.Warn("skipping update (remove): cannot convert endpoint to RR",
				"endpoint", old.DNSName, "err", err)
			continue
		}
		m.Remove(rrs)
		if i < len(changes.UpdateNew) {
			newRRs, err := p.endpointToRRs(changes.UpdateNew[i])
			if err != nil {
				p.log.Warn("skipping update (insert): cannot convert endpoint to RR",
					"endpoint", changes.UpdateNew[i].DNSName, "err", err)
				continue
			}
			m.Insert(newRRs)
		}
	}

	// Creates: insert new RRs.
	for _, ep := range changes.Create {
		rrs, err := p.endpointToRRs(ep)
		if err != nil {
			p.log.Warn("skipping create: cannot convert endpoint to RR",
				"endpoint", ep.DNSName, "err", err)
			continue
		}
		m.Insert(rrs)
	}

	if p.cfg.TSIGKeyName != "" {
		m.SetTsig(dns.Fqdn(p.cfg.TSIGKeyName), p.tsigAlg, 300, time.Now().Unix())
	}

	r, _, err := p.exchanger.ExchangeContext(ctx, m, p.server)
	if err != nil {
		return fmt.Errorf("dns update exchange: %w", err)
	}
	if r.Rcode != dns.RcodeSuccess {
		return fmt.Errorf("dns update failed: rcode %s (%d)", dns.RcodeToString[r.Rcode], r.Rcode)
	}
	return nil
}

// rrToEndpoint converts a miekg/dns RR to an Endpoint. Returns nil for
// unsupported or zone-metadata record types (SOA, NS, TSIG, etc.).
func rrToEndpoint(rr dns.RR) *endpoint.Endpoint {
	hdr := rr.Header()
	name := strings.TrimSuffix(hdr.Name, ".")
	ttl := int64(hdr.Ttl)

	switch v := rr.(type) {
	case *dns.A:
		return endpoint.New(name, []string{v.A.String()}, endpoint.RecordTypeA, ttl, nil)
	case *dns.AAAA:
		return endpoint.New(name, []string{v.AAAA.String()}, endpoint.RecordTypeAAAA, ttl, nil)
	case *dns.CNAME:
		return endpoint.New(name, []string{strings.TrimSuffix(v.Target, ".")}, endpoint.RecordTypeCNAME, ttl, nil)
	case *dns.TXT:
		return endpoint.New(name, v.Txt, endpoint.RecordTypeTXT, ttl, nil)
	default:
		return nil
	}
}

// endpointToRRs converts an Endpoint to one or more miekg/dns RRs.
func (p *Provider) endpointToRRs(ep *endpoint.Endpoint) ([]dns.RR, error) {
	ttl := p.effectiveTTL(ep.TTL)
	name := dns.Fqdn(ep.DNSName)
	var rrs []dns.RR

	for _, target := range ep.Targets {
		hdr := dns.RR_Header{
			Name:   name,
			Rrtype: rrType(ep.RecordType),
			Class:  dns.ClassINET,
			Ttl:    uint32(ttl),
		}
		switch ep.RecordType {
		case endpoint.RecordTypeA:
			ip := net.ParseIP(target).To4()
			if ip == nil {
				return nil, fmt.Errorf("invalid IPv4 address %q for A record", target)
			}
			rrs = append(rrs, &dns.A{Hdr: hdr, A: ip})
		case endpoint.RecordTypeAAAA:
			ip := net.ParseIP(target)
			if ip == nil {
				return nil, fmt.Errorf("invalid IPv6 address %q for AAAA record", target)
			}
			rrs = append(rrs, &dns.AAAA{Hdr: hdr, AAAA: ip})
		case endpoint.RecordTypeCNAME:
			rrs = append(rrs, &dns.CNAME{Hdr: hdr, Target: dns.Fqdn(target)})
		case endpoint.RecordTypeTXT:
			rrs = append(rrs, &dns.TXT{Hdr: hdr, Txt: []string{target}})
		default:
			return nil, fmt.Errorf("unsupported record type %q", ep.RecordType)
		}
	}
	return rrs, nil
}

// effectiveTTL returns the TTL to use, enforcing MinTTL when configured.
func (p *Provider) effectiveTTL(ttl int64) int64 {
	if p.cfg.MinTTL > 0 && ttl < p.cfg.MinTTL {
		return p.cfg.MinTTL
	}
	return ttl
}

// rrType maps an endpoint record type string to a miekg/dns type constant.
func rrType(rt string) uint16 {
	switch rt {
	case endpoint.RecordTypeA:
		return dns.TypeA
	case endpoint.RecordTypeAAAA:
		return dns.TypeAAAA
	case endpoint.RecordTypeCNAME:
		return dns.TypeCNAME
	case endpoint.RecordTypeTXT:
		return dns.TypeTXT
	default:
		return dns.TypeNone
	}
}

// normaliseTSIGAlg ensures the algorithm name has a trailing dot as required
// by miekg/dns. Accepts both "hmac-sha256" and "hmac-sha256.".
func normaliseTSIGAlg(alg string) string {
	if alg == "" {
		return dns.HmacSHA256
	}
	if !strings.HasSuffix(alg, ".") {
		alg += "."
	}
	return strings.ToLower(alg)
}
