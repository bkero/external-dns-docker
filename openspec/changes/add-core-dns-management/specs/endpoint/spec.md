## ADDED Requirements

### Requirement: Endpoint Data Model
The system SHALL define an `Endpoint` struct as the canonical representation of a
desired DNS record. Each endpoint SHALL carry: `DNSName` (string), `Targets` ([]string),
`RecordType` (string: A, AAAA, or CNAME), `TTL` (int64, seconds), and `Labels`
(map[string]string for metadata such as owner tracking).

#### Scenario: Basic A record endpoint
- **WHEN** a container has `external-dns.io/hostname=web.example.com` and `external-dns.io/target=203.0.113.10`
- **THEN** an Endpoint with DNSName=`web.example.com`, Targets=`["203.0.113.10"]`, RecordType=`A`, TTL=`300` is produced

#### Scenario: CNAME endpoint
- **WHEN** a container has `external-dns.io/target=backend.internal.example.com` (a hostname, not an IP)
- **THEN** an Endpoint with RecordType=`CNAME` is produced

#### Scenario: AAAA record endpoint
- **WHEN** a container has `external-dns.io/target=2001:db8::1`
- **THEN** an Endpoint with RecordType=`AAAA` is produced

### Requirement: RecordType Auto-Detection
The system SHALL automatically infer `RecordType` from the target value:
valid IPv4 address → `A`; valid IPv6 address → `AAAA`; any other value → `CNAME`.
The `external-dns.io/record-type` label SHALL override auto-detection.

#### Scenario: IPv4 auto-detection
- **WHEN** target is `192.168.1.5` and no `record-type` label is set
- **THEN** RecordType is `A`

#### Scenario: IPv6 auto-detection
- **WHEN** target is `fe80::1` and no `record-type` label is set
- **THEN** RecordType is `AAAA`

#### Scenario: Hostname auto-detection
- **WHEN** target is `my-service.internal` and no `record-type` label is set
- **THEN** RecordType is `CNAME`

#### Scenario: Override auto-detection
- **WHEN** target is `192.168.1.5` and `external-dns.io/record-type=CNAME` is set
- **THEN** RecordType is `CNAME` (override wins)
