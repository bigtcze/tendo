#!/usr/bin/env python3
"""Selection-policy tests for scripts/autonomy-work-selection.py.

Offline and deterministic: every decision takes an explicit `now`, no test
calls GitHub. Collector tests inject a fake `run_gh(args) -> str`.

Run: python3 scripts/test_autonomy_work_selection.py
"""
from __future__ import annotations

import contextlib
import copy
from datetime import datetime, timedelta, timezone
import importlib.util
import io
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import unittest

HERE = Path(__file__).resolve().parent
SCRIPT = HERE / "autonomy-work-selection.py"
SPEC = importlib.util.spec_from_file_location("autonomy_work_selection", SCRIPT)
sel = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(sel)

NOW = datetime(2026, 11, 20, 12, 0, tzinfo=timezone.utc)
LABEL = "release"
V5_MILESTONE = "Legacy milestone A"  # arbitrary: ownership must ignore milestones


def iso(delta_hours: float) -> str:
    return (NOW - timedelta(hours=delta_hours)).strftime("%Y-%m-%dT%H:%M:%SZ")


def issue(number, code, title, status="status:idle", deps=(), state="OPEN", owned=True,
          milestone=None, body_priority=None, extra_labels=()):
    """Work Issue; `deps=None` omits the `Blocked by:` line entirely."""
    if deps is None:
        dependency_line = ""
    else:
        dependency_line = "Blocked by: " + (", ".join(f"#{d}" for d in deps) if deps else "none")
    body = f"# {code}\n"
    if body_priority is not None:
        body += f"\nPriority: P{body_priority}\n"
    body += "\n## Dependencies\n" + (dependency_line + "\n" if dependency_line else "")
    labels = []
    if status:
        labels.append({"name": status})
    if owned:
        labels.append({"name": LABEL})
    labels += [{"name": name} for name in extra_labels]
    item = {"number": number, "title": f"{code} {title}", "state": state, "body": body,
            "labels": labels}
    if milestone:
        item["milestone"] = {"title": milestone}
    return item


def renovate_pr(number, title, age_hours, triage=None, labels=("dependencies", "renovate"),
                commit_authors=("renovate[bot]",), triage_age_hours=1):
    comments = []
    if triage:
        decision, target = triage
        comments.append({"author": {"login": "bigtcze"}, "createdAt": iso(triage_age_hours),
                         "body": f"TRIAGE agent=a time=x decision={decision} target={target}\ndetails"})
    return {"number": number, "title": title, "author": {"login": "app/renovate", "is_bot": True},
            "headRefName": f"renovate/pkg-{number}", "labels": [{"name": n} for n in labels],
            "createdAt": iso(age_hours), "isDraft": False, "body": "", "comments": comments,
            "commitAuthors": list(commit_authors)}


def human_dependency_pr(number, title, age_hours, labels=("dependencies",), is_draft=False):
    return {"number": number, "title": title, "author": {"login": "bigtcze"},
            "headRefName": f"deps/bump-{number}", "labels": [{"name": n} for n in labels],
            "createdAt": iso(age_hours), "isDraft": is_draft, "body": "", "comments": [],
            "commitAuthors": ["bigtcze"]}


def feature_pr(number, refs, age_hours=1, head="feat/change"):
    return {"number": number, "title": f"feat: work {number}", "author": {"login": "bigtcze"},
            "headRefName": f"{head}-{number}", "labels": [], "createdAt": iso(age_hours),
            "isDraft": False, "body": f"Refs #{refs}", "comments": [], "commitAuthors": []}


def release_pr(number, age_hours=1, draft=False):
    return {"number": number, "title": "chore: prepare release v5.0.1",
            "author": {"login": "bigtcze"}, "headRefName": "release/v5.0.1", "labels": [],
            "createdAt": iso(age_hours), "isDraft": draft,
            "body": "Release: v5.0.1", "comments": [], "commitAuthors": []}


def merged(number, hours_ago, dependency=False, refs=446, security=False):
    """Merged PR; a non-dependency PR is a feature PR when it `Refs` an owned Issue."""
    labels = [{"name": "dependencies"}, {"name": "renovate"}] if dependency else []
    if security:
        labels.append({"name": "security"})
    return {"number": number, "title": f"merged {number}", "mergedAt": iso(hours_ago),
            "author": {"login": "app/renovate" if dependency else "bigtcze"},
            "headRefName": f"renovate/x-{number}" if dependency else f"feat/x-{number}",
            "labels": labels,
            "body": "" if dependency or refs is None else f"Refs #{refs}"}


def backlog():
    """Perpetual feature backlog: #444 closed, everything else open."""
    return [
        issue(444, "V5-00", "Governance", status=None, state="CLOSED"),
        issue(445, "V5-01", "Renovate backlog recovery", deps=[444]),
        issue(446, "V5-02", "Recurrence root-cause audit", deps=[444]),
        issue(447, "V5-03", "Fix recurrence engine", deps=[446]),
        issue(449, "V5-05", "Design audit", deps=[444]),
        issue(452, "V5-08", "Password change", deps=[444]),
    ]


def snapshot(**overrides):
    base = {"main_runs": [{"id": 1, "status": "completed", "conclusion": "success",
                           "created_at": iso(2), "head_sha": "abc"}],
            "main_head_sha": "abc",
            "security_alerts": [], "open_prs": [], "merged_prs": [merged(1, 30, dependency=True)],
            "issues": backlog(), "dependency_states": {}}
    base.update(overrides)
    return base


def release_snapshot(**overrides):
    base = snapshot(issues=[], merged_prs=[], merged_since_release=[])
    base["latest_release"] = {"tagName": "v5.0.0", "publishedAt": iso(24 * 20)}
    base["tags"] = ["v5.0.0"]
    base["release_changes"] = {"files": ["backend/internal/handler.go"],
                               "runtime_dependency_change": None, "truncated": False}
    base.update(overrides)
    return base


class SelectionPolicy(unittest.TestCase):
    def pick(self, snap, now=NOW):
        return sel.select(snap, now)

    # -- fairness, aging, dependency slot --------------------------------
    def test_feature_backlog_never_starves_an_aged_untriaged_renovate_pr(self):
        d = self.pick(snapshot(open_prs=[renovate_pr(404, "update vite to v8.3.6", 49)]))
        self.assertEqual((d["priority"], d["kind"], d["target"]["number"]),
                         (4, "triage-aged-renovate", 404))

    def test_young_renovate_pr_waits_for_feature_work(self):
        d = self.pick(snapshot(open_prs=[renovate_pr(404, "update vite to v8.3.6", 47)]))
        self.assertEqual((d["priority"], d["target"]["number"]), (6, 446))

    def test_triaged_actionable_pr_gets_the_slot_after_two_merged_features(self):
        prs = [renovate_pr(404, "update vite to v8.3.6", 100,
                           triage=("actionable", "update vite to v8.3.6"))]
        one = self.pick(snapshot(open_prs=prs, merged_prs=[merged(1, 30, True), merged(2, 20)]))
        self.assertEqual(one["priority"], 6)
        self.assertEqual(one["context"]["features_merged_since_last_dependency_pr"], 1)
        two = self.pick(snapshot(open_prs=prs,
                                 merged_prs=[merged(1, 30, True), merged(2, 20), merged(3, 10)]))
        self.assertEqual((two["priority"], two["kind"], two["target"]["number"]),
                         (5, "dependency-slot", 404))

    def test_fairness_count_resets_at_the_latest_merged_dependency_pr(self):
        history = [merged(2, 40), merged(3, 35), merged(4, 20, True), merged(5, 10)]
        prs = [renovate_pr(404, "v", 100, triage=("actionable", "v"))]
        d = self.pick(snapshot(open_prs=prs, merged_prs=history))
        self.assertEqual(d["priority"], 6)
        self.assertEqual(d["context"]["features_merged_since_last_dependency_pr"], 1)

    def test_fairness_is_derived_from_history_and_survives_restart(self):
        # A "restart" is a fresh evaluation of the same GitHub state: identical
        # decision, order-independent, no state file written.
        prs = [renovate_pr(405, "update go-jose to v4.1.9", 100,
                           triage=("actionable", "update go-jose to v4.1.9"))]
        history = [merged(1, 30, True), merged(2, 20), merged(3, 10)]
        first = self.pick(snapshot(open_prs=prs, merged_prs=history))
        second = self.pick(snapshot(open_prs=list(prs), merged_prs=list(reversed(history))))
        self.assertEqual(first, second)
        self.assertEqual(first["priority"], 5)
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "snap.json"
            path.write_text(json.dumps(snapshot(open_prs=prs, merged_prs=history)))
            before = sorted(os.listdir(tmp))
            outputs = [subprocess.run([sys.executable, str(SCRIPT), "--snapshot", str(path),
                                       "--now", NOW.isoformat()], cwd=tmp, check=True,
                                      capture_output=True, text=True).stdout for _ in range(2)]
            self.assertEqual(outputs[0], outputs[1])
            self.assertIn("priority=5 kind=dependency-slot", outputs[0])
            self.assertEqual(sorted(os.listdir(tmp)), before)

    def test_fairness_orders_history_by_merge_time_not_list_or_number_order(self):
        history = [merged(9, 30, True), merged(3, 10), merged(2, 20)]
        prs = [renovate_pr(404, "v", 100, triage=("actionable", "v"))]
        d = self.pick(snapshot(open_prs=prs, merged_prs=history))
        self.assertEqual((d["priority"], d["context"]["features_merged_since_last_dependency_pr"]),
                         (5, 2))
        late_dependency = [merged(2, 20), merged(3, 10), merged(1, 5, True)]
        d = self.pick(snapshot(open_prs=prs, merged_prs=late_dependency))
        self.assertEqual((d["priority"], d["context"]["features_merged_since_last_dependency_pr"]),
                         (6, 0))

    def test_only_owned_work_prs_count_as_feature_prs(self):
        history = [merged(1, 30, True), merged(2, 25, refs=None), merged(3, 20, refs=300),
                   merged(4, 15), merged(5, 10, refs=None)]
        prs = [renovate_pr(404, "v", 100, triage=("actionable", "v"))]
        d = self.pick(snapshot(open_prs=prs, merged_prs=history))
        self.assertEqual((d["priority"], d["context"]["features_merged_since_last_dependency_pr"]),
                         (6, 1))
        d = self.pick(snapshot(open_prs=prs, merged_prs=history + [merged(6, 5, refs=452)]))
        self.assertEqual((d["priority"], d["target"]["number"]), (5, 404))

    def test_merging_the_dependency_pr_returns_to_feature_work(self):
        prs = [renovate_pr(406, "update playwright", 100, triage=("actionable", "update playwright"))]
        history = [merged(2, 20), merged(3, 10), merged(4, 1, True)]
        self.assertEqual(self.pick(snapshot(open_prs=prs, merged_prs=history))["priority"], 6)

    # -- active work ------------------------------------------------------
    def test_active_owned_pr_comes_before_aged_renovate_and_feature_claims(self):
        issues = backlog()
        issues[2] = issue(446, "V5-02", "Recurrence root-cause audit",
                          status="status:in-progress", deps=[444])
        prs = [renovate_pr(404, "v", 100), feature_pr(470, 446)]
        d = self.pick(snapshot(open_prs=prs, issues=issues))
        self.assertEqual((d["priority"], d["kind"], d["target"]["number"]),
                         (3, "active-pr", 470))

    def test_in_progress_issue_with_open_pr_is_active_pr(self):
        issues = backlog()
        issues[2] = issue(446, "V5-02", "Recurrence root-cause audit",
                          status="status:in-progress", deps=[444])
        d = self.pick(snapshot(open_prs=[feature_pr(470, 446)], issues=issues))
        self.assertEqual((d["priority"], d["kind"], d["target"]["number"]),
                         (3, "active-pr", 470))

    def test_in_progress_issue_without_pr_is_resumed_before_new_claims(self):
        issues = backlog()
        issues[5] = issue(452, "V5-08", "Password change", status="status:in-progress", deps=[444])
        d = self.pick(snapshot(issues=issues))
        self.assertEqual((d["priority"], d["kind"], d["target"]["number"]),
                         (3, "resume-claimed-issue", 452))

    def test_renovate_pr_with_agent_repair_commits_is_active_work(self):
        prs = [renovate_pr(406, "v", 10, commit_authors=("renovate[bot]", "bigtcze"))]
        d = self.pick(snapshot(open_prs=prs))
        self.assertEqual((d["priority"], d["kind"], d["target"]["number"]),
                         (3, "active-pr", 406))

    def test_renovate_pr_with_unknown_commit_author_is_not_active_work(self):
        prs = [renovate_pr(406, "v", 10, commit_authors=("renovate[bot]", ""))]
        self.assertEqual(self.pick(snapshot(open_prs=prs))["priority"], 6)

    def test_release_branch_pr_is_active_work_even_for_a_draft_bodyless_human_pr(self):
        d = self.pick(snapshot(open_prs=[release_pr(500)]))
        self.assertEqual((d["priority"], d["kind"], d["target"]["number"]), (3, "active-pr", 500))
        # A draft release PR is not active work yet and does not suppress the queue.
        self.assertNotEqual(self.pick(snapshot(open_prs=[release_pr(501, draft=True)]))["priority"], 3)

    def test_priority_three_sub_rules_run_in_the_documented_order(self):
        repair = renovate_pr(406, "v", 10, commit_authors=("renovate[bot]", "bigtcze"))
        release = release_pr(500)
        # (b) an already-repaired Renovate PR outranks (c) a freshly opened release PR.
        d = self.pick(snapshot(open_prs=[repair, release]))
        self.assertEqual((d["priority"], d["target"]["number"]), (3, 406))
        # (a) an owned-Issue PR outranks both.
        d = self.pick(snapshot(open_prs=[repair, release, feature_pr(470, 446)]))
        self.assertEqual((d["priority"], d["target"]["number"]), (3, 470))

    def test_human_pr_without_owned_refs_is_never_adopted(self):
        pr = feature_pr(471, 300)
        d = self.pick(snapshot(open_prs=[pr]))
        self.assertEqual(d["priority"], 6)
        self.assertTrue(any("#471 references no owned Issue; inspect, never adopt" in n
                            for n in d["notes"]))

    def test_pr_for_a_blocked_owned_issue_does_not_pin_priority_3(self):
        issues = backlog()
        issues[2] = issue(446, "V5-02", "Recurrence root-cause audit",
                          status="status:blocked", deps=[444])
        d = self.pick(snapshot(open_prs=[feature_pr(470, 446)], issues=issues))
        self.assertEqual((d["priority"], d["target"]["number"]), (6, 445))
        self.assertTrue(any("#470 references only status:blocked" in n for n in d["notes"]))
        issues[2] = issue(446, "V5-02", "Recurrence root-cause audit",
                          status="status:in-progress", deps=[444])
        d = self.pick(snapshot(open_prs=[feature_pr(470, 446)], issues=issues))
        self.assertEqual((d["priority"], d["target"]["number"]), (3, 470))

    # -- security and red main -------------------------------------------
    def test_security_supersedes_active_pr_and_renovate(self):
        prs = [renovate_pr(404, "v", 100), feature_pr(470, 446),
               renovate_pr(480, "fix CVE", 1, labels=("dependencies", "renovate", "security"))]
        d = self.pick(snapshot(open_prs=prs))
        self.assertEqual((d["priority"], d["kind"], d["target"]["number"]),
                         (2, "security-pr", 480))
        alert = {"number": 7, "security_vulnerability": {"first_patched_version": {"identifier": "1.2.3"}}}
        d = self.pick(snapshot(open_prs=[feature_pr(470, 446)], security_alerts=[alert]))
        self.assertEqual((d["priority"], d["kind"]), (2, "security-alert"))

    def test_unfixable_alert_is_noted_not_selected(self):
        alert = {"number": 8, "security_vulnerability": {"first_patched_version": None}}
        d = self.pick(snapshot(security_alerts=[alert]))
        self.assertEqual(d["priority"], 6)
        self.assertTrue(any("without a patched version" in n for n in d["notes"]))

    def test_red_main_supersedes_everything(self):
        runs = [{"id": 9, "status": "completed", "conclusion": "failure",
                 "created_at": iso(1), "head_sha": "d"},
                {"id": 8, "status": "completed", "conclusion": "success",
                 "created_at": iso(3), "head_sha": "c"}]
        prs = [renovate_pr(480, "s", 1, labels=("security", "renovate")), feature_pr(470, 446)]
        d = self.pick(snapshot(main_runs=runs, open_prs=prs))
        self.assertEqual((d["priority"], d["kind"], d["target"]["run"]), (1, "red-main", 9))

    def test_in_progress_main_run_uses_latest_completed_run(self):
        runs = [{"id": 10, "status": "in_progress", "conclusion": None, "created_at": iso(0.1)},
                {"id": 9, "status": "completed", "conclusion": "success", "created_at": iso(1)}]
        self.assertEqual(self.pick(snapshot(main_runs=runs))["priority"], 6)

    def test_skipped_main_run_is_green(self):
        runs = [{"id": 11, "status": "completed", "conclusion": "skipped",
                 "created_at": iso(1), "head_sha": "e"}]
        self.assertEqual(self.pick(snapshot(main_runs=runs))["priority"], 6)

    # -- triage markers ---------------------------------------------------
    def test_blocked_unchanged_renovate_pr_is_skipped(self):
        prs = [renovate_pr(404, "update vite to v8.3.6", 200,
                           triage=("blocked", "update vite to v8.3.6"))]
        history = [merged(1, 30, True), merged(2, 20), merged(3, 10)]
        d = self.pick(snapshot(open_prs=prs, merged_prs=history))
        self.assertEqual(d["priority"], 6)
        self.assertEqual(d["context"]["blocked_unchanged_skipped"], [404])

    def test_stale_blocked_triage_expires_for_a_recheck(self):
        days = sel.BLOCKED_RECHECK_DAYS
        fresh = renovate_pr(404, "v", 24 * 30, triage=("blocked", "v"),
                            triage_age_hours=24 * days - 1)
        self.assertEqual(self.pick(snapshot(open_prs=[fresh]))["priority"], 6)
        stale = renovate_pr(404, "v", 24 * 30, triage=("blocked", "v"),
                            triage_age_hours=24 * days + 1)
        d = self.pick(snapshot(open_prs=[stale]))
        self.assertEqual((d["priority"], d["kind"], d["target"]["number"]),
                         (4, "triage-aged-renovate", 404))
        repaired = renovate_pr(404, "v", 24 * 30, triage=("blocked", "v"),
                               triage_age_hours=24 * days + 1,
                               commit_authors=("renovate[bot]", "bigtcze"))
        d = self.pick(snapshot(open_prs=[repaired]))
        self.assertEqual((d["priority"], d["kind"], d["target"]["number"]),
                         (4, "triage-aged-renovate", 404))
        repaired["comments"].append({"author": {"login": "bigtcze"}, "createdAt": iso(0.5),
                                     "body": "TRIAGE agent=a time=x decision=actionable target=v"})
        d = self.pick(snapshot(open_prs=[repaired]))
        self.assertEqual((d["priority"], d["kind"], d["target"]["number"]),
                         (3, "active-pr", 404))
        old_ok = renovate_pr(404, "v", 24 * 30, triage=("actionable", "v"),
                             triage_age_hours=24 * days * 4)
        self.assertEqual(self.pick(snapshot(open_prs=[old_ok]))["priority"], 6)

    def test_blocked_pr_with_new_version_needs_triage_again(self):
        prs = [renovate_pr(404, "update vite to v8.4.0", 200,
                           triage=("blocked", "update vite to v8.3.6"))]
        self.assertEqual(self.pick(snapshot(open_prs=prs))["target"]["number"], 404)
        self.assertEqual(self.pick(snapshot(open_prs=prs))["priority"], 4)

    def test_triage_marker_from_renovate_itself_does_not_count(self):
        pr = renovate_pr(404, "v", 100)
        pr["comments"] = [{"author": {"login": "renovate[bot]"}, "createdAt": iso(1),
                           "body": "TRIAGE agent=x time=y decision=blocked target=v"}]
        self.assertEqual(self.pick(snapshot(open_prs=[pr]))["priority"], 4)

    def test_oldest_aged_renovate_pr_first(self):
        prs = [renovate_pr(406, "c", 60), renovate_pr(404, "a", 80), renovate_pr(405, "b", 70)]
        self.assertEqual(self.pick(snapshot(open_prs=prs))["target"]["number"], 404)

    # -- dependency PRs ---------------------------------------------------
    def test_remaining_dependency_prs_are_processed_when_no_issue_is_ready(self):
        issues = [issue(444, "V5-00", "Gov", status=None, state="CLOSED"),
                  issue(447, "V5-03", "Fix recurrence", deps=[446]),
                  issue(446, "V5-02", "Audit", status="status:blocked", deps=[444])]
        prs = [renovate_pr(404, "v", 10)]
        d = self.pick(snapshot(open_prs=prs, issues=issues))
        self.assertEqual((d["priority"], d["kind"], d["target"]["number"]),
                         (7, "dependency-pr", 404))

    def test_human_dependency_pr_is_reviewable_work_at_slot_7(self):
        issues = [issue(444, "V5-00", "Gov", status=None, state="CLOSED")]
        pr = human_dependency_pr(410, "chore(deps): bump vite", 10)
        d = self.pick(snapshot(open_prs=[pr], issues=issues))
        self.assertEqual((d["priority"], d["kind"], d["target"]["number"]),
                         (7, "dependency-pr", 410))

    def test_human_dependency_pr_is_never_aged_triage(self):
        issues = [issue(444, "V5-00", "Gov", status=None, state="CLOSED")]
        pr = human_dependency_pr(410, "chore(deps): bump vite", 200)
        d = self.pick(snapshot(open_prs=[pr], issues=issues))
        self.assertEqual(d["priority"], 7)
        self.assertEqual(d["context"]["open_dependency_prs"], {"410": "untriaged"})

    def test_human_dependency_pr_enters_the_fairness_slot(self):
        pr = human_dependency_pr(410, "chore(deps): bump vite", 10)
        history = [merged(1, 30, True), merged(2, 20), merged(3, 10)]
        d = self.pick(snapshot(open_prs=[pr], merged_prs=history))
        self.assertEqual((d["priority"], d["kind"], d["target"]["number"]),
                         (5, "dependency-slot", 410))

    def test_untriaged_human_pr_without_dependencies_label_is_not_a_dependency_pr(self):
        pr = human_dependency_pr(411, "docs: tidy", 200, labels=())
        issues = [issue(444, "V5-00", "Gov", status=None, state="CLOSED"),
                  issue(446, "V5-02", "Recurrence audit", status="status:blocked", deps=[444])]
        d = self.pick(snapshot(open_prs=[pr], issues=issues))
        self.assertEqual(d["priority"], "idle")
        self.assertTrue(any("#411 references no owned Issue" in n for n in d["notes"]))

    # -- issue readiness and ownership -----------------------------------
    def test_idle_when_nothing_actionable(self):
        issues = [issue(444, "V5-00", "Gov", status=None, state="CLOSED")]
        d = self.pick(snapshot(issues=issues))
        self.assertEqual((d["priority"], d["kind"], d["target"]), ("idle", "none", None))

    def test_recurrence_issues_rank_first_then_issue_code(self):
        self.assertEqual(self.pick(snapshot())["target"]["number"], 446)
        issues = [i for i in backlog() if i["number"] not in (446, 447)]
        self.assertEqual(self.pick(snapshot(issues=issues))["target"]["number"], 445)

    def test_explicit_priority_line_overrides_the_default(self):
        issues = [issue(444, "V5-00", "Gov", status=None, state="CLOSED"),
                  issue(445, "V5-01", "First", deps=[444], body_priority=1),
                  issue(446, "V5-02", "Second", deps=[444], body_priority=2)]
        self.assertEqual(self.pick(snapshot(issues=issues))["target"]["number"], 445)

    def test_dependency_on_closed_non_owned_issue_is_satisfied(self):
        issues = [issue(900, "V6-01", "Household export", deps=[444])]
        d = self.pick(snapshot(issues=issues, dependency_states={"444": "CLOSED"}))
        self.assertEqual((d["priority"], d["target"]["number"]), (6, 900))

    def test_open_non_owned_dependency_blocks(self):
        issues = [issue(900, "V6-01", "Household export", deps=[444])]
        d = self.pick(snapshot(issues=issues, dependency_states={"444": "OPEN"}))
        self.assertEqual(d["priority"], "idle")
        self.assertTrue(any("#900 depends on open non-owned Issue #444" in n for n in d["notes"]))

    def test_unknown_dependency_state_is_not_ready(self):
        issues = [issue(900, "V6-01", "Household export", deps=[444])]
        d = self.pick(snapshot(issues=issues, dependency_states={}))
        self.assertEqual(d["priority"], "idle")
        self.assertTrue(any("state is unknown" in n for n in d["notes"]))

    def test_missing_blocked_by_line_means_no_dependencies_with_a_note(self):
        issues = [issue(900, "V6-01", "Household export", deps=None)]
        d = self.pick(snapshot(issues=issues))
        self.assertEqual((d["priority"], d["target"]["number"]), (6, 900))
        self.assertTrue(any("no 'Blocked by:' line" in n for n in d["notes"]))

    def test_blocked_by_none_means_no_dependencies(self):
        issues = [issue(900, "V6-01", "Household export", deps=())]
        d = self.pick(snapshot(issues=issues))
        self.assertEqual((d["priority"], d["target"]["number"]), (6, 900))
        self.assertFalse(any("no 'Blocked by:' line" in n for n in d["notes"]))

    def test_dependencies_must_be_closed_owned_issues(self):
        issues = [issue(444, "V5-00", "Gov", status=None, state="CLOSED"),
                  issue(447, "V5-03", "Fix recurrence", deps=[446]),
                  issue(446, "V5-02", "Audit", status="status:blocked", deps=[444]),
                  issue(453, "V5-09", "Households", deps=[300])]
        d = self.pick(snapshot(issues=issues))
        self.assertEqual(d["priority"], "idle")
        self.assertTrue(any("#453 depends on" in n for n in d["notes"]))

    def test_ambiguous_status_labels_are_not_claimed(self):
        issues = backlog()
        issues[2]["labels"].append({"name": "status:blocked"})
        d = self.pick(snapshot(issues=issues))
        self.assertNotEqual(d["target"]["number"], 446)
        self.assertTrue(any("#446 has status labels" in n for n in d["notes"]))

    def test_issue_without_a_status_is_excluded_from_active_prs_and_claims(self):
        issues = backlog()
        issues[2] = issue(446, "V5-02", "Recurrence root-cause audit", status=None, deps=[444])
        d = self.pick(snapshot(open_prs=[feature_pr(470, 446)], issues=issues))
        self.assertEqual((d["priority"], d["kind"], d["target"]["number"]),
                         (6, "claim-issue", 445))
        self.assertTrue(any("#446 has status labels []" in n for n in d["notes"]))
        self.assertTrue(any("#470 references only anomalous" in n for n in d["notes"]))

    def test_issue_without_a_status_is_never_claimed(self):
        issues = [issue(900, "V6-01", "Household export", status=None, deps=())]
        d = self.pick(snapshot(issues=issues))
        self.assertEqual((d["priority"], d["kind"]), ("idle", "none"))
        self.assertTrue(any("#900 has status labels []" in n for n in d["notes"]))

    def test_issue_with_two_statuses_is_excluded_from_active_prs(self):
        issues = backlog()
        issues[2]["labels"].append({"name": "status:in-progress"})
        d = self.pick(snapshot(open_prs=[feature_pr(470, 446)], issues=issues))
        self.assertEqual((d["priority"], d["kind"], d["target"]["number"]),
                         (6, "claim-issue", 445))
        self.assertTrue(any("#446 has status labels" in n for n in d["notes"]))
        self.assertTrue(any("#470 references only anomalous" in n for n in d["notes"]))

    def test_issue_with_two_statuses_is_not_resumed(self):
        issues = backlog()
        issues[5] = issue(452, "V5-08", "Password change",
                          status="status:in-progress", deps=[444])
        issues[5]["labels"].append({"name": "status:blocked"})
        d = self.pick(snapshot(issues=issues))
        self.assertEqual((d["priority"], d["kind"], d["target"]["number"]),
                         (6, "claim-issue", 446))
        self.assertTrue(any("#452 has status labels "
                            "['status:in-progress', 'status:blocked']" in n for n in d["notes"]))

    def test_unreadable_security_alerts_are_reported(self):
        d = self.pick(snapshot(security_alerts=None))
        self.assertTrue(any("security alerts unreadable" in n for n in d["notes"]))

    # -- the evergreen ownership model -----------------------------------
    def test_future_release_issue_without_milestone_or_known_title_is_selected(self):
        issues = [issue(900, "V6-01", "Household export", deps=[444])]
        d = self.pick(snapshot(issues=issues, dependency_states={"444": "CLOSED"}))
        self.assertEqual((d["priority"], d["kind"], d["target"]["number"]),
                         (6, "claim-issue", 900))

    def test_unlabelled_idle_lookalike_is_never_selected_nor_owned(self):
        lookalike = issue(900, "V6-01", "Household export", deps=[444], owned=False,
                          milestone=V5_MILESTONE)
        d = self.pick(snapshot(issues=[lookalike], dependency_states={"444": "CLOSED"}))
        self.assertEqual(d["priority"], "idle")
        self.assertEqual(d["context"]["ignored_unowned_issues"], [900])

    def test_issue_with_v5_milestone_and_autonomy_label_but_no_release_label_is_ignored(self):
        legacy = issue(902, "V5-30", "Old ownership", deps=(), owned=False,
                       extra_labels=("tendo:v5-autonomous",), milestone=V5_MILESTONE)
        d = self.pick(snapshot(issues=[legacy]))
        self.assertEqual(d["priority"], "idle")
        self.assertEqual(d["context"]["ignored_unowned_issues"], [902])

    def test_existing_v5_issue_relabelled_release_keeping_milestone_is_selected(self):
        issues = [issue(444, "V5-00", "Gov", status=None, state="CLOSED"),
                  issue(446, "V5-02", "Recurrence root-cause audit", deps=[444],
                        milestone=V5_MILESTONE)]
        d = self.pick(snapshot(issues=issues))
        self.assertEqual((d["priority"], d["target"]["number"]), (6, 446))

    def test_closed_issue_is_never_claimed(self):
        issues = [issue(444, "V5-00", "Gov", status=None, state="CLOSED"),
                  issue(900, "V6-01", "Household export", state="CLOSED", deps=())]
        self.assertEqual(self.pick(snapshot(issues=issues))["priority"], "idle")

    def test_milestone_and_title_version_never_affect_selection(self):
        plain = issue(900, "V6-01", "Household export", deps=())
        tagged = copy.deepcopy(plain)
        tagged["milestone"] = {"title": V5_MILESTONE}
        first = self.pick(snapshot(issues=[plain]))
        second = self.pick(snapshot(issues=[tagged]))
        self.assertEqual(first, second)

    def test_release_coordinator_issue_ranks_after_ordinary_ready_work(self):
        coordinator = issue(910, "V6-02", "Release v6.0.0 coordination", deps=())
        ordinary = issue(909, "V6-01", "Ordinary work", deps=())
        d = self.pick(snapshot(issues=[coordinator, ordinary]))
        self.assertEqual((d["priority"], d["target"]["number"]), (6, 909))


class ReleaseEligibility(unittest.TestCase):
    def pick(self, snap, now=NOW):
        return sel.select(snap, now)

    def test_doc_test_and_ci_only_changes_never_release(self):
        changes = {"files": ["docs/guide.md", "README.md", ".autonomous/NOTES.md",
                             "backend/internal/handler_test.go", "backend/test/helpers.go",
                             ".github/workflows/required.yml", "scripts/verify-local.sh",
                             "frontend/src/App.test.tsx", "frontend/src/__tests__/x.ts",
                             "frontend/e2e/smoke.ts", "renovate.json", "LICENSE",
                             "api/schema.json", "deploy/compose.yml"],
                   "runtime_dependency_change": None, "truncated": False}
        d = self.pick(release_snapshot(release_changes=changes))
        self.assertEqual((d["priority"], d["kind"]), ("idle", "none"))
        self.assertTrue(any("not runtime-meaningful" in n for n in d["notes"]))

    def test_patch_release_after_fourteen_days_with_a_runtime_change(self):
        snap = release_snapshot(latest_release={"tagName": "v5.0.0",
                                                 "publishedAt": iso(24 * 15)})
        d = self.pick(snap)
        self.assertEqual((d["priority"], d["kind"]), (8, "patch-release"))
        self.assertEqual(d["target"]["type"], "release")
        self.assertEqual(d["target"]["version"], "v5.0.1")
        self.assertEqual(d["target"]["since"], "v5.0.0")
        self.assertEqual(d["target"]["runtime_files"], ["backend/internal/handler.go"])

    def test_no_patch_release_before_fourteen_days(self):
        snap = release_snapshot(latest_release={"tagName": "v5.0.0",
                                                 "publishedAt": iso(24 * 13)})
        self.assertEqual(self.pick(snap)["priority"], "idle")

    def test_security_release_is_immediate(self):
        snap = release_snapshot(
            latest_release={"tagName": "v5.0.0", "publishedAt": iso(24)},
            merged_since_release=[merged(7, 2, refs=None, security=True)])
        d = self.pick(snap)
        self.assertEqual((d["priority"], d["kind"]), (8, "security-release"))
        self.assertEqual(d["target"]["version"], "v5.0.1")

    def test_security_merge_from_before_the_latest_release_does_not_release(self):
        # A PR merged 30 days ago can still be listed for a 1-day-old release
        # because the collector walks by updated_at. Only merges inside the
        # release window may trigger a security release.
        snap = release_snapshot(
            latest_release={"tagName": "v5.0.0", "publishedAt": iso(24)},
            merged_since_release=[merged(7, 24 * 30, refs=None, security=True)])
        d = self.pick(snap)
        self.assertEqual((d["priority"], d["kind"]), ("idle", "none"))
        self.assertFalse(any("security PR" in n for n in d["notes"]))

    def test_release_requires_a_run_on_the_current_main_head(self):
        snap = release_snapshot(main_runs=[])
        d = self.pick(snap)
        self.assertEqual((d["priority"], d["kind"]), ("idle", "none"))
        self.assertTrue(any("no required.yml run for current main HEAD" in n for n in d["notes"]))

    def test_queued_run_on_main_head_blocks_release_despite_an_older_success(self):
        runs = [{"id": 3, "status": "queued", "conclusion": None,
                 "created_at": iso(0.5), "head_sha": "abc"},
                {"id": 2, "status": "completed", "conclusion": "success",
                 "created_at": iso(5), "head_sha": "old"}]
        d = self.pick(release_snapshot(main_runs=runs))
        self.assertEqual((d["priority"], d["kind"]), ("idle", "none"))
        self.assertTrue(any("is queued" in n for n in d["notes"]))

    def test_in_progress_rerun_on_main_head_blocks_release(self):
        runs = [{"id": 3, "status": "in_progress", "conclusion": None,
                 "created_at": iso(0.5), "head_sha": "abc"},
                {"id": 2, "status": "completed", "conclusion": "success",
                 "created_at": iso(5), "head_sha": "abc"}]
        d = self.pick(release_snapshot(main_runs=runs))
        self.assertEqual((d["priority"], d["kind"]), ("idle", "none"))
        self.assertTrue(any("is in_progress" in n for n in d["notes"]))

    def test_success_on_an_older_main_sha_is_not_green_for_the_current_head(self):
        runs = [{"id": 2, "status": "completed", "conclusion": "success",
                 "created_at": iso(5), "head_sha": "old"}]
        d = self.pick(release_snapshot(main_runs=runs))
        self.assertEqual((d["priority"], d["kind"]), ("idle", "none"))
        self.assertTrue(any("newest run is for an older commit" in n for n in d["notes"]))

    def test_green_run_on_the_current_main_head_allows_a_patch_release(self):
        runs = [{"id": 2, "status": "completed", "conclusion": "success",
                 "created_at": iso(5), "head_sha": "abc"}]
        d = self.pick(release_snapshot(main_runs=runs))
        self.assertEqual((d["priority"], d["kind"]), (8, "patch-release"))

    def test_security_pr_with_only_a_dev_tool_change_does_not_release(self):
        snap = release_snapshot(
            latest_release={"tagName": "v5.0.0", "publishedAt": iso(24)},
            merged_since_release=[merged(7, 2, refs=None, security=True)],
            release_changes={"files": ["scripts/dev-exec.sh", "docs/guide.md"],
                             "runtime_dependency_change": None, "truncated": False})
        self.assertEqual(self.pick(snap)["priority"], "idle")

    def test_coordinator_issue_suspends_patch_release(self):
        coordinator = issue(910, "V6-01", "Next major release", status="status:blocked", deps=None)
        coordinator["body"] += "\nRelease: v6.0.0\n"
        snap = release_snapshot(latest_release={"tagName": "v5.0.0",
                                                 "publishedAt": iso(24 * 30)},
                                issues=[coordinator])
        d = self.pick(snap)
        self.assertEqual(d["priority"], "idle")
        self.assertTrue(any("release candidate v6.0.0 in flight (#910)" in n for n in d["notes"]))
        self.assertTrue(any("automatic patch releases suspended" in n for n in d["notes"]))

    def test_patch_level_coordinator_token_does_not_suspend(self):
        coordinator = issue(910, "V6-01", "Release v5.0.1", status="status:blocked", deps=None)
        snap = release_snapshot(latest_release={"tagName": "v5.0.0",
                                                 "publishedAt": iso(24 * 30)},
                                issues=[coordinator])
        self.assertEqual(self.pick(snap)["kind"], "patch-release")

    def test_open_release_branch_pr_suspends_patch_release(self):
        snap = release_snapshot(latest_release={"tagName": "v5.0.0",
                                                 "publishedAt": iso(24 * 30)},
                                open_prs=[release_pr(500, draft=True)])
        d = self.pick(snap)
        self.assertEqual(d["priority"], "idle")
        self.assertTrue(any("release PR #500 in flight" in n for n in d["notes"]))

    def test_version_suggestion_compares_semantically(self):
        snap = release_snapshot(latest_release={"tagName": "v4.0.9",
                                                 "publishedAt": iso(24 * 30)},
                                tags=["v4.0.9", "v4.0.10", "v4.0.2"])
        self.assertEqual(self.pick(snap)["target"]["version"], "v4.0.11")

    def test_package_json_change_counts_only_with_a_runtime_dependency_change(self):
        files = ["frontend/package.json", "frontend/package-lock.json"]
        quiet = release_snapshot(release_changes={"files": files,
                                                  "runtime_dependency_change": False,
                                                  "truncated": False})
        self.assertEqual(self.pick(quiet)["priority"], "idle")
        loud = release_snapshot(release_changes={"files": files,
                                                 "runtime_dependency_change": True,
                                                 "truncated": False})
        self.assertEqual(self.pick(loud)["kind"], "patch-release")

    def test_unknown_dependency_change_is_noted_and_not_released(self):
        snap = release_snapshot(release_changes={"files": ["frontend/package.json"],
                                                 "runtime_dependency_change": None,
                                                 "truncated": False})
        d = self.pick(snap)
        self.assertEqual(d["priority"], "idle")
        self.assertTrue(any("runtime_dependency_change=null" in n for n in d["notes"]))

    def test_truncated_compare_with_a_runtime_file_is_meaningful(self):
        files = [f"backend/internal/file{i:03d}.go" for i in range(299)] + ["docs/guide.md"]
        snap = release_snapshot(release_changes={"files": files,
                                                 "runtime_dependency_change": None,
                                                 "truncated": True})
        self.assertEqual(self.pick(snap)["kind"], "patch-release")
        self.assertEqual(len(self.pick(snap)["target"]["runtime_files"]), 20)

    def test_truncated_compare_without_runtime_files_is_not_meaningful(self):
        snap = release_snapshot(release_changes={"files": ["docs/x.md"] * 300,
                                                 "runtime_dependency_change": None,
                                                 "truncated": True})
        d = self.pick(snap)
        self.assertEqual(d["priority"], "idle")
        self.assertTrue(any("truncated" in n for n in d["notes"]))

    def test_no_published_release_means_no_automatic_release(self):
        snap = release_snapshot(latest_release=None)
        d = self.pick(snap)
        self.assertEqual(d["priority"], "idle")
        self.assertTrue(any("no published release" in n for n in d["notes"]))


class RuntimeClassification(unittest.TestCase):
    RUNTIME = [
        "backend/internal/handler.go",
        "backend/cmd/tendo/main.go",
        "backend/go.mod",
        "backend/go.sum",
        "frontend/src/App.tsx",
        "frontend/src/lib/api.ts",
        "frontend/index.html",
        "frontend/public/icon.svg",
        "frontend/vite.config.ts",
        "api/openapi.yaml",
        "Dockerfile",
        "compose.yaml",
        ".env.example",
        "mise.toml",
        "mise.lock",
    ]
    NOT_RUNTIME = [
        "backend/internal/handler_test.go",
        "backend/test/helpers.go",
        "backend/internal/testdata/fixture.json",
        "frontend/src/App.test.tsx",
        "frontend/src/App.spec.ts",
        "frontend/src/App.spec.tsx",
        "frontend/src/__tests__/app.ts",
        "frontend/src/test/setup.ts",
        "frontend/e2e/smoke.ts",
        "docs/guide.md",
        ".autonomous/NOTES.md",
        "README.md",
        "scripts/verify-local.sh",
        ".github/workflows/required.yml",
        "ci/lint.sh",
        "deploy/compose.prod.yml",
        "api/schema.json",
        "renovate.json",
        "LICENSE",
        "frontend/package.json",
        "frontend/package-lock.json",
    ]

    def test_runtime_paths(self):
        for path in self.RUNTIME:
            with self.subTest(path=path):
                self.assertTrue(sel.is_runtime_path(path))

    def test_non_runtime_paths(self):
        for path in self.NOT_RUNTIME:
            with self.subTest(path=path):
                self.assertFalse(sel.is_runtime_path(path))

    def test_package_files_follow_the_dependency_flag(self):
        self.assertTrue(sel.is_runtime_path("frontend/package.json", True))
        self.assertFalse(sel.is_runtime_path("frontend/package.json", False))
        self.assertFalse(sel.is_runtime_path("frontend/package.json", None))
        self.assertTrue(sel.is_runtime_path("./backend/internal/x.go"))


class Collection(unittest.TestCase):
    """collect() must discover all open PRs, fail closed, and page bounded history."""

    def setUp(self):
        self.calls = []

    def fake_gh(self, *, open_count=2, commit_count=1, alerts_error=None, rate=None,
                merged_pages=None, endless_merged=False, alerts_raw=None, prs_raw=None,
                release=None):
        def run_gh(args):
            self.calls.append(list(args))
            joined = " ".join(args)
            if args[:2] == ["api", "rate_limit"]:
                return json.dumps(rate or {"resources": {
                    "core": {"remaining": 5000, "reset": 1},
                    "graphql": {"remaining": 5000, "reset": 1}}})
            if "workflows/required.yml/runs" in joined:
                return json.dumps({"workflow_runs": []})
            if "commits/main" in joined:
                return json.dumps({"sha": "abc"})
            if "dependabot/alerts" in joined:
                if alerts_error is not None:
                    raise alerts_error
                return alerts_raw if alerts_raw is not None else json.dumps([])
            if args[:2] == ["pr", "list"]:
                if prs_raw is not None:
                    return prs_raw
                return json.dumps([renovate_pr(400 + i, "update dep", 1)
                                   for i in range(open_count)])
            if args[:2] == ["issue", "list"]:
                return json.dumps([])
            if args[0] == "api" and "--paginate" in args:
                path = args[-1]
                if "/commits" in path:
                    return json.dumps([[{"author": {"login": "renovate[bot]"}}
                                        for _ in range(commit_count)]])
                return json.dumps([[]])
            if "pulls?state=closed" in joined:
                page = int(re.search(r"[?&]page=(\d+)", joined).group(1))
                if endless_merged:
                    return json.dumps([{"number": page, "merged_at": iso(30),
                                        "updated_at": iso(10), "user": {"login": "human"},
                                        "head": {"ref": f"feat/x-{page}"}, "labels": [],
                                        "body": ""}])
                return json.dumps((merged_pages or {}).get(page, []))
            if "releases/latest" in joined:
                if release is not None:
                    return json.dumps(release)
                raise sel.SelectionError("gh api repos/o/r/releases/latest failed (exit 1): HTTP 404")
            return "null"
        return run_gh

    def call_main(self, run_gh):
        out, err = io.StringIO(), io.StringIO()
        with contextlib.redirect_stdout(out), contextlib.redirect_stderr(err):
            code = sel.main(["--repo", "o/r", "--now", NOW.isoformat()], run_gh=run_gh)
        return code, out.getvalue(), err.getvalue()

    def pulls_calls(self):
        return [c for c in self.calls if "pulls?state=closed" in " ".join(c)]

    def test_lists_use_a_high_limit_and_comments_and_commits_are_paginated(self):
        snap = sel.collect("o/r", self.fake_gh(open_count=3), NOW)
        lists = [c for c in self.calls if c[1:2] == ["list"]]
        self.assertEqual(len(lists), 2)  # open PRs + owned Issues
        for call in lists:
            self.assertEqual(call[call.index("--limit") + 1], str(sel.FETCH_LIMIT))
        issue_call = next(c for c in lists if c[0] == "issue")
        self.assertIn("release", issue_call)
        self.assertNotIn("--milestone", issue_call)
        self.assertEqual(len(snap["open_prs"]), 3)
        self.assertEqual(snap["main_head_sha"], "abc")
        pr = snap["open_prs"][0]
        self.assertEqual(pr["comments"], [])
        self.assertEqual(pr["commitAuthors"], ["renovate[bot]"])

    def test_pr_commits_at_the_api_cap_are_an_error_not_a_truncation(self):
        run_gh = self.fake_gh(open_count=1, commit_count=sel.PR_COMMITS_API_CAP)
        with self.assertRaises(sel.SelectionError):
            sel.collect("o/r", run_gh, NOW)

    def test_a_list_at_the_fetch_limit_is_an_error_not_a_truncation(self):
        run_gh = self.fake_gh(open_count=sel.FETCH_LIMIT)
        with self.assertRaises(sel.SelectionError):
            sel.collect("o/r", run_gh, NOW)

    def test_failed_alerts_exit_2_without_a_decision(self):
        error = subprocess.CalledProcessError(1, ["gh"], stderr="boom")
        code, out, err = self.call_main(self.fake_gh(alerts_error=error))
        self.assertEqual(code, 2)
        self.assertNotIn("DECISION", out)
        self.assertIn("SELECTION ERROR", err)
        self.assertIn("dependabot", err)

    def test_null_alerts_payload_is_a_selection_error_not_an_empty_list(self):
        code, out, err = self.call_main(self.fake_gh(alerts_raw="null"))
        self.assertEqual(code, 2)
        self.assertNotIn("DECISION", out)
        self.assertIn("SELECTION ERROR", err)
        self.assertIn("dependabot alerts", err)

    def test_empty_alerts_payload_is_a_selection_error_not_an_empty_list(self):
        code, out, err = self.call_main(self.fake_gh(alerts_raw=""))
        self.assertEqual(code, 2)
        self.assertNotIn("DECISION", out)
        self.assertIn("SELECTION ERROR", err)
        self.assertIn("empty response", err)

    def test_null_pr_list_payload_is_a_selection_error(self):
        code, out, err = self.call_main(self.fake_gh(prs_raw="null"))
        self.assertEqual(code, 2)
        self.assertNotIn("DECISION", out)
        self.assertIn("gh pr list returned NoneType, expected a list", err)

    def test_merged_since_release_excludes_prs_merged_before_the_release(self):
        # The walker pages by updated_at, so a PR merged 30 days ago can appear
        # for a 1-day-old release. The collector must drop it from the window.
        snap = {"issues": [], "merged_since_release": [], "tags": []}
        pages = {1: [
            {"number": 12, "merged_at": iso(24 * 30), "updated_at": iso(2),
             "user": {"login": "human"}, "head": {"ref": "feat/x"}, "labels": [], "body": ""},
            {"number": 13, "merged_at": iso(2), "updated_at": iso(1),
             "user": {"login": "human"}, "head": {"ref": "feat/x"}, "labels": [], "body": ""},
        ]}
        run_gh = self.fake_gh(merged_pages=pages,
                              release={"tag_name": "v5.0.0", "published_at": iso(24)})
        sel.add_release_data(snap, "o/r", run_gh, NOW)
        self.assertEqual([p["number"] for p in snap["merged_since_release"]], [13])
        self.assertEqual(snap["tags"], [])

    def test_low_quota_exits_4_with_the_reset_time(self):
        rate = {"resources": {"core": {"remaining": 10, "reset": 1700000000},
                              "graphql": {"remaining": 5000, "reset": 1800000000}}}
        code, out, err = self.call_main(self.fake_gh(rate=rate))
        self.assertEqual(code, 4)
        self.assertEqual(out, "")
        self.assertIn("RATE_LIMITED reset=1800000000", err)

    def test_rate_limit_stderr_text_exits_4(self):
        def run_gh(args):
            self.calls.append(list(args))
            if args[:2] == ["api", "rate_limit"]:
                return json.dumps({"resources": {"core": {"remaining": 5000, "reset": 1},
                                                 "graphql": {"remaining": 5000, "reset": 1}}})
            raise subprocess.CalledProcessError(
                1, ["gh"], stderr="API rate limit exceeded for 1.2.3.4")
        code, out, err = self.call_main(run_gh)
        self.assertEqual(code, 4)
        self.assertEqual(out, "")
        self.assertIn("RATE_LIMITED reset=", err)

    def test_pagination_stops_on_the_merge_time_proof(self):
        # The dependency PR was merged 100 h ago but updated 1 h ago; the last
        # item on the page is older than the dependency merge, so one page is
        # enough. Using the dependency's updated_at instead of merged_at would
        # wrongly fetch a second page.
        pages = {1: [
            {"number": 12, "merged_at": iso(100), "updated_at": iso(1),
             "user": {"login": "app/renovate"}, "head": {"ref": "renovate/x"},
             "labels": [{"name": "dependencies"}], "body": ""},
            {"number": 11, "merged_at": iso(200), "updated_at": iso(150),
             "user": {"login": "human"}, "head": {"ref": "feat/x"}, "labels": [], "body": ""},
        ]}
        snap = sel.collect("o/r", self.fake_gh(merged_pages=pages), NOW)
        self.assertEqual(len(self.pulls_calls()), 1)
        self.assertEqual(len(snap["merged_prs"]), 2)

    def test_pagination_continues_until_a_dependency_pr_is_seen(self):
        pages = {
            1: [{"number": 20, "merged_at": iso(5), "updated_at": iso(4),
                 "user": {"login": "human"}, "head": {"ref": "feat/x"}, "labels": [], "body": ""},
                {"number": 21, "merged_at": iso(6), "updated_at": iso(6),
                 "user": {"login": "human"}, "head": {"ref": "feat/x"}, "labels": [], "body": ""}],
            2: [{"number": 22, "merged_at": iso(30), "updated_at": iso(30),
                 "user": {"login": "app/renovate"}, "head": {"ref": "renovate/y"},
                 "labels": [{"name": "dependencies"}], "body": ""},
                {"number": 23, "merged_at": iso(70), "updated_at": iso(70),
                 "user": {"login": "human"}, "head": {"ref": "feat/x"}, "labels": [], "body": ""},
                {"number": 24, "merged_at": iso(80), "updated_at": iso(80),
                 "user": {"login": "human"}, "head": {"ref": "feat/x"}, "labels": [], "body": ""}],
        }
        snap = sel.collect("o/r", self.fake_gh(merged_pages=pages), NOW)
        self.assertEqual(len(self.pulls_calls()), 2)
        self.assertEqual(len(snap["merged_prs"]), 5)

    def test_pagination_errors_instead_of_looping_forever(self):
        with self.assertRaises(sel.SelectionError):
            sel.collect("o/r", self.fake_gh(endless_merged=True), NOW)
        self.assertEqual(len(self.pulls_calls()), sel.MERGED_PAGE_CAP)


def comment(cid, hours_ago, body, assoc="OWNER", login="bigtcze"):
    return {"id": cid, "createdAt": iso(hours_ago), "updatedAt": iso(hours_ago),
            "author": {"login": login}, "authorAssociation": assoc, "body": body}


# Recorded server time of the real #446 blocker comment (before the cutoff).
LEGACY_446_TIME = "2026-11-12T14:21:00Z"


def legacy_comment(cid, body, login="tendo-autonomous-orchestrator", created=LEGACY_446_TIME):
    """Historical blocker comment pinned to its recorded server time."""
    return {"id": cid, "createdAt": created, "updatedAt": created,
            "author": {"login": login}, "authorAssociation": "OWNER", "body": body}


# Verbatim comment body recorded for #446 (read-only evidence fixture; the
# source file is never modified). It starts with a BLOCKED line and carries the
# markdown `## Blocker:` heading and the combined-delivery proposal.
LEGACY_446_BODY = """BLOCKED agent=tendo-autonomous-orchestrator time=2026-11-12

## Evidence (PR #464, head 532c188, required run 43234440474)
Audit: `docs/development/recurrence-audit.md` on the PR branch. Independent review (oracle): approve after rework.

Commit `aecad02` turned failing rows into expected-failure locks and inverted the frontend assertion. The reviewer rejected it (that weakens tests) and `9b23415` reverted it. Every assertion now states the contract value directly.

Required CI fails **only** on these reproducers. api, e2e, visual and all deployment smokes pass:
- **R1 anchor drift** (engine `TestNextCycleContractGolden/Coverage`, service `TestCompletionContractGoldenReturnedAndPersisted`, real PostgreSQL `TestCompletionContractGoldenPostgres`, which now runs Golden()+Coverage()). The same 8 rows fail on all three layers: fixed anchor restoration (2027-03-28, want 03-31), yearly leap day (2032-02-28, want 02-29), quarterly (05-28, want 05-30), half-yearly (08-28, want 08-31), 12-month leap day, 2-month early (04-28, want 04-30), leap century 2000 (2000-02-28, want 02-29), 2104 after non-leap 2100. Causes: no persisted anchor, and `NextCycle` catch-up clamps sequentially.
- **R2 same-date Done** (`ItemDetailScreen.test.tsx`, fixed and after_completion). When the first completion commits while detail is unmounted, its Idempotency-Key is not cleared. The next Done reuses the key, gets the old receipt replayed and does not advance.
- **R3 undo vs intervening edit** (`TestCompletionContractUndoRejectsInterveningAttentionEdit`): undo succeeds and overwrites the 2026-10-25 edit back to 2026-10-20. This collides with the contract rule "reject undo for an intervening changed state". The current behavior is documented in api/openapi.yaml and `TestUndoOverwritesAttentionEditedAfterCompletion`.
- Pass on all layers: year rollover, early/late fixed, fluid, 2-month late, 7/14-day December crossing, daily into Feb 29, DST (Europe/Prague) local date.

## Blocker: dependency ordering (owner decision needed)
Acceptance needs failing tests that are merged. Merging needs green required CI, and the tests must not be weakened (GUARDRAILS, FLOW, V2_1_SCOPE). So #446 cannot merge on its own. #447 (the fix) is `Blocked by: #446`, and #446 can only close after a merged PR and green main. Neither Issue can proceed autonomously.

Proposed resolution (needs owner authorization because it changes Issue-level ordering): deliver #446 and #447 as one PR. The #447 fix builds on branch `test/v5-02-recurrence-golden`; the tests and the fix merge together through normal green checks; both Issues close after a verified green main. Until then PR #464 stays draft as recorded red evidence. Production recurrence logic is unchanged."""


def legacy_issues(claim_comments=(), extra_446=(), body_446=None):
    """Actual #446/#447 shape: #446 blocked with the recorded blocker body; #447 depends on #446."""
    body_446 = LEGACY_446_BODY if body_446 is None else body_446
    return owned_graph(
        [issue(446, "V5-02", "Recurrence audit", status="status:blocked", deps=[444]),
         issue(447, "V5-03", "Fix recurrence engine", status="status:idle", deps=[446])],
        {446: [legacy_comment(10, body_446),
               *claim_comments, *extra_446]})


def with_comments(item, *comments):
    item = copy.deepcopy(item)
    item["comments"] = list(comments)
    return item


def owned_graph(items, comments=None):
    """Closed #444 plus the given owned Issues; `comments` maps number -> list."""
    comments = comments or {}
    base = [issue(444, "V5-00", "Gov", status=None, state="CLOSED")] + list(items)
    return [with_comments(i, *comments.get(i["number"], [])) for i in base]


def legacy_pr(number, refs_number):
    pr = feature_pr(number, refs_number)
    pr["isDraft"] = True
    return pr


def merged_default():
    return merged(1, 30, dependency=True)


class DependencyRecovery(unittest.TestCase):
    def pick(self, issues, dependency_states=None, open_prs=None, merged=None):
        return sel.select(snapshot(issues=issues, dependency_states=dependency_states or {},
                                   open_prs=open_prs or [], merged_prs=merged or [merged_default()]), NOW)

    def assertNoRecovery(self, decision):
        self.assertNotEqual(decision["kind"], "review-owned-dependencies")
        self.assertNotEqual(decision["kind"], "dependency-recovery")

    # -- explicit records: advisory only, never executable -----------------
    def test_explicit_pair_is_advisory_review_not_executable(self):
        items = owned_graph(
            [issue(446, "V5-02", "Audit", status="status:blocked", deps=[444]),
             issue(447, "V5-03", "Fix", status="status:idle", deps=[446])],
            {446: [comment(1, 5, "BLOCKED kind=owned-dependency needs=#447 recovery=combined")]})
        d = self.pick(items)
        self.assertEqual((d["priority"], d["kind"]), (6, "review-owned-dependencies"))
        self.assertEqual(d["target"]["group"], [446, 447])
        self.assertEqual(d["target"]["blocker"]["comment"], 1)

    def test_ordinary_ready_issue_beats_advisory(self):
        items = legacy_issues() + [issue(900, "V6-01", "Ready", deps=[444])]
        d = self.pick(items)
        self.assertEqual((d["priority"], d["kind"], d["target"]["number"]), (6, "claim-issue", 900))

    def test_eligible_outside_work_is_claimed_before_any_held_advisory(self):
        # A foreign active claim on the legacy group must not pre-empt an eligible claim-issue.
        foreign = [comment(11, 4, "CLAIM agent=other time=x", login="other")]
        items = legacy_issues(claim_comments=foreign) + [issue(900, "V6-01", "Ready", deps=[444])]
        d = self.pick(items)
        self.assertEqual((d["kind"], d["target"]["number"]), ("claim-issue", 900))

    def test_unknown_blocked_prose_is_never_recommended(self):
        items = legacy_issues(body_446="BLOCKED agent=a time=x\n\nWaiting on the owner to decide.")
        self.assertNoRecovery(self.pick(items))

    def test_external_blocker_excludes_the_group_even_when_idle(self):
        items = owned_graph(
            [issue(446, "V5-02", "Audit", status="status:blocked", deps=[444]),
             issue(447, "V5-03", "Fix", status="status:idle", deps=[446])],
            {446: [comment(1, 5, "BLOCKED kind=external recovery=none")]})
        self.assertNoRecovery(self.pick(items))

    def test_external_member_blocker_excludes_a_declared_cycle(self):
        items = owned_graph(
            [issue(510, "V5-10", "a", status="status:idle", deps=[511]),
             issue(511, "V5-11", "b", status="status:idle", deps=[510])],
            {511: [comment(1, 5, "BLOCKED kind=external recovery=none")]})
        self.assertNoRecovery(self.pick(items))

    def test_untrusted_blocker_comment_is_ignored(self):
        items = owned_graph(
            [issue(446, "V5-02", "Audit", status="status:blocked", deps=[444]),
             issue(447, "V5-03", "Fix", status="status:idle", deps=[446])],
            {446: [comment(1, 5, "BLOCKED kind=owned-dependency needs=#447 recovery=combined",
                           assoc="NONE")]})
        self.assertNoRecovery(self.pick(items))

    # -- legacy #446/#447 evidence review (advisory, claimant only) ---------
    def test_legacy_446_447_is_reviewed_for_the_existing_claimant(self):
        d = self.pick(legacy_issues(claim_comments=[comment(11, 4, "CLAIM agent=a time=x", login="bigtcze")]),
                      open_prs=[legacy_pr(470, 446)])
        self.assertEqual((d["priority"], d["kind"]), (6, "review-owned-dependencies"))
        self.assertEqual(d["target"]["evidence"], "legacy-ordering")
        self.assertEqual(d["target"]["group"], [446, 447])
        self.assertEqual(d["target"]["number"], 446)
        self.assertEqual(d["target"]["pr"], 470)
        self.assertEqual(d["target"]["claim"]["agent"], "a")
        self.assertEqual(d["target"]["blocker"]["comment"], 10)

    def test_legacy_group_without_a_claimant_or_pr_is_not_reviewed(self):
        self.assertNoRecovery(self.pick(legacy_issues(), open_prs=[legacy_pr(470, 446)]))
        claimed = legacy_issues(claim_comments=[comment(11, 4, "CLAIM agent=a time=x", login="bigtcze")])
        self.assertNoRecovery(self.pick(claimed))

    def test_incidental_evidence_wording_in_the_body_does_not_block_review(self):
        # Body prose mentioning credentials/secrets is evidence, not a decision.
        body = (LEGACY_446_BODY + "\n\nNo credentials or infrastructure are missing. "
                "Checks run without secrets.")
        items = legacy_issues(claim_comments=[comment(11, 4, "CLAIM agent=a time=x", login="bigtcze")],
                              body_446=body)
        d = self.pick(items, open_prs=[legacy_pr(470, 446)])
        self.assertEqual((d["priority"], d["kind"]), (6, "review-owned-dependencies"))
        self.assertEqual(d["target"]["evidence"], "legacy-ordering")

    def test_external_decision_heading_excludes_the_review(self):
        body = LEGACY_446_BODY.replace(
            "## Blocker: dependency ordering (owner decision needed)",
            "## Blocker: external service outage (waiting on upstream)")
        items = legacy_issues(claim_comments=[comment(11, 4, "CLAIM agent=a time=x", login="bigtcze")],
                              body_446=body)
        self.assertNoRecovery(self.pick(items, open_prs=[legacy_pr(470, 446)]))

    def test_current_external_blocker_supersedes_an_earlier_ordering_review(self):
        # The latest trusted blocker decides: an explicit external record posted
        # after the ordering proposal genuinely blocks, so no review is emitted.
        comments = [
            legacy_comment(10, LEGACY_446_BODY),
            comment(11, 8, "CLAIM agent=a time=x", login="bigtcze"),
            comment(12, 7, "BLOCKED kind=external recovery=none", login="bigtcze"),
        ]
        items = owned_graph(
            [issue(446, "V5-02", "Recurrence audit", status="status:blocked", deps=[444]),
             issue(447, "V5-03", "Fix recurrence engine", status="status:idle", deps=[446])],
            {446: comments})
        d = self.pick(items, open_prs=[legacy_pr(470, 446)])
        self.assertNoRecovery(d)
        self.assertEqual((d["priority"], d["kind"]), ("idle", "none"))

    def test_legacy_review_refuses_when_dependency_is_not_a_declared_reverse_edge(self):
        items = owned_graph(
            [issue(446, "V5-02", "Audit", status="status:blocked", deps=[444]),
             issue(447, "V5-03", "Fix", status="status:idle", deps=[444])],
            {446: [legacy_comment(10, LEGACY_446_BODY),
                   comment(11, 4, "CLAIM agent=a time=x", login="bigtcze")]})
        self.assertNoRecovery(self.pick(items, open_prs=[legacy_pr(470, 446)]))

    def test_legacy_review_is_stable_over_time_main_and_unrelated_comments(self):
        base_items = legacy_issues(claim_comments=[comment(11, 4, "CLAIM agent=a time=x", login="bigtcze")])
        first = self.pick(base_items, open_prs=[legacy_pr(470, 446)])
        later = copy.deepcopy(base_items)
        later[1]["comments"].append(comment(20, 1, "Unrelated discussion."))
        later_snap = snapshot(issues=later, open_prs=[legacy_pr(470, 446)],
                              merged_prs=[merged_default()])
        later_snap["main_head_sha"] = "newer"
        second = sel.select(later_snap, NOW + timedelta(days=3))
        self.assertEqual(first["kind"], second["kind"])
        self.assertEqual(first["target"]["group"], second["target"]["group"])

    # -- HANDOFF and RELEASE authorization ----------------------------------
    def test_actual_446_fixture_reviews_via_cli_twice_with_identical_output(self):
        issues = legacy_issues(claim_comments=[comment(11, 4, "CLAIM agent=a time=x", login="bigtcze")])
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "snap.json"
            path.write_text(json.dumps(snapshot(issues=issues, open_prs=[legacy_pr(464, 446)])))
            runs = [subprocess.run([sys.executable, str(SCRIPT), "--snapshot", str(path),
                                    "--now", NOW.isoformat()],
                                   check=True, capture_output=True, text=True).stdout
                    for _ in range(2)]
        self.assertEqual(runs[0], runs[1])
        self.assertIn("DECISION priority=6 kind=review-owned-dependencies", runs[0])
        self.assertIn('"evidence": "legacy-ordering"', runs[0])
        self.assertIn('"group": [446, 447]', runs[0])
        self.assertIn('"pr": 464', runs[0])

    def test_release_then_reclaim_starts_a_new_active_epoch(self):
        items = legacy_issues(claim_comments=[
            comment(11, 8, "CLAIM agent=a time=x", login="bigtcze"),
            comment(12, 7, "RELEASE agent=a time=y", login="bigtcze"),
            comment(13, 6, "CLAIM agent=a time=z", login="bigtcze")])
        issue_446 = next(i for i in items if i["number"] == 446)
        life = sel.deps.replay(issue_446["comments"])
        self.assertEqual(life["claim"]["id"], 13)
        self.assertEqual(life["conflicts"], [])
        d = self.pick(items, open_prs=[legacy_pr(470, 446)])
        self.assertEqual(d["target"]["claim"]["id"], 13)

    def test_old_claim_then_blocker_then_release_then_reclaim_stays_review_not_idle(self):
        # Regression: CLAIM -> BLOCKED -> RELEASE -> CLAIM. The holder RELEASE
        # retires only the old epoch; the recorded blocker persists, so the
        # re-claim still gets the claimant-only review. There is no ack mechanism.
        comments = [
            legacy_comment(11, "CLAIM agent=a time=x", login="bigtcze", created="2026-11-12T12:00:00Z"),
            legacy_comment(10, LEGACY_446_BODY),
            legacy_comment(12, "RELEASE agent=a time=y", login="bigtcze", created="2026-11-12T15:00:00Z"),
            legacy_comment(13, "CLAIM agent=a time=z", login="bigtcze", created="2026-11-12T16:00:00Z"),
        ]
        items = owned_graph(
            [issue(446, "V5-02", "Recurrence audit", status="status:blocked", deps=[444]),
             issue(447, "V5-03", "Fix recurrence engine", status="status:idle", deps=[446])],
            {446: comments})
        d = self.pick(items, open_prs=[legacy_pr(470, 446)])
        self.assertEqual((d["priority"], d["kind"]), (6, "review-owned-dependencies"))
        self.assertEqual(d["target"]["evidence"], "legacy-ordering")
        self.assertEqual(d["target"]["claim"]["id"], 13)
        self.assertEqual(d["target"]["conflicts"], [])

    def test_transition_review_then_active_pr_then_idle_after_closed(self):
        items = legacy_issues(claim_comments=[comment(11, 4, "CLAIM agent=a time=x", login="bigtcze")])
        review = self.pick(items, open_prs=[legacy_pr(464, 446)])
        self.assertEqual((review["priority"], review["kind"]), (6, "review-owned-dependencies"))
        self.assertEqual(review["target"]["number"], 446)
        self.assertEqual(review["target"]["claim"]["agent"], "a")

        # The claimant reconciles live: #446 -> in-progress and a non-draft PR Refs both Issues.
        working = copy.deepcopy(items)
        member = next(i for i in working if i["number"] == 446)
        member["labels"] = [{"name": "status:in-progress"}, {"name": "release"}]
        pr = feature_pr(480, 446)
        pr["body"] = "Refs #446\nRefs #447"
        active = self.pick(working, open_prs=[pr])
        self.assertEqual((active["priority"], active["kind"], active["target"]["number"]),
                         (3, "active-pr", 480))
        self.assertEqual(active["target"]["issues"], [446, 447])

        # Merged + both Issues closed: no work left. The selector never closes Issues itself.
        closed = [issue(444, "V5-00", "Gov", status=None, state="CLOSED"),
                  issue(446, "V5-02", "Recurrence audit", status="status:in-progress",
                        deps=[444], state="CLOSED"),
                  issue(447, "V5-03", "Fix recurrence engine", status="status:idle",
                        deps=[446], state="CLOSED")]
        merged_pr = merged(480, 1, refs=446)
        done = sel.select(snapshot(issues=closed, merged_prs=[merged_pr]), NOW)
        self.assertEqual((done["priority"], done["kind"], done["target"]), ("idle", "none", None))

    def test_handoff_by_the_claimant_login_and_agent_releases_the_claim(self):
        items = legacy_issues(claim_comments=[
            comment(11, 6, "CLAIM agent=a time=x", login="bigtcze"),
            comment(12, 5, "HANDOFF agent=a to=b time=y", login="bigtcze")])
        self.assertNoRecovery(self.pick(items, open_prs=[legacy_pr(470, 446)]))

    def test_handoff_by_a_different_login_with_the_same_agent_string_is_ignored(self):
        items = legacy_issues(claim_comments=[
            comment(11, 6, "CLAIM agent=a time=x", login="bigtcze"),
            comment(12, 5, "HANDOFF agent=a to=b time=y", login="intruder")])
        d = self.pick(items, open_prs=[legacy_pr(470, 446)])
        self.assertEqual(d["kind"], "review-owned-dependencies")
        self.assertEqual(d["target"]["claim"]["login"], "bigtcze")

    def test_untrusted_handoff_is_ignored(self):
        items = legacy_issues(claim_comments=[
            comment(11, 6, "CLAIM agent=a time=x", login="bigtcze"),
            comment(12, 5, "HANDOFF agent=a to=b time=y", login="bigtcze", assoc="NONE")])
        d = self.pick(items, open_prs=[legacy_pr(470, 446)])
        self.assertEqual(d["target"]["claim"]["agent"], "a")

    def test_repeated_claim_keeps_the_first_claim_id(self):
        items = legacy_issues(claim_comments=[
            comment(11, 6, "CLAIM agent=a time=x", login="bigtcze"),
            comment(13, 5, "CLAIM agent=a time=again", login="bigtcze")])
        d = self.pick(items, open_prs=[legacy_pr(470, 446)])
        self.assertEqual(d["target"]["claim"]["id"], 11)

    def test_foreign_claim_is_a_conflict_that_clears_when_the_loser_releases(self):
        held = [comment(11, 6, "CLAIM agent=a time=x", login="bigtcze"),
                comment(12, 5, "CLAIM agent=b time=y", login="other")]
        d = self.pick(legacy_issues(claim_comments=held), open_prs=[legacy_pr(470, 446)])
        self.assertEqual([c["agent"] for c in d["target"]["conflicts"]], ["b"])
        released = held + [comment(13, 4, "RELEASE agent=b time=z", login="other")]
        d = self.pick(legacy_issues(claim_comments=released), open_prs=[legacy_pr(470, 446)])
        self.assertEqual(d["target"]["conflicts"], [])

    def test_holder_release_clears_all_conflicts(self):
        held = [comment(11, 6, "CLAIM agent=a time=x", login="bigtcze"),
                comment(12, 5, "CLAIM agent=b time=y", login="other"),
                comment(13, 4, "RELEASE agent=a time=z", login="bigtcze")]
        issue_446 = next(i for i in legacy_issues(claim_comments=held) if i["number"] == 446)
        life = sel.deps.replay(issue_446["comments"])
        self.assertIsNone(life["claim"])
        self.assertEqual(life["conflicts"], [])

    def test_unknown_and_external_blockers_stay_unrecovered_across_time_main_and_comments(self):
        claim = [comment(11, 4, "CLAIM agent=a time=x", login="bigtcze")]
        cases = {
            # No structured heading/proposal: unknown prose fails closed even for a claimant.
            "unknown": legacy_issues(claim_comments=claim,
                                     body_446="BLOCKED agent=a time=x\n\nWaiting on the owner."),
            # Explicit external record excludes the group even with a claimant and PR.
            "external": owned_graph(
                [issue(446, "V5-02", "Audit", status="status:blocked", deps=[444]),
                 issue(447, "V5-03", "Fix", status="status:idle", deps=[446])],
                {446: [comment(10, 9, "BLOCKED kind=external recovery=none"), *claim]}),
        }
        for name, items in cases.items():
            with self.subTest(case=name):
                for hours, main_sha, extra in ((0, "abc", []), (24 * 30, "newer", []),
                                               (1, "newer", [comment(30, 1, "Unrelated note.")])):
                    current = copy.deepcopy(items)
                    current[1]["comments"].extend(extra)
                    snap = snapshot(issues=current, open_prs=[legacy_pr(470, 446)],
                                    merged_prs=[merged_default()])
                    snap["main_head_sha"] = main_sha
                    decision = sel.select(snap, NOW + timedelta(hours=hours))
                    self.assertNoRecovery(decision)

    # -- declared cycles ------------------------------------------------------
    def cycle_issues(self, edges, status="status:idle", comments=None):
        items = [issue(n, f"V5-{n}", f"c{n}", deps=list(deps), status=status)
                 for n, deps in edges.items()]
        return owned_graph(items, comments)

    def test_self_two_and_three_cycles_are_advisory_and_deterministic(self):
        import random
        cases = [({500: [500]}, "self"),
                 ({510: [511], 511: [510]}, "two"),
                 ({520: [521], 521: [522], 522: [520]}, "three")]
        for edges, shape in cases:
            with self.subTest(shape=shape):
                outputs = set()
                for seed in range(5):
                    items = self.cycle_issues(edges)
                    random.Random(seed).shuffle(items)
                    outputs.add(json.dumps(self.pick(items), sort_keys=True))
                self.assertEqual(len(outputs), 1)
                d = json.loads(outputs.pop())
                self.assertEqual((d["priority"], d["kind"]), (6, "review-owned-dependencies"))
                self.assertEqual(d["target"]["evidence"], "cycle")

    def test_cycle_with_an_unresolved_outside_dependency_is_not_reported(self):
        items = self.cycle_issues({510: [511, 300], 511: [510]})
        self.assertNoRecovery(self.pick(items, dependency_states={"300": "OPEN"}))

    def test_claimed_cycle_member_is_not_reported_as_recoverable(self):
        items = self.cycle_issues({510: [511], 511: [510]},
                                  comments={511: [comment(1, 3, "CLAIM agent=a time=x", login="bigtcze")]})
        self.assertNoRecovery(self.pick(items))

    def test_cycle_member_with_non_idle_status_is_not_reported(self):
        items = self.cycle_issues({510: [511], 511: [510]})
        member = next(i for i in items if i["number"] == 511)
        member["labels"] = [{"name": "status:in-progress"}, {"name": "release"}]
        self.assertNoRecovery(self.pick(items))

    # -- fail closed on malformed lifecycle data --------------------------------
    def test_issues_without_fetched_comments_keep_existing_behavior(self):
        items = [issue(444, "V5-00", "Gov", status=None, state="CLOSED"),
                 issue(510, "V5-10", "a", deps=[511]), issue(511, "V5-11", "b", deps=[510])]
        self.assertEqual(self.pick(items)["priority"], "idle")

    def test_malformed_comment_without_id_is_a_selection_error(self):
        with self.assertRaises(sel.SelectionError):
            sel._normalise_issue_comment({"created_at": iso(1), "body": "x"})

    # -- actual legacy shape through the CLI, twice ------------------------------
    def test_actual_legacy_shape_via_cli_is_advisory_and_stable_across_runs(self):
        issues = legacy_issues(claim_comments=[comment(11, 4, "CLAIM agent=a time=x", login="bigtcze")])
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "snap.json"
            path.write_text(json.dumps(snapshot(issues=issues, open_prs=[legacy_pr(470, 446)])))
            runs = [subprocess.run([sys.executable, str(SCRIPT), "--snapshot", str(path),
                                    "--now", NOW.isoformat()],
                                   check=True, capture_output=True, text=True).stdout for _ in range(2)]
        self.assertEqual(runs[0], runs[1])
        self.assertIn("DECISION priority=6 kind=review-owned-dependencies", runs[0])
        self.assertNotIn("kind=dependency-recovery", runs[0])


class RecoveryRanking(unittest.TestCase):
    """Recovery competes with ready work by the same rank; generic graphs; neutrality."""

    MERGED = [merged(1, 30, dependency=True)]

    def pick(self, items, open_prs=(), dependency_states=None, now=NOW, **overrides):
        snap = snapshot(issues=items, open_prs=list(open_prs), merged_prs=list(self.MERGED),
                        dependency_states=dependency_states or {})
        snap.update(overrides)
        return sel.select(snap, now)

    @staticmethod
    def pair(a, b, priority=0, claim=None, record=None, code_a=None, code_b=None):
        """Test-first `a` blocked, implementation `b` Blocked by `a` (structured record on `a`)."""
        record = record or f"BLOCKED agent=x time=t kind=owned-dependency needs=#{b} recovery=combined"
        comments = [comment(a * 10 + 1, 5, record)]
        if claim:
            comments.append(comment(a * 10 + 2, 4, f"CLAIM agent={claim[1]} time=t", login=claim[0]))
        return [with_comments(issue(a, code_a or f"W-{a}", "tests", status="status:blocked",
                                    deps=[], body_priority=priority), *comments),
                with_comments(issue(b, code_b or f"W-{b}", "impl", deps=[a], body_priority=priority))]

    def ready(self, number, priority, code=None):
        return with_comments(issue(number, code or f"W-{number}", "feature", deps=[],
                                   body_priority=priority))

    # -- the reported starvation, generalised ---------------------------------
    def test_recoverable_p0_blocker_beats_lower_priority_ready_work(self):
        for a, b, other in ((446, 447, 449), (12, 7, 3), (9001, 9000, 5)):
            with self.subTest(numbers=(a, b, other)):
                d = self.pick(self.pair(a, b, priority=0) + [self.ready(other, 2)])
                self.assertEqual((d["priority"], d["kind"]), (6, "review-owned-dependencies"))
                self.assertEqual(d["target"]["group"], sorted([a, b]))
                self.assertEqual(d["target"]["evidence"], "explicit")

    def test_recovery_never_outranks_higher_priority_ready_work(self):
        d = self.pick(self.pair(30, 31, priority=2) + [self.ready(40, 1)])
        self.assertEqual((d["kind"], d["target"]["number"]), ("claim-issue", 40))
        d = self.pick(self.pair(30, 31, priority=2) + [self.ready(40, 3)])
        self.assertEqual(d["kind"], "review-owned-dependencies")

    def test_equal_priority_uses_the_existing_code_and_number_tie_break(self):
        group = self.pair(60, 61, priority=1, code_a="V9-05", code_b="V9-06")
        d = self.pick(group + [self.ready(70, 1, code="V9-04")])
        self.assertEqual((d["kind"], d["target"]["number"]), ("claim-issue", 70))
        d = self.pick(group + [self.ready(70, 1, code="V9-07")])
        self.assertEqual(d["kind"], "review-owned-dependencies")

    def test_group_ranks_at_its_most_urgent_member(self):
        items = self.pair(80, 81, priority=3)
        items[1] = with_comments(issue(81, "W-81", "impl", deps=[80], body_priority=0))
        d = self.pick(items + [self.ready(90, 1)])
        self.assertEqual(d["kind"], "review-owned-dependencies")

    def test_recurrence_group_is_p0_by_the_existing_policy(self):
        items = self.pair(50, 51, priority=None, code_a="V5-02", code_b="V5-03")
        items[0]["title"] = "V5-02 Recurrence golden tests"
        d = self.pick(items + [self.ready(52, 1)])
        self.assertEqual(d["kind"], "review-owned-dependencies")

    def test_coordinator_member_does_not_lift_a_group(self):
        items = self.pair(20, 21, priority=0)
        items[0]["body"] += "\nRelease: v9.0.0\n"
        items[1]["body"] += "\nRelease: v9.0.0\n"
        d = self.pick(items + [self.ready(22, 5)])
        self.assertEqual((d["kind"], d["target"]["number"]), ("claim-issue", 22))

    # -- higher precedence is preserved --------------------------------------
    def test_red_main_security_active_and_resumed_work_still_come_first(self):
        group = self.pair(100, 101, priority=0)
        red = [{"id": 9, "status": "completed", "conclusion": "failure",
                "created_at": iso(1), "head_sha": "abc"}]
        self.assertEqual(self.pick(group, main_runs=red)["kind"], "red-main")
        alert = [{"number": 1, "security_vulnerability": {"first_patched_version": {"identifier": "1"}}}]
        self.assertEqual(self.pick(group, security_alerts=alert)["kind"], "security-alert")
        busy = self.ready(102, 4)
        self.assertEqual(self.pick(group + [busy], open_prs=[feature_pr(700, 102)])["kind"], "active-pr")
        busy["labels"] = [{"name": "status:in-progress"}, {"name": LABEL}]
        self.assertEqual(self.pick(group + [busy])["kind"], "resume-claimed-issue")

    def test_renovate_fairness_is_unchanged(self):
        group = self.pair(100, 101, priority=0)
        aged = renovate_pr(600, "chore(deps): update x to v2", 72)
        self.assertEqual(self.pick(group, open_prs=[aged])["kind"], "triage-aged-renovate")
        young = renovate_pr(601, "chore(deps): update y to v3", 2,
                            triage=("actionable", "chore(deps): update y to v3"))
        two_features = [merged(1, 30, dependency=True), merged(2, 20, refs=101), merged(3, 10, refs=101)]
        self.assertEqual(self.pick(group, open_prs=[young], merged_prs=two_features)["kind"],
                         "dependency-slot")
        self.assertEqual(self.pick(group, open_prs=[young])["kind"], "review-owned-dependencies")
        self.assertEqual(self.pick([], open_prs=[young])["kind"], "dependency-pr")

    # -- arbitrary graphs and fail-closed eligibility --------------------------
    def test_multi_issue_record_and_reorder_recovery(self):
        items = [with_comments(issue(5, "W-5", "a", status="status:blocked", deps=[]),
                               comment(1, 3, "BLOCKED kind=owned-dependency needs=#6,#7 recovery=reorder")),
                 with_comments(issue(6, "W-6", "b", deps=[5])),
                 with_comments(issue(7, "W-7", "c", deps=[6]))]
        d = self.pick(items)
        self.assertEqual(d["target"]["group"], [5, 6, 7])
        self.assertEqual(d["target"]["recovery"], "reorder")

    def test_self_dependency_and_long_cycles_with_random_numbers(self):
        import random
        rng = random.Random(7)
        for size in (1, 2, 4, 6):
            numbers = rng.sample(range(10, 5000), size)
            items = [with_comments(issue(n, f"W-{n}", "c", deps=[numbers[(i + 1) % size]]))
                     for i, n in enumerate(numbers)]
            with self.subTest(size=size, numbers=numbers):
                d = self.pick(items)
                self.assertEqual((d["kind"], d["target"]["evidence"]), ("review-owned-dependencies", "cycle"))
                self.assertEqual(d["target"]["group"], sorted(numbers))

    def test_unsafe_groups_fail_closed(self):
        ext = "BLOCKED kind=external recovery=none"
        cases = {
            "external member": [*self.pair(1, 2)[:1],
                                with_comments(issue(2, "W-2", "i", status="status:blocked", deps=[1]),
                                              comment(5, 2, ext))],
            "unknown record": self.pair(1, 2, record="BLOCKED agent=x time=t\nwaiting for someone"),
            "malformed needs": self.pair(1, 2, record="BLOCKED kind=owned-dependency needs=2 recovery=combined"),
            "blocked without a record": [with_comments(issue(1, "W-1", "t", status="status:blocked", deps=[2])),
                                         with_comments(issue(2, "W-2", "i", deps=[1]))],
            "open outside prerequisite": [*self.pair(1, 2)[:1],
                                          with_comments(issue(2, "W-2", "i", deps=[1, 300]))],
            "coupling to a non-owned Issue": [
                with_comments(issue(1, "W-1", "t", status="status:blocked", deps=[]),
                              comment(9, 3, "BLOCKED kind=owned-dependency needs=#300 recovery=combined")),
                with_comments(issue(2, "W-2", "i", deps=[1]))],
            "untrusted record": [with_comments(issue(1, "W-1", "t", status="status:blocked", deps=[]),
                                               comment(9, 3, "BLOCKED kind=owned-dependency needs=#2 recovery=combined",
                                                       assoc="CONTRIBUTOR")),
                                 with_comments(issue(2, "W-2", "i", deps=[1]))],
            "in-progress member": [*self.pair(1, 2)[:1],
                                   with_comments(issue(2, "W-2", "i", status="status:in-progress", deps=[1]))],
        }
        for name, items in cases.items():
            with self.subTest(case=name):
                d = self.pick(items, dependency_states={"300": "OPEN"}, open_prs=[feature_pr(9, 77)])
                self.assertNotEqual(d["kind"], "review-owned-dependencies")
                self.assertNotEqual(d["kind"], "claim-issue")  # nothing in the group becomes executable

    def test_foreign_claims_are_never_combined_without_handoff(self):
        items = self.pair(1, 2, claim=("alice", "runner-a"))
        items[1]["comments"].append(comment(99, 1, "CLAIM agent=runner-b time=t", login="bob"))
        d = self.pick(items)
        self.assertNotEqual(d["kind"], "review-owned-dependencies")
        self.assertTrue(any("HANDOFF" in note for note in d["notes"]))
        items[0]["comments"].append(comment(100, 0.5, "HANDOFF agent=runner-a to=runner-b time=t",
                                            login="alice"))
        d = self.pick(items)
        self.assertEqual((d["kind"], d["target"]["claim"]["agent"]), ("review-owned-dependencies", "runner-b"))

    def test_post_cutoff_legacy_prose_is_unknown_not_a_protocol(self):
        late = legacy_comment(10, LEGACY_446_BODY, created="2026-11-20T00:00:00Z")
        items = owned_graph(
            [issue(446, "V5-02", "Recurrence audit", status="status:blocked", deps=[444]),
             issue(447, "V5-03", "Fix recurrence engine", status="status:idle", deps=[446])],
            {446: [late, comment(11, 4, "CLAIM agent=a time=x")]})
        d = self.pick(items, open_prs=[legacy_pr(470, 446)])
        self.assertNotEqual(d["kind"], "review-owned-dependencies")

    def test_historical_446_447_now_competes_by_priority(self):
        claim = [comment(11, 4, "CLAIM agent=a time=x")]
        lower = issue(451, "V5-07", "Professional UI", deps=[444])
        d = self.pick(legacy_issues(claim_comments=claim) + [with_comments(lower)],
                      open_prs=[legacy_pr(464, 446)])
        self.assertEqual((d["kind"], d["target"]["group"], d["target"]["pr"]),
                         ("review-owned-dependencies", [446, 447], 464))
        explicit_p0 = issue(900, "V9-01", "Urgent", deps=[444], body_priority=0)
        explicit_p0["title"] = "V0-01 Urgent"
        d = self.pick(legacy_issues(claim_comments=claim) + [with_comments(explicit_p0)],
                      open_prs=[legacy_pr(464, 446)])
        self.assertEqual((d["kind"], d["target"]["number"]), ("claim-issue", 900))

    # -- identity neutrality --------------------------------------------------
    def test_selection_is_independent_of_runner_identity(self):
        runners = ["tendo-autonomous-orchestrator", "codex", "claude-code", "cursor-agent",
                   "future.runner:v2"]
        shapes = set()
        for runner in runners:
            items = self.pair(1, 2, priority=0, claim=("bot-" + runner[:5], runner))
            d = self.pick(items + [self.ready(3, 1)])
            self.assertEqual(d["target"]["claim"]["agent"], runner)
            target = {k: v for k, v in d["target"].items() if k not in ("claim", "fingerprint")}
            shapes.add(json.dumps([d["priority"], d["kind"], target], sort_keys=True))
            foreign = copy.deepcopy(items)
            foreign[0]["comments"].append(comment(50, 1, f"HANDOFF agent={runner} to=x time=t",
                                                  login="someone-else"))
            self.assertEqual(self.pick(foreign + [self.ready(3, 1)])["target"]["claim"]["agent"], runner)
        self.assertEqual(len(shapes), 1)

    # -- repeated unchanged advice -------------------------------------------
    def test_deferral_suppresses_only_unchanged_advice(self):
        items = self.pair(1, 2, priority=0) + [self.ready(3, 2)]
        first = self.pick(items)
        basis = first["target"]["fingerprint"]
        self.assertRegex(basis, r"^[0-9a-f]{16}$")
        self.assertEqual(self.pick(copy.deepcopy(items), now=NOW + timedelta(days=9))["target"]["fingerprint"],
                         basis)
        deferred = copy.deepcopy(items)
        deferred[0]["comments"].append(comment(70, 1, f"RECOVERY agent=any time=t decision=deferred basis={basis}"))
        d = self.pick(deferred)
        self.assertEqual((d["kind"], d["target"]["number"]), ("claim-issue", 3))
        self.assertTrue(any("deferred" in note for note in d["notes"]))
        # Unrelated discussion, time and main changes do not re-open it.
        noisy = copy.deepcopy(deferred)
        noisy[1]["comments"].append(comment(71, 0.5, "Unrelated note."))
        self.assertEqual(self.pick(noisy, now=NOW + timedelta(days=30), main_head_sha="zzz")["kind"],
                         "claim-issue")
        # A relevant change (new linked PR head) produces new advice.
        pr = feature_pr(800, 1)
        pr["headRefOid"] = "f00d"
        changed = self.pick(deferred, open_prs=[pr])
        self.assertEqual(changed["kind"], "review-owned-dependencies")
        self.assertNotEqual(changed["target"]["fingerprint"], basis)
        pr["headRefOid"] = "beef"
        self.assertNotEqual(self.pick(deferred, open_prs=[pr])["target"]["fingerprint"],
                            changed["target"]["fingerprint"])

    def test_malformed_deferral_and_recovery_comment_never_block(self):
        items = self.pair(1, 2, priority=0)
        items[0]["comments"].append(comment(70, 1, "RECOVERY agent=a decision=deferred basis=nothex"))
        self.assertEqual(self.pick(items)["kind"], "review-owned-dependencies")

    # -- review findings: outside prerequisites, protected members, edits ----
    def test_open_owned_prerequisite_outside_the_group_fails_closed(self):
        items = self.pair(1, 2, priority=0)
        items[1] = with_comments(issue(2, "W-2", "impl", deps=[1, 3], body_priority=0))
        for extra in (self.ready(3, 2),
                      with_comments(issue(3, "W-3", "x", status="status:in-progress", deps=[]))):
            with self.subTest(prerequisite_status=extra["labels"]):
                d = self.pick(items + [extra])
                self.assertNotEqual(d["kind"], "review-owned-dependencies")
                self.assertNotIn(d["target"].get("number"), (1, 2))
        # Recorded (not declared) coupling to an open owned Issue outside the SCC.
        rec = self.pair(1, 2, priority=0,
                        record="BLOCKED agent=x time=t kind=owned-dependency needs=#2 recovery=combined")
        rec[0]["comments"].append(comment(19, 0.5, "BLOCKED agent=x time=t kind=owned-dependency "
                                                   "needs=#2,#3 recovery=combined"))
        d = self.pick(rec + [with_comments(issue(3, "W-3", "x", status="status:in-progress", deps=[]))])
        self.assertNotEqual(d["kind"], "review-owned-dependencies")
        # Once the owned prerequisite is closed the group is advised again.
        closed3 = issue(3, "W-3", "x", status=None, state="CLOSED", deps=[])
        d = self.pick(items + [closed3])
        self.assertEqual((d["kind"], d["target"]["group"]), ("review-owned-dependencies", [1, 2]))

    def coupled_only(self, record, claim_b=None):
        """#1 blocked with a coupling record; #2 linked only by that record."""
        one = with_comments(issue(1, "W-1", "tests", status="status:blocked", deps=[], body_priority=0),
                            comment(11, 5, record),
                            comment(12, 4, "CLAIM agent=runner-a time=t", login="alice"))
        two = with_comments(issue(2, "W-2", "impl", deps=[], body_priority=0),
                            *([comment(21, 3, f"CLAIM agent={claim_b[1]} time=t", login=claim_b[0])]
                              if claim_b else []))
        return [one, two, self.ready(3, 2)]

    def test_members_of_a_recorded_group_are_never_offered_as_claims(self):
        combined = "BLOCKED agent=x time=t kind=owned-dependency needs=#2 recovery=combined"
        advised = self.pick(self.coupled_only(combined))
        self.assertEqual((advised["kind"], advised["target"]["group"]),
                         ("review-owned-dependencies", [1, 2]))
        deferred = self.coupled_only(combined)
        deferred[0]["comments"].append(comment(
            70, 1, f"RECOVERY agent=any time=t decision=deferred basis={advised['target']['fingerprint']}"))
        cases = {
            "deferred": (deferred, {}),
            "open non-owned prerequisite": (self.coupled_only(
                "BLOCKED agent=x time=t kind=owned-dependency needs=#2,#300 recovery=combined"),
                {"300": "OPEN"}),
            "contested by another claimant": (self.coupled_only(combined, claim_b=("bob", "runner-b")), {}),
            "reorder record": (self.coupled_only(
                "BLOCKED agent=x time=t kind=owned-dependency needs=#2,#300 recovery=reorder"),
                {"300": "OPEN"}),
        }
        for name, (items, states) in cases.items():
            with self.subTest(case=name):
                d = self.pick(items, dependency_states=states)
                self.assertNotEqual(d["kind"], "review-owned-dependencies")
                self.assertEqual((d["kind"], d["target"]["number"]), ("claim-issue", 3))
                self.assertTrue(any("stay unclaimable" in n or "deferred" in n or "HANDOFF" in n
                                    for n in d["notes"]))

    def test_edited_blocker_record_changes_the_fingerprint(self):
        items = self.pair(1, 2, priority=0) + [self.ready(3, 2)]
        basis = self.pick(items)["target"]["fingerprint"]
        items[0]["comments"].append(comment(70, 1, f"RECOVERY agent=any time=t decision=deferred basis={basis}"))
        self.assertEqual(self.pick(items)["kind"], "claim-issue")
        edited = copy.deepcopy(items)
        record = edited[0]["comments"][0]
        record["body"] = record["body"].replace("recovery=combined", "recovery=reorder")
        record["updatedAt"] = iso(0.1)  # same comment id, edited in place
        d = self.pick(edited)
        self.assertEqual((d["kind"], d["target"]["recovery"]), ("review-owned-dependencies", "reorder"))
        self.assertNotEqual(d["target"]["fingerprint"], basis)
        touched = copy.deepcopy(items)
        touched[0]["comments"][0]["updatedAt"] = iso(0.1)  # any edit is new evidence
        self.assertEqual(self.pick(touched)["kind"], "review-owned-dependencies")

    def test_deferral_never_applies_when_a_linked_pr_head_is_unknown(self):
        items = self.pair(1, 2, priority=0) + [self.ready(3, 2)]
        pr = feature_pr(800, 1)  # no headRefOid in the snapshot
        basis = self.pick(items, open_prs=[pr])["target"]["fingerprint"]
        items[0]["comments"].append(comment(70, 1, f"RECOVERY agent=any time=t decision=deferred basis={basis}"))
        self.assertEqual(self.pick(items, open_prs=[pr])["kind"], "review-owned-dependencies")
        pr["headRefOid"] = "f00d"
        basis = self.pick(items, open_prs=[pr])["target"]["fingerprint"]
        items[0]["comments"].append(comment(71, 0.5, f"RECOVERY agent=any time=t decision=deferred basis={basis}"))
        self.assertEqual(self.pick(items, open_prs=[pr])["kind"], "claim-issue")


class CommentCollection(unittest.TestCase):
    def test_open_owned_issue_comments_are_paginated_and_fail_closed(self):
        calls = []
        pages = {1: [{"id": 1, "created_at": iso(3), "updated_at": iso(3), "user": {"login": "bigtcze"},
                      "author_association": "OWNER", "body": "CLAIM agent=a time=x"}],
                 2: [{"id": 2, "created_at": iso(2), "updated_at": iso(2), "user": {"login": "bigtcze"},
                      "author_association": "MEMBER", "body": "RELEASE agent=a time=y"}]}

        def run_gh(args):
            calls.append(list(args))
            joined = " ".join(args)
            if args[:2] == ["api", "rate_limit"]:
                return json.dumps({"resources": {"core": {"remaining": 5000, "reset": 1},
                                                 "graphql": {"remaining": 5000, "reset": 1}}})
            if "workflows/required.yml/runs" in joined:
                return json.dumps({"workflow_runs": []})
            if "commits/main" in joined:
                return json.dumps({"sha": "abc"})
            if "dependabot/alerts" in joined:
                return json.dumps([])
            if args[:2] == ["pr", "list"]:
                return json.dumps([])
            if args[:2] == ["issue", "list"]:
                return json.dumps([issue(910, "V6-01", "Open", deps=())])
            if "/issues/910/comments" in joined:
                # `--paginate --slurp` returns every page as one list of pages.
                return json.dumps([pages[1], pages[2]])
            if args[0] == "api" and "--paginate" in args:
                return json.dumps([[]])
            if "pulls?state=closed" in joined:
                return json.dumps([])
            return "null"

        snap = sel.collect("o/r", run_gh, NOW)
        comments = snap["issues"][0]["comments"]
        self.assertEqual([c["id"] for c in comments], [1, 2])
        self.assertEqual([c["authorAssociation"] for c in comments], ["OWNER", "MEMBER"])

    def test_comment_without_timestamp_is_a_selection_error(self):
        with self.assertRaises(sel.SelectionError):
            sel._normalise_issue_comment({"id": 3, "body": "CLAIM agent=a"})


class CommandLine(unittest.TestCase):
    def test_self_check_exits_zero(self):
        result = subprocess.run([sys.executable, str(SCRIPT), "--self-check"],
                                capture_output=True, text=True, timeout=30)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("SELF-CHECK OK", result.stdout)

    def test_snapshot_cli_prints_the_documented_first_line(self):
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "snap.json"
            path.write_text(json.dumps(snapshot()))
            result = subprocess.run([sys.executable, str(SCRIPT), "--snapshot", str(path),
                                     "--now", NOW.isoformat()],
                                    check=True, capture_output=True, text=True)
        first = result.stdout.splitlines()[0]
        self.assertEqual(first,
                         "DECISION priority=6 kind=claim-issue target=issue #446 "
                         "V5-02 Recurrence root-cause audit")
        self.assertRegex(result.stdout.splitlines()[1], r"^REASON ")
        self.assertRegex(result.stdout.splitlines()[-1], r"^JSON \{")


if __name__ == "__main__":
    unittest.main(verbosity=1)
