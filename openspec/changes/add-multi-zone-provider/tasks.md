## 1. Provider Package

- [x] 1.1 Create `pkg/provider/rfc2136/multizone.go` with `ZoneConfig`, `zoneEntry`, `MultiProvider`
- [x] 1.2 Implement `NewMulti(configs []ZoneConfig, log *slog.Logger) *MultiProvider`
- [x] 1.3 Implement `Records(ctx)` — parallel fan-out via goroutines + WaitGroup
- [x] 1.4 Implement `ApplyChanges(ctx, changes)` — split by zone, dispatch per zone
- [x] 1.5 Implement `Preflight(ctx)` — sequential SOA check per zone
- [x] 1.6 Implement `zoneFor(dnsName)` — longest-suffix match helper
- [x] 1.7 Write `pkg/provider/rfc2136/multizone_test.go` with full coverage

## 2. Main Binary

- [x] 2.1 Add `--rfc2136-config-file` / `EXTERNAL_DNS_RFC2136_CONFIG_FILE` flag
- [x] 2.2 Implement `loadZoneConfigsFromFile(path)` — read + unmarshal YAML, resolve secret file, validate
- [x] 2.3 Implement `loadZoneConfigsFromEnv()` — scan env, group by NAME, sort, validate
- [x] 2.4 Add mode detection and mutual-exclusivity validation in `main()`
- [x] 2.5 Add `preflightProvider` local interface; call preflight through it
- [x] 2.6 Write `main_test.go` tests for `loadZoneConfigsFromEnv` and `loadZoneConfigsFromFile`

## 3. Deploy and Docs

- [x] 3.1 Create `deploy/zones.example.yaml` with annotated two-zone example
- [x] 3.2 Update `README.md` — add Multi-Zone Configuration section
- [x] 3.3 Update `docs/runbook.md` — add Multi-Zone Deployments subsection

## 4. Verification

- [x] 4.1 `go test ./...` passes
- [x] 4.2 `go build ./...` succeeds
