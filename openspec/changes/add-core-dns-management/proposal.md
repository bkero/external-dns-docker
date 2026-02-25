# Change: Add Core DNS Management

## Why

No tooling currently exists to manage DNS records based on Docker container labels,
analogous to how Traefik dynamically manages HTTP routing from container metadata.
Operators running Docker workloads must manually create and maintain DNS records,
which is error-prone and breaks automation pipelines. This project fills that gap by
implementing a DNS management daemon modeled after kubernetes/external-dns.

## What Changes

- **New project**: Initial implementation of `external-dns-docker`
- Add Endpoint data model (`pkg/endpoint/`)
- Add Source interface and Docker container source (`pkg/source/`)
- Add Provider interface and RFC2136 DNS provider (`pkg/provider/rfc2136/`)
- Add Plan/diff engine for change calculation (`pkg/plan/`)
- Add Controller/reconciliation loop (`pkg/controller/`)
- Add CLI daemon entry point (`cmd/external-dns-docker/`)
- Add unit tests for all packages (≥80% coverage)
- Add integration/e2e tests with Docker Compose + BIND9
- Add CI pipeline (GitHub Actions: lint, test, build, docker push)

## Impact

- Affected specs: endpoint, docker-source, provider, plan, controller, configuration, testing, ci
- Affected code: entire project (greenfield — no existing code)
- Breaking changes: none (new project)
