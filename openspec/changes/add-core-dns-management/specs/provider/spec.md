## ADDED Requirements

### Requirement: Provider Interface
The system SHALL define a `Provider` interface that all DNS backends MUST implement:

```go
type Provider interface {
    Records(ctx context.Context) ([]*endpoint.Endpoint, error)
    ApplyChanges(ctx context.Context, changes *plan.Changes) error
}
```

`Records` SHALL return all DNS records currently managed in the target zone.
`ApplyChanges` SHALL apply a set of create, update, and delete operations atomically
where the underlying DNS protocol supports it.

#### Scenario: Records returns current DNS state
- **WHEN** `Records` is called
- **THEN** it returns all endpoints in the configured DNS zone

#### Scenario: ApplyChanges creates a new record
- **WHEN** `ApplyChanges` is called with a non-empty `Create` list
- **THEN** the new DNS records appear in the zone

#### Scenario: ApplyChanges deletes a record
- **WHEN** `ApplyChanges` is called with a non-empty `Delete` list
- **THEN** the specified DNS records are removed from the zone

### Requirement: RFC2136 Provider
The system SHALL include an RFC2136-compliant DNS provider that:
- Lists current records via AXFR (full zone transfer)
- Applies changes via DNS UPDATE messages (RFC2136)
- Authenticates using TSIG (Transaction Signature)

#### Scenario: AXFR lists zone records
- **WHEN** `Records` is called on the RFC2136 provider
- **THEN** an AXFR request is sent to the configured server and all RRs are returned as Endpoints

#### Scenario: DNS UPDATE creates a record
- **WHEN** `ApplyChanges` is called with a new Endpoint
- **THEN** a DNS UPDATE message with TSIG authentication adds the record to the zone

#### Scenario: DNS UPDATE deletes a record
- **WHEN** `ApplyChanges` is called with an Endpoint to delete
- **THEN** a DNS UPDATE message removes the matching RR from the zone

#### Scenario: TSIG authentication failure is surfaced
- **WHEN** the TSIG key or secret is incorrect
- **THEN** an error is returned and no changes are applied

### Requirement: RFC2136 Configuration
The RFC2136 provider SHALL be configured via the following parameters:

| Parameter | Description |
|-----------|-------------|
| `rfc2136-host` | DNS server hostname or IP |
| `rfc2136-port` | DNS server port (default 53) |
| `rfc2136-zone` | Zone to manage (e.g., `example.com.`) |
| `rfc2136-tsig-keyname` | TSIG key name |
| `rfc2136-tsig-secret` | TSIG secret (base64) |
| `rfc2136-tsig-secret-alg` | TSIG algorithm (default `hmac-sha256`) |
| `rfc2136-min-ttl` | Minimum TTL to enforce (default 0 = no minimum) |

#### Scenario: Configuration applied at startup
- **WHEN** the daemon starts with `--rfc2136-host=ns1.example.com --rfc2136-zone=example.com.`
- **THEN** the RFC2136 provider sends all DNS operations to `ns1.example.com` for zone `example.com.`

#### Scenario: Minimum TTL enforced
- **WHEN** an Endpoint has TTL=60 and `rfc2136-min-ttl=300`
- **THEN** the record is written with TTL=300
