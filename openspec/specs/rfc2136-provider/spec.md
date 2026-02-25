# rfc2136-provider Specification

## Purpose
TBD - created by archiving change add-multi-zone-provider. Update Purpose after archive.
## Requirements
### Requirement: Multi-Zone Configuration via YAML File
The system SHALL accept a `--rfc2136-config-file` flag (or `EXTERNAL_DNS_RFC2136_CONFIG_FILE`
env var) pointing to a YAML file that defines one or more zones, each with its own
RFC2136 host, port, TSIG credentials, min-ttl, and timeout. When this flag is set,
the daemon SHALL manage all listed zones through a single `MultiProvider` instance.

#### Scenario: YAML file with two zones loads successfully
- **WHEN** `--rfc2136-config-file=zones.yaml` is provided with two valid zone entries
- **THEN** the daemon starts and manages both zones

#### Scenario: YAML file missing required host field
- **WHEN** a zone entry in the YAML file omits `host`
- **THEN** the daemon exits with a descriptive error before starting

#### Scenario: YAML file with tsig-secret and tsig-secret-file both set
- **WHEN** a zone entry specifies both `tsig-secret` and `tsig-secret-file`
- **THEN** the daemon exits with a mutually-exclusive error

### Requirement: Multi-Zone Configuration via Environment Variable Prefix
The system SHALL detect `EXTERNAL_DNS_RFC2136_ZONE_<NAME>_<FIELD>` environment
variables and group them by `<NAME>` to construct per-zone configurations. Zones
SHALL be processed in alphabetical order by `<NAME>` for deterministic behaviour.
`TSIG_SECRET_FILE` SHALL be resolved to the secret value at startup.

#### Scenario: Two zones defined via env prefix
- **WHEN** `EXTERNAL_DNS_RFC2136_ZONE_PROD_HOST`, `EXTERNAL_DNS_RFC2136_ZONE_PROD_ZONE`,
  `EXTERNAL_DNS_RFC2136_ZONE_STAGING_HOST`, and `EXTERNAL_DNS_RFC2136_ZONE_STAGING_ZONE`
  are set
- **THEN** the daemon manages both `PROD` and `STAGING` zones via a single `MultiProvider`

#### Scenario: No matching env vars
- **WHEN** no `EXTERNAL_DNS_RFC2136_ZONE_*` env vars are present
- **THEN** env-prefix mode is not activated; single-zone mode is checked instead

#### Scenario: Missing required HOST field in env prefix
- **WHEN** `EXTERNAL_DNS_RFC2136_ZONE_PROD_ZONE` is set but `EXTERNAL_DNS_RFC2136_ZONE_PROD_HOST` is missing
- **THEN** the daemon exits with an error naming the zone and missing field

### Requirement: Mode Mutual Exclusivity
The system SHALL enforce that exactly one configuration mode is active. Mixing
`--rfc2136-config-file` with single-zone flags (`--rfc2136-host`, `--rfc2136-zone`,
`--rfc2136-tsig-*`), or mixing env-prefix vars with single-zone flags, SHALL cause
the daemon to exit with a descriptive error.

#### Scenario: YAML file and single-zone host flag both set
- **WHEN** `--rfc2136-config-file` and `--rfc2136-host` are both provided
- **THEN** the daemon exits with a mode-conflict error

#### Scenario: Env prefix vars and single-zone host flag both set
- **WHEN** `EXTERNAL_DNS_RFC2136_ZONE_PROD_HOST` and `--rfc2136-host` are both set
- **THEN** the daemon exits with a mode-conflict error

### Requirement: Multi-Zone Records Fan-out
The `MultiProvider` SHALL fetch DNS records from all managed zones in parallel
and merge the results into a single endpoint list.

#### Scenario: Records from two zones are merged
- **WHEN** zone A has endpoint `a.zoneA.` and zone B has endpoint `b.zoneB.`
- **THEN** `Records()` returns both endpoints

#### Scenario: One zone AXFR error returns error
- **WHEN** one sub-provider returns an error from `Records()`
- **THEN** `MultiProvider.Records()` returns that error

### Requirement: Multi-Zone Change Routing
The `MultiProvider` SHALL route each endpoint in a `Changes` set to the sub-provider
whose zone is the longest suffix match of the endpoint's DNSName. Endpoints with no
matching zone SHALL be logged at WARN level and skipped.

#### Scenario: Endpoint routed to correct zone
- **WHEN** zones `[example.com., bke.ro.]` are managed and a Create for `app.example.com` arrives
- **THEN** the change is dispatched only to the `example.com.` provider

#### Scenario: Unmatched endpoint is skipped with WARN
- **WHEN** an endpoint for `unknown.other.tld` arrives and no zone matches
- **THEN** a WARN log is emitted and the endpoint is not sent to any provider

#### Scenario: Zone with no changes is not called
- **WHEN** only `example.com.` zone has changes
- **THEN** `ApplyChanges` is not called on the `bke.ro.` provider

### Requirement: Multi-Zone Preflight
The `MultiProvider` SHALL run `Preflight()` sequentially for each zone and return
the first error encountered.

#### Scenario: All zones pass preflight
- **WHEN** all sub-providers return nil from `Preflight()`
- **THEN** `MultiProvider.Preflight()` returns nil

#### Scenario: First zone failure aborts preflight
- **WHEN** the first sub-provider returns an error from `Preflight()`
- **THEN** `MultiProvider.Preflight()` returns that error immediately

