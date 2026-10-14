#!/usr/bin/env python3
"""Text-level CI gate structure tests using only the Python standard library."""
from __future__ import annotations

from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parent.parent
WORKFLOW = ROOT / ".github/workflows/required.yml"

REQUIRED_COMMANDS = (
    "gofmt -l",
    "go vet ./...",
    "go test -race",
    "go list ./...",
    "bash scripts/setup-integration.sh",
    "bash scripts/check-sqlc.sh",
    "go build ./...",
    "npm ci",
    "npm audit --audit-level=moderate",
    "npm run check",
    "python3 -m unittest scripts/test_postgres_test_service.py scripts/test_e2e_native_runner.py scripts/test_verify_local.py",
    "python3 scripts/test_postgres_live.py",
    "python3 -m unittest scripts/test_generation_drift.py",
    "python3 scripts/test_e2e_interrupt.py",
    "python3 -m unittest scripts/test_verify_local.py scripts/test_ci_workflow.py scripts/test_toolchain_manifest.py scripts/test_dev_toolchain.py",
    "bash scripts/dev-exec.sh --versions",
    "bash scripts/dev-exec.sh -- python3 -m unittest scripts/test_dev_toolchain.py",
    "npx --no-install playwright install --with-deps chromium",
    'bash "scripts/${SMOKE}.sh"',
    "bash scripts/e2e-smoke.sh",
    "bash -n scripts/autonomous-loop.sh",
)
SMOKES = (
    "runtime-smoke",
    "setup-smoke",
    "oidc-smoke",
    "proxy-smoke",
    "backup-restore-smoke",
    "household-backup-smoke",
)


def jobs_and_needs(text: str):
    lines = text.splitlines()
    jobs_index = next((i for i, line in enumerate(lines) if line == "jobs:"), None)
    if jobs_index is None:
        raise AssertionError("workflow is missing top-level jobs:")
    jobs = []
    current = None
    needs = None
    for line in lines[jobs_index + 1:]:
        if line and not line[0].isspace():
            break
        match = re.match(r"^  ([A-Za-z0-9_-]+):\s*(?:#.*)?$", line)
        if match:
            if current is not None:
                jobs.append((current, needs))
            current, needs = match.group(1), None
        elif current and re.match(r"^    needs:\s*", line):
            needs = line.split(":", 1)[1].strip()
    if current is not None:
        jobs.append((current, needs))
    return jobs


def unquote_needs(value: str | None):
    if value is None:
        return []
    value = value.strip()
    if value.startswith("[") and value.endswith("]"):
        return [item.strip().strip("'\"") for item in value[1:-1].split(",") if item.strip()]
    return [value.strip("'\"")]


def job_section(text: str, job_id: str) -> str:
    """Return the text of one top-level job (empty if absent)."""
    lines = text.splitlines()
    start = next((i for i, line in enumerate(lines) if re.match(rf"^  {re.escape(job_id)}:\s*(?:#.*)?$", line)), None)
    if start is None:
        return ""
    end = len(lines)
    for i in range(start + 1, len(lines)):
        line = lines[i]
        if re.match(r"^  [A-Za-z0-9_-]+:\s*(?:#.*)?$", line) or (line and not line[0].isspace()):
            end = i
            break
    return "\n".join(lines[start:end])


def active_text(text: str) -> str:
    """Workflow text with comments removed, so commented-out commands never count."""
    return "\n".join(re.sub(r"(?:^|\s)#.*$", "", line) for line in text.splitlines())


PROJECT_CHECKS = {
    "api": ("working-directory: api", "npm ci", "npm audit --audit-level=moderate", "npm run check"),
    "frontend": ("working-directory: frontend", "npm ci", "npm audit --audit-level=moderate", "npm run check"),
}


def workflow_errors(text: str):
    errors = []
    jobs = jobs_and_needs(text)
    ids = [job_id for job_id, _ in jobs]
    by_id = dict(jobs)
    if ids.count("required") != 1:
        errors.append("expected exactly one required job")
    aggregate_text = job_section(text, "required")
    if re.search(r"(?m)^    name:\s*required\s*$", aggregate_text) is None:
        errors.append("required job context name is missing")
    if len(re.findall(r"(?m)^\s+name:\s*required\s*$", text)) != 1:
        errors.append("exactly one job may be named required")
    if re.search(r"(?m)^    if:\s*always\(\)\s*$", aggregate_text) is None:
        errors.append("aggregate required job must use if: always()")
    if "continue-on-error" in text:
        errors.append("continue-on-error is forbidden in the required workflow")
    for job_id, markers in PROJECT_CHECKS.items():
        section = job_section(text, job_id)
        for marker in markers:
            if marker not in section:
                errors.append(f"{job_id} job is missing: {marker}")
    active = active_text(text)
    for command in REQUIRED_COMMANDS:
        if command not in active:
            errors.append(f"mandatory command missing: {command}")
    matrix_line = next((line.strip() for line in text.splitlines() if line.strip().startswith("smoke: [")), "")
    if not matrix_line:
        errors.append("deployment smoke matrix list is missing")
    else:
        for smoke in SMOKES:
            if smoke not in matrix_line:
                errors.append(f"missing deployment matrix smoke: {smoke}")
    aggregate = by_id.get("required")
    expected = set(ids) - {"required"}
    actual = set(unquote_needs(aggregate))
    if actual != expected:
        errors.append(f"aggregate needs mismatch: expected={sorted(expected)} actual={sorted(actual)}")
    if "NEEDS_JSON: ${{ toJSON(needs) }}" not in aggregate_text:
        errors.append("aggregate must evaluate toJSON(needs)")
    count_match = re.search(r"(?m)^\s*jq -e 'length == (\d+) and all\(\.\[\]; \.result == \"success\"\)'", aggregate_text)
    if not count_match:
        errors.append("aggregate jq needs-count guard is missing")
    elif int(count_match.group(1)) != len(expected):
        errors.append(f"aggregate jq expected count {count_match.group(1)} != {len(expected)} jobs")
    if "cancel-in-progress: ${{ github.event_name == 'pull_request' }}" not in text:
        errors.append("PR concurrency cancellation policy is missing")
    return errors


class WorkflowStructureTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.text = WORKFLOW.read_text(encoding="utf-8")

    def test_every_required_assertion_command_and_job_is_present(self):
        errors = workflow_errors(self.text)
        self.assertEqual(errors, [], "\n".join(errors))
        jobs = [job_id for job_id, _ in jobs_and_needs(self.text)]
        self.assertCountEqual(jobs, ["backend", "api", "frontend", "postgres", "e2e", "deployment", "required"])
        self.assertIn("services:", self.text)
        self.assertIn("image: postgres:18.6-bookworm", self.text)
        self.assertIn("sqlc_1.31.1_linux_amd64.tar.gz", self.text)
        self.assertIn("TENDO_TEST_POSTGRES_ADMIN_URL:", self.text)
        self.assertIn("postgresql-client-18", self.text)
        self.assertIn("497ae4fcdfa64c5b0c311ffe4c2bd991e43991e82e5367792ed78bc2dca27354", self.text)
        self.assertIn("postgres:ci-postgres-password@127.0.0.1:5432/postgres", self.text)
        self.assertIn("PGPASSWORD=ci-postgres-password psql", self.text)
        self.assertIn('bash "scripts/${SMOKE}.sh"', self.text)
        self.assertNotIn("docker exec", self.text)

    def test_each_deployment_smoke_is_present(self):
        for smoke in SMOKES:
            with self.subTest(smoke=smoke):
                self.assertIn(smoke, self.text)
        self.assertEqual(self.text.count('bash "scripts/${SMOKE}.sh"'), 1)
        deployment = self.text.split("  deployment:", 1)[1].split("  required:", 1)[0]
        self.assertIn("fail-fast: false", deployment)
        self.assertIn("proxy-smoke) timeout_seconds=720", deployment)
        self.assertIn("household-backup-smoke) timeout_seconds=900", deployment)
        self.assertIn("timeout \"$timeout_seconds\" bash", deployment)

    def test_missing_smoke_fails_structure_validation(self):
        mutated = self.text.replace("oidc-smoke", "removed-oidc-step", 1)
        errors = workflow_errors(mutated)
        self.assertTrue(any("missing deployment matrix smoke: oidc-smoke" in error for error in errors), errors)

    def test_commented_out_command_fails_structure_validation(self):
        command = "python3 -m unittest scripts/test_verify_local.py scripts/test_ci_workflow.py scripts/test_toolchain_manifest.py scripts/test_dev_toolchain.py"
        mutated = self.text.replace(f"run: {command}", f"run: true  # {command}", 1)
        self.assertNotEqual(mutated, self.text)
        errors = workflow_errors(mutated)
        self.assertTrue(any(f"mandatory command missing: {command}" in error for error in errors), errors)

    def test_missing_aggregate_need_fails_structure_validation(self):
        mutated = self.text.replace("needs: [backend, api, frontend, postgres, e2e, deployment]", "needs: [backend, api, frontend, postgres, e2e]", 1)
        errors = workflow_errors(mutated)
        self.assertTrue(any("aggregate needs mismatch" in error for error in errors), errors)

    def job_text(self, job_id: str) -> str:
        ids = [job for job, _ in jobs_and_needs(self.text)]
        start = self.text.index(f"\n  {job_id}:\n")
        following = [self.text.index(f"\n  {other}:\n") for other in ids if self.text.index(f"\n  {other}:\n") > start]
        return self.text[start:min(following) if following else len(self.text)]

    def assert_ordered(self, job: str, *markers: str):
        positions = []
        for marker in markers:
            self.assertIn(marker, job)
            positions.append(job.index(marker))
        self.assertEqual(positions, sorted(positions), f"steps out of order: {markers}")

    def test_prerequisites_precede_consumers_within_each_job(self):
        # Jobs run in parallel on fresh runners, so ordering is only meaningful within one job.
        backend = active_text(self.job_text("backend"))
        self.assertIn("python3 -m unittest scripts/test_verify_local.py scripts/test_ci_workflow.py scripts/test_toolchain_manifest.py scripts/test_dev_toolchain.py", backend)
        self.assert_ordered(backend, "TENDO_TOOLCHAIN_CACHE: ${{ runner.temp }}/tendo-toolchain", "bash scripts/dev-exec.sh --versions", "bash scripts/dev-exec.sh -- bash -c")
        postgres = self.job_text("postgres")
        self.assert_ordered(postgres, "sqlc_1.31.1_linux_amd64.tar.gz", "CREATE ROLE tendo NOLOGIN", "bash scripts/setup-integration.sh", "bash scripts/check-sqlc.sh")
        self.assert_ordered(postgres, "working-directory: api\n        run: npm ci", "python3 scripts/test_postgres_live.py")
        self.assert_ordered(postgres, "working-directory: frontend\n        run: npm ci", "npx --no-install playwright install --with-deps chromium", "python3 -m unittest scripts/test_postgres_test_service.py")
        e2e = self.job_text("e2e")
        self.assert_ordered(e2e, "postgresql-client-18", "CREATE ROLE tendo NOLOGIN", "working-directory: frontend\n        run: npm ci", "npx --no-install playwright install --with-deps chromium", "bash scripts/e2e-smoke.sh")
        for job_id in ("postgres", "e2e"):
            self.assertIn("image: postgres:18.6-bookworm", self.job_text(job_id))
            self.assertIn("TENDO_TEST_POSTGRES_ADMIN_URL:", self.job_text(job_id))

    def test_missing_aggregate_condition_fails_structure_validation(self):
        mutated = self.text.replace("    if: always()\n", "", 1)
        errors = workflow_errors(mutated)
        self.assertTrue(any("if: always()" in error for error in errors), errors)

    def test_aggregate_markers_elsewhere_do_not_satisfy_validation(self):
        aggregate = job_section(self.text, "required")
        relocated = aggregate.replace("    if: always()\n", "", 1)
        mutated = self.text.replace(aggregate, relocated, 1).replace("  backend:\n    name: backend\n", "  backend:\n    name: backend\n    if: always()\n", 1)
        errors = workflow_errors(mutated)
        self.assertTrue(any("if: always()" in error for error in errors), errors)

    def test_missing_project_audit_fails_even_if_other_job_audits(self):
        frontend = job_section(self.text, "frontend")
        mutated = self.text.replace(frontend, frontend.replace("npm audit --audit-level=moderate", "true", 1), 1)
        errors = workflow_errors(mutated)
        self.assertTrue(any("frontend job is missing: npm audit" in error for error in errors), errors)

    def test_continue_on_error_fails_structure_validation(self):
        mutated = self.text.replace("        run: bash scripts/e2e-smoke.sh", "        continue-on-error: true\n        run: bash scripts/e2e-smoke.sh", 1)
        errors = workflow_errors(mutated)
        self.assertTrue(any("continue-on-error" in error for error in errors), errors)

    def test_workflow_test_itself_is_mandatory_in_ci(self):
        mutated = self.text.replace("scripts/test_verify_local.py scripts/test_ci_workflow.py", "scripts/test_verify_local.py", 1)
        errors = workflow_errors(mutated)
        self.assertTrue(any("test_ci_workflow.py" in error for error in errors), errors)

    def test_dropping_wrapper_tests_or_real_provisioning_fails_structure_validation(self):
        for removed, replacement in ((" scripts/test_dev_toolchain.py", ""), ("bash scripts/dev-exec.sh --versions", "true")):
            with self.subTest(removed=removed):
                mutated = self.text.replace(removed, replacement, 1)
                self.assertNotEqual(mutated, self.text)
                self.assertTrue(workflow_errors(mutated))

    def test_wrong_aggregate_count_fails_structure_validation(self):
        mutated = self.text.replace("length == 6 and all(.[]; .result == \"success\")", "length == 5 and all(.[]; .result == \"success\")", 1)
        errors = workflow_errors(mutated)
        self.assertTrue(any("aggregate jq expected count" in error for error in errors), errors)


if __name__ == "__main__":
    unittest.main(verbosity=2)
