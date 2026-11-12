"""Read-only owned-Issue lifecycle parsing and dependency-recovery advice.

Pure functions only: no network, no filesystem, no mutation. The selector
(`autonomy-work-selection.py`) feeds paginated Issue comments and receives
advisory `review-owned-dependencies` evidence. Nothing here authorises a
claim, a takeover or a mutation; an agent may act only after its own live
reconciliation under FLOW.

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

Blockers (any comment with a `BLOCKED` first line or a `Blocker:` line):
  explicit   BLOCKED kind=owned-dependency needs=#N recovery=combined
             BLOCKED kind=external recovery=none
  legacy     heading `Blocker: dependency ordering ...` (optionally a markdown
             heading) plus a proposal line `Proposed resolution ... deliver #A
             and #B as one PR`. Only decision lines (headings, `Blocker:` and
             `BLOCKED` lines) are screened for external/security/credential/
             secret/password/token/upstream wording, so incidental body prose
             never disqualifies. Reviewable only; never executable by prose.
  external   an explicit `BLOCKED ... kind=external` record excludes the group.
  unknown    anything else; fails closed.
"""
from __future__ import annotations

import re

TRUSTED_ASSOCIATIONS = frozenset({"OWNER", "MEMBER", "COLLABORATOR"})
KV_RE = re.compile(r"\b([a-z][a-z-]*)=(\S+)")
AGENT_RE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$")
NEEDS_RE = re.compile(r"^#(\d+)$")
ALLOWED_BLOCKED_KINDS = frozenset({"owned-dependency", "external"})
ALLOWED_RECOVERY = frozenset({"combined", "none"})
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
    """Explicit BLOCKED record from a first line, or unknown (fails closed)."""
    fields = _fields(line)
    kind, recovery, needs = fields.get("kind"), fields.get("recovery"), fields.get("needs")
    if kind not in ALLOWED_BLOCKED_KINDS or recovery not in ALLOWED_RECOVERY:
        return {"status": "unknown", "raw": line}
    if kind == "owned-dependency":
        match = NEEDS_RE.match(needs or "")
        if not match or recovery != "combined":
            return {"status": "unknown", "raw": line}
        return {"status": "explicit", "kind": kind, "needs": int(match.group(1)), "recovery": recovery}
    if needs is not None or recovery != "none":
        return {"status": "unknown", "raw": line}
    return {"status": "explicit", "kind": kind, "needs": None, "recovery": recovery}


def classify_blocker(comment: dict) -> dict | None:
    """Blocker record for a comment, or None when it is not a blocker comment."""
    body = comment.get("body") or ""
    if not BLOCKER_LINE_RE.search(body):
        return None
    base = {"id": int(comment["id"]), "createdAt": comment["createdAt"], "author": _author(comment)}
    first = _first_line(comment)
    if first.startswith("BLOCKED") and _fields(first).get("kind"):
        return {**base, **parse_blocked(first)}
    heading, proposal = LEGACY_HEADING_RE.search(body), PROPOSAL_RE.search(body)
    decision_lines = " ".join(m.group(0) for m in DECISION_LINE_RE.finditer(body))
    if EXTERNAL_RECORD_RE.search(body):
        return {**base, "status": "external", "raw": first}
    if heading and proposal and not DISQUALIFY_RE.search(decision_lines):
        numbers = sorted({int(proposal[1]), int(proposal[2])})
        if len(numbers) == 2:
            return {**base, "status": "legacy-ordering", "numbers": numbers}
    return {**base, "status": "unknown", "raw": first}


def replay(comments: list[dict]) -> dict:
    """Replay the trusted lifecycle of one Issue.

    Returns claim {login, agent, id, createdAt} or None; conflicts (foreign
    claims while a claim is held); blocked (latest blocker record or None).
    """
    claim = None
    conflicts: dict[tuple[str, str], dict] = {}
    blocker = None
    ordered = ordered_trusted(comments)
    for comment in ordered:
        line = _first_line(comment)
        head = line.split(" ", 1)[0] if line else ""
        agent = _fields(line).get("agent", "")
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
        record = classify_blocker(comment)
        if record is not None:
            blocker = record

    return {"claim": claim, "conflicts": list(conflicts.values()), "blocked": blocker}


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


def plan_recovery(owned_open: dict, status: dict, lifecycle: dict, declared: dict,
                  closed, linked_prs: dict) -> dict | None:
    """Advisory dependency-recovery evidence (priority 6), never executable.

    owned_open: {number: issue}; status: {number: status label};
    lifecycle: {number: replay(...)}; declared: {number: [Blocked by numbers]};
    closed: callable(number) -> bool for Issues outside owned_open;
    linked_prs: {number: [open non-dependency PR numbers referencing it]}.

    Groups are evaluated in deterministic order: explicit pairs, legacy
    ordering reviews, declared cycles. Each group must be eligible (members
    owned, open, status-valid, blocker-clean, outside dependencies closed)
    BEFORE a held group is reported. Returns one advisory dict or None.
    """
    def life(number: int) -> dict:
        return lifecycle.get(number) or {}

    def held(number: int) -> bool:
        return bool(life(number).get("claim") or life(number).get("conflicts"))

    edges = {n: {d for d in declared.get(n, []) if d in owned_open} for n in owned_open}
    groups = []  # (order, group, evidence, anchor, blocker record)
    for number in sorted(owned_open):
        record = life(number).get("blocked")
        if not record or status.get(number) != "status:blocked":
            continue
        if record.get("status") == "explicit" and record.get("kind") == "owned-dependency" \
                and record.get("recovery") == "combined":
            other = record.get("needs")
            if other in owned_open and other != number:
                groups.append(((0, number), {number, other}, "explicit", number, record))
                edges[number].add(other)
        elif record.get("status") == "legacy-ordering" and number in record["numbers"]:
            (other,) = [m for m in record["numbers"] if m != number]
            if other in owned_open and number in declared.get(other, []):
                groups.append(((1, number), {number, other}, "legacy-ordering", number, record))
    for scc in _sccs(owned_open, edges):
        only = next(iter(scc)) if len(scc) == 1 else None
        if only is not None and only not in edges.get(only, set()):
            continue
        groups.append(((2, min(scc)), set(scc), "cycle", None, None))

    def member_blocker_ok(member: int, group: set[int]) -> bool:
        record = life(member).get("blocked")
        if record is None:
            return True
        if record.get("status") == "explicit" and record.get("kind") == "owned-dependency" \
                and record.get("recovery") == "combined":
            return record.get("needs") in group and record.get("needs") != member
        if record.get("status") == "legacy-ordering":
            return member in record["numbers"] and set(record["numbers"]) <= group
        return False  # external, unknown or any other blocker excludes the group

    def outside_closed(group: set[int]) -> bool:
        for member in group:
            for dep in declared.get(member, []):
                if dep in group:
                    continue
                if dep in owned_open or not closed(dep):
                    return False
        return True

    for _order, group, evidence, anchor, record in sorted(groups, key=lambda g: g[0]):
        if any(m not in owned_open or status.get(m) not in ("status:idle", "status:blocked")
               for m in group):
            continue
        if evidence == "cycle" and any(status.get(m) != "status:idle" for m in group):
            continue
        if evidence != "cycle" and status.get(anchor) != "status:blocked":
            continue
        if not all(member_blocker_ok(m, group) for m in group) or not outside_closed(group):
            continue  # eligibility first; a held group is reported only when eligible
        holders = sorted(m for m in group if held(m))
        claimed = sorted(m for m in group if life(m).get("claim"))
        if evidence == "legacy-ordering":
            if not claimed:
                continue  # evidence review is for the existing claimant only
            if not linked_prs.get(anchor) and not linked_prs.get(sorted(group - {anchor})[0]):
                continue  # requires an existing linked PR
        if evidence == "cycle" and holders:
            continue  # claimed body cycles are diagnosed, never reported as recoverable
        claimant = claimed[0] if claimed else (holders[0] if holders else None)
        pr_sources = [n for n in sorted(group) if linked_prs.get(n)]
        pr = min(linked_prs[pr_sources[0]]) if pr_sources else None
        return {"mode": "advisory", "kind": "review-owned-dependencies", "group": sorted(group),
                "issue": claimant if claimant is not None else (anchor or min(group)),
                "evidence": evidence,
                "blocker": None if record is None else {"comment": record["id"],
                                                         "createdAt": record["createdAt"],
                                                         "status": record["status"]},
                "pr": pr,
                "claim": None if claimant is None else {**life(claimant)["claim"], "issue": claimant},
                "conflicts": life(claimant).get("conflicts", []) if claimant is not None else [],
                "reason": f"{evidence} group {sorted(group)} needs live reconciliation; "
                          "the selector never authorises a claim, takeover or mutation"}
    return None
