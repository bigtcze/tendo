# V4 baseline audit (shipped v3.0.0)

> Historical record. It describes the V4 stage (released as `v4.0.0`) and is not the current UX or security contract. Current contracts: [UX](../../.autonomous/UX.md), [SECURITY](../../.autonomous/SECURITY.md).

status=V4-01_baseline_evidence
issue=#409 (V4-01)
source_commit=abdcc21 (main after v3.0.0 + V4-00 governance; application code identical to published v3.0.0)
evidence=production-build capture `TENDO_UX_AUDIT_DIR=/tmp/opencode/v4-01/ux-audit bash scripts/dev-exec.sh -- bash scripts/e2e-smoke.sh` on 2026-11-09: real PostgreSQL 18, fixed business date 2026-05-14, Chromium; exit 0, 521 PNG + `metrics.json`, 494 measured route captures, 0 with horizontal overflow, 0 interactive targets under 44 px, smallest visible font 14 px
curated=25 PNG in [`v4-baseline/`](v4-baseline/) (1.4 MiB)
references=`docs/design/reference/tendo-v4-desktop.png`, `docs/design/reference/tendo-v4-mobile.png` (SHA-256 verified against [`.autonomous/V4_DESIGN_BRIEF.md`](https://github.com/bigtcze/tendo/blob/v4.0.0/.autonomous/V4_DESIGN_BRIEF.md); visual targets only, never screenshot evidence)
limits=read-only audit; proposals below are plans for the owning V4 Issues, not implemented behavior

This is the starting point for V4. It records what v3 actually does in code, data, API and browser,
and specifies the migrations and decisions the later V4 Issues implement. It deliberately does not
rewrite architecture. Every implementation choice marked **Proposal** must still be reviewed in its
own PR; security-sensitive proposals (§4, §5, §7) require an independent security-oriented review in
that PR before merge.

Owning Issues: V4-02 #410 tokens, V4-03 #411 shell/Home, V4-04 #412 items/people UI, V4-05 #413
auth/settings UI, V4-06 #414 roles, V4-07 #415 Admin center, V4-08 #416 password reset, V4-09 #417
registered people, V4-10 #418 OIDC JIT, V4-11 #419 remove manual linking, V4-12 #420 expired
invitations, V4-16 #421 two-service Compose.

## 1. Visual baseline vs approved V4 references

Captures (all from the running production build, English light unless named):

| Screen | Desktop | Mobile |
|---|---|---|
| Home (30 items) | [home-30-en-1440-fold](v4-baseline/home-30-en-1440-fold.png) | [home-30-en-390-fold](v4-baseline/home-30-en-390-fold.png), [cs](v4-baseline/home-30-cs-390-fold.png) |
| Home dark (1200 items) | [shell-home-dark-en-1440](v4-baseline/shell-home-dark-en-1440.png) | [shell-home-dark-en-390](v4-baseline/shell-home-dark-en-390.png) |
| Home empty | [home-empty-en-1440](v4-baseline/home-empty-en-1440.png) | — |
| All items | [audit-all-items-en-1440](v4-baseline/audit-all-items-en-1440.png) | [audit-all-items-en-390](v4-baseline/audit-all-items-en-390.png), [error](v4-baseline/audit-all-items-error-390.png) |
| Item detail / add form | [audit-item-detail-en-1440](v4-baseline/audit-item-detail-en-1440.png) | [audit-add-fixed-en-390](v4-baseline/audit-add-fixed-en-390.png) |
| People and things | [audit-people-en-1440](v4-baseline/audit-people-en-1440.png) | [audit-people-en-390](v4-baseline/audit-people-en-390.png), [edit linked](v4-baseline/audit-people-edit-linked-en-390.png) |
| Members | [audit-members-owner-en-1440](v4-baseline/audit-members-owner-en-1440.png) | [owner](v4-baseline/audit-members-owner-en-390.png), [non-owner](v4-baseline/audit-members-non-owner-en-390.png) |
| Account | [audit-account-en-1440](v4-baseline/audit-account-en-1440.png) | [audit-account-en-390](v4-baseline/audit-account-en-390.png) |
| Sign-in / OIDC | [auth-login-en-1440](v4-baseline/auth-login-en-1440.png) | [login](v4-baseline/auth-login-en-390.png), [OIDC button](v4-baseline/auth-login-oidc-available-en-390.png), [OIDC outcome](v4-baseline/auth-login-oidc-outcome-en-390.png) |
| Onboarding / invite | — | [auth-onboarding-en-390](v4-baseline/auth-onboarding-en-390.png), [audit-invite-accept-en-390](v4-baseline/audit-invite-accept-en-390.png) |

Side-by-side review against the references (V4_DESIGN_BRIEF fidelity checklist a–g):

| Check | v3 actual | V4 reference | Owner |
|---|---|---|---|
| (a) Type | System sans `ui-rounded…` body, serif `ui-serif/Charter/Georgia` display (`frontend/src/index.css:7-8`); visual tests alias to Liberation fonts (`frontend/e2e/visual/fonts/README.md`) | Self-hosted Inter Variable, bold 700 sans headline with cyan period, 600–700 row titles | V4-02 |
| (a) Hierarchy | Headline 36 px serif regular weight; row titles 18 px medium (`--text-row-title: 1.125rem`, `index.css:41`) | ~36–40 px bold sans; 14–16 px semibold titles; eyebrow `YOUR HOME · OVERVIEW` + supporting sentence | V4-02/03 |
| (b) Canvas | Warm paper `#faf7f2`, sand `#efe9df`, line `#d8d0c3` (`index.css:63-69`) | Cool `#F1F5FA` canvas, white surfaces, `#D7E2EB` borders | V4-02 |
| (c) Action color | Forest green accent `#2f6b57` (`index.css:70`); dark mode mint `#9bc8ad` filled CTA with dark text | Petrol teal `#006D82`, hover `#005566`, white text | V4-02 |
| (c) Status semantics | Attention brown `#8a3e19`, progress blue `#285a78`, waiting violet `#66518a`, paused gray (`index.css:74-81`); pills are uniformly sand with no semantic tint; no group dot | Coral attention, cyan progress, amber waiting, mint coming up; attention pill tinted; colored group dots | V4-02/03 |
| (d) Density | Rows 97–120 px tall at 1440 (title, then meta on separate lines, Done top-right); Home column capped at `lg:max-w-6xl` but group list ~768 px wide, right ~380 px empty | 48–55 px single-line rows spanning width: icon tile → title+meta → subject pill → date/assignee → Done | V4-03 |
| (d) Header band | Pills row, then a separate "Add item" button row below (desktop and mobile) | Headline + right-aligned Add item in one band (desktop); full-width Add item above pills (mobile) | V4-03 |
| (e) Navigation | Left rail 240 px (`Layout.tsx:21` `15rem`) with sand selected state; mobile bottom bar Home/Items/People/Members/Account already exists | 220–230 px rail, icy-blue `#DDECF4` selected, "WORKSPACE" label, household footer; same five bottom tabs | V4-03 |
| (e) Top bar | Serif "Tendo" text wordmark; "Signed in as login · Sign out" | Teal tile logo + "Tendo" sans wordmark + tagline; identity with avatar initial; mobile: settings gear + avatar | V4-03 |
| (f) Icons/tiles | Nav icons only (`app/NavIcons.tsx`); no item icon tiles; Done is an outlined text button | Per-row semantic/topic icon tiles; outlined `✓ Done` desktop, square check ≥44 px mobile | V4-03 |
| (g) Data | Real data only; subject + metadata + responsible rendered as one middot line (`features/items/HomeGroupSection.tsx`) | Same real facts, split into subject pill and date/assignee column; no invented properties | V4-03 |
| Members / People / Account | Same warm style; People is a two-column card grid with "See items"/Edit per card; Account has a large empty left column; footer language switch duplicates Account | Brief §5: same V4 system everywhere | V4-04/05 |
| Paused group | v3 Home shows a fifth "Paused" pill/group | References show four groups only | V4-03 must keep Paused (real state, PRODUCT lifecycle) while matching the four-pill composition; deviation recorded there, not hidden |

Reusable v3 primitives (keep, restyle via tokens): `components/ui/` button, input, select, switch,
textarea, field, label, notice, dialog, disclosure, empty-state, group-header, list-row, pagination,
skeleton; `app/Heading.tsx` focus/title handling; theme preference with first-paint script
`public/theme-boot.js` + `light-dark()` tokens (`index.css:90-126`) — retained for V4 no-flash
behavior. CSP already allows only `font-src 'self'`
(`backend/internal/platform/webui/webui.go:18`), so Inter must be bundled, not loaded from a CDN.

Visual-regression impact: the required `visual` CI job (`.github/workflows/required.yml:222`) holds
94 baselines in `frontend/e2e/visual/visual.spec.ts-snapshots/` rendered with Liberation fonts via
`FONTCONFIG_FILE` (`scripts/e2e-native-runner.py:268`). V4-02 must add the self-hosted Inter files so
captures use the real product font, and every V4 UI PR re-baselines intentionally with an explanation.
The reference fixture (ten named tasks, counts 4/2/1/3, "Novák family") does not exist yet; the
current visual fixture is `Rodina Novákových` (`frontend/e2e/visual/fixtures.ts:9`). V4-03 adds it.

## 2. Roles: owner/member → admin/member (V4-06, V4-07)

Current model:

| Fact | Evidence |
|---|---|
| `role text NOT NULL CHECK (role IN ('owner','member'))`, PK `(user_id, household_id)` | `backend/internal/platform/database/001_setup.sql:24-29` |
| Constants `RoleOwner`/`RoleMember`; forbidden code `owner_required` | `backend/internal/household/membership.go:14-17` |
| Setup inserts `owner`; invitation acceptance inserts `member` | `household/postgres/queries/setup.sql:4-5`; `household/postgres/queries/membership.sql:4-5` |
| Unknown role values are rejected on read (fail closed) | `household/membership.go:73-75`; `household/postgres/membership.go:73-75` |
| API `MemberRole` enum `[owner, member]` | `api/openapi.yaml:1557-1559`; generated `platform/httpx/apimodel/apimodel.gen.go:321-325` |
| No role change, no member removal, no last-owner rule (no mutation exists) | only `GET /members` (`household/httpapi/members.go:26-27`); queries `membership.sql:1-17` |

Owner-gated surface (the complete list):

| Operation / UI | Gate | Evidence |
|---|---|---|
| `createInvitation` | `RequireOwner` | `identity/invitation_admin.go:29-33` |
| `listInvitations` | `RequireOwner` | `identity/invitation_admin.go:94-101` |
| `revokeInvitation` | `RequireOwner` | `identity/invitation_admin.go:117-131` |
| Members invite panel | `role === 'owner'` | `frontend/src/features/members/MembersScreen.tsx:49-53` |
| Role badge | owner/member label | `features/members/MemberListSection.tsx:22` |
| Owner discovery | scans member pages then probes invitations | `features/members/useMembersDiscovery.ts:6,45,73-74` |
| 403 → `owner_required` mapping | client | `features/members/membersApi.ts:20,31,46,57` |

Subject, item, household-read and member-list operations are any-member and stay so.

**Proposal (V4-06):**
- Migration `017`: drop `household_memberships_role_check` (verify the live constraint name in the
  migration test), `UPDATE … SET role='admin' WHERE role='owner'`, add
  `CHECK (role IN ('admin','member'))`. One transaction; idempotent on re-run because the ledger
  guards it and the update is a no-op once no `owner` rows exist. Assert afterwards that every
  household has ≥1 admin.
- Wire contract: `MemberRole` becomes `[admin, member]`. No client writes roles today, so only
  readers change; document the change in release notes and `docs/development/api.md`. Keep the
  problem code `owner_required` as a stable legacy machine code (renaming breaks clients for no
  security gain) or add `admin_required` only with an explicit compatibility note — decide in V4-06.
- Rename `RoleOwner`→`RoleAdmin`, `RequireOwner`→`RequireAdmin`; all three invitation operations
  move to admin. Admin is never derived from OIDC claims or email.
- New admin operations (list household accounts, change role, issue reset) run in one transaction
  that first locks the household row (`SELECT … FROM households WHERE id=$1 FOR UPDATE`), then
  **re-reads the acting user's membership and requires `role='admin'` under that lock** (the
  request-start `RequireAdmin` check alone is stale: a concurrent demotion of the actor could commit
  between check and write), applies the change, then counts admins and rolls back with
  `409 last_admin` at zero. Locking only membership rows is insufficient (two concurrent demotions of
  different admins). Same rule for any future removal.
- Removal (if offered) must resolve `subjects.linked_user_id` (`014_subject_linked_user.sql:3`) and
  `items.responsible_user_id` (`010_item_responsible_member.sql:3`), both plain NO ACTION FKs to the
  membership, and `user_accounts.default_household_id`, whose composite FK
  `user_default_membership_fk` is `DEFERRABLE INITIALLY DEFERRED` (`001_setup.sql:30`) and so only
  fails at commit — or reject. V4 scope only requires role change, so removal can stay out.

Tests to add: member forbidden on every admin route; admin allowed; cross-household role change 404;
concurrent demotion of two admins leaves one; demoting the last admin → 409; an admin demoted
concurrently with their own in-flight admin write → the write fails with 403 and nothing changes;
migration on a populated v3 fixture maps every owner to admin and keeps memberships/default
households.

## 3. Admin password reset (V4-08)

"Local-password account" ≡ a row in `local_credentials` (`001_setup.sql:19-23`); setup and
invitation acceptance always create one (`identity/setup.go:110-112`,
`identity/postgres/invitations.go:197`); after V4-10 JIT OIDC accounts will not have one.
`user_sessions` has no revocation flag, so invalidation is row deletion (`002_sessions.sql:2-11`).

**Proposal:** table `password_reset_tokens(token_hash bytea PK CHECK 32 bytes, user_id → local_credentials
ON DELETE CASCADE, household_id, created_by_user_id, created_at, expires_at CHECK > created_at,
used_at, revoked_at, CHECK NOT (used_at AND revoked_at))`, partial unique index one active token per
user, runtime grants `SELECT, INSERT` + `UPDATE (used_at, revoked_at)` only.
- Token: 32 random bytes base64url, stored as SHA-256 only (mirrors invitations,
  `identity/invitations.go:22,93-104`), shown once to the admin as a link fragment (never logged,
  never in a query string), 30–60 min lifetime.
- Issue: admin role + target membership in the same household + target has local credential; issuing
  revokes the previous active token. Audit: who/when visible to admins.
- Redeem: atomic `UPDATE … SET used_at=$now WHERE token_hash=$1 AND used_at IS NULL AND revoked_at IS
  NULL AND expires_at>$now RETURNING user_id`; then update `password_hash` and delete all of that
  user's sessions in the same transaction. Throttle redeem per client like invitation acceptance
  (`identity/httpapi/invitations.go:33`); CSRF/origin as for other cookie routes.
- Reset vs. concurrent login race: `createSession` reads the hash, verifies it, then inserts the
  session in a separate step (`identity/session.go:140-172`). A login that verified the old password
  just before the reset commits could insert its session just after the reset deleted all sessions,
  surviving the reset. Fix in V4-08: add `local_credentials.credential_version bigint NOT NULL
  DEFAULT 1`, incremented by every password change/reset. After the (slow, unlocked) hash
  verification, login opens one transaction, takes `SELECT credential_version, password_hash … FOR
  SHARE` on the credential row, requires both to equal what was verified (else fail as bad password),
  inserts the session, and commits while still holding the lock. Redeem takes `FOR UPDATE` on the same
  row before updating hash/version and deleting sessions, so the two serialize: either the login
  commits first and its session is deleted by the reset, or the reset commits first and the login's
  revalidation fails. An unlocked `INSERT … WHERE EXISTS (version=$v)` is **not** sufficient under
  MVCC and must not be used. The same lock-and-revalidate rule applies to every password-authorized
  write, including OIDC row 7 (§5).
- Issuance throttle: per admin and per target user (e.g. ≤5 issues per target per hour), so an
  admin cannot flood or use issuance to probe; each issue revokes the previous token.
- Tests: expired, reused, revoked, superseded, cross-household, member-initiated, OIDC-only target,
  concurrent redeem (exactly one wins), sessions gone after reset, login racing a reset (injected
  pause between verify and insert) never leaves a valid session with the old password, issuance
  throttle, no token in logs.

## 4. Registered members as people (V4-09)

| Fact | Evidence |
|---|---|
| Subjects: household-scoped, `type` incl. `person`, name, archived, version | `004_subjects.sql:2-11` |
| `linked_user_id` FK to membership, person-only CHECK, **non-unique** index | `014_subject_linked_user.sql:2-5` |
| Items reference `subject_id` (required) and optional `responsible_user_id` (membership FK) | `005_items.sql:6,15`; `010_item_responsible_member.sql:2-4` |
| Link validated on create/update; documented as descriptive only, not authorization | `subject/create.go:65-82`; `subject/update.go:46-64`; `api/openapi.yaml:1656,1677,1686` |
| No person is created for a member at setup or invitation acceptance | `household/postgres/queries/setup.sql:4-5`; `membership.sql:4-5` |
| Login is immutable (no update API); UI resolves names from the live member list | `001_setup.sql:15`; `features/subjects/SubjectRow.tsx:31-34`; `features/items/AllItemsResults.tsx:64` |
| Item filters: `subjectId`, `responsibleUserId`/`unassigned`, `q`, state; subject list: archived/id only | `item/postgres/queries/item.sql:26-37`; `api/openapi.yaml:794-817` |

Gap: a member has zero, one or several person subjects; adding "Anna" as a person duplicates the
member identity (`audit-people-en-1440.png` shows linked and unlinked persons side by side).

**Proposal:**
1. Migration preflight: find any `(household_id, linked_user_id)` with >1 subject. Never merge or
   delete: keep the earliest-created linked person (`id` is uuidv7, so `ORDER BY created_at, id`)
   as canonical and unlink the others as specified in step 6 (lossless; items keep their
   `subject_id`). Tested in V4-09 on a fixture with duplicates.
2. Backfill: for every membership without a linked person, insert one `person` subject named after
   the login with `linked_user_id` set (idempotent `NOT EXISTS`). Never claim an unlinked person by
   matching name.
3. `CREATE UNIQUE INDEX … ON subjects(household_id, linked_user_id) WHERE linked_user_id IS NOT NULL`.
4. Setup and invitation acceptance create the member's person in the same transaction
   (`ON CONFLICT DO NOTHING` on the new index).
5. Display name: the member person's name is editable like any subject; login rename is not offered
   today, so nothing to propagate. Item `responsible_user_id` stays membership-based; UI can show the
   member's person pill wherever the member appears.
6. Canonical-person invariants (enforced in the subject service and tested; the partial unique index
   enforces only "at most one"):
   - Exactly one person subject per membership after the backfill; created atomically with the
     membership.
   - Unlink of the canonical person is rejected (`409 member_person_locked`); relinking another
     subject to a member who already has one fails on the index (no silent second link).
   - Changing `type` away from `person` on a linked subject is rejected (the existing person-only
     CHECK on `linked_user_id` would otherwise force an implicit unlink).
   - Archiving the canonical person is rejected while the membership exists; it stays visible in
     pickers and filters.
   - Preflight duplicates (step 1): the non-canonical duplicates are **unlinked, not deleted or
     merged**; their items keep `subject_id`, their names and history are untouched, and the
     migration log lists each unlinked subject id. This is lossless: no row or reference is removed,
     only the descriptive `linked_user_id` (documented as non-authorizing) changes. A household admin
     can later merge manually if desired.

Existing tests: `subject/subject_test.go:252,399,420,448,821`; `subject/httpapi/linked_user_test.go:11,34,102,113`;
`subject/postgres/subject_test.go:78,887`; `item/httpapi/responsible_test.go:31`;
`features/subjects/SubjectsScreen.test.tsx:753,1090`; `subjectFormModel.test.ts:41`;
`features/items/ItemsScreen.test.tsx:440`; `ItemDetailScreen.test.tsx:632`; Playwright
`journey.spec.ts:1337` (n8b link/unlink).

## 5. OIDC identity and email (V4-10, V4-11)

Verified baseline (V4_SCOPE "verified repository baseline" confirmed):

| Fact | Evidence |
|---|---|
| `user_accounts(id, login varchar(64) UNIQUE, default_household_id, created_at)` — **no email column**; no migration adds one | `001_setup.sql:13-18` |
| Provider requests **only `openid`** (auth URL and token request) | `identity/oidcprovider/provider.go:147,216` |
| `OIDCVerifiedIdentity struct{ Issuer, Subject string }`, returned with only those two fields | `identity/oidc.go:45`; `provider.go:265` |
| No userinfo call | only discovery/token/JWKS in `provider.go:69-107,212-243` |
| `oidc_identities` PK `(issuer, subject)`, UNIQUE `(user_id, issuer)`; runtime role SELECT/INSERT only | `011_oidc.sql:2-11` |
| `oidc_flows`: hashed state + browser token, nonce, PKCE verifier, purpose `login`/`link`, 10 min max | `011_oidc.sql:13-29` |
| Protections: PKCE S256, nonce, `iss` param + token issuer pinning, algorithm allowlist, `azp`, single-use flow delete | `provider.go:109-125,144-154,194-196,237-264`; `postgres/queries/oidc.sql:10-12` |
| Unknown `(issuer, subject)` → `/login#oidcError=identity_not_linked`, nothing written (no JIT) | `identity/oidc_stages.go:163-181`; `scripts/oidc-smoke.sh:164-166` |
| Manual linking: `startOidc{purpose:"link", currentPassword}` from Account, revokes initiating session | `oidc_stages.go:26-67`; `postgres/oidc.go:149-188` |
| No unlink exists | no route/query |
| Today no account can exist without a household through the app | every creation path sets one (`identity/setup.go:70-125`, `identity/invitation_accept.go:11-69`); UI already has `home.noHousehold` (`features/home/HomeScreen.tsx:74-77`) |
| Existing-account invitation accept requires no other membership/default household | `identity/invitation_accept.go:90-108` |

Manual-linking surface V4-11 removes from the frontend: `features/auth/AccountOidcLinkForm.tsx`,
`AccountOidcSectionView.tsx`, `useAccountOidcSection.ts`, `useOidcLinkStart.ts`, link branches of
`oidcApi.ts`, wiring in `AccountScreen.tsx` and `app/SignedInApp.tsx`, `#oidc=connected` handling in
`app/useOidcOutcome.ts`, `account.oidc.*` strings (`i18n/en.ts`, `i18n/cs.ts`), and the Account block
of `features/auth/Oidc.test.tsx` plus Playwright n9 (`journey.spec.ts:1392`) which links from Account.
Legacy API (`getOidcIdentity`, `purpose: link`) can be removed only after an audit proves no remaining
caller; existing `oidc_identities` rows keep signing in unchanged.

**Proposal — schema (V4-10):** migration adding to `user_accounts`: `email text NULL`,
`email_verified_at timestamptz NULL`, `email_source text NULL CHECK (email_source IN
('oidc_claim','account_confirmed'))`; all-or-none CHECK (all three set or all three NULL); partial
UNIQUE index on `email` where `email_verified_at IS NOT NULL`. All v3 accounts migrate with
`email NULL` (never inferred). Grants: runtime `UPDATE (email, email_verified_at, email_source)`.
There is no admin-entered email source: an admin-typed address proves nothing about who controls the
mailbox or the provider account (see "Rejected" below).

**Proposal — email canonical form (single Go function, unit + property tested, used for every
write and every comparison):**
- Trim ASCII whitespace; reject any address with control characters, more than one `@`, an empty
  local part or domain, or total length > 254.
- Local part: Unicode NFC, then **kept case-sensitive** (RFC 5321 §2.4 lets mailbox providers treat
  local parts case-sensitively; folding would let `Anna@x` and `anna@x` collide when the provider
  distinguishes them). Never strip dots or `+tags` (provider-specific semantics).
- Domain: lowercase, IDNA → ASCII (A-label) with UTS #46 non-transitional processing; reject on
  conversion failure.
- The database cannot evaluate NFC or IDNA, so the DB CHECK only enforces structural invariants it
  can (`email = btrim(email)`, `position('@' in email) > 1`, `split_part(email,'@',2) =
  lower(split_part(email,'@',2))`, `length(email) <= 254`). Canonical form is otherwise owned by the
  Go function; the migration test asserts that a non-canonical write through the repository is
  rejected before SQL. The unique index compares canonical strings byte-for-byte.
- Consequence: a provider that changes only the case of the local part produces a different
  canonical email; row 1 below never relies on email for an already-linked identity, so this only
  affects first login (falls to row 3, no takeover).

**Proposal — provider:** scopes `openid email`; `OIDCVerifiedIdentity` gains `Email`,
`EmailVerified`. Accept email only when the ID token's `email_verified` is JSON boolean `true` and
the address passes the canonical function. Email from userinfo is not used in V4 (no userinfo call
today); if ever added, require `userinfo.sub == id_token.sub` for the pinned issuer. The test provider
(`backend/internal/testoidc/provider.go`) must learn to issue `email`/`email_verified` (boolean and
string `"true"`, the latter rejected).

**Pending continuation (mandatory, not optional):** every callback outcome that needs a further user
step (rows 3, 5, 7) stores the verified result server-side, never in the URL, `localStorage`, or a
readable cookie:
- New table `oidc_pending_logins(handle_hash bytea PK CHECK 32 bytes, issuer, subject, email NULL,
  email_verified bool, client_id, invite_token_hash bytea NULL, created_at, expires_at CHECK
  expires_at <= created_at + 10 min, attempts int)`; runtime grants `SELECT, INSERT, DELETE,
  UPDATE (attempts)`.
- Handle: 32 random bytes, delivered only as an `HttpOnly; SameSite=Lax` cookie with `Secure`
  under the same configuration rule as the existing OIDC flow cookie
  (`identity/httpapi/oidc.go:49`), stored only as SHA-256; cleared on consume.
- Bound to the pinned issuer and configured client id; a mismatch after a config change rejects it.
- Consumed atomically: `DELETE … WHERE handle_hash=$1 AND expires_at>$now RETURNING …` in the same
  transaction as the write it authorizes; a second use finds nothing. A failed password attempt
  increments `attempts` and deletes the record at 5.
- Continuation endpoints require the existing same-origin/CSRF checks for cookie routes and the
  per-client throttle used by `createSession`.
- Tests: replay after success, replay after expiry, cross-browser (cookie absent), wrong origin,
  issuer/client change between callback and continuation, attempts exhaustion, concurrent double
  submit (exactly one write), row 7 racing an admin password reset (deterministic pause between
  verify and tx) creates neither identity binding nor session.

**Invitation preservation:** today the invite token lives only in React memory
(`frontend/src/app/useInviteVisit.ts:18-56`) and is lost on the provider redirect. V4-10 must carry
it across the round trip server-side: `startOidc` accepts an optional invite token, verifies it is
pending, and stores only its hash on the `oidc_flows` row; the callback copies the hash to the pending
continuation or, when the outcome is "signed in", immediately runs `acceptInvitation` for that hash in
the same transaction as account creation (rows 6, 6i). Expired/revoked/used invites at callback time
degrade to a generic "invitation no longer valid" with the sign-in still completed.

**First-login decision table** (evaluated in order inside one transaction that re-reads all state
after taking the identity row lock; no row ever grants admin; only an explicit valid invitation grants
membership):

| # | Situation | Outcome | Writes |
|---|---|---|---|
| 1 | `(issuer, subject)` already linked | Sign in. Email never used to choose the account. If the token carries a canonical verified email and the account has none, record it as `oidc_claim` only if no other account holds it (unique index; on conflict skip silently) | session (+ optional email) |
| 2 | Unlinked; canonical verified email; exactly one account holds that email with `email_source` `oidc_claim` or `account_confirmed`; that account has **no** identity for this issuer | Auto-attach `(issuer, subject)`, sign in. Safe because that email was itself proved by an OIDC login or by the account's own password (row 7) — never by an admin | identity + session, one tx |
| 2c | As row 2, but the matched account **already has a different subject** for this issuer (`UNIQUE (user_id, issuer)`, `011_oidc.sql`) | Do not attach (provider recycled the email to a new subject, or two provider accounts share it). Generic `identity_conflict`; user can still choose "continue as new" without the email | none |
| 3 | Unlinked; canonical verified email; no account holds it (all legacy v3 accounts, since none has an email) | **Do not attach.** Interstitial via pending continuation: "Already have a Tendo account? Confirm with your Tendo sign-in" (row 7) or "Continue as a new account" (row 6) | pending row only |
| 4 | Unlinked; email unique index lookup returns >1 (impossible with the index; defensive) | Block, generic `identity_conflict` | none |
| 5 | Unlinked; email missing, not canonical, or `email_verified` not boolean `true` | Never auto-attach. Same interstitial as row 3, both choices offered; row 6 creates the account with `email NULL`; row 7 binds identity but records **no** email | pending row only |
| 6 | "Continue as new" from rows 3/5 | JIT account: unique generated login (not user-chosen in this step, never derived from email local part), no local credential, no household. If the pending record carries a valid invite hash → accept it in the same tx (6i); else signed-in "join your household by invitation" screen. Email recorded as `oidc_claim` only if still unowned at commit | account + identity (+ email, + membership via invite) + session, one tx |
| 7 | "Confirm with Tendo sign-in" from rows 3/5: user enters local login + password; verified exactly like `createSession` (same throttle, same timing-safe failure) | Inside the tx, first lock the target credential row `FOR SHARE` and revalidate `credential_version`/hash against what was verified (§3), holding the lock through identity insert and commit, so a concurrent reset makes the old-password proof create neither a binding nor a session. Then re-read: the subject is still unlinked; the local account has no identity for this issuer; the email (if any) is still unowned. Then bind `(issuer, subject)` and, row 3 only, record the email as `account_confirmed`. Any re-read failure → generic `identity_conflict`, nothing written. This one-time sign-in-time proof is the V4_SCOPE "safe path for preexisting local users"; it is not the v3 Account-screen pairing UI that V4-11 removes | identity (+ email) + session, one tx |
| 7a | Row 7 password correct, but the OIDC subject got linked elsewhere between callback and confirm (concurrent tab/device) | Generic conflict; nothing written; pending record consumed | none |
| 7e | Row 7 when the pending email is now owned by another account (claimed mid-flow) | Bind identity only, skip email; tell the user the email was not saved | identity + session |
| 8 | User chose "continue as new" (row 6) by mistake but has a local account | No automatic merge of memberships or items, ever. Because row 1 always wins for that subject afterwards, the subject would be stuck on the JIT account. Recovery route V4-10 must provide: while the JIT account has **no household membership** (nothing to lose), the JIT account itself may "discard this account and connect to my existing Tendo account", which deletes the JIT account and its identity in one tx and restarts at row 7 via a fresh pending continuation. Once the JIT account has joined a household, no self-service recovery; documented as an admin/operator support case. Final UX decided in V4-10 under the security review | user-initiated delete + row 7 |
| 9 | Concurrent callbacks, same subject | Flow single-use delete picks one; identity PK insert conflicts → loser re-reads and becomes row 1 | — |
| 10 | Concurrent callbacks, different subjects, same verified email, no existing owner | Each reaches row 3 (no auto-JIT on email alone); whichever completes row 6/7 first owns the email; the other's email write hits the unique index and is skipped (row 7e behavior) — never retried into an attach | — |
| 11 | Provider email changes for a linked identity | Row 1 applies; ownership never moves; stored email is not overwritten automatically | — |

Retry rule: any unique-violation inside the decision transaction rolls back and **re-runs the whole
table from a fresh read**, at most once; it never re-applies the previously chosen row blindly.

Rejected: an `admin_confirmed` email source that row 2 trusts. An admin typing a member's address
proves nothing about who controls the provider account with that address; a malicious or mistaken
admin (or one fooled by a look-alike) would hand the member's account to whoever owns that provider
identity. Any future admin-assisted path must still require the member's own proof (row 7) and gets
its own security review.

Security review gate: V4-10 must not merge without an independent security-oriented review of this
table, the pending continuation, and invitation carry-over (Issue #418 acceptance).

Threat model: takeover via unverified or self-asserted email (row 2 trusts only emails proved by an
OIDC login or the account's own password; `email_verified` must be boolean `true`; never compare
display name or login); provider email recycling / second provider account with same email (row 2c);
issuer mix-up (keep issuer pinning and `(issuer, subject)` key; never match subject across issuers);
normalization collisions (one canonical function, case-sensitive local part, IDNA domain; unique
index over canonical strings); replay/CSRF of continuation (hashed single-use HttpOnly handle,
origin check, throttle, issuer/client binding); concurrency (unique constraints + full re-evaluation,
never check-then-insert without a constraint); enumeration (unauthenticated callback and
continuation outcomes stay generic; row 7 failure indistinguishable from bad password);
privilege (JIT accounts get no role and no household unless an invitation they carried is valid);
provider outage must not affect local login (local login is an unchanged separate code path; n20
`journey.spec.ts:3291` covers only the login page recovering from a failed `GET /api/v1/auth/oidc`
without submitting credentials, so V4-10 adds a test that local credential sign-in succeeds while the
provider is unreachable).

## 6. Invitations (V4-12)

| Fact | Evidence |
|---|---|
| Table with `expires_at`, `accepted_at`, `revoked_at`; 7-day lifetime | `009_invitations.sql:2-17`; `identity/invitations.go:22` |
| Status derived **server-side**: accepted → revoked → expired → pending | `identity/invitations.go:106-117`; list `invitation_admin.go:106-114` |
| List accepts only `limit`/`cursor`; SQL returns every household row | `api/openapi.yaml:412-421`; `identity/postgres/queries/invitations.sql:18-21` |
| No counts in API; UI renders every returned status, revoke only for pending | `api/openapi.yaml:1583-1589`; `features/members/InvitationRows.tsx:39-48` |

**Proposal:** add `state` query parameter to `listInvitations` (`pending`, `accepted`, `revoked`,
`expired`, `current`=not expired, default `all` for API compatibility) with the predicate applied in
SQL **before** `ORDER BY id LIMIT`, one clock sample per request, and the state bound into the cursor
(reject a cursor used with another state). The Members screen requests `current`. Client-side
filtering is rejected: a page of only expired rows would render empty while `nextCursor` exists. Any
future count must use the same predicate and instant. Server audit history is untouched.

**Delivered (V4-12, #420):** a narrower boolean `includeExpired` (default `true`; the Members screen
sends `false`) instead of the `state` enum, since V4 needs only "current" and no per-state tab. The
predicate, pre-`LIMIT` placement and single clock sample are as proposed. The cursor stays ID-only and
is not bound to the filter: ID keyset order is monotone, so switching the filter mid-walk can only
show or hide expired rows, never duplicate or skip a non-expired one. The Members screen also drops
any `expired` row it receives as a safety net. Contract: `api/openapi.yaml` `listInvitations`;
`docs/development/api.md`.

Existing tests to extend: `identity/postgres/invitations_expiry_test.go:10`,
`identity/postgres/invitations_test.go:387,563`, `invitation_keyset_property_test.go:11`,
`features/members/MembersScreen.test.tsx:170,480`, Playwright n7 (`journey.spec.ts:1170`).

## 7. Deployment: three Compose services → two (V4-16)

Current topology (`compose.yaml`):

| Component | Lines | Why it exists |
|---|---|---|
| `postgres` | 2-23 | PostgreSQL 18.6, named volume `postgres-data:/var/lib/postgresql`, healthcheck; receives `POSTGRES_PASSWORD` and `TENDO_DATABASE_PASSWORD` |
| root `configs.postgres-init` | 11-15, 66-78 | initdb script, runs **only on an empty data directory**: `CREATE ROLE tendo LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT PASSWORD …; GRANT CONNECT ON DATABASE tendo; REVOKE CREATE ON SCHEMA public FROM PUBLIC; GRANT USAGE ON SCHEMA public` |
| `migrate` | 25-33 | one-shot `/tendo migrate` as `postgres` (admin URL), `restart: "no"`, no hardening flags |
| `app` | 35-64 | restricted `DATABASE_URL` as `tendo`; waits for `migrate` `service_completed_successfully`; `read_only`, tmpfs `/tmp`, `no-new-privileges`, `cap_drop: [ALL]`, image user 65532 |

Mechanics to preserve: migration `001` refuses to run unless role `tendo` exists
(`001_setup.sql:32-38`) and grants table privileges per migration; objects are owned by the
migrating admin role; `tendo_schema_migrations(version, dirty)` with per-step dirty marking in one
transaction (`migrate.go:114-135`), session advisory lock `784193214` (`migrate.go:22,37`), refusal
of dirty/newer schema (`migrate.go:56-58,155-175`); the app independently runs `ValidateSchema`
before listening (`backend/cmd/tendo/main.go:99-119`). `/health/ready` only pings the pool
(`platform/httpx/server.go:125-143`). There is **no app healthcheck**, so `docker compose up --wait`
does not prove app readiness today. Credential split today: only `postgres` and the one-shot
`migrate` see the admin password; docs promise "the app itself does not receive database
administrator credentials" (`docs/admin/configuration.md:50`).

**Proposal (V4-16):**
1. Compose: delete `migrate`, root `configs`, and `TENDO_DATABASE_PASSWORD` from `postgres`; app
   `depends_on` only `postgres: service_healthy`; add `TENDO_DATABASE_ADMIN_URL` built from the
   existing `POSTGRES_PASSWORD` (no new `.env` key); keep volume, port, hardening. Add an app
   healthcheck only if the distroless image can run one without a shell (e.g. a `/tendo healthcheck`
   subcommand); otherwise document polling `/health/ready`.
2. Startup when the admin URL is set: open a 1-connection admin pool → ping → transaction with
   `pg_advisory_xact_lock(<separate bootstrap key>)` → create role `tendo` only if absent (same flags
   plus explicit `NOBYPASSRLS NOREPLICATION`, password from the restricted URL, server-side quoting,
   never logged) → idempotent CONNECT/USAGE/REVOKE grants → commit → `database.Migrate` (unchanged
   lock/ledger) → close admin pool → open restricted pool → serving-role check (3a) → `ValidateSchema` → listen. Any failure
   exits non-zero before a listener exists; never fall back to the admin pool for serving.
3. Existing role: never `ALTER ROLE … PASSWORD` (preserves the v3 contract that changing `.env`
   later does not rotate it, `docs/admin/update.md:95`); refuse to start if the existing role has
   superuser/createdb/createrole/bypassrls/replication, **or is a member of any role**
   (`pg_auth_members`), since `NOINHERIT` alone still allows `SET ROLE` to a granted role.
   Today only `household-backup-smoke.sh:345` checks attribute flags, and nothing checks memberships.
3a. Serving-role check, on **every** startup (with or without the admin URL, i.e. also for native
   tests and `go run`), run over the restricted pool before listening: `current_user = session_user`;
   the role has none of superuser/createdb/createrole/bypassrls/replication (`pg_roles`); it is a
   member of no role (`pg_auth_members WHERE member = current_user` empty); it does not own the schema
   or any Tendo table (ownership implies DDL); and `has_database_privilege(current_user,
   current_database(), 'CREATE')` and `has_schema_privilege(current_user, 'public', 'CREATE')` are
   false. In Compose the admin bootstrap additionally requires the serving role name to equal the role
   it created/validated (`tendo`). This replaces trusting whichever role `DATABASE_URL` names.
   Negative tests: `DATABASE_URL` set to the admin URL, role granted membership in
   `pg_write_all_data` or the admin role, role with `CREATEROLE`, role owning a table — each refuses to
   start with a non-secret error, with and without the admin URL configured.
4. Without the admin URL (native tests, `go run`) no bootstrap or migration runs: serving-role check
   (3a) → `ValidateSchema` → serve.
5. Restore: initdb no longer creates the role, so `docs/admin/backup-restore.md` and
   `scripts/household-backup-smoke.sh:297-301` need a role-only bootstrap (`tendo ensure-role` run via
   `docker compose run --rm --no-deps app ensure-role`) before `pg_restore` into an empty database.
6. Upgrade smoke must start the source release with **its own** three-service Compose
   (`scripts/upgrade-smoke.sh:17-18,221-238` currently uses the checkout's file) and add a v3.0.0
   source (schema 16).
7. Credential exposure (must be stated in docs/release notes and reviewed): the long-lived app
   container now holds the PostgreSQL administrator password in its environment even after the admin
   pool closes (visible to `docker inspect` and any code execution in the container). This is a
   deliberate reduction in isolation versus v3; runtime queries still use the restricted role.

Files V4-16 must change: `compose.yaml`; `deploy/compose.build.yaml:6-8`; `backend/cmd/tendo/main.go`;
`backend/internal/platform/config/config.go` + tests; new `platform/database/bootstrap.go` + tests;
`backend/cmd/tendo/runtime_test.go`; scripts `setup-smoke.sh`, `quickstart-smoke.sh`,
`upgrade-smoke.sh`, `household-backup-smoke.sh`, `proxy-smoke.sh:53-54,144-146,210-212`
(review `oidc-smoke.sh`); `.github/workflows/required.yml` deployment matrix; docs
`admin/install.md:3,56`, `admin/configuration.md:12,29-30,43,50`, `admin/update.md:31,40,66-67,95`,
`admin/troubleshooting.md:7,37-44`, `admin/backup-restore.md:48-66`, `development/runtime.md:29,37,45`,
`development/release.md:12,41-43`, `development/testing.md`, `development/release-qualification.md`,
`README.md:25-39`. Historical release notes stay unchanged.

Tests V4-16 must add: fresh empty volume; populated v3.0.0 volume upgrade; restart no-op;
concurrent two-instance start; admin unreachable / wrong admin password; dirty and newer schema;
existing role with different supplied password is not rotated; elevated existing role refused
(attribute flags and role memberships); restricted pool identity check (§7 3a); role flags and
runtime DML-only privileges; restore via `ensure-role`.

## 8. Migration order, backup and downgrade

Planned new migrations (numbering decided by merge order): roles owner→admin (V4-06), password
reset tokens + `credential_version` (V4-08), member person backfill + unique index (V4-09), account
email/provenance + `oidc_pending_logins` + invite hash on `oidc_flows` (V4-10), optional removal of the `link` purpose from `oidc_flows` (V4-11, only after the audit in §5).
Migrations are forward-only in production (the runner ignores `-- +goose Down`,
`migrate.go:107-111`); a v3.0.0 binary refuses the newer schema (`docs/admin/update.md:89`), so going
back to v3 requires restoring the pre-update backup (`docs/admin/update.md:91`). V4 release notes must
say so and the upgrade qualification must take and verify that backup first.

## 9. Acceptance scenarios for the V4 backlog

| Scenario | Proves | Owner |
|---|---|---|
| Populated v3 volume → V4: owner is admin, sessions, OIDC links, persons, items, history intact | migration safety | V4-06/09/10/16, V4-13 |
| Member cannot reach any admin API or the Admin center; admin cannot reach another household | authorization | V4-06/07 |
| Two admins demote each other concurrently → one admin remains | last-admin invariant | V4-06 |
| Reset link: single use, expiry, revoke, sessions invalidated, never logged | reset lifecycle | V4-08 |
| New OIDC user without account → signed in, no household, join by invitation | JIT | V4-10 |
| OIDC user with the same verified email an account proved earlier (OIDC claim or own password) → same account; second subject for same issuer → conflict | auto-attach | V4-10 |
| Unverified/missing email never attaches; legacy local account connects via sign-in-time password proof; continuation replay/cross-browser rejected; invite survives the provider round trip | takeover resistance | V4-10/11 |
| Login racing an admin password reset never leaves a session valid for the old password | reset safety | V4-08 |
| Account screen has no manual pairing form; existing linked identity still signs in | V4-11 | V4-11 |
| Member appears once as a person in pickers, filters, People and things | no duplicates | V4-09 |
| Expired invitations absent from Members list even when a page contains only expired rows | V4-12 | V4-12 |
| One `docker compose up -d` on an empty volume serves only after role + schema init | two-service deploy | V4-16 |
