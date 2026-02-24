//go:build integration

// Package integration_test contains end-to-end tests that require the full
// Docker Compose stack defined in docker-compose.yml to be running:
//
//   - bind9        — RFC2136-capable DNS server seeded with a manual record
//   - external-dns-docker — the daemon under test, watching the host Docker socket
//
// Run with:
//
//	docker compose -f test/integration/docker-compose.yml up -d --build
//	go test -v -tags integration ./test/integration/...
//	docker compose -f test/integration/docker-compose.yml down -v
package integration_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	dockerimage "github.com/docker/docker/api/types/image"
	dockerclient "github.com/docker/docker/client"
	"github.com/miekg/dns"
)

// ---- Test configuration ----

const (
	bindHost = "127.0.0.1"
	bindPort = 5354
	bindAddr = "127.0.0.1:5354"
	zone     = "example.com"

	// testImage is the minimal image used for ephemeral test containers.
	// It must support the exec-form CMD ["sleep", "3600"].
	testImage = "busybox:latest"

	// reconcileTimeout is the maximum time to wait for external-dns-docker
	// to react to a Docker event and update BIND9.
	reconcileTimeout = 20 * time.Second
)

// ---- TestMain — wait for the full stack before running tests ----

func TestMain(m *testing.M) {
	if !waitForBIND9(30 * time.Second) {
		fmt.Fprintln(os.Stderr, "BIND9 not reachable at "+bindAddr+" — is docker compose up?")
		os.Exit(1)
	}
	if err := ensureTestImage(); err != nil {
		fmt.Fprintf(os.Stderr, "prepare test image %s: %v\n", testImage, err)
		os.Exit(1)
	}
	// Give external-dns-docker time to connect to BIND9 and finish its
	// initial reconciliation pass (should complete in < 1 s).
	time.Sleep(5 * time.Second)
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

// ensureTestImage pulls testImage if it is not already present locally.
func ensureTestImage() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cli, err := dockerclient.NewClientWithOpts(
		dockerclient.FromEnv,
		dockerclient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return fmt.Errorf("docker client: %w", err)
	}
	defer cli.Close()

	// Skip the pull if the image is already cached locally.
	if _, _, err := cli.ImageInspectWithRaw(ctx, testImage); err == nil {
		return nil
	}

	rc, err := cli.ImagePull(ctx, testImage, dockerimage.PullOptions{})
	if err != nil {
		return fmt.Errorf("pull %s: %w", testImage, err)
	}
	defer rc.Close()
	_, err = io.Copy(io.Discard, rc)
	return err
}

// ---- Container helpers ----

// newDockerClient returns a host-daemon Docker client that is closed
// automatically when the test ends.
func newDockerClient(t *testing.T) *dockerclient.Client {
	t.Helper()
	cli, err := dockerclient.NewClientWithOpts(
		dockerclient.FromEnv,
		dockerclient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	t.Cleanup(func() { cli.Close() })
	return cli
}

// startLabeledContainer creates and starts a detached container carrying the
// given labels. Docker auto-generates the container name. The container is
// force-removed when the test ends.
func startLabeledContainer(t *testing.T, labels map[string]string) string {
	t.Helper()
	ctx := context.Background()
	cli := newDockerClient(t)

	resp, err := cli.ContainerCreate(ctx,
		&container.Config{
			Image:  testImage,
			Cmd:    []string{"sleep", "3600"},
			Labels: labels,
		},
		nil, nil, nil, "", // hostConfig, networkConfig, platform, name
	)
	if err != nil {
		t.Fatalf("ContainerCreate: %v", err)
	}

	t.Cleanup(func() {
		cli.ContainerRemove(
			context.Background(),
			resp.ID,
			container.RemoveOptions{Force: true},
		)
	})

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		t.Fatalf("ContainerStart: %v", err)
	}

	t.Logf("started container %s", resp.ID[:12])
	return resp.ID
}

// stopContainer immediately stops (but does not remove) the container.
// external-dns-docker will react to the resulting Docker event and delete
// the associated DNS records.
func stopContainer(t *testing.T, id string) {
	t.Helper()
	cli := newDockerClient(t)
	zero := 0
	if err := cli.ContainerStop(
		context.Background(),
		id,
		container.StopOptions{Timeout: &zero},
	); err != nil {
		t.Fatalf("ContainerStop %s: %v", id[:12], err)
	}
	t.Logf("stopped container %s", id[:12])
}

// ---- DNS helpers ----

// query sends a DNS query of the given type and returns the answer section.
func query(fqdn string, qtype uint16) []dns.RR {
	c := new(dns.Client)
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(fqdn), qtype)
	r, _, err := c.Exchange(m, bindAddr)
	if err != nil || r.Rcode != dns.RcodeSuccess {
		return nil
	}
	return r.Answer
}

// queryA queries the test BIND9 server and returns all A record values for fqdn.
func queryA(fqdn string) []string {
	var ips []string
	for _, rr := range query(fqdn, dns.TypeA) {
		if a, ok := rr.(*dns.A); ok {
			ips = append(ips, a.A.String())
		}
	}
	return ips
}

// queryAAAA returns all AAAA record values for fqdn.
func queryAAAA(fqdn string) []string {
	var ips []string
	for _, rr := range query(fqdn, dns.TypeAAAA) {
		if aaaa, ok := rr.(*dns.AAAA); ok {
			ips = append(ips, aaaa.AAAA.String())
		}
	}
	return ips
}

// queryCNAME returns the CNAME target for fqdn (empty string if none).
func queryCNAME(fqdn string) string {
	for _, rr := range query(fqdn, dns.TypeCNAME) {
		if cname, ok := rr.(*dns.CNAME); ok {
			return strings.TrimSuffix(cname.Target, ".")
		}
	}
	return ""
}

// queryTTL returns the TTL of the first A record for fqdn, or -1 if not found.
func queryTTL(fqdn string) int64 {
	for _, rr := range query(fqdn, dns.TypeA) {
		return int64(rr.Header().Ttl)
	}
	return -1
}

// assertARecord polls until fqdn resolves to wantIP or reconcileTimeout expires.
func assertARecord(t *testing.T, fqdn, wantIP string) {
	t.Helper()
	deadline := time.Now().Add(reconcileTimeout)
	for time.Now().Before(deadline) {
		for _, ip := range queryA(fqdn) {
			if ip == wantIP {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Errorf("A record %s → %s not found after %v (got %v)",
		fqdn, wantIP, reconcileTimeout, queryA(fqdn))
}

// assertAAAARecord polls until fqdn resolves to wantIP (AAAA) or reconcileTimeout expires.
func assertAAAARecord(t *testing.T, fqdn, wantIP string) {
	t.Helper()
	deadline := time.Now().Add(reconcileTimeout)
	for time.Now().Before(deadline) {
		for _, ip := range queryAAAA(fqdn) {
			if ip == wantIP {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Errorf("AAAA record %s → %s not found after %v (got %v)",
		fqdn, wantIP, reconcileTimeout, queryAAAA(fqdn))
}

// assertCNAMERecord polls until fqdn has a CNAME pointing to wantTarget.
func assertCNAMERecord(t *testing.T, fqdn, wantTarget string) {
	t.Helper()
	deadline := time.Now().Add(reconcileTimeout)
	for time.Now().Before(deadline) {
		if queryCNAME(fqdn) == wantTarget {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Errorf("CNAME record %s → %s not found after %v (got %q)",
		fqdn, wantTarget, reconcileTimeout, queryCNAME(fqdn))
}

// assertTTL polls until fqdn's A record has the given TTL.
func assertTTL(t *testing.T, fqdn string, wantTTL int64) {
	t.Helper()
	deadline := time.Now().Add(reconcileTimeout)
	for time.Now().Before(deadline) {
		if got := queryTTL(fqdn); got == wantTTL {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Errorf("A record %s TTL = %d, want %d after %v",
		fqdn, queryTTL(fqdn), wantTTL, reconcileTimeout)
}

// assertNoARecord polls until fqdn has no A records or reconcileTimeout expires.
func assertNoARecord(t *testing.T, fqdn string) {
	t.Helper()
	deadline := time.Now().Add(reconcileTimeout)
	for time.Now().Before(deadline) {
		if len(queryA(fqdn)) == 0 {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Errorf("expected no A record for %s after %v, still got %v",
		fqdn, reconcileTimeout, queryA(fqdn))
}

// ---- Tests ----

// TestContainerStart_CreatesARecord verifies that starting a container with
// external-dns.io labels causes external-dns-docker to create the A record.
func TestContainerStart_CreatesARecord(t *testing.T) {
	fqdn := "e2e-create.example.com"

	startLabeledContainer(t, map[string]string{
		"external-dns.io/hostname": fqdn,
		"external-dns.io/target":   "10.99.1.1",
	})

	assertARecord(t, fqdn, "10.99.1.1")
}

// TestContainerStop_DeletesARecord verifies that stopping a container causes
// external-dns-docker to delete the A record it previously created.
func TestContainerStop_DeletesARecord(t *testing.T) {
	fqdn := "e2e-delete.example.com"

	id := startLabeledContainer(t, map[string]string{
		"external-dns.io/hostname": fqdn,
		"external-dns.io/target":   "10.99.1.2",
	})
	assertARecord(t, fqdn, "10.99.1.2")

	stopContainer(t, id)
	assertNoARecord(t, fqdn)
}

// TestUnownedRecord_NotDeleted verifies that a DNS record without a companion
// ownership TXT record is never touched by external-dns-docker.
// manual.example.com is seeded in the initial zone file with no ownership TXT.
func TestUnownedRecord_NotDeleted(t *testing.T) {
	assertARecord(t, "manual.example.com", "10.0.0.1")
}

// TestContainerStart_CreatesAAAARecord verifies that an IPv6 target produces
// an AAAA record via RFC2136 dynamic update.
func TestContainerStart_CreatesAAAARecord(t *testing.T) {
	fqdn := "e2e-aaaa.example.com"

	startLabeledContainer(t, map[string]string{
		"external-dns.io/hostname": fqdn,
		"external-dns.io/target":   "2001:db8::99",
	})

	assertAAAARecord(t, fqdn, "2001:db8::99")
}

// TestContainerStart_CreatesCNAMERecord verifies that a hostname target
// (not an IP address) produces a CNAME record.
func TestContainerStart_CreatesCNAMERecord(t *testing.T) {
	fqdn := "e2e-cname.example.com"
	target := "backend.example.com"

	startLabeledContainer(t, map[string]string{
		"external-dns.io/hostname": fqdn,
		"external-dns.io/target":   target,
	})

	assertCNAMERecord(t, fqdn, target)
}

// TestTTLLabel_Respected verifies that the external-dns.io/ttl label sets
// the TTL of the created DNS record.
func TestTTLLabel_Respected(t *testing.T) {
	fqdn := "e2e-ttl.example.com"

	startLabeledContainer(t, map[string]string{
		"external-dns.io/hostname": fqdn,
		"external-dns.io/target":   "10.99.3.1",
		"external-dns.io/ttl":      "60",
	})

	assertARecord(t, fqdn, "10.99.3.1")
	assertTTL(t, fqdn, 60)
}

// TestMultipleContainers_SameHostname verifies that two containers carrying
// the same hostname label do not cause a crash and that at least one A record
// is created (behaviour for duplicate names is last-writer-wins in the plan).
func TestMultipleContainers_SameHostname(t *testing.T) {
	fqdn := "e2e-multi.example.com"

	startLabeledContainer(t, map[string]string{
		"external-dns.io/hostname": fqdn,
		"external-dns.io/target":   "10.99.4.1",
	})
	startLabeledContainer(t, map[string]string{
		"external-dns.io/hostname": fqdn,
		"external-dns.io/target":   "10.99.4.2",
	})

	// At least one of the two IPs should appear — the daemon must not crash.
	deadline := time.Now().Add(reconcileTimeout)
	for time.Now().Before(deadline) {
		ips := queryA(fqdn)
		for _, ip := range ips {
			if ip == "10.99.4.1" || ip == "10.99.4.2" {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Errorf("expected at least one A record for %s after %v", fqdn, reconcileTimeout)
}

// TestContainerRestart_RecordPreserved verifies that stopping and restarting
// a container causes the A record to be deleted and then re-created.
func TestContainerRestart_RecordPreserved(t *testing.T) {
	fqdn := "e2e-restart.example.com"
	ip := "10.99.5.1"

	cli := newDockerClient(t)
	ctx := context.Background()

	// Start container — record created.
	resp, err := cli.ContainerCreate(ctx,
		&container.Config{
			Image: testImage,
			Cmd:   []string{"sleep", "3600"},
			Labels: map[string]string{
				"external-dns.io/hostname": fqdn,
				"external-dns.io/target":   ip,
			},
		},
		nil, nil, nil, "",
	)
	if err != nil {
		t.Fatalf("ContainerCreate: %v", err)
	}
	t.Cleanup(func() {
		cli.ContainerRemove(context.Background(), resp.ID, container.RemoveOptions{Force: true})
	})

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		t.Fatalf("ContainerStart: %v", err)
	}
	assertARecord(t, fqdn, ip)

	// Stop — record deleted.
	zero := 0
	if err := cli.ContainerStop(ctx, resp.ID, container.StopOptions{Timeout: &zero}); err != nil {
		t.Fatalf("ContainerStop: %v", err)
	}
	assertNoARecord(t, fqdn)

	// Restart — record re-created.
	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		t.Fatalf("ContainerStart (restart): %v", err)
	}
	assertARecord(t, fqdn, ip)
}

// TestInvalidTarget_ContainerSkipped verifies that a container with an
// invalid A-record target (a non-IP value with explicit record-type=A) is
// skipped by the provider without crashing the daemon, and that subsequent
// valid containers continue to work.
func TestInvalidTarget_ContainerSkipped(t *testing.T) {
	// Start a container with an invalid target for an explicit A record.
	// The RFC2136 provider will warn and skip this endpoint.
	startLabeledContainer(t, map[string]string{
		"external-dns.io/hostname":    "e2e-invalid.example.com",
		"external-dns.io/target":      "not-a-valid-ip",
		"external-dns.io/record-type": "A",
	})

	// Give the daemon time to process the invalid container.
	time.Sleep(3 * time.Second)

	// The invalid container must not produce any A record.
	if ips := queryA("e2e-invalid.example.com"); len(ips) > 0 {
		t.Errorf("expected no A record for invalid target, got %v", ips)
	}

	// The daemon must still be running — a valid container started after the
	// invalid one should produce its record normally.
	fqdn := "e2e-after-invalid.example.com"
	startLabeledContainer(t, map[string]string{
		"external-dns.io/hostname": fqdn,
		"external-dns.io/target":   "10.99.6.1",
	})
	assertARecord(t, fqdn, "10.99.6.1")
}
