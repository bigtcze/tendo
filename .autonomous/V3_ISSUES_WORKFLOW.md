# V3 GitHub Issues — Permanent autonomous execution rules

status=locked_human_authorized
agent_must_not_edit=this_file
release=v3.0.0
milestone=Tendo v3.0.0 - Everyday Elegance
managed_issue_label=tendo:v3-autonomous
source_of_truth=GitHub_Issues_for_V3_work_status_and_progress

## Authority and scope
- Product/architecture/design/DoD invariants remain in locked repository
  documents. GitHub Issues are the ONLY current planning/progress/claim/status
  source of truth for V3. Do not recreate STATE.md or another progress log.
- An Issue is AGENT-MANAGED V3 work ONLY if it is in `bigtcze/tendo`, is
  NOT a pull request, belongs to milestone `Tendo v3.0.0 - Everyday Elegance`
  AND has the EXACT label `tendo:v3-autonomous`. Require BOTH milestone and
  label on every backlog, status, claim, PR-link and completion operation.
- All other Issues are outside autonomous authority, including user-created
  Issues, issues with a V3-looking title, and unlabelled Issues placed into
  the V3 milestone. Never claim, modify labels, comment on, close, auto-fix,
  or use those issues as work items or completion blockers. Do not silently
  adopt existing third-party Issues by adding `tendo:v3-autonomous`.
- The dedicated label is an opt-in ownership boundary, NOT a generic status
  label. It is created by the V3 migration; future agents create NEW V3 work
  Issues using `gh issue create --milestone ... --label tendo:v3-autonomous`
  (and `--label status:idle`) at creation time, never afterward as a repair.
- If a required V3-XX identifier already exists on a non-managed Issue,
  report the collision as a blocker; NEVER repurpose, label, edit or close
  that Issue. Resolve naming without touching external Issues.
- Managed V3 backlog must account for ALL in-scope deliverables and defects.
  Each issue needs concrete acceptance criteria, DoD references, dependencies
  and verification. When an unrelated issue reveals a genuine release bug,
  investigate the problem independently and create a NEW managed V3 defect
  Issue if verified; do not take ownership of the external Issue.
- New defects, missing V3 work and UX findings are tracked in NEW labelled
  issues and linked to owning PRs. Never silently lose scope or close
  unfinished managed work.
- Issue bodies carry the stable acceptance contract. Comments record claims,
  decisions, test evidence, blockers, PR links, and handoff. Never use comments
  as an unstructured substitute for updating stale acceptance criteria.
- Avoid separate state files, status dashboards, ticket sync bots, external
  locks/leases or additional coordinators. Use GitHub CLI/API directly.

## Labels and states
- `status:idle`: OPEN issue available for claim once dependencies are done.
- `status:in-progress`: OPEN issue claimed by a specific agent; claim and
  progress are also recorded in chronological issue comments.
- `status:blocked`: OPEN issue cannot currently proceed, with a comment
  naming the blocker, evidence and safe next action.
- Done = CLOSED issue after linked implementation PR has MERGED, required CI
  and green main are verified, all acceptance criteria have evidence.
- Exactly ONE status:* label on every open AGENT-MANAGED V3 issue, in addition
  to permanent `tendo:v3-autonomous`; keep the ownership label on closed Issues.
  Do not use a `done` status label; closing is the completion signal.
- If the ownership label or milestone is removed from a claimed Issue, stop
  all actions on it immediately and treat it as externally managed.
- If interrupted, reconcile real PRs, issue comments and CI before taking
  another issue. A new owner must explain the previous claim/handoff.

## Claim protocol (simple, no coordinator)
1. At EVERY cycle query ONLY managed open Issues using BOTH V3 milestone AND
   `--label tendo:v3-autonomous` (or equivalent GitHub API filter), then
   reconcile their dependencies, claims and associated PRs. Never use all
   repository Issues or all milestone Issues as the agent's work queue.
2. Choose an OPEN managed `status:idle` Issue with completed prerequisites.
   Reconfirm milestone AND label immediately before claiming or mutating it.
   Do not bypass a blocker or start another agent's active Issue.
3. Add a unique claim comment, e.g. `CLAIM agent=<stable identity> time=<UTC>`;
   change `status:idle` to `status:in-progress` using GitHub API.
4. Wait 3–5 seconds, re-read issue, milestone, ownership/status labels AND
   complete chronological comments (paginate), then confirm claim ownership.
   If it lost `tendo:v3-autonomous` or the milestone, STOP without new edits.
5. If two agents claimed concurrently, the earliest still-active claim
   comment by server-created timestamp, then comment ID, wins. Loser adds
   `RELEASE ... collision` comment, does not code, and selects another idle
   issue. A RELEASE/HANDOFF comment explicitly retires that agent's claim.
6. Work in ONE focused branch/PR. Link via `Closes #<number>` ONLY to an
   agent-managed labelled Issue and only when acceptance is fully satisfied.
   NEVER link/auto-close outside Issues. Put evidence in owned Issue/PR.
7. Before post-merge comments/status updates/closure, RECHECK exact milestone
   and ownership label. After green merged main and proof, comment DONE and
   close the managed Issue. If unresolved, leave it owned and open with
   `status:idle` or `status:blocked` plus the remaining work.
8. Never claim success from CI alone or close all Issues prematurely.

## Release gates
- V3-18 owns release notes, annotated v3.0.0 tag, published GitHub Release,
  public amd64/arm64 GHCR images and actual install/upgrade verification;
  close ONLY after the release is publicly verified.
- V3-19 owns the FINAL V3_COMPLETE marker PR, green checks/merge and clean
  `main`. Close only after those conditions are proven.
- No V3_COMPLETE and no loop exit while ANY agent-managed, V3-milestone,
  `tendo:v3-autonomous`-labelled Issue remains open (except the active V3-19
  finalization protocol), a verified release-blocking defect remains, or
  published release evidence is absent. Unlabelled user/third-party Issues
  are NOT autonomous work items and are NOT counted in V3 backlog closure.
- Historical V1/V2/V2.1 completion markers and v2.1.0 remain untouched.
