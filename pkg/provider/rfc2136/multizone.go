package rfc2136

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"

	"github.com/bkero/external-dns-docker/pkg/endpoint"
	"github.com/bkero/external-dns-docker/pkg/plan"
)

// ZoneConfig holds per-zone RFC2136 provider configuration.
// TSIGSecretFile, if set, must be resolved to TSIGSecret by the caller before
// passing to NewMulti.
type ZoneConfig struct {
	Host           string
	Port           int
	Zone           string
	TSIGKey        string
	TSIGSecret     string
	TSIGSecretFile string
	TSIGAlg        string
	MinTTL         int64
	Timeout        time.Duration
}

// zoneEntry pairs a normalised zone FQDN with its single-zone Provider.
type zoneEntry struct {
	zone string // dns.Fqdn-normalised, e.g. "example.com."
	prov *Provider
}

// MultiProvider implements provider.Provider for multiple RFC2136-managed zones.
type MultiProvider struct {
	zones []zoneEntry
	log   *slog.Logger
}

// NewMulti creates a MultiProvider from a slice of ZoneConfigs.
// TSIGSecretFile in each config must already be resolved to TSIGSecret.
func NewMulti(configs []ZoneConfig, log *slog.Logger) *MultiProvider {
	if log == nil {
		log = slog.Default()
	}
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
			prov: New(cfg, log),
		})
	}
	return &MultiProvider{zones: entries, log: log}
}

// Records fans out to all sub-providers in parallel and merges the results.
// Returns the first error encountered, if any.
func (m *MultiProvider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	type result struct {
		eps []*endpoint.Endpoint
		err error
	}
	results := make([]result, len(m.zones))
	var wg sync.WaitGroup
	for i, ze := range m.zones {
		wg.Add(1)
		go func(idx int, z zoneEntry) {
			defer wg.Done()
			eps, err := z.prov.Records(ctx)
			results[idx] = result{eps: eps, err: err}
		}(i, ze)
	}
	wg.Wait()

	var all []*endpoint.Endpoint
	for _, r := range results {
		if r.err != nil {
			return nil, r.err
		}
		all = append(all, r.eps...)
	}
	return all, nil
}

// ApplyChanges splits the Changes set by zone using longest-suffix matching and
// dispatches each subset to the matching sub-provider. Endpoints with no matching
// zone are logged at WARN level and skipped. Zones with no changes are not called.
func (m *MultiProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	byZone := make(map[string]*plan.Changes, len(m.zones))
	for _, ze := range m.zones {
		byZone[ze.zone] = &plan.Changes{}
	}

	for _, ep := range changes.Create {
		ze := m.zoneFor(ep.DNSName)
		if ze == nil {
			m.log.Warn("no zone match for endpoint, skipping", "dnsName", ep.DNSName)
			continue
		}
		byZone[ze.zone].Create = append(byZone[ze.zone].Create, ep)
	}
	for _, ep := range changes.Delete {
		ze := m.zoneFor(ep.DNSName)
		if ze == nil {
			m.log.Warn("no zone match for endpoint, skipping", "dnsName", ep.DNSName)
			continue
		}
		byZone[ze.zone].Delete = append(byZone[ze.zone].Delete, ep)
	}
	for i, old := range changes.UpdateOld {
		ze := m.zoneFor(old.DNSName)
		if ze == nil {
			m.log.Warn("no zone match for endpoint, skipping", "dnsName", old.DNSName)
			continue
		}
		byZone[ze.zone].UpdateOld = append(byZone[ze.zone].UpdateOld, old)
		if i < len(changes.UpdateNew) {
			byZone[ze.zone].UpdateNew = append(byZone[ze.zone].UpdateNew, changes.UpdateNew[i])
		}
	}

	for _, ze := range m.zones {
		zc := byZone[ze.zone]
		if zc.IsEmpty() {
			continue
		}
		if err := ze.prov.ApplyChanges(ctx, zc); err != nil {
			return err
		}
	}
	return nil
}

// Preflight runs SOA preflight checks against all zones sequentially.
// Returns the first error encountered.
func (m *MultiProvider) Preflight(ctx context.Context) error {
	for _, ze := range m.zones {
		if err := ze.prov.Preflight(ctx); err != nil {
			return fmt.Errorf("zone %s: %w", ze.zone, err)
		}
	}
	return nil
}

// zoneFor returns the zoneEntry whose zone FQDN is the longest suffix match
// for dnsName. Returns nil if no zone matches.
func (m *MultiProvider) zoneFor(dnsName string) *zoneEntry {
	name := strings.TrimSuffix(dnsName, ".")

	var best *zoneEntry
	bestLen := 0
	for i := range m.zones {
		ze := &m.zones[i]
		zoneWithoutDot := strings.TrimSuffix(ze.zone, ".")
		if name == zoneWithoutDot || strings.HasSuffix(name, "."+zoneWithoutDot) {
			if len(zoneWithoutDot) > bestLen {
				bestLen = len(zoneWithoutDot)
				best = ze
			}
		}
	}
	return best
}
