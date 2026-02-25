## ADDED Requirements

### Requirement: Container Discovery
The system SHALL list all currently running Docker containers at startup and on each
reconciliation cycle, extracting `external-dns.io/*` labels to produce Endpoints.
Containers missing the `external-dns.io/hostname` label SHALL be silently skipped.
Containers missing the `external-dns.io/target` label SHALL be skipped with a warning log.

#### Scenario: Running container with valid labels is discovered
- **WHEN** a container is running with `external-dns.io/hostname=app.example.com` and `external-dns.io/target=10.0.0.1`
- **THEN** the source returns an Endpoint for `app.example.com`

#### Scenario: Container without hostname label is skipped
- **WHEN** a container has no `external-dns.io/hostname` label
- **THEN** the container is ignored and no Endpoint is produced

#### Scenario: Container without target label is skipped with warning
- **WHEN** a container has `external-dns.io/hostname` but no `external-dns.io/target`
- **THEN** the container is skipped and a warning is logged

### Requirement: Event Watching
The system SHALL subscribe to the Docker Events API and trigger a reconciliation
when any of the following container events occur: `start`, `stop`, `die`, `update`.
The event subscription SHALL reconnect automatically on disconnection.

#### Scenario: Container start triggers reconciliation
- **WHEN** a new container starts with DNS labels
- **THEN** a reconciliation is triggered within the event debounce window

#### Scenario: Container stop triggers reconciliation
- **WHEN** a running container with DNS labels stops
- **THEN** a reconciliation is triggered within the event debounce window

#### Scenario: Event stream reconnect
- **WHEN** the Docker event stream is interrupted
- **THEN** the source reconnects and resumes watching events

### Requirement: Label Parsing
The system SHALL parse the following labels from container metadata:

| Label | Required | Default | Description |
|-------|----------|---------|-------------|
| `external-dns.io/hostname` | Yes | — | DNS name to manage |
| `external-dns.io/target` | Yes | — | IP or hostname target |
| `external-dns.io/ttl` | No | 300 | TTL in seconds |
| `external-dns.io/record-type` | No | auto | A, AAAA, or CNAME |

Invalid TTL values (non-numeric, negative) SHALL be rejected with a warning and
the container SHALL be skipped.

#### Scenario: TTL label parsed correctly
- **WHEN** a container has `external-dns.io/ttl=3600`
- **THEN** the resulting Endpoint has TTL=3600

#### Scenario: Invalid TTL causes skip with warning
- **WHEN** a container has `external-dns.io/ttl=bad`
- **THEN** the container is skipped and a warning is logged

#### Scenario: Default TTL applied when not set
- **WHEN** a container has no `external-dns.io/ttl` label
- **THEN** the resulting Endpoint has TTL=300

### Requirement: Multi-Record Support
The system SHALL support multiple DNS records per container via indexed labels.
If `external-dns.io/hostname-0`, `external-dns.io/hostname-1`, etc. are present
(alongside matching `external-dns.io/target-0`, `external-dns.io/target-1`),
each index pair SHALL produce a separate Endpoint. Non-indexed labels (`hostname`,
`target`) define a single record and MAY coexist with indexed labels.

#### Scenario: Indexed labels produce multiple endpoints
- **WHEN** a container has `hostname-0=a.example.com`, `target-0=1.2.3.4`, `hostname-1=b.example.com`, `target-1=5.6.7.8`
- **THEN** two Endpoints are produced: one for `a.example.com` and one for `b.example.com`

#### Scenario: Missing indexed target causes that index to be skipped
- **WHEN** a container has `hostname-0=a.example.com` but no `target-0`
- **THEN** index 0 is skipped with a warning; other valid indices are still processed
