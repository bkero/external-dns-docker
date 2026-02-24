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
| `--rfc2136-host` | `EXTERNAL_DNS_RFC2136_HOST` | — | DNS server hostname or IP |
| `--rfc2136-port` | `EXTERNAL_DNS_RFC2136_PORT` | `53` | DNS server port |
| `--rfc2136-zone` | `EXTERNAL_DNS_RFC2136_ZONE` | — | Zone to manage (trailing dot required) |
| `--rfc2136-tsig-keyname` | `EXTERNAL_DNS_RFC2136_TSIG_KEYNAME` | — | TSIG key name |
| `--rfc2136-tsig-secret` | `EXTERNAL_DNS_RFC2136_TSIG_SECRET` | — | TSIG secret (base64) |
| `--rfc2136-tsig-secret-alg` | `EXTERNAL_DNS_RFC2136_TSIG_SECRET_ALG` | `hmac-sha256` | TSIG algorithm |
| `--rfc2136-min-ttl` | `EXTERNAL_DNS_RFC2136_MIN_TTL` | `0` | Minimum TTL to enforce |
| `--docker-host` | `EXTERNAL_DNS_DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker socket or TCP address |
| `--interval` | `EXTERNAL_DNS_INTERVAL` | `60s` | Reconciliation interval |
| `--owner-id` | `EXTERNAL_DNS_OWNER_ID` | `external-dns-docker` | Ownership identifier for TXT records |
| `--dry-run` | `EXTERNAL_DNS_DRY_RUN` | `false` | Log planned changes without applying |
| `--once` | `EXTERNAL_DNS_ONCE` | `false` | Run one reconciliation cycle and exit |
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
