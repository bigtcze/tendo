# ADR 0002: Household access policy

Status: accepted

## Context

Household resources live under `/api/v1/households/{householdId}/...`. The path ID is client-supplied, so it can be forged, guessed, or malformed. The schema already supports many-to-many memberships, and the session carries a `defaultHouseholdId` hint (see ADR 0001).

## Decision

- The household ID in a path is authorization context only. Every request is checked against a `household_memberships` row for the session user, in the same query that loads the household. Any role grants read access to the household itself; role-specific rules are added per operation.
- A malformed ID, a nonexistent household, and a household the caller does not belong to all return the same `404` problem (`code: not_found`). `403` is not used for household membership, so responses do not reveal whether a household exists. A malformed ID is rejected before any database call.
- `DefaultHouseholdID` is a navigation hint, never an authorization grant. The guarantee is structural: the household access path (service, repository, and query) never reads `DefaultHouseholdID`, so only a membership row can grant access.
- A household's `ETag` is the strong, quoted decimal value of `households.version` (starts at 1). Future mutations must increment it and honor `If-Match`. The version is not part of the response body.
- Business code stays transport-neutral: the household HTTP adapter receives the authentication middleware and a user-ID-from-context function by injection, and `cmd/tendo` composes them with the identity session handler.

## Consequences

- Clients cannot distinguish "does not exist" from "not yours"; debugging relies on the request ID and server logs.
- Adding a household switcher or a second household later needs no authorization change.
- Child resources (members, items, ...) must reuse the same membership check before touching household-owned rows.
