## Context

Synapse already separates login/setup and authenticated API groups, while its TRMNL stats configuration currently uses a placeholder bearer value. The policy must make public reads intentional and protect application mutations without staging unrelated work.

## Goals / Non-Goals

### Goals

- Preserve public TRMNL stats reads.
- Protect all mutation groups with user identity and a hashed bearer token.
- Provide a migration from the placeholder integration token.

### Non-Goals

- Making monitoring reads private.
- Replacing existing login or role checks.
- Committing a real token.

## Decisions

- Add a reversible token store with owner, hash, timestamps, expiry, and revocation state.
- Compose bearer validation with existing authenticated route groups and retain stronger role checks.
- Cover settings, service links, monitors, NPM instances, and future mutation routes through explicit middleware placement.
- Provide owner-only token lifecycle operations and one-time secret display.

## Risks / Trade-offs

Existing integrations need user sessions and token provisioning. Public monitoring endpoints must be carefully separated from writes so the exception cannot widen accidentally.

## Migration Plan

1. Add token migration and lifecycle API.
2. Audit route groups and protect mutations.
3. Replace placeholder TRMNL credentials with a provisioned token where needed.
4. Add tests and migrate clients without staging existing user WIP.

## Open Questions

- Should the TRMNL read remain completely unauthenticated or support optional token validation for private deployments?
