# external-dns-docker — Production Runbook

This guide covers deployment verification, common failure modes, monitoring,
performance tuning, and security hardening for `external-dns-docker` in production.

---

## Table of Contents

1. [Pre-flight Checklist](#pre-flight-checklist)
2. [Common Failure Modes](#common-failure-modes)
3. [Prometheus Alerting Rules](#prometheus-alerting-rules)
4. [Log Parsing Cheat Sheet](#log-parsing-cheat-sheet)
5. [Performance Tuning](#performance-tuning)
6. [Security Hardening Checklist](#security-hardening-checklist)

---

## Pre-flight Checklist

Complete these checks before deploying to production.

### DNS Server

- [ ] The RFC2136 server is reachable from the host running `external-dns-docker`
      (`dig SOA example.com. @ns1.example.com`).
- [ ] The zone is authoritative on the target server (type `master` / `primary`).
- [ ] A TSIG key with `allow-update` permission has been created and configured
      on the DNS server.
- [ ] The TSIG key name, secret (base64), and algorithm match in both the DNS
      server config and the `external-dns-docker` flags/env.
- [ ] Startup preflight passes: run with `--skip-preflight=false` (default) and
      confirm `"DNS preflight check passed"` appears in the logs.

### Docker

- [ ] The Docker socket (`/var/run/docker.sock`) is accessible from the container
      (bind-mount with `:ro`).
- [ ] The daemon user has read access to the socket or the container runs with
      supplementary group `docker`.
- [ ] For remote Docker daemons, TLS client certificates are in place
      (`--docker-tls-ca`, `--docker-tls-cert`, `--docker-tls-key`).

### Ownership

- [ ] `--owner-id` is set to a unique string per environment (e.g. `prod-dc1`).
      Multiple instances managing the same zone **must** have different owner IDs
      to avoid ownership conflicts.

---

## Common Failure Modes

### Records not being created after container start

**Symptoms:** Container is running with correct labels; no A/AAAA/CNAME record appears.

**Checks:**

1. Confirm the hostname label is a valid RFC 1123 DNS name (no underscores or
   special characters). Malformed labels are skipped with a `WARN` log.
2. Confirm the target label is a valid IP address or hostname.
   `999.999.999.999` is rejected as an invalid IPv4.
3. Verify the container has **both** `external-dns.io/hostname` and
   `external-dns.io/target` labels set.
4. Check the `--rfc2136-zone` flag — the hostname must be within the managed zone.
5. Look for `reconciliation failed` errors in the logs; the daemon may be in
   exponential backoff (`backing off before next reconciliation`).

### Records not being deleted after container stop

**Symptoms:** Container has stopped but the DNS record persists.

**Checks:**

1. The TXT ownership record (`external-dns-docker-owner.<hostname>`) must exist.
   If it was manually deleted, `external-dns-docker` cannot identify ownership
   and will not delete the A/AAAA/CNAME record.
2. Verify the `--owner-id` matches what was used when the record was created.
3. If `--dry-run=true`, changes are logged but never applied.

### TSIG authentication failures

**Symptoms:** Logs contain `rcode NOTAUTH` or `tsig: bad time`.

**Checks:**

1. Confirm the TSIG key name, secret, and algorithm match the DNS server config
   exactly (including case and trailing dot handling).
2. Check clock skew — TSIG is time-sensitive (±5 minutes default window).
   Ensure NTP is running on both the daemon host and the DNS server.
3. Verify the secret is raw base64 (not double-encoded).
4. Test manually: `nsupdate -k /path/to/tsig.key`.

### Docker event stream disconnects

**Symptoms:** Logs contain `docker event stream error`; containers start/stop
without triggering reconciliation.

**Checks:**

1. The daemon reconnects automatically after `--debounce` (default 5s). This is
   informational unless reconnects happen continuously.
2. For remote Docker daemons, check TLS certificate expiry.
3. Docker daemon upgrades can cause temporary socket interruptions; these resolve
   on reconnect.

### Daemon hangs / does not exit on SIGTERM

**Symptoms:** `docker stop` takes longer than `--shutdown-timeout` (default 30s).

**Checks:**

1. Ensure `--rfc2136-timeout` (default 10s) is less than
   `--shutdown-timeout` (default 30s) so in-flight DNS operations can complete.
2. If the DNS server is unreachable, the in-flight operation will time out after
   `--rfc2136-timeout`; the daemon will then exit cleanly.

### Daemon stuck in backoff

**Symptoms:** Logs show `backing off before next reconciliation` with increasing
durations; no records are being updated.

**Checks:**

1. Identify the root cause from the `reconciliation failed` log line above the
   backoff message.
2. Common causes: DNS server unreachable, TSIG misconfiguration, zone transfer
   (AXFR) rejected.
3. Fixing the underlying issue will cause the next reconciliation to succeed and
   reset the backoff immediately.

---

## Prometheus Alerting Rules

Scrape the `/metrics` endpoint (default port 8080) with Prometheus.

```yaml
groups:
  - name: external-dns-docker
    interval: 60s
    rules:

      # Alert when the reconciliation error rate exceeds 50% over 10 minutes
      - alert: ExternalDnsDockerhighReconcileErrorRate
        expr: |
          rate(external_dns_docker_reconciliations_total{result="error"}[10m])
          /
          rate(external_dns_docker_reconciliations_total[10m]) > 0.5
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "external-dns-docker reconciliation error rate > 50%"
          description: >
            More than half of reconciliation cycles are failing.
            Check DNS server connectivity and TSIG credentials.

      # Alert when no successful reconciliation has completed in 10 minutes
      - alert: ExternalDnsDockerReconcileStalled
        expr: |
          increase(external_dns_docker_reconciliations_total{result="success"}[10m]) == 0
        for: 10m
        labels:
          severity: critical
        annotations:
          summary: "external-dns-docker has not completed a successful reconciliation"
          description: >
            No successful reconciliation in the last 10 minutes.
            The daemon may be stuck in backoff or unable to reach the DNS server.

      # Alert when the number of managed records drops unexpectedly
      - alert: ExternalDnsDockerRecordCountDrift
        expr: |
          external_dns_docker_records_managed < 1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "external-dns-docker manages zero records"
          description: >
            No DNS records are currently managed. This may be expected during
            initial deployment or may indicate all containers have stopped.

      # Alert on sustained high reconciliation latency
      - alert: ExternalDnsDockerSlowReconcile
        expr: |
          histogram_quantile(0.95,
            rate(external_dns_docker_reconciliation_duration_seconds_bucket[10m])
          ) > 30
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "external-dns-docker p95 reconciliation duration > 30s"
          description: >
            The 95th-percentile reconciliation duration exceeds 30 seconds,
            suggesting DNS server latency or a large zone transfer.
```

### Key metrics reference

| Metric | Type | Description |
|--------|------|-------------|
| `external_dns_docker_reconciliations_total{result}` | counter | Reconciliation attempts by result (`success`/`error`) |
| `external_dns_docker_reconciliation_duration_seconds` | histogram | Reconciliation wall-clock time |
| `external_dns_docker_records_managed` | gauge | Records currently owned by this instance |
| `external_dns_docker_dns_operations_total{op,result}` | counter | DNS create/update/delete operations by result |
| `external_dns_docker_docker_events_total` | counter | Docker container lifecycle events received |

---

## Log Parsing Cheat Sheet

All logs are JSON (structured slog) written to stderr. Key fields:

| Field | Meaning |
|-------|---------|
| `level` | `DEBUG`, `INFO`, `WARN`, `ERROR` |
| `msg` | Human-readable message |
| `err` | Error detail when `level=ERROR` or `level=WARN` |
| `container` | Short (12-char) container ID |
| `hostname` | DNS name from container label |
| `target` | Record target from container label |
| `backoff` | Backoff duration before next reconciliation attempt |
| `consecutive_errors` | Number of consecutive reconciliation failures |
| `rfc2136-host` | DNS server address (logged at startup) |
| `rfc2136-zone` | Zone being managed (logged at startup) |

### Useful log patterns

```bash
# All reconciliation errors
docker logs external-dns-docker 2>&1 | jq 'select(.msg == "reconciliation failed")'

# Containers skipped due to invalid labels
docker logs external-dns-docker 2>&1 | jq 'select(.level == "WARN" and (.msg | startswith("container has invalid")))'

# Current backoff state
docker logs external-dns-docker 2>&1 | jq 'select(.msg == "backing off before next reconciliation")'

# DNS operation results
docker logs external-dns-docker 2>&1 | jq 'select(.msg | contains("dns update"))'
```

---

## Performance Tuning

### `--interval` (default: 60s)

Full reconciliation period. Increase for stable environments; decrease if you
need faster eventual consistency after manual DNS changes. Event-driven
reconciliation (Docker socket) fires independently of this timer.

### `--debounce` (default: 5s)

Quiet period after a Docker event before reconciling. Prevents a burst of
container starts from triggering one reconciliation per container. Set lower
(e.g. `1s`) in environments with tight SLAs; set higher (e.g. `30s`) in
environments with frequent rolling deployments.

### `--rfc2136-timeout` (default: 10s)

Per-operation DNS timeout (AXFR + UPDATE). Increase if zone transfers are large
or DNS server latency is high. Keep it below `--shutdown-timeout` / 3 to allow
clean shutdown.

### `--rfc2136-min-ttl` (default: 0 = disabled)

Enforces a floor on record TTLs. Set to `60` or higher to reduce DNS resolver
load in environments where containers use very short TTLs.

### `--reconcile-backoff-base` / `--reconcile-backoff-max` (defaults: 5s / 5m)

Controls exponential backoff on consecutive failures. Tune `backoff-base`
downward (e.g. `2s`) for faster recovery after transient DNS outages; keep
`backoff-max` high enough to avoid hammering a degraded DNS server.

---

## Security Hardening Checklist

- [ ] **TSIG secret via file**: use `--rfc2136-tsig-secret-file` pointing to a
      file with mode `0600` rather than passing the secret as a flag or env var
      (visible in `ps` output and `/proc/<pid>/environ`).
- [ ] **Docker socket**: mount read-only (`:ro`). The daemon only calls
      `ContainerList` and `Events` — it does not need write access.
- [ ] **Read-only root filesystem**: add `read_only: true` in Compose / Swarm.
      The daemon writes no files at runtime.
- [ ] **No new privileges**: add `security_opt: [no-new-privileges:true]` in
      Compose / Swarm.
- [ ] **Root in container, read-only socket**: the container runs as root (required
      to read the Docker socket). Mount the socket read-only (`:ro`) to restrict
      what the container can do with that access.
- [ ] **Network policy**: restrict outbound access to only the DNS server's IP
      and port (TCP 53 for UPDATE, AXFR). No inbound is required except for the
      health-check port from monitoring systems.
- [ ] **Resource limits**: set CPU and memory limits (see `deploy/docker-compose.yml`
      for recommended values) to prevent resource exhaustion.
- [ ] **Health check port**: bind the health check server to `127.0.0.1` or a
      dedicated monitoring network interface. Do not expose it publicly.
- [ ] **Secrets rotation**: rotate the TSIG secret by updating the secret file
      and restarting the container. No zero-downtime rotation is needed since
      DNS updates are idempotent.
- [ ] **Log redaction**: the daemon never logs the TSIG secret value. Verify
      your log aggregator does not persist `EXTERNAL_DNS_RFC2136_TSIG_SECRET`
      from container inspect output.
