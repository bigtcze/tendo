#!/usr/bin/env python3
"""Control-flow regression tests for mandatory verifier command failures."""
from __future__ import annotations

import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent
CONTEXT_FILES = (
    "AGENTS.md", ".autonomous/GUARDRAILS.md", ".autonomous/CHARTER.md", ".autonomous/PRODUCT.md",
    ".autonomous/ROADMAP.md", ".autonomous/UX.md", ".autonomous/ARCHITECTURE.md", ".autonomous/API.md",
    ".autonomous/TESTING.md", ".autonomous/DOCUMENTATION.md", ".autonomous/DEFINITION_OF_DONE.md",
    ".autonomous/FLOW.md", ".autonomous/STATE.md", ".autonomous/LEARNINGS.md", ".autonomous/CYCLE_PROMPT.md",
)
HARNESS = (
    "scripts/test_postgres_test_service.py",
    "scripts/test_e2e_native_runner.py",
    "scripts/test_verify_local.py",
    "scripts/test_generation_drift.py",
    "scripts/test_e2e_interrupt.py",
)


class VerifyLocalFailurePropagation(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix="tendo-verify-control-")
        self.root = Path(self.tmp.name) / "repo"
        (self.root / "scripts").mkdir(parents=True)
        (self.root / ".autonomous").mkdir()
        for filename in CONTEXT_FILES:
            (self.root / filename).write_text("control fixture\n")
        (self.root / "scripts/autonomous-loop.sh").write_text("#!/usr/bin/env bash\nexit 0\n")
        shutil.copy2(ROOT / "scripts/verify-local.sh", self.root / "scripts/verify-local.sh")
        for filename in ("setup-integration.sh", "check-sqlc.sh", "e2e-smoke.sh"):
            path = self.root / "scripts" / filename
            stage = filename.removesuffix(".sh")
            path.write_text(
                "#!/usr/bin/env bash\n"
                f"printf 'script:{stage}\\n' >> \"$VERIFY_LOG\"\n"
                f"if [[ \"$FAIL_COMMAND\" == script-{stage} ]]; then exit 37; fi\n"
                "exit 0\n"
            )
            path.chmod(0o755)
        self.bin = Path(self.tmp.name) / "bin"
        self.bin.mkdir()
        self.write_tool("gofmt", """printf 'gofmt\\n' >> "$VERIFY_LOG"
if [[ "$FAIL_COMMAND" == go-format ]]; then printf 'backend/internal/example.go\\n'; fi
""")
        self.write_tool("find", """if [[ "$FAIL_COMMAND" == go-format ]]; then printf 'backend/internal/example.go\\n'; fi
exit 0
""")
        self.write_tool("go", """printf 'go:%s\\n' "$1" >> "$VERIFY_LOG"
if [[ "$FAIL_COMMAND" == go-list && "$1" == list ]]; then exit 37; fi
if [[ "$FAIL_COMMAND" == go-vet && "$1" == vet ]]; then exit 37; fi
if [[ "$FAIL_COMMAND" == go-test && "$1" == test ]]; then exit 37; fi
if [[ "$FAIL_COMMAND" == go-build && "$1" == build ]]; then exit 37; fi
if [[ "$1" == list ]]; then printf 'example.test/pkg\\n'; fi
exit 0
""")
        self.write_tool("npm", """printf 'npm:%s:%s\\n' "$PWD" "$1" >> "$VERIFY_LOG"
project=api
if [[ "$PWD" == */frontend ]]; then project=frontend; fi
if [[ "$1" == ci && "$FAIL_COMMAND" == npm-ci-$project ]]; then exit 37; fi
if [[ "$1" == audit && "$FAIL_COMMAND" == npm-audit-$project ]]; then exit 37; fi
if [[ "$1" == run && "$FAIL_COMMAND" == npm-check-$project ]]; then exit 37; fi
exit 0
""")
        self.write_tool("python3", """printf 'python:%s\\n' "$*" >> "$VERIFY_LOG"
if [[ "$1" == -m && "$2" == unittest ]]; then
  shift 2
  for arg in "$@"; do
    printf 'harness:%s\\n' "$arg" >> "$VERIFY_LOG"
    case "$FAIL_COMMAND:$arg" in
      python-unit-postgres:scripts/test_postgres_test_service.py) exit 37 ;;
      python-e2e-runner:scripts/test_e2e_native_runner.py) exit 37 ;;
      python-verify-local:scripts/test_verify_local.py) exit 37 ;;
      python-ci-workflow:scripts/test_ci_workflow.py) exit 37 ;;
      python-toolchain-manifest:scripts/test_toolchain_manifest.py) exit 37 ;;
      python-unit-generator:scripts/test_generation_drift.py) exit 37 ;;
      python-interrupt:scripts/test_e2e_interrupt.py) exit 37 ;;
    esac
  done
elif [[ "$1" == scripts/test_postgres_live.py ]]; then
  printf 'harness:%s\\n' "$1" >> "$VERIFY_LOG"
  if [[ "$FAIL_COMMAND" == python-live-postgres ]]; then exit 37; fi
elif [[ "$1" == scripts/test_e2e_interrupt.py ]]; then
  printf 'harness:%s\\n' "$1" >> "$VERIFY_LOG"
  if [[ "$FAIL_COMMAND" == python-interrupt ]]; then exit 37; fi
fi
exit 0
""")
        (self.root / "backend").mkdir()
        (self.root / "api").mkdir()
        (self.root / "frontend").mkdir()
        self.log = Path(self.tmp.name) / "calls.log"

    def tearDown(self):
        self.tmp.cleanup()

    def write_tool(self, name: str, script: str):
        path = self.bin / name
        path.write_text("#!/usr/bin/env bash\n" + script)
        path.chmod(0o755)

    def run_gate(self, fail_command: str, env_updates=None):
        self.log.unlink(missing_ok=True)
        env = os.environ.copy()
        env.update({
            "PATH": str(self.bin) + os.pathsep + env.get("PATH", ""),
            "VERIFY_ROOT": str(self.root),
            "VERIFY_LOG": str(self.log),
            "FAIL_COMMAND": fail_command,
            "TENDO_TEST_POSTGRES_ADMIN_URL": "postgresql://not-used.invalid/postgres",
        })
        if env_updates:
            env.update(env_updates)
        return subprocess.run(
            ["bash", str(self.root / "scripts/verify-local.sh")], cwd=self.root, env=env,
            text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=30,
        )

    def assert_failed_stage(self, command, stage):
        result = self.run_gate(command)
        self.assertEqual(result.returncode, 37, result.stdout)
        self.assertIn(f"{stage}:FAIL:", result.stdout)
        self.assertIn(f"downstream stages: NOT RUN (fail-fast at {stage})", result.stdout)
        return self.log.read_text()

    def test_go_list_vet_test_failures_propagate(self):
        self.assertNotIn("script:setup-integration", self.assert_failed_stage("go-list", "go-unit-race"))
        self.assertNotIn("go:list", self.assert_failed_stage("go-vet", "go-vet"))
        self.assertNotIn("script:setup-integration", self.assert_failed_stage("go-test", "go-unit-race"))

    def test_external_required_script_failures_propagate(self):
        for command, stage, later in (
            ("script-setup-integration", "postgres-integration", "script:check-sqlc"),
            ("script-check-sqlc", "sqlc-drift", "go:build"),
            ("script-e2e-smoke", "native-production-e2e", ""),
        ):
            with self.subTest(command=command):
                log = self.assert_failed_stage(command, stage)
                if later:
                    self.assertNotIn(later, log)

    def test_api_and_frontend_npm_failure_propagation(self):
        cases = (
            ("npm-ci-api", "api-contract-audit", 1),
            ("npm-audit-api", "api-contract-audit", 2),
            ("npm-check-api", "api-contract-audit", 3),
            ("npm-ci-frontend", "frontend-check", 4),
            ("npm-audit-frontend", "frontend-check", 5),
            ("npm-check-frontend", "frontend-check", 6),
        )
        for command, stage, calls_before_failure in cases:
            with self.subTest(command=command):
                log = self.assert_failed_stage(command, stage)
                npm_calls = [line for line in log.splitlines() if line.startswith("npm:")]
                self.assertEqual(len(npm_calls), calls_before_failure)
                self.assertNotIn("script:e2e-smoke", log)

    def test_ci_workflow_mutation_test_failure_is_mandatory(self):
        log = self.assert_failed_stage("python-ci-workflow", "native-harness-tests")
        self.assertIn("scripts/test_ci_workflow.py", log)
        self.assertNotIn("script:e2e-smoke", log)

    def test_each_harness_python_command_is_fail_fast(self):
        cases = (
            ("python-unit-postgres", 1),
            ("python-e2e-runner", 2),
            ("python-verify-local", 3),
            ("python-toolchain-manifest", 5),
            ("python-live-postgres", 6),
            ("python-unit-generator", 7),
            ("python-interrupt", 8),
        )
        for command, expected_count in cases:
            with self.subTest(command=command):
                log = self.assert_failed_stage(command, "native-harness-tests")
                ran = [line.removeprefix("harness:") for line in log.splitlines() if line.startswith("harness:")]
                self.assertEqual(len(ran), expected_count)
                self.assertNotIn("script:e2e-smoke", log)


if __name__ == "__main__":
    unittest.main(verbosity=2)
