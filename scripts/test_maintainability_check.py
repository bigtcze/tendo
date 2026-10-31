"""Unit tests for the pure maintainability metric/ratchet logic."""
from __future__ import annotations

import importlib.util
from pathlib import Path
import subprocess
from unittest.mock import patch
import tempfile
import unittest

GIT_IDENTITY = ("-c", "user.name=Test", "-c", "user.email=test@example.invalid")

MODULE_PATH = Path(__file__).with_name("maintainability_check.py")
spec = importlib.util.spec_from_file_location("maintainability_check", MODULE_PATH)
check = importlib.util.module_from_spec(spec)
spec.loader.exec_module(check)


class MaintainabilityTests(unittest.TestCase):
    def test_file_counts_nonblank_and_long_lines(self):
        result = check.file_metrics("one\n\n" + "x" * 161 + "\n")
        self.assertEqual(result, {"file_lines": 2, "long_lines": 1})

    def test_exclusions(self):
        for path in (
            "backend/foo_test.go",
            "backend/internal/platform/database/001_setup.sql",
            "api/generated/api.d.ts",
            "api/generated/health.d.ts",
            "frontend/src/a.test.tsx",
            "frontend/e2e/home.spec.ts",
            "scripts/test_ci.py",
        ):
            with self.subTest(path=path):
                self.assertTrue(check.excluded(path))
        self.assertTrue(check.excluded("backend/internal/platform/database/014_subject_linked_user.sql"))
        self.assertFalse(check.excluded("backend/internal/platform/database/seed_data.sql"))
        self.assertFalse(check.excluded("backend/internal/platform/migrations/legacy.sql"))
        self.assertFalse(check.excluded("frontend/src/app.tsx"))
        self.assertFalse(check.excluded("frontend/e2e/helper.ts"))
        self.assertTrue(check.excluded("backend/internal/item/postgres/dbgen/item.sql.go"))
        self.assertTrue(check.excluded("backend/internal/identity/httpapi/setup.gen.go"))
        self.assertFalse(check.excluded("backend/internal/identity/httpapi/unknown.gen.go"))
        self.assertFalse(check.excluded("backend/internal/identity/oidcprovider/provider.go"))

    def assert_error(self, metrics, baseline, expected):
        errors = check.check_metrics(metrics, baseline)
        self.assertTrue(any(expected in error for error in errors), errors)

    def test_base_ref_absent_baseline_bootstraps(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            subprocess.run(["git", "init", "-q", str(root)], check=True)
            subprocess.run(["git", "-C", str(root), *GIT_IDENTITY, "commit", "--allow-empty", "-qm", "fixture"], check=True)
            self.assertIsNone(check.load_base_baseline("HEAD", root))

    def test_base_ref_errors_fail_closed(self):
        with self.assertRaises(RuntimeError):
            check.load_base_baseline("not-a-real-ref")
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            subprocess.run(["git", "init", "-q", str(root)], check=True)
            subprocess.run(["git", "-C", str(root), *GIT_IDENTITY, "commit", "--allow-empty", "-qm", "fixture"], check=True)
            (root / "ci").mkdir()
            (root / "ci/maintainability-baseline.json").write_text("{bad", encoding="utf-8")
            subprocess.run(["git", "-C", str(root), "add", "ci/maintainability-baseline.json"], check=True)
            subprocess.run(["git", "-C", str(root), *GIT_IDENTITY, "commit", "-qm", "malformed baseline"], check=True)
            with self.assertRaises(Exception):
                check.load_base_baseline("HEAD", root)

    def test_base_path_present_but_unreadable_fails_closed(self):
        def run(command, **kwargs):
            if command[1] == "rev-parse":
                return subprocess.CompletedProcess(command, 0, stdout="deadbeef\n", stderr="")
            if command[1] == "cat-file":
                return subprocess.CompletedProcess(command, 1, stdout=b"", stderr=b"object unavailable")
            if command[1] == "ls-tree":
                return subprocess.CompletedProcess(command, 0, stdout="ci/maintainability-baseline.json\n", stderr="")
            raise AssertionError(command)

        with patch.object(check.subprocess, "run", side_effect=run):
            with self.assertRaisesRegex(RuntimeError, "exists in tree but cannot be read"):
                check.load_base_baseline("HEAD")

    def test_duplicate_go_metric_identity_is_rejected(self):
        item = {"path": "backend/a.go", "name": "init#1", "lines": 20, "complexity": 17, "nesting": 1}
        with self.assertRaisesRegex(RuntimeError, "duplicate Go function metric identity"):
            check.add_function_metrics({}, [item, item])

    def test_discover_go_files_preserves_backend_paths(self):
        paths = ["backend/cmd/tendo/items.go", "frontend/src/App.tsx"]
        self.assertEqual(check.discover_go_files(check.ROOT, paths), ["cmd/tendo/items.go"])

    def test_base_ref_rejects_raised_and_new_exceptions(self):
        prior = [{"kind": "file_lines", "path": "backend/a.go", "allowed": 510}]
        raised = [{"kind": "file_lines", "path": "backend/a.go", "allowed": 520}]
        added = prior + [{"kind": "func_lines", "path": "backend/a.go", "name": "F", "allowed": 81}]
        self.assertTrue(any("allowed raised=520 base=510" in error for error in check.baseline_errors_against_base(raised, prior)))
        self.assertTrue(any("new exception relative to base" in error for error in check.baseline_errors_against_base(added, prior)))

    def test_new_violation(self):
        self.assert_error({("func_lines", "backend/a.go", "F"): 81}, [], "measured=81 allowed=80")

    def test_worsened_exception(self):
        self.assert_error({("func_lines", "backend/a.go", "F"): 82},
                          [{"kind": "func_lines", "path": "backend/a.go", "name": "F", "allowed": 81, "reason": "legacy"}],
                          "measured=82 allowed=81")

    def test_stale_exception(self):
        self.assert_error({("func_lines", "backend/a.go", "F"): 80},
                          [{"kind": "func_lines", "path": "backend/a.go", "name": "F", "allowed": 81, "reason": "legacy"}],
                          "stale measured=80")

    def test_exception_within_threshold_is_rejected(self):
        self.assert_error({("long_lines", "backend/a.go", ""): 0},
                          [{"kind": "long_lines", "path": "backend/a.go", "allowed": 0, "reason": "legacy", "owner": "backend"}],
                          "exception allowed=0 is within threshold; remove it")

    def test_missing_target(self):
        self.assert_error({}, [{"kind": "file_lines", "path": "backend/gone.go", "allowed": 600, "reason": "legacy"}], "missing measured entry")

    def test_missing_reason_and_owner(self):
        baseline = [{"kind": "func_lines", "path": "backend/a.go", "name": "F", "allowed": 82}]
        errors = check.check_metrics({("func_lines", "backend/a.go", "F"): 81}, baseline)
        self.assertTrue(any("missing reason" in error for error in errors), errors)
        self.assertTrue(any("missing owner" in error for error in errors), errors)

    def test_todo_reason_and_owner_are_rejected(self):
        baseline = [{"kind": "func_lines", "path": "backend/a.go", "name": "F", "allowed": 81,
                     "reason": "TODO: replace", "owner": "TODO: assign"}]
        errors = check.check_metrics({("func_lines", "backend/a.go", "F"): 81}, baseline)
        self.assertTrue(any("missing reason" in error for error in errors), errors)
        self.assertTrue(any("missing owner" in error for error in errors), errors)

    def test_duplicate_entry(self):
        item = {"kind": "file_lines", "path": "backend/a.go", "allowed": 510, "reason": "legacy"}
        self.assert_error({("file_lines", "backend/a.go", ""): 510}, [item, item], "duplicate baseline")

    def test_repository_passes(self):
        metrics = check.gather(check.ROOT)
        import json
        baseline = json.loads(check.BASELINE.read_text(encoding="utf-8"))
        self.assertEqual(check.check_metrics(metrics, baseline), [])

    def test_write_baseline_refuses_raise_and_preserves_reasons(self):
        entry = {"kind": "file_lines", "path": "backend/a.go", "allowed": 510, "reason": "known cohesive file"}
        unchanged = check.write_baseline({("file_lines", "backend/a.go", ""): 510}, [entry])
        self.assertEqual(unchanged[0]["reason"], "known cohesive file")
        with self.assertRaises(ValueError):
            check.write_baseline({("file_lines", "backend/a.go", ""): 520}, [entry])

    def test_test_classification_uses_repository_conventions(self):
        cases = {
            "backend/item_test.go": "test",
            "frontend/src/app.test.tsx": "test",
            "scripts/test_ci.py": "test",
            "frontend/e2e/home.spec.ts": "test",
            "frontend/e2e/helper.ts": "test-support",
            "backend/internal/item/itemtest/repo.go": "test-support",
            "backend/internal/testpostgres/fixture.go": "test-support",
            "backend/internal/item/service.go": None,
        }
        for path, expected in cases.items():
            with self.subTest(path=path):
                self.assertEqual(check.test_classification(path), expected)

    def test_inventory_rendering_is_sorted_and_shows_dispositions(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "frontend").mkdir()
            (root / "frontend/maintainability-baseline.json").write_text("[]", encoding="utf-8")
            (root / "backend").mkdir()
            (root / "backend/a.go").write_text("package backend\\n\\nfunc Run() {}\\n", encoding="utf-8")
            (root / "frontend/app.ts").write_text("export const app = true;\\n", encoding="utf-8")
            paths = ["backend/a.go", "frontend/app.ts"]
            metrics = {
                ("file_lines", "backend/a.go", ""): 3,
                ("long_lines", "backend/a.go", ""): 0,
                ("func_lines", "backend/a.go", "Run"): 1,
                ("func_complexity", "backend/a.go", "Run"): 1,
                ("func_nesting", "backend/a.go", "Run"): 0,
                ("file_lines", "frontend/app.ts", ""): 1,
                ("long_lines", "frontend/app.ts", ""): 0,
            }
            with (
                patch.object(check, "source_candidates", return_value=paths),
                patch.object(check, "enumerate_files", return_value=paths),
                patch.object(check.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, stdout="abc123\n")),
            ):
                rendered = check.inventory_markdown(root, metrics, [])
            self.assertIn("Git HEAD `abc123`", rendered)
            self.assertIn("| `backend/a.go` | backend Go | 3 | 0 | 1; 1 (Run); 1 (Run); 0 (Run) | within thresholds |", rendered)
            self.assertLess(rendered.index("backend/a.go"), rendered.index("frontend/app.ts"))
            self.assertIn("Frontend function-level rules are enforced by ESLint", rendered)


if __name__ == "__main__":
    unittest.main(verbosity=2)
