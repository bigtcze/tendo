# Post-V1 Direction

status=locked_direction_not_implementation_permission
ordering=not_committed

Tendo V1 is the stable household backlog core. Future capabilities attach through application/API contracts; they must not replace the backlog core or make basic use dependent on external services.

## Extension rule
- Future modules never read/write core PostgreSQL tables directly.
- Internal features call application services.
- External/independently deployable features use public REST API/webhook/event contracts available at that time.
- Future API/service credentials are scoped principals (household/user/capabilities), not DB credentials or shared admin secrets.
- Core domain may emit typed internal events at meaningful state changes; V1 handlers may remain synchronous.
- Do not build a broker/plugin marketplace/outbox before a real integration requires it.
- When async delivery becomes necessary, add a transactional outbox behind the existing event boundary rather than changing domain semantics.

## Multi-household UI
- DB/membership/API model is prepared in V1.
- V1 presents one default household only.
- Future UI may add household creation/switching without schema/auth redesign.

## AI provider
- Configurable provider abstraction.
- OpenAI API plus OpenAI-compatible custom base URL/key support (covers private/custom gateways such as CPA); no vendor identity in domain code.
- AI extracts/suggests; it is not a general autonomous household agent.
- Consequential extracted items/dates require human review.

## Documents
- Upload file.
- Extract candidate dates, obligations, recurrence hints, names, summary.
- Preserve provenance between suggestion/item and source document.
- Attach/link document to item/history.

## Nextcloud
- Optional per-user connection.
- User chooses/authorizes destination.
- Uploaded document can be offered for storage in the user's Nextcloud.
- Tendo stores durable reference/metadata after successful transfer.
- Credentials/tokens are user-scoped and protected.

## Calendar
- Optional, never required for backlog.
- Tendo remains source of truth for the obligation.
- From an item that needs attention, user may explicitly choose date/time and create a calendar event.
- If a concrete appointment already exists, user may create/sync that event directly.
- Calendar event creation never completes/resets/advances the Tendo item.
- Candidate providers: CalDAV/Nextcloud, Google Calendar, Microsoft 365.

## Family inbox
- Email/message/source intake proposes items/events.
- Inbox is an input channel, not the product core.

## Notifications
- Future reminders surface Tendo state; they are not a second state store.

## Platform constraint
Do not create empty V1 tables/services/plugins merely to anticipate these features. Preserve seams, not speculative infrastructure.
