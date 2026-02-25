# Change: Multi-Zone RFC2136 Provider

## Why

Today `external-dns-docker` manages a single DNS zone per daemon instance.
Users managing containers across multiple DNS zones must run a separate daemon
per zone, duplicating configuration and resources. A single daemon should be
able to manage N zones, each with its own RFC2136 server and TSIG credentials.

## What Changes

- Add `ZoneConfig` struct and `MultiProvider` wrapper in `pkg/provider/rfc2136/multizone.go`
- `MultiProvider` fans out `Records()` in parallel, routes `ApplyChanges()` by longest-suffix match, and runs `Preflight()` sequentially per zone
- Add `--rfc2136-config-file` flag for YAML multi-zone config (Mode 3)
- Add env-prefix mode: `EXTERNAL_DNS_RFC2136_ZONE_<NAME>_<FIELD>` (Mode 2)
- Existing single-zone flags remain unchanged and fully backward compatible (Mode 1)
- Mutual exclusivity enforced: mixing modes exits with error
- Add `deploy/zones.example.yaml` annotated two-zone example
- Update `README.md` with Multi-Zone Configuration section
- Update `docs/runbook.md` with Multi-Zone Deployments subsection

## Impact

- Affected specs: rfc2136-provider
- Affected code: `pkg/provider/rfc2136/`, `cmd/external-dns-docker/main.go`, `deploy/`, `README.md`, `docs/runbook.md`
- No changes to `controller`, `plan`, `source`, or `endpoint` packages
- No breaking changes — existing single-zone deployments continue to work unchanged
