## 1. Audit and persistence

- [ ] 1.1 Inventory settings, service-link, monitor, NPM-instance, and other mutation groups.
- [ ] 1.2 Add reversible hashed-token migration and indexes.

## 2. Enforcement

- [ ] 2.1 Implement bearer validation, owner matching, expiry, and revocation.
- [ ] 2.2 Protect mutations while preserving public TRMNL stats and existing role checks.
- [ ] 2.3 Implement owner-only create/list/revoke/rotate operations.

## 3. Tests and migration

- [ ] 3.1 Test public reads and all anonymous/token-only/malformed/expired/revoked cases.
- [ ] 3.2 Test ownership, role boundaries, and one-time secret handling.
- [ ] 3.3 Document and migrate the TRMNL token; run Synapse tests.
