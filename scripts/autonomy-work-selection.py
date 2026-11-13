#!/usr/bin/env python3
"""Evergreen autonomy work selection.

Derives the single next work item for an autonomy controller cycle purely
from live GitHub state. It keeps no state of its own; the dependency-fairness
count is recomputed from merged PR history (ordered by merge time) on every
run, so a controller restart cannot lose or reset it. Incomplete data (a list
that hits its fetch limit) is an error, never a silent truncation.

Ownership is version-neutral: an agent-owned work Issue is an Issue in the
target repository labelled exactly `release`. Milestone, title and any version
names are ignored by the ownership test, so a future release line needs no
code change.

Priority (the first matching rule wins; see the shared spec for the full
contract):
  1 red-main               latest completed required.yml push run on main is
                           not success/skipped
  2 security-pr            open `security`-labelled PR (not blocked-triaged)
    security-alert         open Dependabot alert with a patched version
  3 active-pr              open PR referencing an open, not `status:blocked`
                           owned Issue; a Renovate PR already carrying commits
                           by a known non-Renovate login; or an open non-draft
                           `release/v*` release-notes PR
    resume-claimed-issue   owned `status:in-progress` Issue with no open PR
                           referencing it
  4 triage-aged-renovate   Renovate PR untriaged for more than 48 h, oldest first
  5 dependency-slot        >= 2 feature PRs merged since the last dependency
                           PR, and an actionable dependency PR exists
  6 claim-issue            dependency-ready `status:idle` owned Issue, or
    review-owned-dependencies  an eligible recoverable dependency group
                           (advisory only); both ranked together by the same
                           Issue rank (recurrence, explicit Priority: P<n>,
                           then numeric code, then Issue number; release
                           coordinator Issues last), a group at the rank of
                           its most urgent member
  7 dependency-pr          remaining actionable dependency PR
  8 security-release       a `security`-labelled PR merged since the latest
                           published release, plus runtime-meaningful changes
    patch-release          >= 14 days since the latest published release, plus
                           runtime-meaningful changes; both also require a
                           completed green required.yml run on current main HEAD
  idle                     nothing actionable

Triage marker: a PR comment whose first line is
  TRIAGE agent=<id> time=<UTC> decision=<actionable|blocked> target=<exact PR title>
The latest marker counts while its target equals the current PR title (the
title names the dependency and version). `blocked` with an unchanged target
is skipped for BLOCKED_RECHECK_DAYS; after that, or once the title (version)
changes, the PR counts as untriaged again so upstream and local changes are
re-checked (a still-unchanged blocker is re-recorded, not re-run).

Usage: autonomy-work-selection.py [--repo OWNER/NAME] [--snapshot FILE]
                                  [--now ISO8601] [--self-check]

Exit codes: 0 decision printed, 2 data error (`SELECTION ERROR` on stderr; no
DECISION), 4 rate limited (`RATE_LIMITED reset=<unix epoch>` on stderr; no
DECISION), 1 --self-check failure.
"""
from __future__ import annotations

import argparse
from datetime import datetime, timedelta, timezone
import json
from pathlib import Path
import re
import subprocess
import sys

sys.path.insert(0, str(Path(__file__).resolve().parent))
import autonomy_dependencies as deps  # noqa: E402  (read-only lifecycle helpers)

REPO = "bigtcze/tendo"
OWNED_LABEL = "release"
STATUS_LABELS = ("status:idle", "status:in-progress", "status:blocked")
AGED_HOURS = 48
BLOCKED_RECHECK_DAYS = 7
FETCH_LIMIT = 1000
PR_COMMITS_API_CAP = 250  # GitHub's pulls/{n}/commits returns at most 250 commits
FEATURES_PER_DEPENDENCY_SLOT = 2
PATCH_RELEASE_DAYS = 14
GH_TIMEOUT_SECONDS = 240
MIN_QUOTA = 150  # keep a reserve so a whole controller cycle fits in the quota
MERGED_PAGE_CAP = 50
COMPARE_FILE_CAP = 300  # the compare API truncates its `files` array at 300
RENOVATE_LOGINS = {"app/renovate", "renovate[bot]", "renovate"}

TRIAGE_RE = re.compile(
    r"^TRIAGE\s+.*?\bdecision=(?P<decision>actionable|blocked)\s+target=(?P<target>.+?)\s*$")
REFS_RE = re.compile(r"\brefs\s+#(\d+)\b", re.IGNORECASE)
BLOCKED_BY_RE = re.compile(r"^Blocked by:\s*(?P<refs>.*)$", re.MULTILINE)
CODE_RE = re.compile(r"^[A-Za-z][A-Za-z0-9]*-(\d+)\b")
PRIORITY_RE = re.compile(r"^Priority:\s*P(\d+)\s*$", re.MULTILINE)
RELEASE_LINE_RE = re.compile(r"^Release:\s*(v\d+\.\d+\.\d+)\s*$", re.MULTILINE)
VERSION_TOKEN_RE = re.compile(r"\bv(\d+)\.(\d+)\.(\d+)\b")
TAG_RE = re.compile(r"^v(\d+)\.(\d+)\.(\d+)$")
RATE_LIMIT_RE = re.compile(r"rate limit|HTTP 429", re.IGNORECASE)
RESET_RE = re.compile(r"(?:x-ratelimit-reset|reset)[^0-9]{0,20}(\d{9,})", re.IGNORECASE)

# Release-change classification: paths that ship to users at runtime.
RUNTIME_EXACT = {
    "api/openapi.yaml",
    "Dockerfile",
    "compose.yaml",
    ".env.example",
    "mise.toml",
    "mise.lock",
    "backend/go.mod",
    "backend/go.sum",
    "frontend/index.html",
    "frontend/vite.config.ts",
}
NON_RUNTIME_EXACT = {"frontend/package.json", "frontend/package-lock.json"}


# --------------------------------------------------------------------------
# Errors
# --------------------------------------------------------------------------
class SelectionError(Exception):
    """Data error: exit 2, never an idle decision."""


class RateLimited(Exception):
    """API quota exhaustion: exit 4, no decision."""

    def __init__(self, reset: int = 0, message: str = "rate limited"):
        super().__init__(message)
        self.reset = int(reset or 0)


# --------------------------------------------------------------------------
# Small shared helpers
# --------------------------------------------------------------------------
def parse_time(value: str) -> datetime:
    return datetime.fromisoformat(value.replace("Z", "+00:00"))


def label_names(item: dict) -> set[str]:
    return {label["name"] if isinstance(label, dict) else label for label in item.get("labels", [])}


def login(item: dict) -> str:
    author = item.get("author") or {}
    return author.get("login", "") if isinstance(author, dict) else str(author)


def is_renovate(pr: dict) -> bool:
    return (login(pr) in RENOVATE_LOGINS or pr.get("headRefName", "").startswith("renovate/")
            or "renovate" in label_names(pr))


def is_dependency(pr: dict) -> bool:
    """Renovate PR (author/branch/label) or any PR labelled `dependencies`."""
    return is_renovate(pr) or "dependencies" in label_names(pr)


def is_owned(issue: dict) -> bool:
    """Ownership is exactly the `release` label; milestone/title are ignored."""
    return OWNED_LABEL in label_names(issue)


def is_release_branch_pr(pr: dict) -> bool:
    return str(pr.get("headRefName", "")).startswith("release/v")


def parse_version(value: str | None) -> tuple[int, int, int] | None:
    if not value:
        return None
    match = TAG_RE.match(value.strip())
    if not match:
        return None
    return (int(match.group(1)), int(match.group(2)), int(match.group(3)))


# --------------------------------------------------------------------------
# Triage markers
# --------------------------------------------------------------------------
def latest_triage(pr: dict) -> dict | None:
    marker = None
    comments = sorted(pr.get("comments", []), key=lambda c: c.get("createdAt") or "")
    for comment in comments:
        if login(comment) in RENOVATE_LOGINS:
            continue
        first_line = (comment.get("body") or "").strip().splitlines()[:1]
        match = TRIAGE_RE.match(first_line[0]) if first_line else None
        if match:
            marker = {"decision": match["decision"], "target": match["target"],
                      "createdAt": comment.get("createdAt")}
    return marker


def triage_state(pr: dict, now: datetime) -> str:
    """untriaged | actionable | blocked.

    The marker must match the current title; a `blocked` marker older than
    BLOCKED_RECHECK_DAYS (or without a parseable time) expires to untriaged.
    """
    marker = latest_triage(pr)
    if marker is None or marker["target"] != pr.get("title", "").strip():
        return "untriaged"
    if marker["decision"] == "blocked":
        try:
            created = parse_time(marker["createdAt"] or "")
        except ValueError:
            return "untriaged"
        if now - created > timedelta(days=BLOCKED_RECHECK_DAYS):
            return "untriaged"
    return marker["decision"]


# --------------------------------------------------------------------------
# PR / Issue projections
# --------------------------------------------------------------------------
def pr_ref(pr: dict, **extra) -> dict:
    return {"type": "pr", "number": pr["number"], "title": pr.get("title", ""), **extra}


def issue_ref(issue: dict, **extra) -> dict:
    return {"type": "issue", "number": issue["number"], "title": issue.get("title", ""), **extra}


def main_is_red(runs: list[dict]) -> dict | None:
    completed = [r for r in runs if r.get("status") == "completed"]
    if not completed:
        return None
    latest = max(completed, key=lambda r: r.get("created_at") or r.get("createdAt") or "")
    if latest.get("conclusion") in ("success", "skipped"):
        return None
    return latest


def blocked_by(issue: dict) -> list[int] | None:
    """Dependency Issue numbers from the `Blocked by:` line; None when absent."""
    match = BLOCKED_BY_RE.search(issue.get("body") or "")
    if not match:
        return None
    return [int(n) for n in re.findall(r"#(\d+)", match["refs"])]


def release_token(issue: dict) -> str | None:
    """Release coordinator marker: body `Release: vX.Y.Z` or a title vX.Y.Z token."""
    match = RELEASE_LINE_RE.search(issue.get("body") or "")
    if match:
        return match.group(1)
    match = VERSION_TOKEN_RE.search(issue.get("title") or "")
    return match.group(0) if match else None


def issue_rank(issue: dict) -> tuple:
    """Deterministic, version-neutral rank: smaller is claimed first."""
    title = issue.get("title", "")
    priority = 2
    match = PRIORITY_RE.search(issue.get("body") or "")
    if match:
        priority = int(match.group(1))
    if "recurrence" in title.lower():
        # Recurrence correctness is a standing P0 product invariant.
        priority = 0
    code = CODE_RE.match(title)
    coordinator = 1 if release_token(issue) else 0
    return (coordinator, priority, int(code.group(1)) if code else 10_000, issue["number"])


def refs(pr: dict) -> set[int]:
    return {int(n) for n in REFS_RE.findall(pr.get("body") or "")}


def is_feature(pr: dict, owned: dict) -> bool:
    """A merged work PR: not a dependency PR and `Refs` an owned Issue (any state)."""
    return not is_dependency(pr) and any(n in owned for n in refs(pr))


def features_since_last_dependency(merged: list[dict], owned: dict) -> tuple[int, str | None]:
    ordered = sorted((p for p in merged if p.get("mergedAt")),
                     key=lambda p: (parse_time(p["mergedAt"]), p.get("number", 0)))
    count, last_dependency = 0, None
    for pr in ordered:
        if is_dependency(pr):
            count, last_dependency = 0, pr["mergedAt"]
        elif is_feature(pr, owned):
            count += 1
    return count, last_dependency


def is_repair_author(author: str) -> bool:
    """Only a known, non-Renovate GitHub login proves an agent repair commit."""
    return bool(author) and author not in RENOVATE_LOGINS


# --------------------------------------------------------------------------
# Issue readiness
# --------------------------------------------------------------------------
def dependency_states_for(issue: dict, owned: dict, dependency_states: dict) -> tuple[bool, list[str]]:
    """(ready, notes) for one Issue's `Blocked by:` dependencies.

    A dependency is satisfied only when the referenced Issue is CLOSED,
    whether or not it carries the `release` label. State of referenced Issues
    outside the owned list comes from the snapshot (`dependency_states`); an
    unknown state is not ready and is reported.
    """
    notes: list[str] = []
    deps = blocked_by(issue)
    if deps is None:
        notes.append(f"#{issue['number']} has no 'Blocked by:' line; treated as no dependencies")
        deps = []
    for number in deps:
        if number in owned:
            state = str(owned[number].get("state", "")).upper()
        else:
            raw = dependency_states.get(str(number)) or dependency_states.get(number)
            state = str(raw).upper() if raw else ""
        if state == "CLOSED":
            continue
        if not state:
            notes.append(f"#{issue['number']} depends on Issue #{number} whose state is unknown; not ready")
        elif number not in owned:
            notes.append(f"#{issue['number']} depends on open non-owned Issue #{number}; not ready")
        return False, notes
    return True, notes


def recovery_candidates(snapshot: dict, owned: dict, selectable: list[dict],
                        status_by_number: dict, notes: list[str],
                        open_prs: list[dict] | None = None) -> tuple[list[dict], set[int]]:
    """Eligible advisory recovery groups (priority 6) and protected members, fail closed.

    An Issue takes part only when its comment history is present in the
    snapshot (`comments`, paginated by the collector). A malformed trusted
    lifecycle comment is noted and excludes that Issue, so any group touching it
    (or depending on it while open) is not eligible.
    """
    owned_open: dict[int, dict] = {}
    lifecycle: dict[int, dict] = {}
    for issue in selectable:
        number = issue["number"]
        if "comments" not in issue:
            continue
        try:
            lifecycle[number] = deps.replay(issue.get("comments") or [])
        except deps.LifecycleError as exc:
            notes.append(f"#{number} lifecycle comments malformed ({exc}); excluded from recovery")
            continue
        owned_open[number] = issue
        blocked = lifecycle[number]["blocked"]
        if (status_by_number[number] == ["status:blocked"] and blocked is not None
                and blocked.get("status") == "unknown"):
            notes.append(f"#{number} has unstructured or external BLOCKED prose; no dependency "
                         "review is emitted and no work is selected from it")
    dependency_states = snapshot.get("dependency_states") or {}

    def closed(number: int) -> bool:
        if number in owned:
            return str(owned[number].get("state", "")).upper() == "CLOSED"
        raw = dependency_states.get(str(number)) or dependency_states.get(number)
        return str(raw or "").upper() == "CLOSED"

    declared = {n: (blocked_by(issue) or []) for n, issue in owned_open.items()}
    status = {n: status_by_number[n][0] for n in owned_open}
    # Open, non-dependency PRs that reference an owned Issue (evidence only).
    linked: dict[int, list[int]] = {}
    for pr in open_prs or []:
        if is_dependency(pr):
            continue
        for number in refs(pr):
            if number in owned_open:
                linked.setdefault(number, []).append(pr["number"])
    heads = {pr["number"]: pr.get("headRefOid") for pr in open_prs or []}
    return deps.recovery_groups(owned_open, status, lifecycle, declared, closed, linked, notes, heads)


def coordinator_in_flight(open_owned: list[dict], latest_tag: str | None) -> tuple[str, int] | None:
    """Return (version, issue number) of an in-flight minor/major release candidate.

    A candidate counts only when it is newer than the latest published release
    with a minor or major bump; a patch-level token (e.g. `Release: v5.0.1`
    when `v5.0.0` is published) is not a release candidate for this purpose.
    """
    latest = parse_version(latest_tag)
    for issue in sorted(open_owned, key=lambda i: i["number"]):
        version = release_token(issue)
        if not version:
            continue
        candidate = parse_version(version)
        if candidate is None:
            continue
        if latest is None:
            return version, issue["number"]
        if candidate[0] > latest[0] or (candidate[0] == latest[0] and candidate[1] > latest[1]):
            return version, issue["number"]
    return None


# --------------------------------------------------------------------------
# Release-change classification (pure)
# --------------------------------------------------------------------------
def _strip_prefix(path: str) -> str:
    path = path.strip()
    while path.startswith("./"):
        path = path[2:]
    return path


def is_runtime_path(path: str, runtime_dependency_change: bool | None = None) -> bool:
    """True when a changed path is runtime-meaningful for a release.

    `frontend/package.json` and `frontend/package-lock.json` only count when the
    collector proved a runtime dependency change; an unknown (`None`) or false
    value is treated as not runtime (the caller adds a note for None).
    """
    path = _strip_prefix(path)
    if not path:
        return False
    if path in RUNTIME_EXACT:
        return True
    if path in NON_RUNTIME_EXACT:
        return runtime_dependency_change is True
    if path.startswith("backend/"):
        if path.endswith("_test.go"):
            return False
        if path.startswith("backend/test/"):
            return False
        if path.startswith("backend/testdata/") or "/testdata/" in path:
            return False
        return True
    if path.startswith("frontend/"):
        rest = path[len("frontend/"):]
        if rest == "index.html" or rest == "vite.config.ts":
            return True
        if rest.startswith("public/"):
            return True
        if rest.startswith("src/"):
            name = rest.rsplit("/", 1)[-1]
            if name.endswith((".test.ts", ".test.tsx", ".spec.ts", ".spec.tsx")):
                return False
            if rest.startswith("src/__tests__/") or "/__tests__/" in rest:
                return False
            if rest.startswith("src/test/") or "/test/" in rest:
                return False
            if rest.startswith("src/tests/") or "/tests/" in rest:
                return False
            return True
        return False
    if path == "api/openapi.yaml":
        return True
    if path.startswith("api/"):
        return False
    return False


def analyse_release_changes(changes: dict) -> tuple[bool, list[str], list[str]]:
    """(meaningful, runtime_files, notes) for a `release_changes` snapshot block."""
    notes: list[str] = []
    files = [f for f in (changes.get("files") or []) if isinstance(f, str)]
    runtime_dependency_change = changes.get("runtime_dependency_change")
    if runtime_dependency_change is None and any(
            _strip_prefix(f) in NON_RUNTIME_EXACT for f in files):
        notes.append("frontend dependency change unknown (runtime_dependency_change=null); "
                     "treated as not runtime")
    runtime_files: list[str] = []
    seen: set[str] = set()
    for path in files:
        if is_runtime_path(path, runtime_dependency_change):
            normalised = _strip_prefix(path)
            if normalised not in seen:
                seen.add(normalised)
                runtime_files.append(normalised)
    if changes.get("truncated"):
        if runtime_files:
            notes.append("release compare truncated at the API file cap; runtime changes found")
            return True, runtime_files, notes
        notes.append("release compare truncated at the API file cap with no runtime file listed; "
                     "treated as not release-worthy")
        return False, [], notes
    if not runtime_files:
        notes.append("changes since the latest release are not runtime-meaningful; no release")
        return False, [], notes
    return True, runtime_files, notes


def next_version(tags, latest_tag: str | None = None) -> str | None:
    """Highest semver tag (not string order) with a patch bump."""
    versions = []
    for tag in list(tags or []) + ([latest_tag] if latest_tag else []):
        if isinstance(tag, dict):
            tag = tag.get("name", "")
        parsed = parse_version(tag)
        if parsed:
            versions.append(parsed)
    if not versions:
        return None
    major, minor, patch = max(versions)
    return f"v{major}.{minor}.{patch + 1}"


# --------------------------------------------------------------------------
# Selection (pure)
# --------------------------------------------------------------------------
def select(snapshot: dict, now: datetime) -> dict:
    """Single next work item from a snapshot. Pure: reads only its arguments."""
    notes: list[str] = []
    open_prs = sorted(snapshot.get("open_prs", []), key=lambda p: (p.get("createdAt", ""), p["number"]))
    issues = snapshot.get("issues", [])
    owned = {i["number"]: i for i in issues if is_owned(i)}
    ignored = sorted(i["number"] for i in issues if not is_owned(i))
    open_owned = [i for i in owned.values() if str(i.get("state", "")).upper() == "OPEN"]

    # Validate the lifecycle label exactly once, before any selection path can
    # use an Issue. An anomalous Issue (0 or >1 status labels) is excluded from
    # active-PR linkage, resume and claim until a human repairs it.
    status_by_number: dict[int, list[str]] = {}
    anomalous: dict[int, list[str]] = {}
    for issue in open_owned:
        names = label_names(issue)
        statuses = [s for s in STATUS_LABELS if s in names]
        status_by_number[issue["number"]] = statuses
        if len(statuses) != 1:
            anomalous[issue["number"]] = statuses
            notes.append(f"#{issue['number']} has status labels {statuses}; repair needed "
                         "(excluded from active PRs, resume and claim)")
    selectable = [i for i in open_owned if i["number"] not in anomalous]

    dependency_prs = [p for p in open_prs if is_dependency(p)]
    other_prs = [p for p in open_prs if not is_dependency(p)]
    triage = {p["number"]: triage_state(p, now) for p in dependency_prs}
    blocked_skipped = [p["number"] for p in dependency_prs if triage[p["number"]] == "blocked"]
    features, last_dependency = features_since_last_dependency(snapshot.get("merged_prs", []), owned)
    dependency_states = snapshot.get("dependency_states") or {}
    context = {
        "features_merged_since_last_dependency_pr": features,
        "last_dependency_pr_merged_at": last_dependency,
        "open_dependency_prs": {str(n): s for n, s in triage.items()},
        "blocked_unchanged_skipped": blocked_skipped,
        "ignored_unowned_issues": ignored,
    }

    def decide(priority, kind: str, target: dict | None, reason: str) -> dict:
        return {"priority": priority, "kind": kind, "target": target, "reason": reason,
                "context": context, "notes": notes}

    # 1 red-main
    red = main_is_red(snapshot.get("main_runs", []))
    if red is not None:
        return decide(1, "red-main", {"type": "ci", "run": red.get("id"), "sha": red.get("head_sha")},
                      f"latest completed required run on main concluded {red.get('conclusion')}")

    # 2 security-pr, then security-alert
    for pr in dependency_prs + other_prs:
        if "security" in label_names(pr) and triage.get(pr["number"]) != "blocked":
            return decide(2, "security-pr", pr_ref(pr), "open security-labelled PR")
    alerts = snapshot.get("security_alerts")
    if alerts is None:
        # The collector fails closed on an unreadable alerts endpoint; a snapshot
        # without alert data is still reported rather than silently treated as empty.
        notes.append("security alerts unreadable; check `gh api repos/<repo>/dependabot/alerts` manually")
    else:
        fixable = [a for a in alerts if ((a.get("security_vulnerability") or {})
                                         .get("first_patched_version"))]
        if fixable:
            return decide(2, "security-alert",
                          {"type": "alerts", "numbers": [a.get("number") for a in fixable]},
                          f"{len(fixable)} open security alert(s) with a patched version")
        if alerts:
            notes.append(f"{len(alerts)} open alert(s) without a patched version; record, do not spin")

    # 3 active-pr, then resume-claimed-issue
    # Sub-rules in spec order: (a) open PR referencing an open non-blocked owned
    # Issue, (b) a Renovate PR with agent repair commits, (c) an in-flight
    # release/v* release-notes PR (author irrelevant).
    referenced = {n for p in open_prs for n in refs(p)}
    claimed_pr = None
    for pr in open_prs:
        open_refs = sorted(n for n in refs(pr) if n in owned and str(owned[n].get("state", "")).upper() == "OPEN")
        active_issue = [n for n in open_refs
                        if n not in anomalous and "status:blocked" not in label_names(owned[n])]
        if open_refs and not active_issue:
            blocked_refs = [n for n in open_refs if "status:blocked" in label_names(owned[n])]
            if blocked_refs:
                notes.append(f"open PR #{pr['number']} references only status:blocked owned Issue(s) "
                             f"{open_refs}; skipped until the blocker changes")
            else:
                notes.append(f"open PR #{pr['number']} references only anomalous owned Issue(s) "
                             f"{open_refs}; not active work")
            continue
        if active_issue and claimed_pr is None:
            claimed_pr = (pr, active_issue)
    if claimed_pr is not None:
        return decide(3, "active-pr", pr_ref(claimed_pr[0], issues=claimed_pr[1]),
                      "finish the active owned PR first")
    for pr in open_prs:
        repaired = is_renovate(pr) and any(is_repair_author(a) for a in pr.get("commitAuthors", []))
        if not repaired:
            continue
        marker = latest_triage(pr)
        needs_recheck = (marker is not None and marker["decision"] == "blocked"
                         and triage.get(pr["number"]) == "untriaged")
        if needs_recheck:
            notes.append(f"Renovate PR #{pr['number']} has repair commits but an expired/outdated blocked "
                         "triage; re-triage before resuming")
            continue
        if triage.get(pr["number"]) != "blocked":
            return decide(3, "active-pr", pr_ref(pr), "Renovate PR already carries agent repair commits")
    for pr in open_prs:
        if is_release_branch_pr(pr) and not pr.get("isDraft"):
            return decide(3, "active-pr", pr_ref(pr),
                          "in-flight release-notes PR (release/v* branch); finish the release")
    for pr in other_prs:
        if not any(n in owned for n in refs(pr)):
            notes.append(f"open PR #{pr['number']} references no owned Issue; inspect, never adopt")
    for issue in sorted(selectable, key=issue_rank):
        if "status:in-progress" in label_names(issue) and issue["number"] not in referenced:
            return decide(3, "resume-claimed-issue", issue_ref(issue),
                          "owned Issue is status:in-progress without an open PR; reconcile claim and work")

    # 4 triage-aged-renovate (Renovate only; oldest first)
    aged = [p for p in dependency_prs if is_renovate(p) and triage[p["number"]] == "untriaged"
            and now - parse_time(p["createdAt"]) > timedelta(hours=AGED_HOURS)]
    if aged:
        return decide(4, "triage-aged-renovate", pr_ref(aged[0]),
                      f"Renovate PR untriaged for more than {AGED_HOURS} h")

    # 5 dependency-slot
    actionable = [p for p in dependency_prs if triage[p["number"]] != "blocked" and not p.get("isDraft")]
    if features >= FEATURES_PER_DEPENDENCY_SLOT and actionable:
        return decide(5, "dependency-slot", pr_ref(actionable[0]),
                      f"{features} feature PR(s) merged since the last dependency PR")

    # 6 claim-issue / review-owned-dependencies, ranked together by issue_rank.
    # An eligible recovery group competes with ordinary ready Issues at the rank
    # of its most urgent member, so a recoverable high-priority blocker is not
    # starved by lower-priority ready work and never outranks higher-priority
    # work. Advice only: the agent reconciles live state under FLOW first and
    # the selector grants no claim.
    groups, protected = recovery_candidates(snapshot, owned, selectable, status_by_number, notes,
                                            open_prs)
    # Members of any recorded recovery group (advised, deferred or ineligible)
    # are never offered as ordinary claims.
    in_group = protected | {n for group in groups for n in group["group"]}
    candidates = []
    for issue in selectable:
        if status_by_number[issue["number"]][0] != "status:idle" or issue["number"] in in_group:
            continue
        ok, dep_notes = dependency_states_for(issue, owned, dependency_states)
        notes.extend(dep_notes)
        if ok:
            candidates.append((issue_rank(issue), "claim", issue))
    for group in groups:
        candidates.append((min(issue_rank(owned[n]) for n in group["group"]), "review", group))
    if candidates:
        _rank, what, best = min(candidates, key=lambda c: c[0])
        if what == "claim":
            return decide(6, "claim-issue", issue_ref(best),
                          "highest-value dependency-ready idle owned Issue")
        return decide(6, "review-owned-dependencies",
                      {"type": "issue", "number": best["issue"], "group": best["group"],
                       "evidence": best["evidence"], "fingerprint": best["fingerprint"],
                       "recovery": best["recovery"],
                       "blocker": best["blocker"], "pr": best["pr"], "claim": best["claim"],
                       "conflicts": best["conflicts"]},
                      best["reason"])

    # 7 dependency-pr
    if actionable:
        return decide(7, "dependency-pr", pr_ref(actionable[0]), "remaining actionable dependency PR")

    # 8 security-release / patch-release
    release = evaluate_release(snapshot, now, notes)
    if release is not None:
        return decide(8, release["kind"],
                      {"type": "release", "version": release["version"], "since": release["since"],
                       "runtime_files": release["runtime_files"][:20]},
                      release["reason"])
    return decide("idle", "none", None, "no actionable work; end the cycle without changes")


def main_head_green(snapshot: dict) -> tuple[bool, str]:
    """(green, note) for an automatic release: required CI must pass on main HEAD.

    A success on an older commit does not prove the code that would ship, so a
    missing run for HEAD, a newest run still queued/in progress, or a success
    only on an older SHA all defer or block the release.
    """
    head = snapshot.get("main_head_sha")
    runs = snapshot.get("main_runs", [])
    short = str(head)[:12] if head else "-"
    if not head:
        return False, ("current main HEAD sha unavailable; automatic release requires a green "
                       "required run on HEAD")
    head_runs = [r for r in runs if (r.get("head_sha") or r.get("headSha")) == head]
    if not head_runs:
        if runs:
            return False, (f"no required.yml run for current main HEAD {short}; the newest run is "
                           "for an older commit, so automatic release is deferred")
        return False, (f"no required.yml run for current main HEAD {short}; no automatic release")
    newest = max(head_runs, key=lambda r: r.get("created_at") or r.get("createdAt") or "")
    if newest.get("status") != "completed":
        return False, (f"newest required.yml run for main HEAD {short} is "
                       f"{newest.get('status')}; automatic release deferred until it completes")
    if newest.get("conclusion") not in ("success", "skipped"):
        return False, (f"required.yml run for main HEAD {short} concluded "
                       f"{newest.get('conclusion')}; automatic release blocked")
    return True, ""


def evaluate_release(snapshot: dict, now: datetime, notes: list[str]) -> dict | None:
    """Automatic release eligibility (priority 8). Returns a partial decision."""
    latest = snapshot.get("latest_release")
    issues = snapshot.get("issues", [])
    open_owned = [i for i in issues if is_owned(i) and str(i.get("state", "")).upper() == "OPEN"]
    latest_tag = (latest or {}).get("tagName")

    open_release_prs = [p for p in snapshot.get("open_prs", []) if is_release_branch_pr(p)]
    if open_release_prs:
        notes.append(f"open release PR #{open_release_prs[0]['number']} in flight; "
                     "automatic patch releases suspended")
        return None

    candidate = coordinator_in_flight(open_owned, latest_tag)
    if candidate is not None:
        notes.append(f"release candidate {candidate[0]} in flight (#{candidate[1]}); "
                     "automatic patch releases suspended")
        return None

    if not latest or not latest.get("publishedAt"):
        notes.append("no published release found; no automatic patch/security release")
        return None

    published_at = parse_time(latest["publishedAt"])

    green, green_note = main_head_green(snapshot)
    if not green:
        notes.append(green_note)
        return None

    since = snapshot.get("merged_since_release")
    if since is None:
        since = snapshot.get("merged_prs", [])
    security_merged = [p for p in since
                       if p.get("mergedAt") and parse_time(p["mergedAt"]) > published_at
                       and "security" in label_names(p)]
    window_reached = now - published_at >= timedelta(days=PATCH_RELEASE_DAYS)
    if not window_reached and not security_merged:
        notes.append(f"release window not reached since {latest_tag}; no automatic release")
        return None

    changes = snapshot.get("release_changes")
    if changes is None:
        notes.append("release change set unavailable; no automatic release")
        return None
    meaningful, runtime_files, change_notes = analyse_release_changes(changes)
    notes.extend(change_notes)
    if not meaningful:
        return None

    version = next_version(snapshot.get("tags") or [], latest_tag)
    if version is None:
        notes.append("cannot determine the next version from published tags; no release")
        return None

    if security_merged:
        return {"kind": "security-release", "version": version, "since": latest_tag,
                "runtime_files": runtime_files,
                "reason": f"security PR #{security_merged[0].get('number')} merged since "
                          f"{latest_tag} with runtime-meaningful changes"}
    return {"kind": "patch-release", "version": version, "since": latest_tag,
            "runtime_files": runtime_files,
            "reason": f"{PATCH_RELEASE_DAYS}+ days since {latest_tag} with runtime-meaningful changes"}


# --------------------------------------------------------------------------
# Collector (live mode): bounded, fail-closed, rate-limit aware
# --------------------------------------------------------------------------
def default_run_gh(args: list[str]) -> str:
    """Run `gh <args>` and return stdout; map failures to typed errors."""
    command = ["gh", *args]
    try:
        result = subprocess.run(command, capture_output=True, text=True, timeout=GH_TIMEOUT_SECONDS)
    except subprocess.TimeoutExpired as exc:
        raise SelectionError(f"gh {' '.join(args)} timed out after {GH_TIMEOUT_SECONDS}s") from exc
    except OSError as exc:
        raise SelectionError(f"cannot run gh: {exc}") from exc
    if result.returncode != 0:
        stderr = result.stderr or ""
        if RATE_LIMIT_RE.search(stderr):
            raise RateLimited(_reset_from_text(stderr),
                              f"gh {' '.join(args)} was rate limited")
        raise SelectionError(f"gh {' '.join(args)} failed (exit {result.returncode}): "
                             f"{stderr.strip()[:300]}")
    return result.stdout


def _reset_from_text(text: str) -> int:
    match = RESET_RE.search(text or "")
    return int(match.group(1)) if match else 0


def gh_text(run_gh, args: list[str]) -> str:
    """Invoke the injectable runner, normalising every failure mode."""
    try:
        return run_gh(args)
    except (RateLimited, SelectionError):
        raise
    except subprocess.CalledProcessError as exc:
        stderr = exc.stderr if isinstance(exc.stderr, str) else ""
        if RATE_LIMIT_RE.search(stderr):
            raise RateLimited(_reset_from_text(stderr), "gh was rate limited") from exc
        raise SelectionError(f"gh {' '.join(args)} failed: {stderr.strip()[:300] or exc}") from exc
    except subprocess.TimeoutExpired as exc:
        raise SelectionError(f"gh {' '.join(args)} timed out") from exc


def _load_json(raw: str, what: str):
    """Parse a JSON response; an empty payload is malformed, never an empty list."""
    if raw is None or not str(raw).strip():
        raise SelectionError(f"{what} returned an empty response; expected JSON")
    try:
        return json.loads(raw)
    except json.JSONDecodeError as exc:
        raise SelectionError(f"invalid JSON from {what}: {exc}") from exc


def _expect_list(value, what: str) -> list:
    if not isinstance(value, list):
        raise SelectionError(f"{what} returned {type(value).__name__}, expected a list")
    return value


def _expect_dict(value, what: str, required=()) -> dict:
    if not isinstance(value, dict):
        raise SelectionError(f"{what} returned {type(value).__name__}, expected an object")
    missing = [key for key in required if key not in value]
    if missing:
        raise SelectionError(f"{what} is missing required key(s): {', '.join(missing)}")
    return value


def _flatten_pages(data, what: str) -> list[dict]:
    """Flatten `--paginate --slurp` output; every page must be a list of objects."""
    items: list[dict] = []
    for page in _expect_list(data, what):
        for item in _expect_list(page, f"{what} page"):
            if not isinstance(item, dict):
                raise SelectionError(f"{what} returned a non-object item "
                                     f"({type(item).__name__}), expected an object")
            items.append(item)
    return items


def gh_list(run_gh, kind: str, *args: str) -> list[dict]:
    """`gh <kind> list` up to FETCH_LIMIT; hitting the limit is an error."""
    items = _expect_list(
        _load_json(gh_text(run_gh, [kind, "list", *args, "--limit", str(FETCH_LIMIT)]),
                   f"gh {kind} list"),
        f"gh {kind} list")
    if len(items) >= FETCH_LIMIT:
        raise SelectionError(f"gh {kind} list returned {len(items)} items (limit {FETCH_LIMIT}); "
                             "data incomplete")
    return items


def gh_paginated(run_gh, path: str) -> list[dict]:
    what = f"paginated {path}"
    return _flatten_pages(_load_json(gh_text(run_gh, ["api", "--paginate", "--slurp", path]), what),
                          what)


def check_quota(run_gh) -> None:
    """Pre-flight quota check; `rate_limit` itself does not consume quota."""
    data = _expect_dict(_load_json(gh_text(run_gh, ["api", "rate_limit"]), "rate_limit"),
                        "rate_limit", ("resources",))
    resources = _expect_dict(data["resources"], "rate_limit resources", ("core", "graphql"))
    blocks = {name: _expect_dict(resources[name], f"rate_limit resources.{name}", ("remaining",))
              for name in ("core", "graphql")}
    reset = max(int(block.get("reset") or 0) for block in blocks.values())
    for name, block in blocks.items():
        remaining = int(block.get("remaining") or 0)
        if remaining < MIN_QUOTA:
            raise RateLimited(reset, f"{name} quota remaining {remaining} < {MIN_QUOTA}")


def _normalise_issue_comment(item: dict) -> dict:
    """Lifecycle fields of an Issue comment; missing ordering keys are an error."""
    user = item.get("user") or {}
    if item.get("id") is None or not item.get("created_at"):
        raise SelectionError("issue comment without id/created_at; lifecycle cannot be ordered")
    return {"id": item["id"], "createdAt": item["created_at"],
            "updatedAt": item.get("updated_at"),
            "author": {"login": user.get("login", "")},
            "authorAssociation": item.get("author_association", ""),
            "body": item.get("body") or ""}


def _normalise_merged(item: dict) -> dict:
    labels = item.get("labels") or []
    return {"number": item.get("number"), "title": item.get("title", ""),
            "author": item.get("user") or item.get("author") or {},
            "headRefName": (item.get("head") or {}).get("ref", item.get("headRefName", "")),
            "labels": labels, "mergedAt": item.get("merged_at") or item.get("mergedAt"),
            "body": item.get("body") or ""}


def _dependency_stop(merged: list[dict], batch: list[dict]) -> bool:
    dependency_times = [parse_time(p["mergedAt"]) for p in merged if is_dependency(p)]
    if not dependency_times:
        return False
    last_updated = batch[-1].get("updated_at") or batch[-1].get("updatedAt")
    if not last_updated:
        return False
    return parse_time(last_updated) < max(dependency_times)


def walk_merged(repo: str, run_gh=default_run_gh, stop=None) -> list[dict]:
    """Merged PRs on main, newest `updated_at` first, paged until the stop rule.

    Proof of the stopping rule: REST `pulls?sort=updated&direction=desc` is
    ordered by `updated_at` descending, and any PR merged at time T has
    `updated_at >= merged_at > T`. So once the last item on a page has
    `updated_at < T`, every PR merged after T was already on an earlier page
    (or earlier in this one). The default bound T is the newest dependency PR
    `mergedAt` seen so far: fairness needs every dependency merge (to reset the
    feature count) and every merge after the latest one (to count features),
    and nothing older. The release walker passes the same walker with T equal
    to the latest release `publishedAt`, which only needs merges since then.
    """
    merged: list[dict] = []
    for page in range(1, MERGED_PAGE_CAP + 1):
        raw = gh_text(run_gh, ["api", f"repos/{repo}/pulls?state=closed&base=main"
                                        f"&sort=updated&direction=desc&per_page=100&page={page}"])
        batch = _expect_list(_load_json(raw, "merged PR page"), "merged PR page")
        if not batch:
            return merged
        for item in batch:
            if item.get("merged_at") or item.get("mergedAt"):
                merged.append(_normalise_merged(item))
        if (stop is None and _dependency_stop(merged, batch)) or (stop is not None and stop(merged, batch)):
            return merged
    raise SelectionError(f"merged PR history exceeded {MERGED_PAGE_CAP} pages; data incomplete")


def collect(repo: str, run_gh=default_run_gh, now: datetime | None = None) -> dict:
    """Build a selection snapshot from live, read-only GitHub state."""
    now = now or datetime.now(timezone.utc)
    check_quota(run_gh)

    runs = _expect_dict(_load_json(gh_text(run_gh, [
        "api", f"repos/{repo}/actions/workflows/required.yml/runs"
               "?branch=main&event=push&per_page=10"]), "required runs"),
        "required runs", ("workflow_runs",))
    main_runs = _expect_list(runs["workflow_runs"], "required runs workflow_runs")

    # The automatic-release gate needs the commit the green run must cover;
    # a missing or empty HEAD is malformed data, not "nothing to verify".
    head = _expect_dict(_load_json(gh_text(run_gh, ["api", f"repos/{repo}/commits/main"]),
                                   "main HEAD"), "main HEAD", ("sha",))
    main_head_sha = head["sha"].strip() if isinstance(head["sha"], str) else ""
    if not main_head_sha:
        raise SelectionError("main HEAD sha is empty; cannot verify required CI for a release")

    # Fail closed: an unreadable alerts endpoint is an error, never "no alerts".
    alerts_raw = gh_text(run_gh, [
        "api", "--paginate", "--slurp", f"repos/{repo}/dependabot/alerts?state=open&per_page=100"])
    alerts = _flatten_pages(_load_json(alerts_raw, "dependabot alerts"), "dependabot alerts")

    open_prs = gh_list(run_gh, "pr", "--repo", repo, "--state", "open", "--json",
                       "number,title,author,headRefName,headRefOid,labels,createdAt,isDraft,body")
    for pr in open_prs:
        # Full comment history (REST, paginated) so the latest triage marker is never cut off.
        pr["comments"] = [{"author": {"login": (c.get("user") or {}).get("login", "")},
                           "createdAt": c.get("created_at"), "body": c.get("body")}
                          for c in gh_paginated(run_gh,
                                                f"repos/{repo}/issues/{pr['number']}/comments?per_page=100")]
        if is_renovate(pr):
            commits = gh_paginated(run_gh, f"repos/{repo}/pulls/{pr['number']}/commits?per_page=100")
            if len(commits) >= PR_COMMITS_API_CAP:
                raise SelectionError(f"PR #{pr['number']} lists {len(commits)} commits "
                                     f"(API cap {PR_COMMITS_API_CAP}); commit authors incomplete")
            pr["commitAuthors"] = sorted({(c.get("author") or {}).get("login", "") for c in commits})

    # Only owned Issues are ever listed; unowned lookalikes are not work items.
    issues = gh_list(run_gh, "issue", "--repo", repo, "--label", OWNED_LABEL, "--state", "all",
                     "--json", "number,title,state,labels,body")
    # Lifecycle comments (paginated, fail closed) only for open owned Issues; they
    # drive claim/handoff/blocker replay for dependency recovery.
    for issue in issues:
        if is_owned(issue) and str(issue.get("state", "")).upper() == "OPEN":
            issue["comments"] = [_normalise_issue_comment(c) for c in gh_paginated(
                run_gh, f"repos/{repo}/issues/{issue['number']}/comments?per_page=100")]
    merged = walk_merged(repo, run_gh)

    owned = {i["number"]: i for i in issues if is_owned(i)}
    dependency_states = {}
    for number in sorted({n for i in issues for n in (blocked_by(i) or []) if n not in owned}):
        raw = gh_text(run_gh, ["api", f"repos/{repo}/issues/{number}", "--jq", ".state"])
        dependency_states[str(number)] = raw.strip().strip('"').upper()

    snapshot = {"main_runs": main_runs, "main_head_sha": main_head_sha,
                "security_alerts": alerts, "open_prs": open_prs,
                "merged_prs": merged, "issues": issues, "dependency_states": dependency_states,
                "latest_release": None, "tags": [], "release_changes": None,
                "merged_since_release": []}

    # Release data is fetched only when nothing in 1..7 was selected.
    if select(snapshot, now)["kind"] == "none":
        add_release_data(snapshot, repo, run_gh, now)
    return snapshot


def add_release_data(snapshot: dict, repo: str, run_gh, now: datetime) -> None:
    """Attach latest_release, tags, release_changes and merged_since_release."""
    try:
        raw = gh_text(run_gh, ["api", f"repos/{repo}/releases/latest"])
    except SelectionError as exc:
        if "404" in str(exc):
            return  # no published release: latest_release stays null, no patch release
        raise
    latest = _expect_dict(_load_json(raw, "releases/latest"), "releases/latest")
    tag = latest.get("tag_name") or latest.get("tagName")
    published = latest.get("published_at") or latest.get("publishedAt")
    if not tag or not published:
        raise SelectionError("releases/latest is missing tag_name/published_at; "
                             "cannot derive a release baseline")
    snapshot["latest_release"] = {"tagName": tag, "publishedAt": published}
    published_at = parse_time(published)

    open_owned = [i for i in snapshot["issues"] if is_owned(i) and str(i.get("state", "")).upper() == "OPEN"]
    if coordinator_in_flight(open_owned, tag) is not None:
        return  # automatic release is suspended; nothing else to compare

    # The merged walk is required to detect a security merge since the release;
    # only the (expensive) tags/compare fetches stay behind the eligibility gate.
    # PRs merged before the release are excluded even when updated later.
    merged_since = walk_merged(
        repo, run_gh, stop=lambda merged, batch: _oldest_updated_before(batch, published_at))
    since_release = [p for p in merged_since
                     if p.get("mergedAt") and parse_time(p["mergedAt"]) > published_at]
    snapshot["merged_since_release"] = since_release
    security_merged = any("security" in label_names(p) for p in since_release)
    window_reached = now - published_at >= timedelta(days=PATCH_RELEASE_DAYS)
    if not window_reached and not security_merged:
        return  # too early for a patch release and no security PR: skip tags/compare entirely

    tags_raw = gh_text(run_gh, ["api", "--paginate", "--slurp", f"repos/{repo}/tags?per_page=100"])
    snapshot["tags"] = [t["name"] for t in
                        _flatten_pages(_load_json(tags_raw, "tags"), "tags")
                        if isinstance(t.get("name"), str) and t["name"]]

    compare = _expect_dict(
        _load_json(gh_text(run_gh, ["api", f"repos/{repo}/compare/{tag}...main?per_page=100"]),
                   "compare"), "compare", ("files",))
    files: list[str] = []
    for entry in _expect_list(compare["files"], "compare files"):
        files.append(_expect_dict(entry, "compare file entry", ("filename",))["filename"])
    runtime_dependency_change = None
    if any(_strip_prefix(f) in NON_RUNTIME_EXACT for f in files):
        runtime_dependency_change = _runtime_dependency_change(repo, run_gh, tag)
    snapshot["release_changes"] = {"files": files,
                                   "runtime_dependency_change": runtime_dependency_change,
                                   "truncated": len(files) >= COMPARE_FILE_CAP}


def _oldest_updated_before(batch: list[dict], bound: datetime) -> bool:
    last_updated = batch[-1].get("updated_at") or batch[-1].get("updatedAt")
    return bool(last_updated) and parse_time(last_updated) < bound


def _fetch_contents(repo: str, run_gh, path: str, ref: str) -> str | None:
    try:
        return gh_text(run_gh, ["api", f"repos/{repo}/contents/{path}?ref={ref}",
                                "-H", "Accept: application/vnd.github.raw"])
    except SelectionError as exc:
        if "404" in str(exc):
            return None  # the file did not exist at that ref
        raise


def _runtime_dependencies(text: str | None) -> dict:
    if not text:
        return {}
    data = _expect_dict(_load_json(text, "frontend/package.json"), "frontend/package.json")
    dependencies = data.get("dependencies") or {}
    if not isinstance(dependencies, dict):
        raise SelectionError("frontend/package.json dependencies is not an object")
    return dict(sorted(dependencies.items()))


def _runtime_packages(text: str | None) -> dict:
    if not text:
        return {}
    data = _expect_dict(_load_json(text, "frontend/package-lock.json"), "frontend/package-lock.json")
    packages = data.get("packages") or {}
    if not isinstance(packages, dict):
        raise SelectionError("frontend/package-lock.json packages is not an object")
    return {path: entry.get("version") for path, entry in packages.items()
            if isinstance(entry, dict) and not entry.get("dev")}


def _runtime_dependency_change(repo: str, run_gh, tag: str) -> bool:
    """Compare runtime dependency sets at the tag vs main."""
    package_old = _runtime_dependencies(_fetch_contents(repo, run_gh, "frontend/package.json", tag))
    package_new = _runtime_dependencies(_fetch_contents(repo, run_gh, "frontend/package.json", "main"))
    lock_old = _runtime_packages(_fetch_contents(repo, run_gh, "frontend/package-lock.json", tag))
    lock_new = _runtime_packages(_fetch_contents(repo, run_gh, "frontend/package-lock.json", "main"))
    return package_old != package_new or lock_old != lock_new


# --------------------------------------------------------------------------
# Rendering, self-check, CLI
# --------------------------------------------------------------------------
def render(decision: dict) -> str:
    target = decision["target"]
    what = "-" if target is None else (f"{target['type']} #{target['number']} {target.get('title', '')}".strip()
                                       if "number" in target else json.dumps(target, sort_keys=True))
    lines = [f"DECISION priority={decision['priority']} kind={decision['kind']} target={what}",
             f"REASON {decision['reason']}"]
    lines += [f"NOTE {note}" for note in decision["notes"]]
    lines.append("JSON " + json.dumps(decision, sort_keys=True))
    return "\n".join(lines)


def _self_check_issue(number: int, title: str, labels=(), body: str = "",
                      state: str = "OPEN") -> dict:
    return {"number": number, "title": title, "state": state,
            "labels": [{"name": name} for name in labels], "body": body}


def _self_check_snapshot(issues: list[dict], dependency_states: dict | None = None) -> dict:
    return {"main_runs": [{"id": 1, "status": "completed", "conclusion": "success",
                           "created_at": "2026-01-01T00:00:00Z", "head_sha": "selfcheck"}],
            "main_head_sha": "selfcheck",
            "security_alerts": [], "open_prs": [], "merged_prs": [], "issues": issues,
            "dependency_states": dependency_states or {}, "latest_release": None, "tags": [],
            "release_changes": None, "merged_since_release": []}


def self_check(now: datetime | None = None) -> int:
    """Offline embedded snapshots proving the version-neutral ownership model."""
    now = now or datetime.now(timezone.utc)
    failures: list[str] = []

    future = _self_check_issue(900, "Next release: household export",
                               ("release", "status:idle"), "## Dependencies\nBlocked by: #444\n")
    decision = select(_self_check_snapshot([future], {"444": "CLOSED"}), now)
    if (decision["priority"], decision["kind"]) != (6, "claim-issue") \
            or (decision.get("target") or {}).get("number") != 900:
        failures.append(f"future release Issue #900 not selected as claim-issue: "
                        f"{decision['priority']}/{decision['kind']}/{decision.get('target')}")

    lookalike = _self_check_issue(900, "Next release: household export", ("status:idle",),
                                  "## Dependencies\nBlocked by: #444\n")
    closed_owned = _self_check_issue(899, "Previous release", ("release", "status:idle"),
                                     "## Dependencies\nBlocked by: none\n", state="CLOSED")
    decision = select(_self_check_snapshot([lookalike, closed_owned], {"444": "CLOSED"}), now)
    if decision["kind"] != "none" or (decision.get("target") or {}).get("number") == 900:
        failures.append(f"unlabelled lookalike #900 was treated as work: "
                        f"{decision['priority']}/{decision['kind']}/{decision.get('target')}")

    next_line = _self_check_issue(901, "V6-01 Household export", ("release", "status:idle"),
                                  "## Dependencies\nBlocked by: none\n")
    decision = select(_self_check_snapshot([next_line]), now)
    if (decision["priority"], decision["kind"]) != (6, "claim-issue") \
            or (decision.get("target") or {}).get("number") != 901:
        failures.append(f"future-version Issue #901 not selected: "
                        f"{decision['priority']}/{decision['kind']}/{decision.get('target')}")

    if failures:
        print("SELF-CHECK FAILED: " + "; ".join(failures), file=sys.stderr)
        return 1
    print("SELF-CHECK OK")
    return 0


def main(argv: list[str], run_gh=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--repo", default=REPO)
    parser.add_argument("--snapshot", help="JSON snapshot instead of live gh queries")
    parser.add_argument("--now", help="ISO-8601 time instead of the current UTC time")
    parser.add_argument("--self-check", action="store_true",
                        help="run embedded offline ownership snapshots and exit")
    args = parser.parse_args(argv)
    if args.self_check:
        return self_check()

    now = parse_time(args.now) if args.now else datetime.now(timezone.utc)
    if args.snapshot:
        try:
            with open(args.snapshot, encoding="utf-8") as handle:
                snapshot = json.load(handle)
        except (OSError, json.JSONDecodeError) as exc:
            print(f"SELECTION ERROR cannot read snapshot {args.snapshot}: {exc}", file=sys.stderr)
            return 2
    else:
        try:
            snapshot = collect(args.repo, run_gh or default_run_gh, now)
        except RateLimited as exc:
            print(f"RATE_LIMITED reset={exc.reset}", file=sys.stderr)
            return 4
        except SelectionError as exc:
            print(f"SELECTION ERROR {exc}", file=sys.stderr)
            return 2
    try:
        print(render(select(snapshot, now)))
    except (KeyError, TypeError, ValueError) as exc:
        print(f"SELECTION ERROR malformed snapshot: {exc!r}", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
