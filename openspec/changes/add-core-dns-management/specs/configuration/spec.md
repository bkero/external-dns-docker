## ADDED Requirements

### Requirement: CLI Flags and Environment Variables
The system SHALL expose all configuration as CLI flags. Each flag SHALL have a
corresponding environment variable override using the `EXTERNAL_DNS_` prefix with
flag names uppercased and hyphens replaced by underscores.
Example: `--rfc2136-host` ↔ `EXTERNAL_DNS_RFC2136_HOST`.

Environment variables SHALL take lower precedence than explicit CLI flags.

#### Scenario: Flag overrides env var
- **WHEN** `EXTERNAL_DNS_RFC2136_HOST=ns1.example.com` is set and `--rfc2136-host=ns2.example.com` is passed
- **THEN** `ns2.example.com` is used

#### Scenario: Env var used when flag is absent
- **WHEN** `EXTERNAL_DNS_RFC2136_HOST=ns1.example.com` is set and no `--rfc2136-host` flag is passed
- **THEN** `ns1.example.com` is used

### Requirement: Docker Socket Configuration
The system SHALL support configuring the Docker socket path (default: `/var/run/docker.sock`)
and optional TLS certificates for TCP-based Docker connections.

#### Scenario: Custom socket path
- **WHEN** `--docker-host=unix:///run/docker.sock` is configured
- **THEN** the daemon connects to that socket path

#### Scenario: TLS Docker connection
- **WHEN** `--docker-host=tcp://remote:2376` with `--docker-tls-*` cert flags is configured
- **THEN** the daemon connects with TLS

### Requirement: Logging Configuration
The system SHALL use structured JSON logging via Go stdlib `log/slog`. The log level
SHALL be configurable via `--log-level` (values: `debug`, `info`, `warn`, `error`;
default: `info`). All log output SHALL go to stderr.

#### Scenario: Debug logging enabled
- **WHEN** `--log-level=debug` is set
- **THEN** debug-level log messages are emitted to stderr in JSON format

#### Scenario: Default log level is info
- **WHEN** no `--log-level` flag is set
- **THEN** only info-level and above messages are emitted
