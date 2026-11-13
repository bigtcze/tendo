"""Read-only owned-Issue lifecycle parsing and dependency-recovery advice.

Pure functions only: no network, no filesystem, no mutation. The selector
(`autonomy-work-selection.py`) feeds paginated Issue comments and receives
advisory `review-owned-dependencies` groups. Nothing here authorises a claim, a
takeover or a mutation; the executing agent acts only after its own live
reconciliation under FLOW "Dependency recovery". Nothing here depends on which
coding agent, model or runtime wrote a comment: identity is the comment author
login plus the `agent=` value, compared verbatim.

Trust: only comments by OWNER, MEMBER or COLLABORATOR count. Lifecycle order
is the server `createdAt` timestamp, then the comment `id`.

Lifecycle (first line of a comment body):
  CLAIM agent=<id> time=<UTC>      the first active claim is kept. Repeats by
                                   the same login+agent keep the first id. A
                                   different login+agent is a conflict; it
                                   never takes over.
  RELEASE agent=<id> time=<UTC>    retires the active claim only when the
                                   comment author login AND agent equal the
                                   claimant's. A non-holder RELEASE removes only
                                   its own conflict record.
  HANDOFF agent=<id> to=<id> ...   same authorisation as RELEASE; retires only.
  Holder retirement clears all conflicts (no permanent stale conflicts).
  RECOVERY agent=<id> time=<UTC> decision=deferred basis=<fingerprint>
                                   a reviewer examined the advisory group with
                                   that evidence fingerprint and cannot proceed
                                   now; the same unchanged advice is suppressed
                                   (suppression only, never authority). Any
                                   relevant change (status, blocker, claim,
                                   declared edges, linked PR or its head)
                                   yields a new fingerprint.

Blockers (any comment with a `BLOCKED` first line or a `Blocker:` line):
  structured BLOCKED kind=owned-dependency needs=#A[,#B...] recovery=<combined|reorder>
             BLOCKED kind=external recovery=none
  legacy     (compatibility only, comments created before LEGACY_PROSE_UNTIL)
             heading `Blocker: dependency ordering ...` plus a proposal line
             `Proposed resolution ... deliver #A and #B as one PR`. Only
             decision lines are screened for external/security/credential/
             secret/password/token/upstream wording. Never executable by prose.
  external   an explicit `BLOCKED ... kind=external` record excludes the group.
  unknown    anything else (including newer legacy-style prose); fails closed.

Recovery groups are derived from one rule for any graph: declared `Blocked by`
edges among open owned Issues plus mutual coupling edges from structured (or
legacy) blocker records; every strongly connected component that is larger
than one Issue or has a self-edge is a candidate. Eligibility is identical for
every shape and fails closed on anything unknown.
"""
from __future__ import annotations

import hashlib
import json
import re

TRUSTED_ASSOCIATIONS = frozenset({"OWNER", "MEMBER", "COLLABORATOR"})
KV_RE = re.compile(r"\b([a-z][a-z-]*)=(\S+)")
AGENT_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$")
NEEDS_RE = re.compile(r"^#\d+(?:,#\d+)*$")
FINGERPRINT_RE = re.compile(r"^[0-9a-f]{16}$")
COMBINABLE_RECOVERY = frozenset({"combined", "reorder"})
# Legacy prose is honoured only for comments recorded before structured BLOCKED
# records became canonical (merge of governance PR #466); later prose of that
# shape is unknown and fails closed. Compatibility, not a permanent protocol.
LEGACY_PROSE_UNTIL = "2026-11-12T22:58:48Z"
BLOCKER_LINE_RE = re.compile(r"^[ \t]*(?:#{1,6}[ \t]*)?(?:BLOCKED\b|Blocker:)", re.MULTILINE)
LEGACY_HEADING_RE = re.compile(r"^[ \t]*(?:#{1,6}[ \t]*)?(?:BLOCKED:|Blocker:)[ \t]*dependency ordering\b",
                               re.IGNORECASE | re.MULTILINE)
PROPOSAL_RE = re.compile(r"^[ \t]*Proposed resolution\b[^\n]*?\bdeliver\s+#(\d+)\s+and\s+#(\d+)\s+as one PR\b",
                         re.IGNORECASE | re.MULTILINE)
# Decision lines are markdown headings and `Blocker:`/`BLOCKED` lines only.
# Incidental body prose (e.g. "No credentials are missing.") never disqualifies.
DECISION_LINE_RE = re.compile(r"^[ \t]*(?:#{1,6}[ \t]+.*|Blocker:.*|BLOCKED\b.*)$", re.MULTILINE)
EXTERNAL_RECORD_RE = re.compile(r"^[ \t]*(?:#{1,6}[ \t]*)?BLOCKED\b[^\n]*\bkind=external\b", re.MULTILINE)
DISQUALIFY_RE = re.compile(r"external|security|credential|secret|password|token|upstream", re.IGNORECASE)


class LifecycleError(Exception):
    """Malformed lifecycle input; callers exclude the Issue and note it."""


def is_trusted(comment: dict) -> bool:
    return str(comment.get("authorAssociation") or "").upper() in TRUSTED_ASSOCIATIONS


def _author(comment: dict) -> str:
    author = comment.get("author") or {}
    return author.get("login", "") if isinstance(author, dict) else str(author)


def _first_line(comment: dict) -> str:
    lines = (comment.get("body") or "").strip().splitlines()
    return lines[0].strip() if lines else ""


def _fields(line: str) -> dict:
    return {key: value for key, value in KV_RE.findall(line)}


def ordered_trusted(comments: list[dict]) -> list[dict]:
    """Trusted comments in lifecycle order (server timestamp, then id)."""
    for comment in comments:
        if not isinstance(comment, dict):
            raise LifecycleError("comment is not an object")
        if not comment.get("createdAt") or comment.get("id") is None:
            raise LifecycleError("comment without createdAt/id cannot be ordered")
    trusted = [c for c in comments if is_trusted(c)]
    return sorted(trusted, key=lambda c: (c["createdAt"], int(c["id"])))


def parse_blocked(line: str) -> dict:
    """Structured BLOCKED record from a first line, or unknown (fails closed)."""
    fields = _fields(line)
    kind, recovery, needs = fields.get("kind"), fields.get("recovery"), fields.get("needs")
    if kind == "owned-dependency":
        if recovery not in COMBINABLE_RECOVERY or not NEEDS_RE.match(needs or ""):
            return {"status": "unknown", "raw": line}
        numbers = sorted({int(n) for n in re.findall(r"\d+", needs)})
        return {"status": "explicit", "kind": kind, "needs": numbers, "recovery": recovery}
    if kind == "external" and recovery == "none" and needs is None:
        return {"status": "explicit", "kind": kind, "needs": [], "recovery": recovery}
    return {"status": "unknown", "raw": line}


def classify_blocker(comment: dict) -> dict | None:
    """Blocker record for a comment, or None when it is not a blocker comment."""
    body = comment.get("body") or ""
    if not BLOCKER_LINE_RE.search(body):
        return None
    base = {"id": int(comment["id"]), "createdAt": comment["createdAt"],
            "updatedAt": comment.get("updatedAt"), "author": _author(comment)}
    first = _first_line(comment)
    if first.startswith("BLOCKED") and _fields(first).get("kind"):
        return {**base, **parse_blocked(first)}
    if EXTERNAL_RECORD_RE.search(body):
        return {**base, "status": "external", "raw": first}
    # GitHub serialises timestamps as fixed-width UTC `...Z`, so string order is time order.
    if comment["createdAt"] < LEGACY_PROSE_UNTIL:
        heading, proposal = LEGACY_HEADING_RE.search(body), PROPOSAL_RE.search(body)
        decision_lines = " ".join(m.group(0) for m in DECISION_LINE_RE.finditer(body))
        if heading and proposal and not DISQUALIFY_RE.search(decision_lines):
            numbers = sorted({int(proposal[1]), int(proposal[2])})
            if len(numbers) == 2:
                return {**base, "status": "legacy-ordering", "numbers": numbers}
    return {**base, "status": "unknown", "raw": first}


def replay(comments: list[dict]) -> dict:
    """Replay the trusted lifecycle of one Issue.

    Returns claim {login, agent, id, createdAt} or None; conflicts (foreign
    claims while a claim is held); blocked (latest blocker record or None);
    deferrals (RECOVERY decision=deferred records, oldest first).
    """
    claim = None
    conflicts: dict[tuple[str, str], dict] = {}
    blocker = None
    deferrals: list[dict] = []
    for comment in ordered_trusted(comments):
        line = _first_line(comment)
        head = line.split(" ", 1)[0] if line else ""
        fields = _fields(line)
        agent = fields.get("agent", "")
        login = _author(comment)
        cid = int(comment["id"])
        who = (login, agent)
        if head == "CLAIM":
            if not AGENT_RE.match(agent) or not login:
                raise LifecycleError(f"CLAIM without a valid agent and login in comment {cid}")
            if claim is None:
                claim = {"login": login, "agent": agent, "id": cid, "createdAt": comment["createdAt"]}
            elif who != (claim["login"], claim["agent"]):
                conflicts.setdefault(who, {"login": login, "agent": agent, "id": cid,
                                           "createdAt": comment["createdAt"],
                                           "holder": claim["agent"]})
            continue
        if head in ("RELEASE", "HANDOFF"):
            if claim is not None and who == (claim["login"], claim["agent"]):
                claim, conflicts = None, {}
            else:
                conflicts.pop(who, None)
            continue
        if head == "RECOVERY":
            basis = fields.get("basis", "")
            if fields.get("decision") == "deferred" and FINGERPRINT_RE.match(basis) \
                    and AGENT_RE.match(agent) and login:
                deferrals.append({"id": cid, "login": login, "agent": agent, "basis": basis})
            continue  # never a blocker record, whatever the body says
        record = classify_blocker(comment)
        if record is not None:
            blocker = record

    return {"claim": claim, "conflicts": list(conflicts.values()), "blocked": blocker,
            "deferrals": deferrals}


def _sccs(nodes, edges: dict) -> list[set[int]]:
    """Strongly connected components (Tarjan), deterministic node order."""
    index: dict[int, int] = {}
    low: dict[int, int] = {}
    stack: list[int] = []
    on_stack: set[int] = set()
    out: list[set[int]] = []
    counter = [0]

    def visit(node: int) -> None:
        index[node] = low[node] = counter[0]
        counter[0] += 1
        stack.append(node)
        on_stack.add(node)
        for other in sorted(edges.get(node, ())):
            if other not in index:
                visit(other)
                low[node] = min(low[node], low[other])
            elif other in on_stack:
                low[node] = min(low[node], index[other])
        if low[node] == index[node]:
            component: set[int] = set()
            while True:
                other = stack.pop()
                on_stack.discard(other)
                component.add(other)
                if other == node:
                    break
            out.append(component)

    for node in sorted(nodes):
        if node not in index:
            visit(node)
    return out


def _coupling(record: dict | None) -> tuple[str, list[int]] | None:
    """(evidence, coupled Issue numbers) for a combinable blocker record."""
    if not record:
        return None
    if record.get("status") == "explicit" and record.get("kind") == "owned-dependency" \
            and record.get("recovery") in COMBINABLE_RECOVERY:
        return "explicit", list(record["needs"])
    if record.get("status") == "legacy-ordering":
        return "legacy-ordering", list(record["numbers"])
    return None


def fingerprint(evidence: dict) -> str:
    """Stable short digest of the evidence that justifies one advisory group."""
    raw = json.dumps(evidence, sort_keys=True, separators=(",", ":"))
    return hashlib.sha256(raw.encode()).hexdigest()[:16]


def recovery_groups(owned_open: dict, status: dict, lifecycle: dict, declared: dict,
                    closed, linked_prs: dict, notes: list[str] | None = None,
                    pr_heads: dict | None = None) -> tuple[list[dict], set[int]]:
    """Eligible advisory recovery groups and the Issues they protect.

    owned_open: {number: issue} (open owned Issues with replayed comments);
    status: {number: status label}; lifecycle: {number: replay(...)};
    declared: {number: [Blocked by numbers]}; closed: callable(number) -> bool,
    which must be False for every open Issue (owned or not); linked_prs:
    {number: [open non-dependency PR numbers referencing it]}; pr_heads:
    {PR number: head sha} (evidence only).

    Returns (groups, protected). groups is deterministic and never executable.
    protected holds every member of every group that carries a coupling record,
    whether or not it is advised (deferred, ineligible or contested): such an
    Issue is part of a recorded combined/reordered delivery and must never be
    offered as an ordinary claim. Ranking against other work is the caller's job.
    """
    notes = notes if notes is not None else []
    pr_heads = pr_heads or {}

    def life(number: int) -> dict:
        return lifecycle.get(number) or {}

    edges = {n: {d for d in declared.get(n, []) if d in owned_open} for n in owned_open}
    anchors: dict[int, tuple[str, list[int]]] = {}
    protected: set[int] = set()
    for number in sorted(owned_open):
        coupling = _coupling(life(number).get("blocked"))
        if coupling is None or status.get(number) != "status:blocked":
            continue
        evidence, coupled = coupling
        others = [m for m in coupled if m != number]
        if evidence == "legacy-ordering" and (number not in coupled or len(others) != 1
                                              or others[0] not in owned_open
                                              or number not in declared.get(others[0], [])):
            continue  # legacy prose only counts with the declared reverse edge
        if not others:
            continue
        anchors[number] = (evidence, others)
        # A recorded partner is protected even if it cannot join a group (for
        # example its own lifecycle is malformed); non-owned numbers are inert.
        protected.update([number, *others])
        for other in others:
            if other in owned_open:
                edges[number].add(other)
                edges[other].add(number)  # coupled Issues must ship together

    found: list[dict] = []
    for scc in _sccs(owned_open, edges):
        only = next(iter(scc)) if len(scc) == 1 else None
        if only is not None and only not in edges.get(only, set()):
            continue
        group = sorted(scc)
        if any(m in anchors for m in group):
            protected.update(group)
        # Every prerequisite of any member that lies outside this group, owned
        # or not, declared or recorded (by a blocked or an idle member), must
        # already be closed.
        outside = sorted({d for m in group
                          for d in [*declared.get(m, []),
                                    *(_coupling(life(m).get("blocked")) or ("", []))[1]]
                          if d not in scc})
        advice = _eligible(group, anchors, status, life, outside, closed, linked_prs, notes,
                           {m: sorted(declared.get(m, [])) for m in group}, pr_heads)
        if advice is not None:
            found.append(advice)
        elif any(m in anchors for m in group):
            notes.append(f"recovery group {group} is not advised; its members stay unclaimable "
                         "until its blocker record, claims or prerequisites change")
    return found, protected


def _record_evidence(record: dict | None) -> dict | None:
    """Everything a blocker record asserts, including its edit time."""
    if not record:
        return None
    return {k: record.get(k) for k in ("id", "status", "kind", "needs", "numbers", "recovery",
                                       "updatedAt", "raw")}


def _eligible(group, anchors, status, life, outside, closed, linked_prs, notes,
              declared, pr_heads) -> dict | None:
    """Advice for one strongly connected group, or None when it is not safely reviewable."""
    if any(status.get(m) not in ("status:idle", "status:blocked") for m in group):
        return None
    for member in group:
        record = life(member).get("blocked")
        if record is None and status.get(member) == "status:idle":
            continue
        if status.get(member) == "status:blocked" and member not in anchors:
            return None  # blocked by an external, unknown or unproven record
        if _coupling(record) is None:
            return None  # an idle member with a non-coupling blocker record is unknown evidence
    if any(not closed(dep) for dep in outside):
        return None  # unresolved outside prerequisite: not an internal ordering problem
    group_anchors = [m for m in group if m in anchors]
    claims = {m: life(m)["claim"] for m in group if life(m).get("claim")}
    held = [m for m in group if claims.get(m) or life(m).get("conflicts")]
    kinds = {anchors[a][0] for a in group_anchors}
    evidence = "explicit" if "explicit" in kinds else ("legacy-ordering" if kinds else "cycle")
    if evidence == "cycle" and held:
        return None  # a claimed declared cycle is the claimant's own diagnosis
    if len({(c["login"], c["agent"]) for c in claims.values()}) > 1:
        notes.append(f"recovery group {group} spans claims by different identities; "
                     "explicit HANDOFF is required before combined delivery")
        return None
    if evidence == "legacy-ordering" and (not claims or not any(linked_prs.get(m) for m in group)):
        return None  # historical prose is reviewed only for its claimant with a linked PR
    claimant = min(claims) if claims else None
    target = claimant if claimant is not None else (min(group_anchors) if group_anchors else min(group))
    anchor = target if target in anchors else (min(group_anchors) if group_anchors else None)
    record = life(anchor).get("blocked") if anchor is not None else None
    linked = sorted({n for m in group for n in linked_prs.get(m, [])})
    basis = fingerprint({
        "group": group,
        "members": {str(m): {"status": status.get(m),
                             "blocked": _record_evidence(life(m).get("blocked")),
                             "claim": (life(m).get("claim") or {}).get("id"),
                             "conflicts": sorted(c["id"] for c in life(m).get("conflicts", [])),
                             "declared": declared[m],
                             "prs": [[n, pr_heads.get(n)] for n in sorted(linked_prs.get(m, []))]}
                     for m in group},
        "outside": outside,
    })
    # A deferral only suppresses advice whose evidence is byte-for-byte unchanged;
    # it never grants or removes authority, so any trusted reviewer may record it.
    # Without every linked PR head the evidence is incomplete, so no deferral applies.
    heads_known = all(pr_heads.get(n) for n in linked)
    for member in group:
        for deferral in life(member).get("deferrals", []):
            if heads_known and deferral["basis"] == basis:
                notes.append(f"recovery group {group} deferred by comment {deferral['id']} on "
                             f"#{member} with unchanged evidence {basis}; not re-reviewed")
                return None
    holder = claims.get(claimant) if claimant is not None else None
    pr_sources = [m for m in group if linked_prs.get(m)]
    return {"mode": "advisory", "kind": "review-owned-dependencies", "group": group,
            "issue": target, "evidence": evidence, "fingerprint": basis,
            "recovery": (record or {}).get("recovery") if evidence == "explicit" else None,
            "blocker": None if record is None else {"comment": record["id"],
                                                     "createdAt": record["createdAt"],
                                                     "status": record["status"]},
            "pr": min(linked_prs[pr_sources[0]]) if pr_sources else None,
            "claim": None if holder is None else {**holder, "issue": claimant},
            "conflicts": life(claimant).get("conflicts", []) if claimant is not None else [],
            "reason": f"{evidence} group {group} needs live reconciliation; "
                      "the selector never authorises a claim, takeover or mutation"}
