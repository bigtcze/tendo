#!/usr/bin/env python3
"""Selection-policy tests for scripts/v5_work_selection.py (locked V5_ISSUES_WORKFLOW).

Run: python3 scripts/test_v5_work_selection.py (part of scripts/autonomous-loop-test.sh).
"""
from __future__ import annotations

from datetime import datetime, timedelta, timezone
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

HERE = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("v5_work_selection", HERE / "v5_work_selection.py")
sel = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(sel)

NOW = datetime(2026, 11, 20, 12, 0, tzinfo=timezone.utc)
MILESTONE = "Tendo v5.0.0 - Refined & Reliable"
LABEL = "tendo:v5-autonomous"


def iso(delta_hours: float) -> str:
    return (NOW - timedelta(hours=delta_hours)).strftime("%Y-%m-%dT%H:%M:%SZ")


def issue(number, code, title, status="status:idle", deps=(), state="OPEN", owned=True):
    body = f"# {code}\n\n## Dependencies\nBlocked by: {', '.join(f'#{d}' for d in deps) or 'none'}\n"
    labels = [{"name": status}] if status else []
    if owned:
        labels.append({"name": LABEL})
    return {"number": number, "title": f"{code} {title}", "state": state, "body": body,
            "labels": labels, "milestone": {"title": MILESTONE if owned else "Tendo v4.0.0 - Modern Everyday"}}


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


def feature_pr(number, refs, age_hours=1):
    return {"number": number, "title": f"feat: work {number}", "author": {"login": "bigtcze"},
            "headRefName": f"feat/v5-{number}", "labels": [], "createdAt": iso(age_hours),
            "isDraft": False, "body": f"Refs #{refs}", "comments": []}


def merged(number, hours_ago, dependency=False, refs=446):
    """Merged PR; a non-dependency PR is a V5 feature PR when it `Refs` an owned Issue."""
    return {"number": number, "title": f"merged {number}", "mergedAt": iso(hours_ago),
            "author": {"login": "app/renovate" if dependency else "bigtcze"},
            "headRefName": f"renovate/x-{number}" if dependency else f"feat/x-{number}",
            "labels": [{"name": "dependencies"}, {"name": "renovate"}] if dependency else [],
            "body": "" if dependency or refs is None else f"Refs #{refs}"}


def backlog():
    """Perpetual V5 feature backlog: V5-00 closed, everything else open."""
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
            "security_alerts": [], "open_prs": [], "merged_prs": [merged(1, 30, dependency=True)],
            "issues": backlog()}
    base.update(overrides)
    return base


class SelectionPolicy(unittest.TestCase):
    def pick(self, snap, now=NOW):
        return sel.select(snap, now)

    def test_feature_backlog_never_starves_an_aged_untriaged_renovate_pr(self):
        d = self.pick(snapshot(open_prs=[renovate_pr(404, "update vite to v8.3.6", 49)]))
        self.assertEqual((d["priority"], d["kind"], d["target"]["number"]), ("d", "triage-aged-renovate", 404))

    def test_young_renovate_pr_waits_for_feature_work(self):
        d = self.pick(snapshot(open_prs=[renovate_pr(404, "update vite to v8.3.6", 47)]))
        self.assertEqual((d["priority"], d["target"]["number"]), ("f", 446))

    def test_triaged_actionable_pr_gets_the_slot_after_two_merged_features(self):
        prs = [renovate_pr(404, "update vite to v8.3.6", 100, triage=("actionable", "update vite to v8.3.6"))]
        one = self.pick(snapshot(open_prs=prs, merged_prs=[merged(1, 30, True), merged(2, 20)]))
        self.assertEqual(one["priority"], "f")
        self.assertEqual(one["context"]["features_merged_since_last_dependency_pr"], 1)
        two = self.pick(snapshot(open_prs=prs, merged_prs=[merged(1, 30, True), merged(2, 20), merged(3, 10)]))
        self.assertEqual((two["priority"], two["kind"], two["target"]["number"]), ("e", "dependency-slot", 404))

    def test_fairness_count_resets_at_the_latest_merged_dependency_pr(self):
        history = [merged(2, 40), merged(3, 35), merged(4, 20, True), merged(5, 10)]
        prs = [renovate_pr(404, "v", 100, triage=("actionable", "v"))]
        d = self.pick(snapshot(open_prs=prs, merged_prs=history))
        self.assertEqual(d["priority"], "f")
        self.assertEqual(d["context"]["features_merged_since_last_dependency_pr"], 1)

    def test_fairness_is_derived_from_history_and_survives_restart(self):
        # A "restart" is a fresh evaluation of the same GitHub state: the
        # decision is identical, order-independent and leaves no file behind.
        prs = [renovate_pr(405, "update go-jose to v4.1.9", 100, triage=("actionable", "update go-jose to v4.1.9"))]
        history = [merged(1, 30, True), merged(2, 20), merged(3, 10)]
        first = self.pick(snapshot(open_prs=prs, merged_prs=history))
        second = self.pick(snapshot(open_prs=list(prs), merged_prs=list(reversed(history))))
        self.assertEqual(first, second)
        self.assertEqual(first["priority"], "e")
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "snap.json"
            path.write_text(json.dumps(snapshot(open_prs=prs, merged_prs=history)))
            before = sorted(os.listdir(tmp))
            outputs = [subprocess.run([sys.executable, str(HERE / "v5_work_selection.py"), "--snapshot",
                                       str(path), "--now", NOW.isoformat()], cwd=tmp, check=True,
                                      capture_output=True, text=True).stdout for _ in range(2)]
            self.assertEqual(outputs[0], outputs[1])
            self.assertIn("priority=e kind=dependency-slot", outputs[0])
            self.assertEqual(sorted(os.listdir(tmp)), before)

    def test_fairness_orders_history_by_merge_time_not_list_or_number_order(self):
        # #9 was created (numbered) last but merged first; #2/#3 merged after the
        # dependency PR. gh lists merged PRs newest-created first, so the
        # policy must order by mergedAt itself.
        history = [merged(9, 30, True), merged(3, 10), merged(2, 20)]
        prs = [renovate_pr(404, "v", 100, triage=("actionable", "v"))]
        d = self.pick(snapshot(open_prs=prs, merged_prs=history))
        self.assertEqual((d["priority"], d["context"]["features_merged_since_last_dependency_pr"]), ("e", 2))
        late_dependency = [merged(2, 20), merged(3, 10), merged(1, 5, True)]
        d = self.pick(snapshot(open_prs=prs, merged_prs=late_dependency))
        self.assertEqual((d["priority"], d["context"]["features_merged_since_last_dependency_pr"]), ("f", 0))

    def test_only_owned_v5_work_prs_count_as_feature_prs(self):
        # Governance/docs/release merges without `Refs` to an owned Issue, and
        # PRs referencing unowned Issues, neither count nor reset the slot.
        history = [merged(1, 30, True), merged(2, 25, refs=None), merged(3, 20, refs=300),
                   merged(4, 15), merged(5, 10, refs=None)]
        prs = [renovate_pr(404, "v", 100, triage=("actionable", "v"))]
        d = self.pick(snapshot(open_prs=prs, merged_prs=history))
        self.assertEqual((d["priority"], d["context"]["features_merged_since_last_dependency_pr"]), ("f", 1))
        d = self.pick(snapshot(open_prs=prs, merged_prs=history + [merged(6, 5, refs=452)]))
        self.assertEqual((d["priority"], d["target"]["number"]), ("e", 404))

    def test_merging_the_dependency_pr_returns_to_feature_work(self):
        prs = [renovate_pr(406, "update playwright", 100, triage=("actionable", "update playwright"))]
        history = [merged(2, 20), merged(3, 10), merged(4, 1, True)]
        d = self.pick(snapshot(open_prs=prs, merged_prs=history))
        self.assertEqual(d["priority"], "f")

    def test_active_owned_pr_comes_before_aged_renovate_and_feature_claims(self):
        issues = backlog()
        issues[2] = issue(446, "V5-02", "Recurrence root-cause audit", status="status:in-progress", deps=[444])
        prs = [renovate_pr(404, "v", 100), feature_pr(470, 446)]
        d = self.pick(snapshot(open_prs=prs, issues=issues))
        self.assertEqual((d["priority"], d["kind"], d["target"]["number"]), ("c", "active-pr", 470))

    def test_in_progress_issue_without_pr_is_resumed_before_new_claims(self):
        issues = backlog()
        issues[5] = issue(452, "V5-08", "Password change", status="status:in-progress", deps=[444])
        d = self.pick(snapshot(issues=issues))
        self.assertEqual((d["priority"], d["kind"], d["target"]["number"]), ("c", "resume-claimed-issue", 452))

    def test_renovate_pr_with_agent_repair_commits_is_active_work(self):
        prs = [renovate_pr(406, "v", 10, commit_authors=("renovate[bot]", "bigtcze"))]
        d = self.pick(snapshot(open_prs=prs))
        self.assertEqual((d["priority"], d["kind"], d["target"]["number"]), ("c", "active-pr", 406))

    def test_renovate_pr_with_unknown_commit_author_is_not_active_work(self):
        # A commit without a linked GitHub login (empty) is not proof of an agent repair.
        prs = [renovate_pr(406, "v", 10, commit_authors=("renovate[bot]", ""))]
        d = self.pick(snapshot(open_prs=prs))
        self.assertEqual(d["priority"], "f")

    def test_pr_for_a_blocked_owned_issue_does_not_pin_priority_c(self):
        issues = backlog()
        issues[2] = issue(446, "V5-02", "Recurrence root-cause audit", status="status:blocked", deps=[444])
        d = self.pick(snapshot(open_prs=[feature_pr(470, 446)], issues=issues))
        self.assertEqual((d["priority"], d["target"]["number"]), ("f", 445))
        self.assertTrue(any("#470 references only status:blocked" in n for n in d["notes"]))
        # The same PR becomes active again once the Issue is unblocked.
        issues[2] = issue(446, "V5-02", "Recurrence root-cause audit", status="status:in-progress", deps=[444])
        d = self.pick(snapshot(open_prs=[feature_pr(470, 446)], issues=issues))
        self.assertEqual((d["priority"], d["target"]["number"]), ("c", 470))

    def test_security_supersedes_active_pr_and_renovate(self):
        prs = [renovate_pr(404, "v", 100), feature_pr(470, 446),
               renovate_pr(480, "fix CVE", 1, labels=("dependencies", "renovate", "security"))]
        d = self.pick(snapshot(open_prs=prs))
        self.assertEqual((d["priority"], d["kind"], d["target"]["number"]), ("b", "security-pr", 480))
        alert = {"number": 7, "security_vulnerability": {"first_patched_version": {"identifier": "1.2.3"}}}
        d = self.pick(snapshot(open_prs=[feature_pr(470, 446)], security_alerts=[alert]))
        self.assertEqual((d["priority"], d["kind"]), ("b", "security-alert"))

    def test_unfixable_alert_is_noted_not_selected(self):
        alert = {"number": 8, "security_vulnerability": {"first_patched_version": None}}
        d = self.pick(snapshot(security_alerts=[alert]))
        self.assertEqual(d["priority"], "f")
        self.assertTrue(any("without a patched version" in n for n in d["notes"]))

    def test_red_main_supersedes_everything(self):
        runs = [{"id": 9, "status": "completed", "conclusion": "failure", "created_at": iso(1), "head_sha": "d"},
                {"id": 8, "status": "completed", "conclusion": "success", "created_at": iso(3), "head_sha": "c"}]
        prs = [renovate_pr(480, "s", 1, labels=("security", "renovate")), feature_pr(470, 446)]
        d = self.pick(snapshot(main_runs=runs, open_prs=prs))
        self.assertEqual((d["priority"], d["kind"], d["target"]["run"]), ("a", "red-main", 9))

    def test_in_progress_main_run_uses_latest_completed_run(self):
        runs = [{"id": 10, "status": "in_progress", "conclusion": None, "created_at": iso(0.1)},
                {"id": 9, "status": "completed", "conclusion": "success", "created_at": iso(1)}]
        self.assertEqual(self.pick(snapshot(main_runs=runs))["priority"], "f")

    def test_blocked_unchanged_renovate_pr_is_skipped(self):
        prs = [renovate_pr(404, "update vite to v8.3.6", 200, triage=("blocked", "update vite to v8.3.6"))]
        history = [merged(1, 30, True), merged(2, 20), merged(3, 10)]
        d = self.pick(snapshot(open_prs=prs, merged_prs=history))
        self.assertEqual(d["priority"], "f")
        self.assertEqual(d["context"]["blocked_unchanged_skipped"], [404])

    def test_stale_blocked_triage_expires_for_a_recheck(self):
        days = sel.BLOCKED_RECHECK_DAYS
        fresh = renovate_pr(404, "v", 24 * 30, triage=("blocked", "v"), triage_age_hours=24 * days - 1)
        self.assertEqual(self.pick(snapshot(open_prs=[fresh]))["priority"], "f")
        stale = renovate_pr(404, "v", 24 * 30, triage=("blocked", "v"), triage_age_hours=24 * days + 1)
        d = self.pick(snapshot(open_prs=[stale]))
        self.assertEqual((d["priority"], d["kind"], d["target"]["number"]), ("d", "triage-aged-renovate", 404))
        # A repaired PR with an expired blocker is re-triaged first, not resumed.
        repaired = renovate_pr(404, "v", 24 * 30, triage=("blocked", "v"), triage_age_hours=24 * days + 1,
                               commit_authors=("renovate[bot]", "bigtcze"))
        d = self.pick(snapshot(open_prs=[repaired]))
        self.assertEqual((d["priority"], d["kind"], d["target"]["number"]), ("d", "triage-aged-renovate", 404))
        repaired["comments"].append({"author": {"login": "bigtcze"}, "createdAt": iso(0.5),
                                     "body": "TRIAGE agent=a time=x decision=actionable target=v"})
        d = self.pick(snapshot(open_prs=[repaired]))
        self.assertEqual((d["priority"], d["kind"], d["target"]["number"]), ("c", "active-pr", 404))
        # An actionable decision does not expire: it is processed in the slot.
        old_ok = renovate_pr(404, "v", 24 * 30, triage=("actionable", "v"), triage_age_hours=24 * days * 4)
        self.assertEqual(self.pick(snapshot(open_prs=[old_ok]))["priority"], "f")

    def test_blocked_pr_with_new_version_needs_triage_again(self):
        prs = [renovate_pr(404, "update vite to v8.4.0", 200, triage=("blocked", "update vite to v8.3.6"))]
        d = self.pick(snapshot(open_prs=prs))
        self.assertEqual((d["priority"], d["target"]["number"]), ("d", 404))

    def test_triage_marker_from_renovate_itself_does_not_count(self):
        pr = renovate_pr(404, "v", 100)
        pr["comments"] = [{"author": {"login": "renovate[bot]"}, "createdAt": iso(1),
                           "body": "TRIAGE agent=x time=y decision=blocked target=v"}]
        self.assertEqual(self.pick(snapshot(open_prs=[pr]))["priority"], "d")

    def test_oldest_aged_renovate_pr_first(self):
        prs = [renovate_pr(406, "c", 60), renovate_pr(404, "a", 80), renovate_pr(405, "b", 70)]
        self.assertEqual(self.pick(snapshot(open_prs=prs))["target"]["number"], 404)

    def test_remaining_dependency_prs_are_processed_when_no_issue_is_ready(self):
        issues = [issue(444, "V5-00", "Gov", status=None, state="CLOSED"),
                  issue(447, "V5-03", "Fix recurrence", deps=[446]),
                  issue(446, "V5-02", "Audit", status="status:blocked", deps=[444])]
        prs = [renovate_pr(404, "v", 10)]
        d = self.pick(snapshot(open_prs=prs, issues=issues))
        self.assertEqual((d["priority"], d["kind"], d["target"]["number"]), ("g", "dependency-pr", 404))

    def test_idle_when_nothing_actionable(self):
        issues = [issue(444, "V5-00", "Gov", status=None, state="CLOSED")]
        d = self.pick(snapshot(issues=issues))
        self.assertEqual((d["priority"], d["kind"], d["target"]), ("idle", "none", None))

    def test_recurrence_issues_rank_first_then_issue_code(self):
        d = self.pick(snapshot())
        self.assertEqual(d["target"]["number"], 446)
        issues = [i for i in backlog() if i["number"] not in (446, 447)]
        self.assertEqual(self.pick(snapshot(issues=issues))["target"]["number"], 445)

    def test_dependencies_must_be_closed_owned_issues(self):
        issues = [issue(444, "V5-00", "Gov", status=None, state="CLOSED"),
                  issue(447, "V5-03", "Fix recurrence", deps=[446]),
                  issue(446, "V5-02", "Audit", status="status:blocked", deps=[444]),
                  issue(453, "V5-09", "Households", deps=[300])]
        d = self.pick(snapshot(issues=issues))
        self.assertEqual(d["priority"], "idle")
        self.assertTrue(any("#453 depends on unowned" in n for n in d["notes"]))

    def test_external_and_lookalike_issues_are_never_selected(self):
        lookalike = issue(500, "V5-02", "Recurrence look-alike", owned=False)
        label_only = issue(501, "V5-04", "Recurrence label only")
        label_only["milestone"] = None
        issues = [issue(444, "V5-00", "Gov", status=None, state="CLOSED"), lookalike, label_only]
        d = self.pick(snapshot(issues=issues))
        self.assertEqual(d["priority"], "idle")
        self.assertEqual(d["context"]["ignored_unowned_issues"], [500, 501])

    def test_pr_referencing_unowned_issue_is_not_active_work(self):
        prs = [feature_pr(471, 300)]
        d = self.pick(snapshot(open_prs=prs))
        self.assertEqual(d["priority"], "f")
        self.assertTrue(any("#471 references no open owned" in n for n in d["notes"]))

    def test_ambiguous_status_labels_are_not_claimed(self):
        issues = backlog()
        issues[2]["labels"].append({"name": "status:blocked"})
        d = self.pick(snapshot(issues=issues))
        self.assertNotEqual(d["target"]["number"], 446)
        self.assertTrue(any("#446 has status labels" in n for n in d["notes"]))

    def test_unreadable_security_alerts_are_reported(self):
        d = self.pick(snapshot(security_alerts=None))
        self.assertTrue(any("security alerts unreadable" in n for n in d["notes"]))


class Collection(unittest.TestCase):
    """collect() must discover ALL open PRs and full merged history, never a truncated window."""

    def setUp(self):
        self.calls = []
        self.original = sel.gh_json

    def tearDown(self):
        sel.gh_json = self.original

    def fake(self, open_count=3, commit_count=2):
        def gh_json(*args):
            self.calls.append(args)
            if args[:2] == ("pr", "list") and "open" in args:
                return [renovate_pr(400 + i, "v", 1) for i in range(open_count)]
            if args[:2] in (("pr", "list"), ("issue", "list")):
                return []
            if args[0] == "api" and "--paginate" in args:
                path = args[-1]
                if path.endswith("/commits?per_page=100"):
                    if commit_count != 2:
                        return [[{"author": {"login": "renovate[bot]"}}] * commit_count]
                    return [[{"author": {"login": "renovate[bot]"}}], [{"author": None}]]
                if "/comments" in path:
                    return [[{"user": {"login": "bigtcze"}, "created_at": iso(1), "body": "c1"}],
                            [{"user": {"login": "bigtcze"}, "created_at": iso(0.5), "body": "c2"}]]
                return [[]]
            return {"workflow_runs": []}
        sel.gh_json = gh_json

    def test_lists_use_a_high_limit_and_comments_and_commits_are_paginated(self):
        self.fake()
        snap = sel.collect("o/r")
        lists = [c for c in self.calls if c[1:2] == ("list",)]
        self.assertEqual(len(lists), 3)
        for call in lists:
            self.assertEqual(call[call.index("--limit") + 1], str(sel.FETCH_LIMIT))
        merged_call = next(c for c in lists if "merged" in c)
        self.assertIn("mergedAt", merged_call[merged_call.index("--json") + 1])
        self.assertEqual(len(snap["open_prs"]), 3)
        pr = snap["open_prs"][0]
        self.assertEqual([c["body"] for c in pr["comments"]], ["c1", "c2"])
        self.assertEqual(pr["commitAuthors"], ["", "renovate[bot]"])

    def test_pr_commits_at_the_api_cap_are_an_error_not_a_truncation(self):
        self.fake(commit_count=sel.PR_COMMITS_API_CAP)
        with self.assertRaises(RuntimeError):
            sel.collect("o/r")

    def test_a_list_at_the_fetch_limit_is_an_error_not_a_truncation(self):
        self.fake(open_count=sel.FETCH_LIMIT)
        with self.assertRaises(RuntimeError):
            sel.collect("o/r")


if __name__ == "__main__":
    unittest.main(verbosity=1)
