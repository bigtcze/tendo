#!/usr/bin/env python3
"""V5 controller work selection (locked V5_ISSUES_WORKFLOW priority).

Derives the single next work item for a V5 controller cycle purely from live
GitHub state: required CI on main, open security alerts, ALL open PRs
(human and bot, fully paginated), the complete merged PR history and the
owned V5 Issues. It keeps no state of its own; the dependency-fairness count
is recomputed from merged PR history (ordered by merge time) on every run, so
a controller restart cannot lose or reset it. Incomplete data (a list that
hits its fetch limit) is an error, never a silent truncation.

Priority (V5_ISSUES_WORKFLOW "Dependency maintenance"):
  a  red required CI on main
  b  urgent security: open `security` PR or open alert with a patched version
  c  active PR (references an open, not `status:blocked` owned V5 Issue, or
     a Renovate PR that already carries commits by a known non-Renovate
     GitHub login) or an owned Issue left `status:in-progress` without an
     open PR
  d  untriaged Renovate PR older than 48 h
  e  dependency PR when >= 2 feature PRs merged since the last dependency PR
     (feature PR: merged non-dependency PR whose body `Refs` an owned V5
     Issue; other merged PRs neither count nor reset)
  f  highest-value dependency-ready `status:idle` owned V5 Issue
  g  remaining (not blocked-unchanged) dependency PRs
  idle  nothing actionable

Triage marker: a PR comment whose first line is
  TRIAGE agent=<id> time=<UTC> decision=<actionable|blocked> target=<exact PR title>
The latest marker counts while its target equals the current PR title (the
title names the dependency and version). `blocked` with an unchanged target
is skipped for BLOCKED_RECHECK_DAYS; after that, or once the title (version)
changes, the PR counts as untriaged again so upstream and Tendo changes are
re-checked (a still-unchanged blocker is re-recorded, not re-run).

Usage: v5_work_selection.py [--repo OWNER/NAME] [--snapshot FILE] [--now ISO8601]
"""
from __future__ import annotations

import argparse
from datetime import datetime, timedelta, timezone
import json
import re
import subprocess
import sys

REPO = "bigtcze/tendo"
MILESTONE = "Tendo v5.0.0 - Refined & Reliable"
OWNED_LABEL = "tendo:v5-autonomous"
STATUS_LABELS = ("status:idle", "status:in-progress", "status:blocked")
AGED_HOURS = 48
BLOCKED_RECHECK_DAYS = 7
FETCH_LIMIT = 5000
PR_COMMITS_API_CAP = 250  # GitHub's pulls/{n}/commits returns at most 250 commits
FEATURES_PER_DEPENDENCY_SLOT = 2
RENOVATE_LOGINS = {"app/renovate", "renovate[bot]", "renovate"}
TRIAGE_RE = re.compile(
    r"^TRIAGE\s+.*?\bdecision=(?P<decision>actionable|blocked)\s+target=(?P<target>.+?)\s*$")
REFS_RE = re.compile(r"\b(?:refs|Refs|REFS)\s+#(\d+)\b")
BLOCKED_BY_RE = re.compile(r"^Blocked by:\s*(?P<refs>.*)$", re.MULTILINE)
CODE_RE = re.compile(r"^V5-(\d+)\b")


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
    return is_renovate(pr) or "dependencies" in label_names(pr)


def is_owned(issue: dict) -> bool:
    milestone = (issue.get("milestone") or {}).get("title")
    return milestone == MILESTONE and OWNED_LABEL in label_names(issue)


def latest_triage(pr: dict) -> dict | None:
    marker = None
    for comment in sorted(pr.get("comments", []), key=lambda c: c.get("createdAt", "")):
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
    match = BLOCKED_BY_RE.search(issue.get("body") or "")
    if not match:
        return None
    return [int(n) for n in re.findall(r"#(\d+)", match["refs"])]


def issue_rank(issue: dict) -> tuple:
    title = issue.get("title", "")
    code = CODE_RE.match(title)
    recurrence_first = 0 if "recurrence" in title.lower() else 1
    return (recurrence_first, int(code.group(1)) if code else 10_000, issue["number"])


def refs(pr: dict) -> set[int]:
    return {int(n) for n in REFS_RE.findall(pr.get("body") or "")}


def is_feature(pr: dict, owned: dict) -> bool:
    """A merged V5 work PR: not a dependency PR and `Refs` an owned V5 Issue."""
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


def select(snapshot: dict, now: datetime) -> dict:
    notes: list[str] = []
    open_prs = sorted(snapshot.get("open_prs", []), key=lambda p: (p.get("createdAt", ""), p["number"]))
    issues = snapshot.get("issues", [])
    owned = {i["number"]: i for i in issues if is_owned(i)}
    ignored = sorted(i["number"] for i in issues if not is_owned(i))
    open_owned = [i for i in owned.values() if i.get("state", "").upper() == "OPEN"]
    dependency_prs = [p for p in open_prs if is_dependency(p)]
    other_prs = [p for p in open_prs if not is_dependency(p)]
    triage = {p["number"]: triage_state(p, now) for p in dependency_prs}
    blocked_skipped = [p["number"] for p in dependency_prs if triage[p["number"]] == "blocked"]
    features, last_dependency = features_since_last_dependency(snapshot.get("merged_prs", []), owned)
    context = {
        "features_merged_since_last_dependency_pr": features,
        "last_dependency_pr_merged_at": last_dependency,
        "open_dependency_prs": {str(n): s for n, s in triage.items()},
        "blocked_unchanged_skipped": blocked_skipped,
        "ignored_unowned_issues": ignored,
    }

    def decide(priority: str, kind: str, target: dict | None, reason: str) -> dict:
        return {"priority": priority, "kind": kind, "target": target, "reason": reason,
                "context": context, "notes": notes}

    red = main_is_red(snapshot.get("main_runs", []))
    if red is not None:
        return decide("a", "red-main", {"type": "ci", "run": red.get("id"), "sha": red.get("head_sha")},
                      f"latest completed required run on main concluded {red.get('conclusion')}")

    for pr in dependency_prs + other_prs:
        if "security" in label_names(pr) and triage.get(pr["number"]) != "blocked":
            return decide("b", "security-pr", pr_ref(pr), "open security-labelled PR")
    alerts = snapshot.get("security_alerts")
    if alerts is None:
        notes.append("security alerts unreadable; check `gh api repos/<repo>/dependabot/alerts` manually")
    else:
        fixable = [a for a in alerts if ((a.get("security_vulnerability") or {})
                                         .get("first_patched_version"))]
        if fixable:
            return decide("b", "security-alert", {"type": "alerts", "numbers": [a.get("number") for a in fixable]},
                          f"{len(fixable)} open security alert(s) with a patched version")
        if alerts:
            notes.append(f"{len(alerts)} open alert(s) without a patched version; record, do not spin")

    referenced = {n for p in open_prs for n in refs(p)}
    for pr in open_prs:
        open_refs = sorted(n for n in refs(pr) if n in owned and owned[n].get("state", "").upper() == "OPEN")
        active_issue = [n for n in open_refs if "status:blocked" not in label_names(owned[n])]
        repaired = is_renovate(pr) and any(is_repair_author(a) for a in pr.get("commitAuthors", []))
        if open_refs and not active_issue:
            notes.append(f"open PR #{pr['number']} references only status:blocked owned Issue(s) {open_refs}; "
                         "skipped until the blocker changes")
            continue
        if active_issue:
            return decide("c", "active-pr", pr_ref(pr, issues=active_issue), "finish the active owned PR first")
        marker = latest_triage(pr) if repaired else None
        needs_recheck = marker is not None and marker["decision"] == "blocked" and triage[pr["number"]] == "untriaged"
        if repaired and needs_recheck:
            notes.append(f"Renovate PR #{pr['number']} has repair commits but an expired/outdated blocked triage; "
                         "re-triage before resuming")
        if repaired and triage[pr["number"]] != "blocked" and not needs_recheck:
            return decide("c", "active-pr", pr_ref(pr), "Renovate PR already carries agent repair commits")
    for pr in other_prs:
        if not any(n in owned for n in refs(pr)):
            notes.append(f"open PR #{pr['number']} references no open owned V5 Issue; inspect, never adopt")
    for issue in sorted(open_owned, key=issue_rank):
        if "status:in-progress" in label_names(issue) and issue["number"] not in referenced:
            return decide("c", "resume-claimed-issue", issue_ref(issue),
                          "owned Issue is status:in-progress without an open PR; reconcile claim and work")

    aged = [p for p in dependency_prs if is_renovate(p) and triage[p["number"]] == "untriaged"
            and now - parse_time(p["createdAt"]) > timedelta(hours=AGED_HOURS)]
    if aged:
        return decide("d", "triage-aged-renovate", pr_ref(aged[0]),
                      f"Renovate PR untriaged for more than {AGED_HOURS} h")

    actionable = [p for p in dependency_prs if triage[p["number"]] != "blocked" and not p.get("isDraft")]
    if features >= FEATURES_PER_DEPENDENCY_SLOT and actionable:
        return decide("e", "dependency-slot", pr_ref(actionable[0]),
                      f"{features} feature PR(s) merged since the last dependency PR")

    ready = []
    for issue in open_owned:
        names = label_names(issue)
        statuses = [s for s in STATUS_LABELS if s in names]
        if len(statuses) != 1:
            notes.append(f"#{issue['number']} has status labels {statuses}; repair before claiming")
            continue
        if statuses[0] != "status:idle":
            continue
        deps = blocked_by(issue)
        if deps is None:
            notes.append(f"#{issue['number']} has no 'Blocked by:' line; not treated as ready")
            continue
        unknown = [n for n in deps if n not in owned]
        if unknown:
            notes.append(f"#{issue['number']} depends on unowned/unknown Issue(s) {unknown}; not ready")
            continue
        if all(owned[n].get("state", "").upper() == "CLOSED" for n in deps):
            ready.append(issue)
    if ready:
        best = min(ready, key=issue_rank)
        return decide("f", "claim-issue", issue_ref(best), "highest-value dependency-ready idle owned V5 Issue")

    if actionable:
        return decide("g", "dependency-pr", pr_ref(actionable[0]), "remaining actionable dependency PR")
    return decide("idle", "none", None, "no actionable V5 work; end the cycle without changes")


def gh_json(*args: str):
    out = subprocess.run(["gh", *args], check=True, capture_output=True, text=True, timeout=240)
    return json.loads(out.stdout or "null")


def gh_list(kind: str, *args: str) -> list[dict]:
    """`gh <kind> list` paginated up to FETCH_LIMIT; hitting the limit is an error."""
    items = gh_json(kind, "list", *args, "--limit", str(FETCH_LIMIT))
    if len(items) >= FETCH_LIMIT:
        raise RuntimeError(f"gh {kind} list returned {len(items)} items (limit {FETCH_LIMIT}); data incomplete")
    return items


def gh_paginated(path: str) -> list[dict]:
    pages = gh_json("api", "--paginate", "--slurp", path)
    return [item for page in pages for item in page]


def collect(repo: str) -> dict:
    runs = gh_json("api", f"repos/{repo}/actions/workflows/required.yml/runs?branch=main&event=push&per_page=10")
    try:
        alerts = gh_paginated(f"repos/{repo}/dependabot/alerts?state=open&per_page=100")
    except (subprocess.CalledProcessError, json.JSONDecodeError):
        alerts = None
    open_prs = gh_list("pr", "--repo", repo, "--state", "open", "--json",
                       "number,title,author,headRefName,labels,createdAt,isDraft,body")
    for pr in open_prs:
        # Full comment history (REST, paginated) so the latest triage marker is never cut off.
        pr["comments"] = [{"author": {"login": (c.get("user") or {}).get("login", "")},
                           "createdAt": c.get("created_at"), "body": c.get("body")}
                          for c in gh_paginated(f"repos/{repo}/issues/{pr['number']}/comments?per_page=100")]
        if is_renovate(pr):
            commits = gh_paginated(f"repos/{repo}/pulls/{pr['number']}/commits?per_page=100")
            if len(commits) >= PR_COMMITS_API_CAP:
                raise RuntimeError(f"PR #{pr['number']} lists {len(commits)} commits (API cap "
                                   f"{PR_COMMITS_API_CAP}); commit authors incomplete")
            pr["commitAuthors"] = sorted({(c.get("author") or {}).get("login", "") for c in commits})
    # Complete merged history (ordered by mergedAt locally), not a newest-created window.
    merged = gh_list("pr", "--repo", repo, "--state", "merged", "--base", "main",
                     "--json", "number,title,author,headRefName,labels,mergedAt,body")
    issues = gh_list("issue", "--repo", repo, "--milestone", MILESTONE, "--label", OWNED_LABEL,
                     "--state", "all", "--json", "number,title,state,labels,milestone,body")
    return {"main_runs": (runs or {}).get("workflow_runs", []), "security_alerts": alerts,
            "open_prs": open_prs, "merged_prs": merged, "issues": issues}


def render(decision: dict) -> str:
    target = decision["target"]
    what = "-" if target is None else (f"{target['type']} #{target['number']} {target.get('title', '')}".strip()
                                       if "number" in target else json.dumps(target, sort_keys=True))
    lines = [f"DECISION priority={decision['priority']} kind={decision['kind']} target={what}",
             f"REASON {decision['reason']}"]
    lines += [f"NOTE {note}" for note in decision["notes"]]
    lines.append("JSON " + json.dumps(decision, sort_keys=True))
    return "\n".join(lines)


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--repo", default=REPO)
    parser.add_argument("--snapshot", help="JSON snapshot instead of live gh queries")
    parser.add_argument("--now", help="ISO-8601 time instead of the current UTC time")
    args = parser.parse_args(argv)
    now = parse_time(args.now) if args.now else datetime.now(timezone.utc)
    if args.snapshot:
        with open(args.snapshot, encoding="utf-8") as handle:
            snapshot = json.load(handle)
    else:
        snapshot = collect(args.repo)
    print(render(select(snapshot, now)))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
