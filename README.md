# external-dns-docker

A DNS management daemon for Docker containers, modeled after
[kubernetes/external-dns](https://github.com/kubernetes-sigs/external-dns).

It watches the Docker socket for container lifecycle events, reads DNS-related
labels from containers, and reconciles the desired DNS state with an
[RFC2136](https://datatracker.ietf.org/doc/html/rfc2136)-compliant DNS server
(e.g. BIND9, PowerDNS).

---

## Quickstart

### 1. Configure your DNS server for RFC2136 updates

Your DNS server must allow dynamic updates (RFC2136) authenticated with a TSIG key.

Example BIND9 zone config:

```
zone "example.com" {
    type master;
    file "/etc/bind/zones/example.com.db";
    allow-update { key "external-dns-docker"; };
};

key "external-dns-docker" {
    algorithm hmac-sha256;
    secret "YOUR_BASE64_SECRET_HERE";
};
```

### 2. Run external-dns-docker

```bash
docker run --rm \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  external-dns-docker \
    --rfc2136-host=ns1.example.com \
    --rfc2136-zone=example.com. \
    --rfc2136-tsig-keyname=external-dns-docker \
    --rfc2136-tsig-secret=YOUR_BASE64_SECRET_HERE \
    --rfc2136-tsig-secret-alg=hmac-sha256
```

### 3. Label your containers

```bash
docker run -d \
  --label "external-dns.io/hostname=myapp.example.com" \
  --label "external-dns.io/target=203.0.113.10" \
  nginx
```

The daemon will create an `A` record for `myapp.example.com` pointing to `203.0.113.10`.

---

## Supported Labels

| Label | Required | Default | Description |
|-------|----------|---------|-------------|
| `external-dns.io/hostname` | Yes | — | DNS name to manage |
| `external-dns.io/target` | Yes | — | IP address or hostname to point to |
| `external-dns.io/ttl` | No | `300` | TTL in seconds |
| `external-dns.io/record-type` | No | auto-detected | `A`, `AAAA`, or `CNAME` |

### Record type auto-detection

The record type is inferred from the `target` value unless overridden:

| Target value | Record type |
|-------------|-------------|
| Valid IPv4 address (`1.2.3.4`) | `A` |
| Valid IPv6 address (`2001:db8::1`) | `AAAA` |
| Hostname (`backend.internal`) | `CNAME` |

### Multiple records per container

Use indexed labels to create more than one DNS record per container:

```bash
docker run -d \
  --label "external-dns.io/hostname-0=www.example.com" \
  --label "external-dns.io/target-0=203.0.113.10" \
  --label "external-dns.io/hostname-1=api.example.com" \
  --label "external-dns.io/target-1=203.0.113.11" \
  myimage
```

---

## Configuration

All flags can also be set via environment variables using the `EXTERNAL_DNS_` prefix
(hyphens → underscores, uppercase). CLI flags take precedence over env vars.

| Flag | Env Var | Default | Description |
|------|---------|---------|-------------|
| `--rfc2136-host` | `EXTERNAL_DNS_RFC2136_HOST` | — | DNS server hostname or IP (required) |
| `--rfc2136-port` | `EXTERNAL_DNS_RFC2136_PORT` | `53` | DNS server port |
| `--rfc2136-zone` | `EXTERNAL_DNS_RFC2136_ZONE` | — | Zone to manage (trailing dot required, required) |
| `--rfc2136-tsig-key` | `EXTERNAL_DNS_RFC2136_TSIG_KEY` | — | TSIG key name |
| `--rfc2136-tsig-secret` | `EXTERNAL_DNS_RFC2136_TSIG_SECRET` | — | TSIG secret (base64); mutually exclusive with `--rfc2136-tsig-secret-file` |
| `--rfc2136-tsig-secret-file` | `EXTERNAL_DNS_RFC2136_TSIG_SECRET_FILE` | — | Path to file containing base64 TSIG secret; mutually exclusive with `--rfc2136-tsig-secret` |
| `--rfc2136-tsig-alg` | `EXTERNAL_DNS_RFC2136_TSIG_ALG` | `hmac-sha256` | TSIG algorithm |
| `--rfc2136-min-ttl` | `EXTERNAL_DNS_RFC2136_MIN_TTL` | `0` | Minimum TTL to enforce (0 = disabled) |
| `--rfc2136-timeout` | `EXTERNAL_DNS_RFC2136_TIMEOUT` | `10s` | Timeout for RFC2136 DNS operations |
| `--docker-host` | `EXTERNAL_DNS_DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker socket or TCP address |
| `--docker-tls-ca` | `EXTERNAL_DNS_DOCKER_TLS_CA` | — | Path to Docker CA certificate |
| `--docker-tls-cert` | `EXTERNAL_DNS_DOCKER_TLS_CERT` | — | Path to Docker client TLS certificate |
| `--docker-tls-key` | `EXTERNAL_DNS_DOCKER_TLS_KEY` | — | Path to Docker client TLS key |
| `--interval` | `EXTERNAL_DNS_INTERVAL` | `60s` | Periodic reconciliation interval |
| `--debounce` | `EXTERNAL_DNS_DEBOUNCE` | `5s` | Quiet period after Docker events before reconciling |
| `--owner-id` | `EXTERNAL_DNS_OWNER_ID` | `external-dns-docker` | Ownership identifier for TXT records |
| `--dry-run` | `EXTERNAL_DNS_DRY_RUN` | `false` | Log planned changes without applying |
| `--once` | `EXTERNAL_DNS_ONCE` | `false` | Run one reconciliation cycle and exit |
| `--skip-preflight` | `EXTERNAL_DNS_SKIP_PREFLIGHT` | `false` | Skip startup DNS connectivity check |
| `--reconcile-backoff-base` | `EXTERNAL_DNS_RECONCILE_BACKOFF_BASE` | `5s` | Base duration for exponential backoff on failures |
| `--reconcile-backoff-max` | `EXTERNAL_DNS_RECONCILE_BACKOFF_MAX` | `5m` | Maximum backoff duration |
| `--health-port` | `EXTERNAL_DNS_HEALTH_PORT` | `8080` | Port for `/healthz`, `/readyz`, and `/metrics` (0 = disabled) |
| `--metrics-path` | `EXTERNAL_DNS_METRICS_PATH` | `/metrics` | HTTP path for Prometheus metrics |
| `--shutdown-timeout` | `EXTERNAL_DNS_SHUTDOWN_TIMEOUT` | `30s` | Maximum time to wait for graceful shutdown |
| `--log-level` | `EXTERNAL_DNS_LOG_LEVEL` | `info` | Log level: debug, info, warn, error |

---

## Ownership and Safety

To avoid accidentally modifying DNS records you manage by hand, `external-dns-docker`
tracks ownership via TXT record sidecars. For each managed record, a companion TXT
record is written:

```
external-dns-docker-owner.myapp.example.com  TXT  "heritage=external-dns-docker,external-dns-docker/owner=external-dns-docker"
```

Only records with a matching ownership TXT record are ever modified or deleted.
Manually-created records are left untouched.

---

## Production Deployment

Ready-to-use deployment files are provided in the `deploy/` directory:

| File | Description |
|------|-------------|
| `deploy/.env.example` | Documented environment variable template — copy to `deploy/.env` |
| `deploy/docker-compose.yml` | Production Compose file with resource limits, healthcheck, and secret file support |
| `deploy/swarm-stack.yml` | Docker Swarm stack with rolling update/rollback config and Docker secrets |

Quick start with Docker Compose:

```bash
# 1. Copy and configure the environment file
cp deploy/.env.example deploy/.env
$EDITOR deploy/.env

# 2. Create the TSIG secret file
echo -n "YOUR_BASE64_SECRET" > /run/secrets/tsig_secret
chmod 600 /run/secrets/tsig_secret

# 3. Start
docker compose -f deploy/docker-compose.yml up -d
```

See [docs/runbook.md](docs/runbook.md) for the full production runbook, including
monitoring, troubleshooting, and security hardening.

---

## Building

```bash
# Binary
make build          # output: bin/external-dns-docker

# Run tests
make test

# Docker image (requires buildx for multi-arch)
make docker
```

---

## License

Apache 2.0
