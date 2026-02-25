## ADDED Requirements

### Requirement: Unit Tests
The system SHALL include unit tests for all packages with a minimum coverage of
80%. Unit tests SHALL run with `go test ./...` and SHALL NOT require Docker or
a live DNS server to be present.

#### Scenario: Unit tests pass without external services
- **WHEN** `go test ./...` is run in a clean environment with no Docker socket or DNS server
- **THEN** all unit tests pass

#### Scenario: Coverage threshold met
- **WHEN** `go test -cover ./...` is run
- **THEN** each package reports ≥80% statement coverage

### Requirement: Fake Provider
The system SHALL include an in-memory fake DNS provider (`pkg/provider/fake`) that
implements the `Provider` interface. The fake provider SHALL store records in memory
and support inspection of applied changes in tests.

#### Scenario: Fake provider records changes
- **WHEN** `ApplyChanges` is called on the fake provider
- **THEN** the in-memory state is updated and can be inspected by test assertions

### Requirement: Fake Docker Source
The system SHALL include a fake Docker source (`pkg/source/fake`) that implements
the `Source` interface. The fake source SHALL accept a pre-configured list of
Endpoints and support manual triggering of event callbacks for testing.

#### Scenario: Fake source returns configured endpoints
- **WHEN** the fake source is configured with two endpoints
- **THEN** `Endpoints()` returns exactly those two endpoints

#### Scenario: Fake source triggers event handler
- **WHEN** the test calls `TriggerEvent()` on the fake source
- **THEN** the registered event handler is called

### Requirement: Integration Tests
The system SHALL include integration tests under `test/integration/` that use
Docker Compose to spin up a BIND9 server configured for RFC2136 updates and one or
more test containers with DNS labels. Integration tests SHALL verify end-to-end
DNS record creation, update, and deletion.

#### Scenario: Container start creates DNS record
- **WHEN** a test container with `external-dns.io/hostname=test.example.com` and `external-dns.io/target=1.2.3.4` starts
- **THEN** an A record for `test.example.com` pointing to `1.2.3.4` exists in the BIND9 zone after reconciliation

#### Scenario: Container stop deletes DNS record
- **WHEN** a test container with DNS labels stops
- **THEN** the corresponding DNS record is removed from the BIND9 zone after reconciliation

#### Scenario: Unowned records are not deleted
- **WHEN** a DNS record exists in BIND9 without an ownership TXT record
- **THEN** the daemon does NOT delete the record during reconciliation
