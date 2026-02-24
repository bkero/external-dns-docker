// Package endpoint defines the Endpoint type that represents a desired DNS record.
package endpoint

import (
	"fmt"
	"net"
	"strings"
)

// DNS record type constants.
const (
	RecordTypeA     = "A"
	RecordTypeAAAA  = "AAAA"
	RecordTypeCNAME = "CNAME"
	RecordTypeTXT   = "TXT"

	// DefaultTTL is the TTL applied when none is specified.
	DefaultTTL = int64(300)
)

// Endpoint represents a desired DNS record.
type Endpoint struct {
	// DNSName is the fully-qualified DNS name (e.g. "app.example.com").
	DNSName string
	// Targets is the list of values the record points to (IPs or hostnames).
	Targets []string
	// RecordType is the DNS record type: A, AAAA, CNAME, or TXT.
	RecordType string
	// TTL is the time-to-live in seconds.
	TTL int64
	// Labels carries arbitrary metadata (e.g. ownership tracking).
	Labels map[string]string
}

// New returns an Endpoint with TTL defaulting to DefaultTTL.
func New(dnsName string, targets []string, recordType string, ttl int64, labels map[string]string) *Endpoint {
	if ttl == 0 {
		ttl = DefaultTTL
	}
	if labels == nil {
		labels = map[string]string{}
	}
	return &Endpoint{
		DNSName:    dnsName,
		Targets:    targets,
		RecordType: recordType,
		TTL:        ttl,
		Labels:     labels,
	}
}

// String returns a human-readable representation of the endpoint.
func (e *Endpoint) String() string {
	return fmt.Sprintf("%s %s %s (TTL %d)", e.DNSName, e.RecordType, strings.Join(e.Targets, ","), e.TTL)
}

// InferRecordType returns the DNS record type inferred from target.
// A valid IPv4 address → "A", a valid IPv6 address → "AAAA", anything else → "CNAME".
func InferRecordType(target string) string {
	ip := net.ParseIP(target)
	if ip == nil {
		return RecordTypeCNAME
	}
	if ip.To4() != nil {
		return RecordTypeA
	}
	return RecordTypeAAAA
}
