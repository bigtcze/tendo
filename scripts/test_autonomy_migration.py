#!/usr/bin/env python3
"""Offline unit tests for scripts/autonomy_migration.py.

No network, no gh, no git: every call goes through an in-memory fake GitHub
store (`FakeGitHub`) behind the injectable runner (`FakeGh`). The same store
also powers the `gh` shim used by scripts/install-autonomy-service-test.sh:

    python3 scripts/test_autonomy_migration.py --gh-shim <gh api args...>

The shim reads/writes the JSON store named by $FAKE_GH_STORE and appends every
call to $FAKE_GH_LOG. Failure injection: FAKE_GH_FAIL="METHOD:path-substring;..."
"""
from __future__ import annotations

import copy
import io
import json
import os
import re
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout
from urllib.parse import parse_qs, quote, unquote, urlparse

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import autonomy_migration as migration
from autonomy_migration import (
    GhError,
    SOURCE_LABEL,
    SOURCE_MILESTONE,
    STATUS_LABELS,
    TARGET_LABEL,
)

REPO = "bigtcze/tendo"
NUMBERS = list(range(445, 462))


# ---------------------------------------------------------------------------
# Fake GitHub REST store
# ---------------------------------------------------------------------------
class FakeGitHub:
    """In-memory emulation of the REST endpoints the migration uses."""

    def __init__(self, issues=None, labels=None, check_runs=None, page_size=100,
                 drop_writes=False, create_label_race=False):
        self.issues = {int(number): copy.deepcopy(issue) for number, issue in (issues or {}).items()}
        self.labels = {label["name"]: copy.deepcopy(label) for label in (labels or [])}
        self.check_runs = copy.deepcopy(check_runs or {})
        self.page_size = page_size
        self.drop_writes = drop_writes
        self.create_label_race = create_label_race

    # -- persistence (used by the CLI shim) --------------------------------
    def to_dict(self) -> dict:
        return {
            "issues": {str(number): issue for number, issue in self.issues.items()},
            "labels": list(self.labels.values()),
            "check_runs": self.check_runs,
            "page_size": self.page_size,
            "drop_writes": self.drop_writes,
            "create_label_race": self.create_label_race,
        }

    @classmethod
    def from_dict(cls, data: dict) -> "FakeGitHub":
        return cls(
            issues=data.get("issues") or {},
            labels=data.get("labels") or [],
            check_runs=data.get("check_runs") or {},
            page_size=int(data.get("page_size") or 100),
            drop_writes=bool(data.get("drop_writes")),
            create_label_race=bool(data.get("create_label_race")),
        )

    @classmethod
    def from_file(cls, path: str) -> "FakeGitHub":
        with open(path, encoding="utf-8") as handle:
            return cls.from_dict(json.load(handle))

    def save(self, path: str) -> None:
        tmp = path + ".tmp"
        with open(tmp, "w", encoding="utf-8") as handle:
            json.dump(self.to_dict(), handle, indent=1, sort_keys=True)
            handle.write("\n")
        os.replace(tmp, path)

    # -- query helpers -----------------------------------------------------
    @property
    def page_limit(self) -> int:
        return min(100, self.page_size)

    def _page(self, items: list, query: dict) -> list:
        per_page = int((query.get("per_page") or ["30"])[0] or 30)
        page = int((query.get("page") or ["1"])[0] or 1)
        size = min(per_page, self.page_size)
        start = (page - 1) * size
        return items[start:start + size]

    def _label_objects(self, names) -> list:
        return [{"name": name} for name in names]

    def _issue_copy(self, number: int) -> dict:
        return copy.deepcopy(self.issues[number])

    # -- routing -----------------------------------------------------------
    def request(self, method: str, path: str, fields: dict | None = None):
        method = method.upper()
        parsed = urlparse(path)
        clean = parsed.path.lstrip("/")
        query = parse_qs(parsed.query)
        fields = dict(fields or {})
        repo_prefix = f"repos/{REPO}/"
        if not clean.startswith(repo_prefix):
            return 404, {"message": "Not Found", "path": clean}
        rest = clean[len(repo_prefix):]

        if rest == "labels":
            if method == "GET":
                return 200, copy.deepcopy(self._page(list(self.labels.values()), query))
            if method == "POST":
                name = fields.get("name", "")
                if name in self.labels:
                    return 422, {
                        "message": "Validation Failed",
                        "errors": [{"code": "already_exists", "field": "name"}],
                    }
                if self.create_label_race:
                    # A concurrent creator won the race: the label exists but
                    # this POST still reports already_exists.
                    self.labels[name] = {"name": name, "color": fields.get("color", ""),
                                         "description": fields.get("description", "")}
                    return 422, {
                        "message": "Validation Failed",
                        "errors": [{"code": "already_exists", "field": "name"}],
                    }
                label = {"name": name, "color": fields.get("color", ""),
                         "description": fields.get("description", "")}
                self.labels[name] = label
                return 201, copy.deepcopy(label)
            return 405, {"message": "Method Not Allowed"}

        if rest == "issues":
            if method != "GET":
                return 405, {"message": "Method Not Allowed"}
            state = (query.get("state") or ["open"])[0]
            wanted = [name for value in query.get("labels", []) for name in value.split(",") if name]
            items = []
            for number in sorted(self.issues):
                issue = self.issues[number]
                if state == "open" and str(issue.get("state", "")).lower() != "open":
                    continue
                if state == "closed" and str(issue.get("state", "")).lower() != "closed":
                    continue
                names = {label["name"] for label in issue.get("labels", [])}
                if not all(label in names for label in wanted):
                    continue
                items.append(copy.deepcopy(issue))
            return 200, self._page(items, query)

        match = re.fullmatch(r"issues/(\d+)", rest)
        if match:
            number = int(match.group(1))
            if number not in self.issues:
                return 404, {"message": "Not Found"}
            return 200, self._issue_copy(number)

        match = re.fullmatch(r"issues/(\d+)/labels", rest)
        if match:
            number = int(match.group(1))
            if number not in self.issues:
                return 404, {"message": "Not Found"}
            issue = self.issues[number]
            if method == "POST":
                if not self.drop_writes:
                    existing = {label["name"] for label in issue.setdefault("labels", [])}
                    for name in fields.get("labels", []):
                        if name not in existing:
                            issue["labels"].append({"name": name})
                return 200, copy.deepcopy(issue.get("labels", []))
            if method == "GET":
                return 200, copy.deepcopy(issue.get("labels", []))
            return 405, {"message": "Method Not Allowed"}

        match = re.fullmatch(r"issues/(\d+)/labels/(.+)", rest)
        if match:
            number = int(match.group(1))
            label_name = unquote(match.group(2))
            if number not in self.issues:
                return 404, {"message": "Not Found"}
            issue = self.issues[number]
            if method == "DELETE":
                names = [label["name"] for label in issue.get("labels", [])]
                if label_name not in names:
                    return 404, {"message": "Label does not exist"}
                if not self.drop_writes:
                    issue["labels"] = [label for label in issue["labels"]
                                       if label["name"] != label_name]
                return 200, copy.deepcopy(issue.get("labels", []))
            return 405, {"message": "Method Not Allowed"}

        match = re.fullmatch(r"commits/([0-9a-fA-F]+)/check-runs", rest)
        if match and method == "GET":
            sha = match.group(1)
            runs = copy.deepcopy(self.check_runs.get(sha, []))
            wanted = (query.get("check_name") or [None])[0]
            if wanted:
                runs = [run for run in runs if run.get("name") == wanted]
            return 200, {"total_count": len(runs), "check_runs": runs}

        return 404, {"message": "Not Found", "path": clean}

    # -- test helpers ------------------------------------------------------
    def label_names(self, number: int) -> list:
        return sorted(label["name"] for label in self.issues[number].get("labels", []))

    def set_labels(self, number: int, names) -> None:
        self.issues[number]["labels"] = [{"name": name} for name in names]


class FakeGh:
    """Injectable gh runner with call log, mutation log and failure injection."""

    def __init__(self, store: FakeGitHub, fail_rules=(), fail_calls=()):
        self.store = store
        self.fail_rules = [(method.upper(), needle) for method, needle in fail_rules]
        self.fail_calls = set(fail_calls)
        self.calls: list[tuple[str, str]] = []
        self.mutations: list[tuple[str, str]] = []

    def _hit_failure(self, method: str, path: str) -> None:
        index = len(self.calls)
        if index in self.fail_calls:
            raise GhError(f"injected failure at call {index}: {method} {path}")
        for rule_method, needle in self.fail_rules:
            if rule_method == method and needle in path:
                raise GhError(f"injected failure: {method} {path}")

    def request(self, method: str, path: str, fields: dict | None = None):
        method = method.upper()
        self.calls.append((method, path))
        self._hit_failure(method, path)
        status, body = self.store.request(method, path, fields)
        if status >= 400:
            raise GhError(f"{method} {path} -> {status}: {json.dumps(body)}", status=status)
        if method != "GET":
            self.mutations.append((method, path))
        return body

    def paginate(self, path: str) -> list:
        items: list = []
        page = 1
        while True:
            page_path = f"{path}{'&' if '?' in path else '?'}page={page}"
            self.calls.append(("GET", page_path))
            self._hit_failure("GET", page_path)
            status, body = self.store.request("GET", page_path)
            if status >= 400:
                raise GhError(f"GET {page_path} -> {status}: {json.dumps(body)}", status=status)
            if not isinstance(body, list):
                raise GhError(f"invalid page from GET {page_path}")
            items.extend(body)
            if len(body) < self.store.page_limit:
                break
            page += 1
        return items


def build_store(page_size: int = 100, **kwargs) -> FakeGitHub:
    labels = [
        {"name": "status:idle", "color": "ededed", "description": ""},
        {"name": "status:in-progress", "color": "ededed", "description": ""},
        {"name": "status:blocked", "color": "ededed", "description": ""},
        {"name": SOURCE_LABEL, "color": "ededed", "description": ""},
    ]
    issues = {}
    for number in NUMBERS:
        issues[number] = {
            "number": number,
            "title": f"Ship work {number}",
            "state": "open",
            "labels": [{"name": "status:idle"}, {"name": SOURCE_LABEL}],
            "milestone": {"title": SOURCE_MILESTONE},
        }
    issues[444] = {
        "number": 444,
        "title": "Closed historical issue",
        "state": "closed",
        "labels": [{"name": SOURCE_LABEL}],
        "milestone": {"title": "Tendo v4.0.0 - Modern Everyday"},
    }
    issues[500] = {
        "number": 500,
        "title": "Unowned status:idle lookalike",
        "state": "open",
        "labels": [{"name": "status:idle"}],
        "milestone": None,
    }
    issues[501] = {
        "number": 501,
        "title": "V5 milestone without source label",
        "state": "open",
        "labels": [{"name": "status:idle"}],
        "milestone": {"title": SOURCE_MILESTONE},
    }
    issues[600] = {
        "number": 600,
        "title": "Pull request entry",
        "state": "open",
        "labels": [{"name": SOURCE_LABEL}],
        "milestone": {"title": SOURCE_MILESTONE},
        "pull_request": {"url": "https://api.github.com/repos/x/y/pulls/600"},
    }
    return FakeGitHub(issues=issues, labels=labels, page_size=page_size, **kwargs)


class MigrationTestCase(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.manifest = os.path.join(self.tmp.name, "labels.json")
        self.selector = os.path.join(self.tmp.name, "selector.py")
        with open(self.selector, "w", encoding="utf-8") as handle:
            handle.write("#!/usr/bin/env python3\nprint('SELF-CHECK OK')\n")

    def run_cli(self, command: str, gh: FakeGh, extra=()):
        argv = [command, "--manifest", self.manifest, "--repo", REPO, *extra]
        if command == "validate" and "--selector" not in extra:
            argv += ["--selector", self.selector]
        out, err = io.StringIO(), io.StringIO()
        with redirect_stdout(out), redirect_stderr(err):
            rc = migration.main(argv, gh=gh)
        return rc, out.getvalue(), err.getvalue()

    def plan(self, store: FakeGitHub) -> FakeGh:
        gh = FakeGh(store)
        rc, out, err = self.run_cli("plan", gh)
        self.assertEqual(rc, 0, err)
        self.assertIn("plan:", out)
        return gh

    # 1 ------------------------------------------------------------------
    def test_plan_inventory_paginates_and_excludes_non_targets(self):
        store = build_store(page_size=8)  # 17 source issues -> 3+ pages
        gh = self.plan(store)
        manifest = migration.load_manifest(self.manifest)

        self.assertEqual([issue["number"] for issue in manifest["issues"]], NUMBERS)
        self.assertEqual(manifest["untouched_closed"], [444])
        self.assertFalse(manifest["release_label_existed"])
        self.assertFalse(manifest["release_label_created"])
        self.assertEqual(manifest["repo"], REPO)
        self.assertEqual(manifest["phase"], "planned")
        for issue in manifest["issues"]:
            self.assertEqual(issue["milestone"], SOURCE_MILESTONE)
            self.assertEqual(issue["status_label"], "status:idle")
            self.assertEqual(issue["original_labels"], ["status:idle", SOURCE_LABEL])
            self.assertEqual(issue["steps"], {"added_release": False, "removed_source": False})
        self.assertEqual(gh.mutations, [])
        # #444, the unowned lookalike, the milestone-only issue and the PR entry
        # are never targets.
        numbers = {issue["number"] for issue in manifest["issues"]}
        for excluded in (444, 500, 501, 600):
            self.assertNotIn(excluded, numbers)
        self.assertGreaterEqual(len(gh.calls), 4, "expected paginated inventory calls")

    # 2 ------------------------------------------------------------------
    def test_plan_anomaly_two_status_labels_aborts_before_any_write(self):
        store = build_store()
        store.set_labels(450, ["status:idle", "status:blocked", SOURCE_LABEL])
        gh = FakeGh(store)
        rc, _, err = self.run_cli("plan", gh)
        self.assertEqual(rc, 2)
        self.assertIn("anomaly", err)
        self.assertIn("#450", err)
        self.assertFalse(os.path.exists(self.manifest))
        self.assertEqual(gh.mutations, [])

    # 3 ------------------------------------------------------------------
    def test_apply_adds_release_before_removing_source(self):
        store = build_store()
        self.plan(store)
        gh = FakeGh(store)
        rc, out, err = self.run_cli("apply", gh)
        self.assertEqual(rc, 0, err)
        self.assertIn("17 issue(s) migrated", out)

        manifest = migration.load_manifest(self.manifest)
        self.assertEqual(manifest["phase"], "labels-migrated")
        self.assertTrue(manifest["release_label_created"])
        self.assertIn(TARGET_LABEL, store.labels)

        for issue in manifest["issues"]:
            self.assertEqual(issue["steps"], {"added_release": True, "removed_source": True})
            self.assertEqual(store.label_names(issue["number"]), sorted(["status:idle", TARGET_LABEL]))

        for number in NUMBERS:
            relevant = [(method, path) for method, path in gh.mutations
                        if f"/issues/{number}/" in path]
            self.assertEqual([method for method, _ in relevant], ["POST", "DELETE"], number)
            self.assertIn("/labels", relevant[0][1])
            self.assertIn(quote(SOURCE_LABEL, safe=""), relevant[1][1])

        # Closed #444 and the non-targets are untouched.
        self.assertEqual(store.label_names(444), [SOURCE_LABEL])
        self.assertEqual(store.label_names(500), ["status:idle"])
        self.assertEqual(store.label_names(501), ["status:idle"])

    # 4 ------------------------------------------------------------------
    def test_apply_verifies_after_each_write(self):
        store = build_store(drop_writes=True)
        self.plan(store)
        gh = FakeGh(store)
        rc, _, err = self.run_cli("apply", gh)
        self.assertEqual(rc, 2)
        self.assertIn("verification failed for #445", err)
        manifest = migration.load_manifest(self.manifest)
        first = manifest["issues"][0]
        self.assertFalse(first["steps"]["added_release"])
        # The POST is followed by a verifying GET before anything else happens.
        post_index = gh.calls.index(("POST", f"repos/{REPO}/issues/445/labels"))
        self.assertEqual(gh.calls[post_index + 1], ("GET", f"repos/{REPO}/issues/445"))

    # 5 ------------------------------------------------------------------
    def test_apply_delete_failure_on_third_issue_records_partial_progress(self):
        store = build_store()
        self.plan(store)
        gh = FakeGh(store, fail_rules=[("DELETE", "/issues/447/labels")])
        rc, _, err = self.run_cli("apply", gh)
        self.assertEqual(rc, 2)
        self.assertIn("injected failure", err)

        manifest = migration.load_manifest(self.manifest)
        by_number = {issue["number"]: issue for issue in manifest["issues"]}
        self.assertEqual(by_number[445]["steps"], {"added_release": True, "removed_source": True})
        self.assertEqual(by_number[446]["steps"], {"added_release": True, "removed_source": True})
        self.assertEqual(by_number[447]["steps"], {"added_release": True, "removed_source": False})
        self.assertEqual(by_number[448]["steps"], {"added_release": False, "removed_source": False})
        self.assertNotEqual(manifest["phase"], "labels-migrated")
        self.assertEqual(store.label_names(447), sorted(["status:idle", TARGET_LABEL, SOURCE_LABEL]))
        self.assertEqual(store.label_names(448), ["status:idle", SOURCE_LABEL])

    # 6 ------------------------------------------------------------------
    def test_resume_apply_completes_without_re_adding(self):
        store = build_store()
        self.plan(store)
        failing = FakeGh(store, fail_rules=[("DELETE", "/issues/447/labels")])
        self.assertEqual(self.run_cli("apply", failing)[0], 2)

        resumed = FakeGh(store)
        rc, _, err = self.run_cli("apply", resumed)
        self.assertEqual(rc, 0, err)
        post_numbers = {path.split("/")[4] for method, path in resumed.mutations
                        if method == "POST" and "/issues/" in path}
        # 447 already carries release from the interrupted run; only its
        # pending DELETE plus the untouched tail are posted.
        self.assertEqual(post_numbers, {str(number) for number in NUMBERS[3:]})
        for number in NUMBERS:
            self.assertEqual(store.label_names(number), sorted(["status:idle", TARGET_LABEL]))

    # 7 ------------------------------------------------------------------
    def test_apply_detects_concurrent_human_edit(self):
        store = build_store()
        self.plan(store)
        store.set_labels(450, ["status:idle", SOURCE_LABEL, "bug"])
        gh = FakeGh(store)
        rc, _, err = self.run_cli("apply", gh)
        self.assertEqual(rc, 2)
        self.assertIn("concurrent edit on #450", err)
        self.assertEqual(store.label_names(450), ["bug", "status:idle", SOURCE_LABEL])
        manifest = migration.load_manifest(self.manifest)
        self.assertTrue(os.path.exists(self.manifest))
        self.assertNotEqual(manifest["phase"], "labels-migrated")

    # 8 ------------------------------------------------------------------
    def test_apply_on_completed_manifest_makes_zero_mutations(self):
        store = build_store()
        self.plan(store)
        self.assertEqual(self.run_cli("apply", FakeGh(store))[0], 0)
        again = FakeGh(store)
        rc, out, err = self.run_cli("apply", again)
        self.assertEqual(rc, 0, err)
        self.assertEqual(again.mutations, [])
        self.assertIn("migrated to release", out)

    # 9 ------------------------------------------------------------------
    def test_rollback_restores_exact_original_labels(self):
        store = build_store()
        original = {number: store.label_names(number) for number in NUMBERS}
        self.plan(store)
        self.assertEqual(self.run_cli("apply", FakeGh(store))[0], 0)
        rc, _, err = self.run_cli("rollback", FakeGh(store))
        self.assertEqual(rc, 0, err)
        for number in NUMBERS:
            self.assertEqual(store.label_names(number), original[number])
        self.assertIn(TARGET_LABEL, store.labels, "the release label itself is never deleted")
        self.assertEqual(migration.load_manifest(self.manifest)["phase"], "labels-rolled-back")
        self.assertEqual(store.label_names(444), [SOURCE_LABEL])

    # 10 -----------------------------------------------------------------
    def test_rollback_restores_partial_migration(self):
        store = build_store()
        original = {number: store.label_names(number) for number in NUMBERS}
        self.plan(store)
        self.assertEqual(self.run_cli("apply", FakeGh(store, fail_rules=[("DELETE", "/issues/447/labels")]))[0], 2)
        rc, _, err = self.run_cli("rollback", FakeGh(store))
        self.assertEqual(rc, 0, err)
        for number in NUMBERS:
            self.assertEqual(store.label_names(number), original[number], number)

    # 11 -----------------------------------------------------------------
    def test_rollback_keeps_current_status(self):
        store = build_store()
        self.plan(store)
        self.assertEqual(self.run_cli("apply", FakeGh(store))[0], 0)
        store.set_labels(450, ["status:in-progress", TARGET_LABEL])
        rc, out, err = self.run_cli("rollback", FakeGh(store))
        self.assertEqual(rc, 0, err)
        self.assertEqual(store.label_names(450), ["status:in-progress", SOURCE_LABEL])
        self.assertIn("keeping the current status", out)
        self.assertIn("#450", out)

    # 12 -----------------------------------------------------------------
    def test_rollback_failure_exits_3_with_manual_commands(self):
        store = build_store()
        self.plan(store)
        self.assertEqual(self.run_cli("apply", FakeGh(store))[0], 0)
        gh = FakeGh(store, fail_rules=[("POST", "/issues/459/labels")])
        rc, out, err = self.run_cli("rollback", gh)
        self.assertEqual(rc, 3)
        self.assertIn(f"gh issue edit 459 --repo {REPO} --add-label {SOURCE_LABEL} --remove-label {TARGET_LABEL}", err)
        self.assertIn("gh issue edit 445 --repo", err)
        self.assertNotIn("gh issue edit 460 --repo", err, "already restored issues need no manual step")
        self.assertNotEqual(migration.load_manifest(self.manifest)["phase"], "labels-rolled-back")

    # 13 -----------------------------------------------------------------
    def test_label_create_already_exists_is_reconciled(self):
        store = build_store(create_label_race=True)
        self.plan(store)
        manifest = migration.load_manifest(self.manifest)
        self.assertFalse(manifest["release_label_existed"])
        rc, _, err = self.run_cli("apply", FakeGh(store))
        self.assertEqual(rc, 0, err)
        manifest = migration.load_manifest(self.manifest)
        self.assertFalse(manifest["release_label_created"])
        self.assertIn(TARGET_LABEL, store.labels)

    # 14 -----------------------------------------------------------------
    def test_api_error_never_yields_empty_inventory(self):
        store = build_store()
        gh = FakeGh(store, fail_rules=[("GET", "/issues?state=all")])
        rc, _, err = self.run_cli("plan", gh)
        self.assertEqual(rc, 2)
        self.assertIn("MIGRATION ERROR", err)
        self.assertFalse(os.path.exists(self.manifest))

    # 15 -----------------------------------------------------------------
    def test_manifest_is_mode_0600(self):
        self.plan(build_store())
        mode = os.stat(self.manifest).st_mode & 0o777
        self.assertEqual(oct(mode), oct(0o600))

    # 16 -----------------------------------------------------------------
    def test_validate_accepts_migration_and_warns_on_progress(self):
        store = build_store()
        self.plan(store)
        self.assertEqual(self.run_cli("apply", FakeGh(store))[0], 0)
        rc, out, err = self.run_cli("validate", FakeGh(store))
        self.assertEqual(rc, 0, err)
        self.assertIn("validate: OK", out)

        store.set_labels(450, ["status:in-progress", TARGET_LABEL])
        rc, _, err = self.run_cli("validate", FakeGh(store))
        self.assertEqual(rc, 0, err)
        self.assertIn("work progressed", err)

        store.set_labels(451, ["status:idle"])  # release removed by a human
        rc, _, err = self.run_cli("validate", FakeGh(store))
        self.assertEqual(rc, 2)
        self.assertIn("#451 lacks release", err)

    # 17 -----------------------------------------------------------------
    def test_validate_fails_when_selector_self_check_fails(self):
        store = build_store()
        self.plan(store)
        self.assertEqual(self.run_cli("apply", FakeGh(store))[0], 0)
        with open(self.selector, "w", encoding="utf-8") as handle:
            handle.write("import sys\nprint('SELF-CHECK FAILED: nope', file=sys.stderr)\nsys.exit(1)\n")
        rc, _, err = self.run_cli("validate", FakeGh(store))
        self.assertEqual(rc, 2)
        self.assertIn("selector self-check failed", err)

    # 18 -----------------------------------------------------------------
    def test_plan_resume_keeps_original_labels(self):
        store = build_store()
        self.plan(store)
        original = migration.load_manifest(self.manifest)["issues"][0]["original_labels"]
        store.set_labels(445, ["status:idle", SOURCE_LABEL, TARGET_LABEL])
        gh = FakeGh(store)
        rc, out, err = self.run_cli("plan", gh)
        self.assertEqual(rc, 0, err)
        self.assertIn("manifest re-verified", out)
        self.assertEqual(
            migration.load_manifest(self.manifest)["issues"][0]["original_labels"], original
        )
        self.assertEqual(gh.mutations, [])

    # 19 -----------------------------------------------------------------
    def test_validate_warns_but_passes_for_a_human_closed_issue(self):
        store = build_store()
        self.plan(store)
        self.assertEqual(self.run_cli("apply", FakeGh(store))[0], 0)
        store.issues[460]["state"] = "closed"
        rc, out, err = self.run_cli("validate", FakeGh(store))
        self.assertEqual(rc, 0, err)
        self.assertIn("#460 is closed (closed by a human)", err)

    # 20 -----------------------------------------------------------------
    def test_failure_injection_by_call_index(self):
        store = build_store()
        gh = FakeGh(store, fail_calls={2})
        rc, _, err = self.run_cli("plan", gh)
        self.assertEqual(rc, 2)
        self.assertIn("injected failure at call 2", err)
        self.assertFalse(os.path.exists(self.manifest))

    # 21 -----------------------------------------------------------------
    def test_malformed_paginated_api_data_aborts(self):
        class ScriptedGh(migration.GhCli):
            def __init__(self, output):
                super().__init__(binary="gh")
                self.output = output

            def _run(self, args):
                return self.output

        for output in ("", "   \n", "null", "{}", '{"a": 1}', '"string"', "42", "[[], {}]"):
            runner = ScriptedGh(output)
            with self.assertRaises(migration.GhError, msg=f"paginate accepted {output!r}"):
                runner.paginate("repos/bigtcze/tendo/issues?per_page=100")

        empty_runner = ScriptedGh("[[]]")
        self.assertEqual(empty_runner.paginate("repos/bigtcze/tendo/issues?per_page=100"), [])

        for output in ("", "null"):
            runner = ScriptedGh(output)
            with self.assertRaises(migration.GhError, msg=f"request accepted {output!r}"):
                runner.request("GET", "repos/bigtcze/tendo/issues/1")

        # A single issue must be an object; a list/string is malformed data.
        for output in ("[]", '"string"'):
            with self.assertRaises(migration.GhError, msg=f"get_issue accepted {output!r}"):
                migration.get_issue(ScriptedGh(output), "bigtcze/tendo", 1)

    # 22 -----------------------------------------------------------------
    def test_malformed_inventory_never_becomes_empty(self):
        class ShapeBrokenGh(FakeGh):
            def __init__(self, store, result):
                super().__init__(store)
                self.result = result

            def paginate(self, path):
                return self.result

        store = build_store()
        for result in (None, {}, "nope", [None], ["issue"]):
            gh = ShapeBrokenGh(store, result)
            rc, _, err = self.run_cli("plan", gh)
            self.assertEqual(rc, 2, f"plan accepted {result!r}")
            self.assertIn("MIGRATION ERROR", err)
            self.assertFalse(os.path.exists(self.manifest), f"manifest written for {result!r}")

        # A genuinely empty inventory is valid and plans zero targets.
        empty = build_store()
        empty.issues = {}
        gh = FakeGh(empty)
        rc, out, err = self.run_cli("plan", gh)
        self.assertEqual(rc, 0, err)
        self.assertEqual(migration.load_manifest(self.manifest)["issues"], [])

    # 23 -----------------------------------------------------------------
    def test_show_plan_is_redacted_and_human_readable(self):
        store = build_store()
        self.plan(store)
        out = io.StringIO()
        with redirect_stdout(out):
            rc = migration.main(["show-plan", "--manifest", self.manifest], gh=FakeGh(store))
        text = out.getvalue()
        self.assertEqual(rc, 0)
        self.assertIn(f"LABEL PLAN repo={REPO} source={SOURCE_LABEL} target={TARGET_LABEL}", text)
        self.assertIn(f"#445 Ship work 445: +{TARGET_LABEL} -{SOURCE_LABEL}", text)
        self.assertIn("#444", text)
        self.assertNotIn("TOKEN", text)


# ---------------------------------------------------------------------------
# gh CLI shim (used by scripts/install-autonomy-service-test.sh)
# ---------------------------------------------------------------------------
def parse_gh_api_argv(argv: list[str]) -> dict:
    if not argv or argv[0] != "api":
        raise SystemExit("gh shim only supports `gh api`")
    args = list(argv[1:])
    method = "GET"
    paginate = False
    slurp = False
    path = ""
    fields: dict = {}
    index = 0
    while index < len(args):
        arg = args[index]
        if arg == "--method":
            method = args[index + 1].upper()
            index += 2
        elif arg == "--paginate":
            paginate = True
            index += 1
        elif arg == "--slurp":
            slurp = True
            index += 1
        elif arg in ("-f", "--raw-field", "-F", "--field"):
            key, _, value = args[index + 1].partition("=")
            if key.endswith("[]"):
                fields.setdefault(key[:-2], []).append(value)
            else:
                fields[key] = value
            index += 2
        elif arg.startswith("-"):
            index += 1
        else:
            path = arg
            index += 1
    return {"method": method, "path": path, "fields": fields,
            "paginate": paginate, "slurp": slurp}


def gh_shim_main(store: FakeGitHub, argv: list[str], fail_spec: str = ""):
    parsed = parse_gh_api_argv(argv)
    method, path = parsed["method"], parsed["path"]
    rules = []
    for rule in (fail_spec or "").split(";"):
        rule = rule.strip()
        if ":" in rule:
            rule_method, _, needle = rule.partition(":")
            rules.append((rule_method.upper(), needle))
    if any(rule_method == method and needle in path for rule_method, needle in rules):
        return 1, "", json.dumps({"message": f"injected shim failure: {method} {path}"}) + "\n"

    if parsed["paginate"]:
        pages = []
        items: list = []
        page = 1
        while True:
            page_path = f"{path}{'&' if '?' in path else '?'}page={page}"
            status, body = store.request("GET", page_path)
            if status >= 400:
                return 1, "", json.dumps(body) + "\n"
            pages.append(body)
            items.extend(body)
            if len(body) < store.page_limit:
                break
            page += 1
        if parsed["slurp"]:
            payload = pages
        else:
            payload = items
    else:
        status, payload = store.request(method, path, parsed["fields"])
        if status >= 400:
            return 1, "", json.dumps(payload) + "\n"
    if payload is None:
        payload = {}
    return 0, json.dumps(payload) + "\n", ""


def shim_entry(argv: list[str]) -> int:
    store_path = os.environ["FAKE_GH_STORE"]
    store = FakeGitHub.from_file(store_path)
    parsed = parse_gh_api_argv(argv)
    rc, out, err = gh_shim_main(store, argv, os.environ.get("FAKE_GH_FAIL", ""))
    store.save(store_path)
    log_path = os.environ.get("FAKE_GH_LOG")
    if log_path:
        with open(log_path, "a", encoding="utf-8") as handle:
            handle.write(f"gh {parsed['method']} {parsed['path']}\n")
    if out:
        sys.stdout.write(out)
    if err:
        sys.stderr.write(err)
    return rc


if __name__ == "__main__":
    if len(sys.argv) > 1 and sys.argv[1] == "--gh-shim":
        sys.exit(shim_entry(sys.argv[2:]))
    unittest.main()
