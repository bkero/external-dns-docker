## ADDED Requirements

### Requirement: Change Calculation
The system SHALL implement a `Calculate` function that diffs a desired set of
Endpoints (from the source) against a current set (from the provider) and produces
a `Changes` struct with three lists: `Create` (new records), `UpdateOld`/`UpdateNew`
(records to replace), and `Delete` (records to remove).

A record is considered for update when the DNS name and record type match but
targets or TTL differ.

#### Scenario: New endpoint produces Create
- **WHEN** desired contains `app.example.com` but current does not
- **THEN** `Changes.Create` includes an Endpoint for `app.example.com`

#### Scenario: Removed endpoint produces Delete
- **WHEN** current contains `old.example.com` (owned by this daemon) but desired does not
- **THEN** `Changes.Delete` includes an Endpoint for `old.example.com`

#### Scenario: Changed target produces Update
- **WHEN** current has `app.example.com → 1.2.3.4` and desired has `app.example.com → 5.6.7.8`
- **THEN** `Changes.UpdateOld` has the old endpoint and `Changes.UpdateNew` has the new endpoint

#### Scenario: Unchanged endpoint produces no change
- **WHEN** current and desired both have `app.example.com → 1.2.3.4` with the same TTL
- **THEN** no entry appears in any Changes list

### Requirement: Ownership Filtering
The system SHALL only modify or delete DNS records that have a corresponding
ownership TXT record written by this daemon instance. Records without an ownership
TXT record SHALL be left untouched.

Ownership TXT records SHALL be written alongside every managed record using the
naming convention: `external-dns-docker-owner.<dns-name>` with value
`"heritage=external-dns-docker,external-dns-docker/owner=<owner-id>"`.

The owner ID is configurable (default: `external-dns-docker`).

#### Scenario: Owned record is eligible for deletion
- **WHEN** current DNS state has `app.example.com` and a matching ownership TXT record with the correct owner ID
- **THEN** the record is eligible for Delete if no longer desired

#### Scenario: Unowned record is not deleted
- **WHEN** current DNS state has `manual.example.com` with no ownership TXT record
- **THEN** `manual.example.com` does NOT appear in `Changes.Delete`

#### Scenario: Ownership TXT record created with managed record
- **WHEN** a new Endpoint is created
- **THEN** a companion TXT ownership record is also added to `Changes.Create`

#### Scenario: Ownership TXT record deleted with managed record
- **WHEN** a managed Endpoint is deleted
- **THEN** its companion ownership TXT record is also added to `Changes.Delete`
