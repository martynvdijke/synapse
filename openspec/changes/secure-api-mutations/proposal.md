## Why

Synapse has a token-shaped TRMNL stats integration and authenticated application routes, but its mutation policy is not expressed as one consistent user-plus-token contract.

## What Changes

- Keep the TRMNL stats read endpoint public as a read-only integration.
- Require an authenticated user and bearer API token for settings, service-link, monitor, NPM-instance, and every other create/update/delete route.
- Add secure token creation, listing, revocation, and rotation.

## Capabilities

### New Capabilities

- `api-authentication`

### Modified Capabilities

- `public-api-reads`

## Impact

Synapse route groups, auth middleware, persistence/migration, token management, tests, and TRMNL token migration are affected. Existing browser auth and public monitoring data must remain compatible.
