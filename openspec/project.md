# Project: external-dns-docker

## Overview

`external-dns-docker` is a Go daemon that watches the Docker socket for container events,
reads DNS-related labels from containers, and reconciles the desired DNS state against a
DNS server. It is modeled after [kubernetes/external-dns](https://github.com/kubernetes-sigs/external-dns).

## Language & Runtime

- **Language**: Go 1.22+
- **Module name**: `github.com/bkero/external-dns-docker`

## Label Convention

All Docker labels use the prefix `external-dns.io/`:

| Label | Required | Description |
|-------|----------|-------------|
| `external-dns.io/hostname` | Yes | DNS name to manage |
| `external-dns.io/target` | Yes | IP or hostname target |
| `external-dns.io/ttl` | No (default 300) | TTL in seconds |
| `external-dns.io/record-type` | No (auto-detected) | A, AAAA, or CNAME |

Multi-record support via indexed labels: `external-dns.io/hostname-0`, `external-dns.io/hostname-1`, etc.

## Code Layout

```
cmd/
  external-dns-docker/     # CLI entry point (main.go + config)
pkg/
  endpoint/                # Endpoint data model
  source/                  # Source interface + Docker implementation
  provider/
    rfc2136/               # RFC2136 DNS provider
  plan/                    # Diff/change calculation engine
  controller/              # Reconciliation loop
test/
  integration/             # Integration tests (Docker Compose + BIND9)
```

## Provider Interface Pattern

Inspired by kubernetes/external-dns. Every DNS provider implements:

```go
type Provider interface {
    Records(ctx context.Context) ([]*endpoint.Endpoint, error)
    ApplyChanges(ctx context.Context, changes *plan.Changes) error
}
```

## Source Interface Pattern

```go
type Source interface {
    Endpoints(ctx context.Context) ([]*endpoint.Endpoint, error)
    AddEventHandler(ctx context.Context, handler func())
}
```

## Test Conventions

- **Unit tests**: alongside source files (`_test.go`), `go test ./...`
- **Coverage target**: ≥ 80% on all packages
- **Integration tests**: `test/integration/` with Docker Compose
- **Fakes**: `pkg/source/fake` and `pkg/provider/fake` for unit testing

## CI

- GitHub Actions workflows in `.github/workflows/`
- Pipeline stages: lint (golangci-lint), unit tests, integration tests, Docker build
- Multi-arch Docker image: `linux/amd64`, `linux/arm64`

## Key Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/miekg/dns` | DNS wire protocol, RFC2136 |
| `github.com/docker/docker/client` | Docker socket client |
| `log/slog` | Structured JSON logging (stdlib) |

## Architecture Notes

- **Stateless**: DNS server is the source of truth; no local state DB
- **Ownership**: TXT record sidecars track which records this daemon manages
- **Hybrid event model**: Docker Events API for fast reaction + periodic reconciliation loop for drift detection
- **Container opt-in**: Container MUST have `external-dns.io/hostname` label to be considered
