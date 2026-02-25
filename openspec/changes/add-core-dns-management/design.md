# Design: add-core-dns-management

## Context

`external-dns-docker` is a greenfield project. There are no legacy constraints.
The design borrows heavily from [kubernetes/external-dns](https://github.com/kubernetes-sigs/external-dns)
but targets Docker instead of Kubernetes as the container runtime source.

Primary stakeholders: self-hosted Docker operators who want automatic DNS management
without running Kubernetes.

## Goals / Non-Goals

- **Goals**:
  - Watch Docker container lifecycle events
  - Parse DNS labels from containers
  - Reconcile desired DNS state with an RFC2136-compliant DNS server
  - Support multiple DNS records per container (indexed labels)
  - Ownership tracking to avoid stomping on manually-managed records
  - Dry-run and once modes for safe operation
- **Non-Goals**:
  - Docker Swarm / Docker Compose stack awareness (future work)
  - Multi-provider fan-out (future work)
  - Web UI or REST API
  - Local DNS caching or serving

## Decisions

### Event Model: Hybrid (Events + Periodic Reconciliation)

**Decision**: Use Docker Events API for fast reaction, combined with a configurable
periodic reconciliation loop (default 60s interval).

**Rationale**: Events alone miss drift caused by out-of-band DNS changes (manual
edits, DNS server restarts). A pure polling model is slower to react. Hybrid gives
both correctness (via polling) and responsiveness (via events).

**Alternatives considered**:
- Pure polling: simple but slow (up to 60s reaction time)
- Pure events: fast but misses drift and requires reliable event stream

### DNS Library: `github.com/miekg/dns`

**Decision**: Use `miekg/dns` for all DNS wire protocol work (AXFR, RFC2136 UPDATE, TSIG).

**Rationale**: Same library used by kubernetes/external-dns RFC2136 provider.
Battle-tested, well-maintained, comprehensive RFC support.

**Alternatives considered**:
- Go stdlib `net` package: insufficient for AXFR/RFC2136
- Custom DNS client: unnecessary complexity

### Docker Client: `github.com/docker/docker/client`

**Decision**: Use the official Docker Go SDK.

**Rationale**: Official, stable API. Supports both Unix socket and TCP connections,
including TLS. Provides typed event structs.

### Ownership Tracking: TXT Record Sidecars

**Decision**: For each managed DNS record `foo.example.com`, write a companion TXT
record `external-dns-docker-owner.foo.example.com` containing the daemon's owner ID
(configurable, default: `external-dns-docker`).

**Rationale**: Same ownership model as kubernetes/external-dns. Ensures the daemon
only modifies or deletes records it originally created. Prevents accidental deletion
of manually-managed records.

**Format**: `"heritage=external-dns-docker,external-dns-docker/owner=<owner-id>"`

**Alternatives considered**:
- Local state file: fragile (lost on restart), doesn't survive host migration
- DNS record comments: not universally supported by DNS servers
- No ownership: too dangerous for production use

### State: Stateless (DNS Server as Source of Truth)

**Decision**: No local database. Always fetch current state from the DNS server via
AXFR at the start of each reconciliation cycle.

**Rationale**: Simpler ops (no state migration on upgrade), correct by design
(always reflects actual DNS state), works naturally with DNS server HA setups.

**Trade-off**: AXFR on every cycle adds DNS server load. Acceptable for typical
home/small-org deployments. Large zones may need caching (future work).

### Container Opt-In: Label-Based

**Decision**: Containers MUST have at least `external-dns.io/hostname` to be managed.
No opt-out mechanism needed (absence of label = opt-out).

**Rationale**: Explicit is safer than implicit. Prevents accidental DNS record
creation for containers that don't expect it.

### RecordType Auto-Detection

**Decision**: Infer record type from target value:
- Valid IPv4 address → `A`
- Valid IPv6 address → `AAAA`
- Anything else → `CNAME`

Can be overridden with `external-dns.io/record-type` label.

### Configuration: Flags + Env Vars

**Decision**: All configuration exposed as CLI flags, with `EXTERNAL_DNS_` prefixed
environment variable overrides (e.g., `--rfc2136-host` ↔ `EXTERNAL_DNS_RFC2136_HOST`).

**Rationale**: Standard 12-factor app pattern. Easy to use in Docker/Kubernetes.

### Logging: `log/slog` (stdlib)

**Decision**: Use Go 1.21+ `log/slog` for structured JSON logging.

**Rationale**: No external dependency needed. JSON output is machine-parseable.
Configurable log level (debug/info/warn/error).

## Risks / Trade-offs

- **AXFR load**: Full zone transfer on every cycle. Mitigation: configurable interval
  (default 60s); future caching layer if needed.
- **Docker socket security**: Daemon needs read access to Docker socket. Mitigation:
  document principle of least privilege; consider read-only socket mount.
- **DNS server downtime**: If RFC2136 server is unreachable, reconciliation fails.
  Mitigation: retry with backoff; log errors; don't crash.
- **TSIG key management**: TSIG secrets must be provided via config. Mitigation:
  support env vars and file-based secrets.

## Migration Plan

Greenfield project — no migration needed.

## Open Questions

- Should we support multiple zones per daemon instance? (Defer to future work.)
- Should we support Docker Swarm service labels? (Defer to future work.)
- Prefix for ownership TXT records: `external-dns-docker-owner.` vs `_external-dns-docker.`?
  The leading-underscore form is more RFC-compliant but less readable. Decision: use
  `external-dns-docker-owner.` prefix for now, matching external-dns behavior.
