//go:build integration

// Package integration_test contains end-to-end tests that require a running
// BIND9 server started via docker compose (see docker-compose.yml).
//
// Run with:
//
//	docker compose -f test/integration/docker-compose.yml up -d
//	go test -v -tags integration ./test/integration/...
//	docker compose -f test/integration/docker-compose.yml down -v
package integration_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/bkero/external-dns-docker/pkg/controller"
	"github.com/bkero/external-dns-docker/pkg/endpoint"
	"github.com/bkero/external-dns-docker/pkg/provider/rfc2136"
	fake_source "github.com/bkero/external-dns-docker/pkg/source/fake"
)

// ---- Test configuration ----

const (
	bindHost    = "127.0.0.1"
	bindPort    = 5353
	bindAddr    = "127.0.0.1:5353"
	zone        = "example.com"
	tsigKeyName = "external-dns-test"
	// base64("test-key-for-external-dns-docker")
	tsigSecret = "dGVzdC1rZXktZm9yLWV4dGVybmFsLWRucy1kb2NrZXI="
	tsigAlg    = "hmac-sha256"
)

func providerCfg() rfc2136.Config {
	return rfc2136.Config{
		Host:          bindHost,
		Port:          bindPort,
		Zone:          zone,
		TSIGKeyName:   tsigKeyName,
		TSIGSecret:    tsigSecret,
		TSIGSecretAlg: tsigAlg,
	}
}

// ---- TestMain — wait for BIND9 before running tests ----

func TestMain(m *testing.M) {
	if !waitForBIND9(30 * time.Second) {
		fmt.Fprintln(os.Stderr, "BIND9 not reachable at "+bindAddr+" — is docker compose up?")
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// waitForBIND9 retries an SOA query until BIND9 is ready or the deadline passes.
func waitForBIND9(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	c := new(dns.Client)
	for time.Now().Before(deadline) {
		m := new(dns.Msg)
		m.SetQuestion(dns.Fqdn(zone), dns.TypeSOA)
		r, _, err := c.Exchange(m, bindAddr)
		if err == nil && r.Rcode == dns.RcodeSuccess {
			return true
		}
		time.Sleep(500 * time.Millisecond)
	}
	return false
}

// ---- Helpers ----

// reconcileOnce runs one controller cycle with the given desired endpoints
// against the test BIND9 server.
func reconcileOnce(t *testing.T, desired []*endpoint.Endpoint) {
	t.Helper()
	src := fake_source.New(desired)
	prov := rfc2136.New(providerCfg(), nil)
	ctrl := controller.New(src, prov, nil, controller.Config{Once: true})
	if err := ctrl.Run(context.Background()); err != nil {
		t.Fatalf("controller.Run: %v", err)
	}
}

// queryA queries the test BIND9 server directly and returns all A record values
// for the given fully-qualified domain name.
func queryA(fqdn string) []string {
	c := new(dns.Client)
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(fqdn), dns.TypeA)
	r, _, err := c.Exchange(m, bindAddr)
	if err != nil || r.Rcode != dns.RcodeSuccess {
		return nil
	}
	var ips []string
	for _, rr := range r.Answer {
		if a, ok := rr.(*dns.A); ok {
			ips = append(ips, a.A.String())
		}
	}
	return ips
}

// assertARecord polls until fqdn has an A record pointing to wantIP, or fails
// after a short deadline.
func assertARecord(t *testing.T, fqdn, wantIP string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, ip := range queryA(fqdn) {
			if ip == wantIP {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Errorf("A record %s → %s not found (got %v)", fqdn, wantIP, queryA(fqdn))
}

// assertNoARecord polls until fqdn has no A records, or fails after a deadline.
func assertNoARecord(t *testing.T, fqdn string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(queryA(fqdn)) == 0 {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Errorf("expected no A record for %s, but got %v", fqdn, queryA(fqdn))
}

// ---- Tests ----

// TestContainerStart_CreatesARecord verifies that when desired endpoints contain
// a new hostname, reconciliation creates the corresponding A record in BIND9.
func TestContainerStart_CreatesARecord(t *testing.T) {
	fqdn := "create-test.example.com"

	reconcileOnce(t, []*endpoint.Endpoint{
		endpoint.New(fqdn, []string{"1.2.3.4"}, endpoint.RecordTypeA, 300, nil),
	})

	assertARecord(t, fqdn, "1.2.3.4")
}

// TestContainerStop_DeletesARecord verifies that when a previously-created record
// is removed from desired endpoints, reconciliation deletes it from BIND9.
func TestContainerStop_DeletesARecord(t *testing.T) {
	fqdn := "delete-test.example.com"

	// Setup: create the record first.
	reconcileOnce(t, []*endpoint.Endpoint{
		endpoint.New(fqdn, []string{"5.6.7.8"}, endpoint.RecordTypeA, 300, nil),
	})
	assertARecord(t, fqdn, "5.6.7.8")

	// Teardown: reconcile with empty desired state — record should be deleted.
	reconcileOnce(t, nil)
	assertNoARecord(t, fqdn)
}

// TestUnownedRecord_NotDeleted verifies that a DNS record that has no companion
// ownership TXT record is never deleted by the daemon, even when it is absent
// from the desired state.
func TestUnownedRecord_NotDeleted(t *testing.T) {
	// manual.example.com is seeded in the initial zone file with no ownership
	// TXT record — external-dns-docker must leave it untouched.
	reconcileOnce(t, nil) // empty desired state

	assertARecord(t, "manual.example.com", "10.0.0.1")
}
