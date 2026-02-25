# Tasks: add-core-dns-management

## 1. Project Scaffolding
- [ ] 1.1 Initialize Go module (`go.mod`, `go.sum`)
- [ ] 1.2 Create `Makefile` with `build`, `test`, `lint`, `docker` targets
- [ ] 1.3 Create `Dockerfile` (multi-stage, multi-arch)
- [ ] 1.4 Write `README.md` with quickstart and label reference

## 2. Endpoint Data Model
- [ ] 2.1 Define `Endpoint` struct (`DNSName`, `Targets []string`, `RecordType`, `TTL`, `Labels`)
- [ ] 2.2 Implement `RecordType` auto-detection (IPv4→A, IPv6→AAAA, hostname→CNAME)
- [ ] 2.3 Add endpoint helpers and `String()` method
- [ ] 2.4 Write unit tests (`pkg/endpoint/`)

## 3. Source Interface
- [ ] 3.1 Define `Source` interface (`Endpoints`, `AddEventHandler`)
- [ ] 3.2 Create fake/in-memory source for unit testing

## 4. Docker Source Implementation
- [ ] 4.1 Implement Docker client wrapper (`pkg/source/docker.go`)
- [ ] 4.2 List running containers and extract labels
- [ ] 4.3 Parse `external-dns.io/*` labels into Endpoints
- [ ] 4.4 Support multi-record indexed labels (`hostname-0`, `hostname-1`)
- [ ] 4.5 Subscribe to Docker Events API (start/stop/die/update)
- [ ] 4.6 Write unit tests with mock Docker client

## 5. Provider Interface
- [ ] 5.1 Define `Provider` interface (`Records`, `ApplyChanges`)
- [ ] 5.2 Define `Changes` struct (Create, UpdateOld, UpdateNew, Delete slices)
- [ ] 5.3 Create fake/in-memory provider for unit testing

## 6. RFC2136 Provider Implementation
- [ ] 6.1 Implement AXFR zone transfer for listing records
- [ ] 6.2 Implement DNS UPDATE (RFC2136) for applying changes
- [ ] 6.3 Add TSIG authentication support
- [ ] 6.4 Add configuration (host, port, zone, TSIG key/secret/algorithm, min-TTL)
- [ ] 6.5 Write unit tests for RFC2136 provider

## 7. Plan / Diff Engine
- [ ] 7.1 Implement `Calculate` to diff desired vs current endpoints
- [ ] 7.2 Produce `Changes` struct (Create/UpdateOld+UpdateNew/Delete)
- [ ] 7.3 Implement TXT ownership record generation and filtering
- [ ] 7.4 Ensure only owned records are modified or deleted
- [ ] 7.5 Write unit tests for plan engine

## 8. Controller / Reconciliation Loop
- [ ] 8.1 Implement periodic reconciliation loop (configurable interval, default 60s)
- [ ] 8.2 Trigger reconciliation on Docker events (via `AddEventHandler`)
- [ ] 8.3 Add event debouncing window (configurable)
- [ ] 8.4 Implement dry-run mode (log changes without applying)
- [ ] 8.5 Implement once mode (one reconciliation cycle then exit)
- [ ] 8.6 Write unit tests for controller

## 9. CLI Entry Point
- [ ] 9.1 Create `cmd/external-dns-docker/main.go`
- [ ] 9.2 Define all CLI flags with `EXTERNAL_DNS_` env var overrides
- [ ] 9.3 Wire up source, provider, and controller
- [ ] 9.4 Configure structured JSON logging via `log/slog`
- [ ] 9.5 Add graceful shutdown (SIGTERM/SIGINT)

## 10. Unit Tests
- [ ] 10.1 Ensure ≥80% coverage on `pkg/endpoint/`
- [ ] 10.2 Ensure ≥80% coverage on `pkg/source/`
- [ ] 10.3 Ensure ≥80% coverage on `pkg/provider/rfc2136/`
- [ ] 10.4 Ensure ≥80% coverage on `pkg/plan/`
- [ ] 10.5 Ensure ≥80% coverage on `pkg/controller/`

## 11. Integration / E2E Tests
- [ ] 11.1 Create Docker Compose setup (`test/integration/docker-compose.yml`)
- [ ] 11.2 Configure BIND9 with RFC2136 updates enabled
- [ ] 11.3 Write test scenarios: container start → DNS record created
- [ ] 11.4 Write test scenarios: container stop → DNS record deleted
- [ ] 11.5 Write test scenarios: ownership — unowned records not deleted

## 12. CI Setup
- [ ] 12.1 Create `.github/workflows/ci.yml` (lint, test, build)
- [ ] 12.2 Add golangci-lint configuration (`.golangci.yml`)
- [ ] 12.3 Configure multi-arch Docker build (`linux/amd64`, `linux/arm64`) via buildx
- [ ] 12.4 Add semantic release / auto-tagging on merge to main
