## Context

Single-zone RFC2136 provider serves one `--rfc2136-zone` per daemon. Users
with containers in multiple DNS zones must run multiple daemons. This
cross-cutting change adds a `MultiProvider` wrapper that composes N single-zone
`Provider` instances without modifying the controller, plan, source, or
endpoint packages.

## Goals / Non-Goals

- Goals: multi-zone in one daemon; backward compat for all single-zone configs; three config modes
- Non-Goals: dynamic zone reload at runtime; heterogeneous backends per zone; GUI/API

## Decisions

- **MultiProvider wraps []Provider**: zero changes to provider.Provider interface;
  both `*Provider` and `*MultiProvider` satisfy it via Records/ApplyChanges.
  A local `preflightProvider` interface in main.go handles Preflight for both types.

- **Longest-suffix zone routing**: `zoneFor()` finds the managed zone whose FQDN
  is the longest suffix of the endpoint DNSName. This correctly handles overlapping
  zones (e.g. `sub.example.com.` and `example.com.`). Unmatched endpoints are
  WARN-logged and skipped — not an error.

- **Parallel Records fan-out**: goroutines + WaitGroup; first error wins (no partial
  results). Deterministic result ordering not guaranteed across zones (consistent
  with a single large zone via AXFR).

- **Sequential Preflight**: fail-fast on first zone error gives a clear error message.
  Parallel preflight would save a few seconds but obscures which zone failed.

- **Three config modes, mutual exclusivity**: priority order — YAML file > env prefix > single-zone flags.
  Mixing modes exits with a descriptive error. This prevents silent misconfiguration.

- **YAML library**: `go.yaml.in/yaml/v2` already in go.mod (indirect); promoting
  to direct with no version bump.

- **ZoneConfig vs Config**: `ZoneConfig` is a new public type in the rfc2136 package
  matching the existing `Config` fields. `NewMulti` converts ZoneConfig → Config
  internally. `TSIGSecretFile` is resolved to `TSIGSecret` by the caller (main.go)
  before `NewMulti` is called.

## Risks / Trade-offs

- Parallel Records fan-out: if one zone is slow, it blocks the entire result.
  Mitigation: ctx cancellation propagates; per-zone timeout via `Timeout` field.

- Longest-suffix routing: a misconfigured zone list could silently swallow
  endpoints for unmanaged zones. Mitigation: WARN log for every unmatched endpoint.

## Migration Plan

Existing single-zone deployments need no changes. The daemon continues to accept
`--rfc2136-host` + `--rfc2136-zone` as before (Mode 1). To migrate to multi-zone,
operators switch to `--rfc2136-config-file` with a YAML listing both their old zone
and any new zones.

## Open Questions

None.
