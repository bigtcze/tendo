# Post-V1 direction

status=locked_direction_not_implementation_permission
source_of_truth=this_file
ordering=not_committed

Tendo is the stable household backlog core. Future capabilities attach through application and API contracts. They must not replace the backlog core or make basic use depend on external services.

Authorized product requirements, no longer deferred roadmap items (not an implementation or completion claim): multi-household membership and switching, instance administration, local password change and CLI reset, invitation cleanup, configurable host IP, per-user OAuth-protected MCP. Their current contract lives in PRODUCT.md and SECURITY.md, not here.

## Extension rule
- Future modules never read or write core PostgreSQL tables directly.
- Internal features call application services.
- External or independently deployable features use public REST API, webhook or event contracts available at that time.
- Future API or service credentials are scoped principals (household, user, capabilities), never DB credentials or shared admin secrets.
- Core domain may emit typed internal events at meaningful state changes; handlers may remain synchronous.
- Do not build a broker, plugin marketplace or outbox before a real integration requires it. When async delivery becomes necessary, add a transactional outbox behind the existing event boundary without changing domain semantics.

## AI provider
- Configurable provider abstraction.
- OpenAI API plus OpenAI-compatible custom base URL and key support. No vendor identity in domain code.
- AI extracts and suggests; it is not a general autonomous household agent.
- Consequential extracted items and dates require human review.

## Documents
- Upload a file.
- Extract candidate dates, obligations, recurrence hints, names and a summary.
- Preserve provenance between a suggestion or item and its source document.
- Attach or link a document to an item or its history.

## Nextcloud
- Optional per-user connection.
- The user chooses and authorizes the destination.
- An uploaded document can be offered for storage in the user's Nextcloud.
- Tendo stores a durable reference and metadata after a successful transfer.
- Credentials and tokens are user-scoped and protected.

## Calendar
- Optional, never required for the backlog.
- Tendo remains the source of truth for the obligation.
- From an item that needs attention, the user may explicitly choose a date and time and create a calendar event.
- If a concrete appointment already exists, the user may create or sync that event directly.
- Calendar event creation never completes, resets or advances the Tendo item.
- Candidate providers: CalDAV or Nextcloud, Google Calendar, Microsoft 365.

## Family inbox
- Email, message or source intake proposes items and events.
- The inbox is an input channel, not the product core.

## Notifications
- Future reminders surface Tendo state; they are not a second state store.
- Notification settings and delivery are not in the current product and need a separate authorized release.

## Platform constraint
Do not create empty tables, services or plugins to anticipate these features. Preserve seams, not speculative infrastructure.
