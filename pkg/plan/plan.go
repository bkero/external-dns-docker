package plan

import (
	"fmt"
	"sort"
	"strings"

	"github.com/bkero/external-dns-docker/pkg/endpoint"
)

const (
	// ownerPrefix is prepended to a managed record's DNS name to form the
	// companion ownership TXT record name.
	// e.g. app.example.com → external-dns-docker-owner.app.example.com
	ownerPrefix = "external-dns-docker-owner."

	// DefaultOwnerID is used when no explicit owner ID is configured.
	DefaultOwnerID = "external-dns-docker"

	// ownershipTTL is the TTL assigned to ownership TXT records.
	ownershipTTL = int64(300)
)

// ownershipValue returns the TXT record value that identifies ownership.
func ownershipValue(ownerID string) string {
	return fmt.Sprintf("heritage=external-dns-docker,external-dns-docker/owner=%s", ownerID)
}

// ownershipName returns the DNS name of the ownership TXT record for a managed name.
func ownershipName(dnsName string) string {
	return ownerPrefix + dnsName
}

// Plan calculates DNS changes between a desired and current state, enforcing
// ownership so that only records this daemon manages are ever modified.
type Plan struct {
	ownerID string
}

// New returns a Plan with the given owner ID (use DefaultOwnerID if empty).
func New(ownerID string) *Plan {
	if ownerID == "" {
		ownerID = DefaultOwnerID
	}
	return &Plan{ownerID: ownerID}
}

// Calculate diffs desired endpoints (from the source) against current endpoints
// (from the provider) and returns the minimal set of Changes needed to converge
// the DNS state. Ownership TXT companion records are created and deleted
// alongside their managed records.
//
// Records present in current that have no matching ownership TXT record are
// never modified or deleted.
func (p *Plan) Calculate(desired, current []*endpoint.Endpoint) *Changes {
	// Step 1: build the owned-name set from current ownership TXT records.
	owned := p.buildOwnedSet(current)

	// Step 2: index current non-ownership records by (DNSName, RecordType).
	currentIdx := indexEndpoints(filterOwnershipTXTs(current))

	// Step 3: index desired records by (DNSName, RecordType).
	desiredIdx := indexEndpoints(desired)

	changes := &Changes{}

	// Step 4: walk desired — create new records, update owned changed records.
	for key, want := range desiredIdx {
		have, exists := currentIdx[key]
		if !exists {
			// New record: create it and its ownership TXT companion.
			changes.Create = append(changes.Create, want)
			changes.Create = append(changes.Create, p.ownershipTXTFor(want.DNSName))
			continue
		}
		if !owned[want.DNSName] {
			// Record exists but is not owned by us — leave it alone.
			continue
		}
		if !endpointsEqual(have, want) {
			// Owned and changed: schedule an update.
			changes.UpdateOld = append(changes.UpdateOld, have)
			changes.UpdateNew = append(changes.UpdateNew, want)
		}
		// Equal — no-op.
	}

	// Step 5: walk current — delete owned records that are no longer desired.
	for key, have := range currentIdx {
		if _, wanted := desiredIdx[key]; wanted {
			continue
		}
		if !owned[have.DNSName] {
			// Not owned by us — never delete.
			continue
		}
		changes.Delete = append(changes.Delete, have)
		changes.Delete = append(changes.Delete, p.ownershipTXTFor(have.DNSName))
	}

	return changes
}

// buildOwnedSet returns a set of DNS names whose ownership TXT records match
// this plan's owner ID.
func (p *Plan) buildOwnedSet(current []*endpoint.Endpoint) map[string]bool {
	want := ownershipValue(p.ownerID)
	owned := make(map[string]bool)
	for _, ep := range current {
		if ep.RecordType != endpoint.RecordTypeTXT {
			continue
		}
		if !strings.HasPrefix(ep.DNSName, ownerPrefix) {
			continue
		}
		managedName := strings.TrimPrefix(ep.DNSName, ownerPrefix)
		for _, v := range ep.Targets {
			if v == want {
				owned[managedName] = true
				break
			}
		}
	}
	return owned
}

// ownershipTXTFor returns the ownership TXT endpoint companion for dnsName.
func (p *Plan) ownershipTXTFor(dnsName string) *endpoint.Endpoint {
	return endpoint.New(
		ownershipName(dnsName),
		[]string{ownershipValue(p.ownerID)},
		endpoint.RecordTypeTXT,
		ownershipTTL,
		nil,
	)
}

// filterOwnershipTXTs returns endpoints that are NOT ownership TXT records.
func filterOwnershipTXTs(eps []*endpoint.Endpoint) []*endpoint.Endpoint {
	out := make([]*endpoint.Endpoint, 0, len(eps))
	for _, ep := range eps {
		if ep.RecordType == endpoint.RecordTypeTXT && strings.HasPrefix(ep.DNSName, ownerPrefix) {
			continue
		}
		out = append(out, ep)
	}
	return out
}

// indexEndpoints builds a map from "DNSName|RecordType" to Endpoint.
// If duplicate keys exist the last one wins (undefined provider behaviour).
func indexEndpoints(eps []*endpoint.Endpoint) map[string]*endpoint.Endpoint {
	idx := make(map[string]*endpoint.Endpoint, len(eps))
	for _, ep := range eps {
		idx[epKey(ep)] = ep
	}
	return idx
}

// epKey returns a stable map key for an endpoint.
func epKey(ep *endpoint.Endpoint) string {
	return ep.DNSName + "|" + ep.RecordType
}

// endpointsEqual returns true when two endpoints have the same targets and TTL.
// DNSName and RecordType are assumed to already match (they are the map key).
func endpointsEqual(a, b *endpoint.Endpoint) bool {
	if a.TTL != b.TTL {
		return false
	}
	if len(a.Targets) != len(b.Targets) {
		return false
	}
	as := sortedCopy(a.Targets)
	bs := sortedCopy(b.Targets)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

func sortedCopy(s []string) []string {
	c := make([]string, len(s))
	copy(c, s)
	sort.Strings(c)
	return c
}
