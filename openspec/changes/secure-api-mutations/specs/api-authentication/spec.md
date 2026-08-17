## ADDED Requirements

### Requirement: Public monitoring reads

The service SHALL allow unauthenticated read access to the TRMNL stats endpoint and other explicitly public GET monitoring data.

#### Scenario: TRMNL stats read

- WHEN a TRMNL client requests `/api/v1/trmnl/stats` without a credential
- THEN Synapse returns the read-only stats payload

### Requirement: Mutations require user and token

Settings writes, service-link create/update/delete, monitor create/update/delete, NPM-instance create/update/delete, and every other mutation MUST require an authenticated user and a valid bearer API token.

#### Scenario: Token-only settings write

- WHEN a request has a valid bearer token but no authenticated user
- THEN Synapse rejects it and does not modify settings

### Requirement: Token lifecycle and failure safety

Users SHALL create, list metadata for, revoke, and rotate owned tokens. Secrets MUST be hash-only, one-time visible, and never logged. Malformed, expired, revoked, and cross-user tokens MUST fail without writes.

#### Scenario: Revoked monitor token

- WHEN a revoked token is used to create a monitor
- THEN the request is rejected without creating the monitor
