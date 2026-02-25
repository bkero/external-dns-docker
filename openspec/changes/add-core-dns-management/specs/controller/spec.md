## ADDED Requirements

### Requirement: Reconciliation Loop
The system SHALL run a periodic reconciliation loop at a configurable interval
(default 60 seconds). Each cycle SHALL:
1. Fetch desired endpoints from the source
2. Fetch current endpoints from the provider
3. Calculate changes via the plan engine
4. Apply changes via the provider

Errors during any step SHALL be logged and the cycle retried at the next interval.
The loop SHALL NOT crash the daemon on transient errors.

#### Scenario: Periodic reconciliation creates missing record
- **WHEN** a container starts with DNS labels between reconciliation cycles
- **THEN** on the next reconciliation cycle the DNS record is created

#### Scenario: Transient provider error does not crash daemon
- **WHEN** the DNS server is temporarily unreachable during `ApplyChanges`
- **THEN** an error is logged and the daemon continues to run; the next cycle retries

### Requirement: Event-Driven Reconciliation
The system SHALL trigger an additional reconciliation cycle when Docker container
events are received (via `Source.AddEventHandler`). This provides faster DNS
convergence compared to relying solely on the periodic interval.

#### Scenario: Container start triggers fast reconciliation
- **WHEN** a container with DNS labels starts
- **THEN** a reconciliation is triggered within the debounce window (not waiting for the full interval)

#### Scenario: Rapid events are debounced
- **WHEN** multiple container events arrive within the debounce window
- **THEN** only one reconciliation cycle is triggered for the batch

### Requirement: Event Batching / Debounce
The system SHALL debounce rapid Docker events within a configurable window
(default 5 seconds). Multiple events within the window SHALL trigger a single
reconciliation cycle rather than one cycle per event.

#### Scenario: Two events within debounce window → one reconciliation
- **WHEN** two container start events arrive 1 second apart and debounce is 5s
- **THEN** only one reconciliation cycle is triggered

#### Scenario: Events after debounce window → separate reconciliations
- **WHEN** two container start events arrive 10 seconds apart and debounce is 5s
- **THEN** two reconciliation cycles are triggered

### Requirement: Dry-Run Mode
The system SHALL support a `--dry-run` flag. In dry-run mode, the controller SHALL
calculate changes and log what would be applied, but SHALL NOT call `ApplyChanges`
on the provider.

#### Scenario: Dry-run logs intended changes without applying
- **WHEN** the daemon runs with `--dry-run` and a new container starts
- **THEN** the planned DNS changes are logged at INFO level but no DNS records are modified

### Requirement: Once Mode
The system SHALL support a `--once` flag. In once mode, the controller SHALL run
exactly one reconciliation cycle and then exit with status 0 (or non-zero on error).

#### Scenario: Once mode exits after single cycle
- **WHEN** the daemon runs with `--once`
- **THEN** it performs one full reconciliation cycle and exits
