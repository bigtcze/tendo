#!/usr/bin/env python3
"""Run native production-binary Playwright E2E under bounded child supervision."""
from __future__ import annotations

import argparse
import base64
import os
import re
import secrets
import signal
import socket
import subprocess
import sys
import threading
import time
import uuid
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path


# Generated credentials are registered here so failure output can be redacted before printing.
SENSITIVE_VALUES: set[str] = set()
FAILURE_LOG_TAIL_LINES = 200


def generated_secret(nbytes: int) -> str:
    value = base64.b64encode(secrets.token_bytes(nbytes)).decode("ascii")
    SENSITIVE_VALUES.add(value)
    return value


def register_database_url(value: str):
    """Register a database URL plus its encoded and decoded password for failure-log redaction."""
    if not value:
        return
    SENSITIVE_VALUES.add(value)
    password = urllib.parse.urlsplit(value).password
    if password:
        SENSITIVE_VALUES.add(password)
        SENSITIVE_VALUES.add(urllib.parse.unquote(password))


ANSI_ESCAPE = re.compile(r"\x1b\[[0-9;?]*[ -/]*[@-~]")
# Structural redaction for secrets the runner never sees by value: browser/API cookies, auth headers,
# OIDC callback parameters, invitation fragments, and any credential-bearing URL.
STRUCTURAL_PATTERNS = (
    (re.compile(r"(?i)\b(set-cookie|cookie|authorization|proxy-authorization|x-csrf-token)(\s*[:=]\s*)[^\r\n]*"), r"\1\2[redacted]"),
    (re.compile(r"(?i)\b(tendo_session[A-Za-z0-9_-]*)=[^;\s'\"`,)]+"), r"\1=[redacted]"),
    (re.compile(r"(?i)([?&#](?:code|state|nonce|token|id_token|access_token|refresh_token|session_state|client_secret|password)=)[^&#\s'\"`)]+"), r"\1[redacted]"),
    (re.compile(r"(?i)(/invite#)[^\s'\"`)]+"), r"\1[redacted]"),
    (re.compile(r"(?i)\b((?:postgres(?:ql)?|https?)://[^:/@\s'\"`]+:)[^@\s'\"`]+@"), r"\1[redacted]@"),
    (re.compile(r"(?i)\b(password|passwd|secret|setup[_ -]?token|session[_ -]?token|sessionToken)(\s*[:=]\s*)['\"`]?[^\s'\"`,;)]+"), r"\1\2[redacted]"),
)


def redact(text: str) -> str:
    text = ANSI_ESCAPE.sub("", text)
    for value in sorted(SENSITIVE_VALUES, key=len, reverse=True):
        text = text.replace(value, "[redacted]")
    for pattern, replacement in STRUCTURAL_PATTERNS:
        text = pattern.sub(replacement, text)
    return text


def print_failure_log_tail(log_path: Path, lines: int = FAILURE_LOG_TAIL_LINES):
    """CI keeps no runner files; surface Playwright/app output so a failure is diagnosable from the job log."""
    try:
        tail = log_path.read_text(encoding="utf-8", errors="replace").splitlines()[-lines:]
    except OSError as exc:
        print(f"E2E process log unavailable: {exc}", file=sys.stderr)
        return
    print(f"---- last {len(tail)} lines of {log_path} (credentials redacted) ----", file=sys.stderr)
    for line in tail:
        print(redact(line), file=sys.stderr)
    print("---- end of E2E process log ----", file=sys.stderr)


def port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


class Children:
    def __init__(self):
        self.items = []
        self.log = None
        self.lock = threading.RLock()
        self.closed = False
        self.cancelled = False

    def start(self, args, env, label):
        with self.lock:
            if self.cancelled:
                raise KeyboardInterrupt("E2E process supervision cancelled")
            if self.closed:
                raise RuntimeError("cannot start an E2E child after shutdown began")
            log_file = env.get("E2E_LOG_FILE")
            if log_file and self.log is None:
                self.log = open(log_file, "a", encoding="utf-8")
            proc = subprocess.Popen(args, env=env, stdout=self.log, stderr=subprocess.STDOUT if self.log else None, start_new_session=True)
            self.items.append((label, proc))
            return proc

    @staticmethod
    def group_exists(proc):
        # killpg(0) counts zombies; on Linux inspect the kernel's process-group
        # membership and treat a fully-exited/all-zombie group as stopped.
        live = False
        try:
            entries = os.listdir("/proc")
        except OSError:
            try:
                os.killpg(proc.pid, 0)
                return True
            except ProcessLookupError:
                return False
            except PermissionError:
                return True
        for entry in entries:
            if not entry.isdigit():
                continue
            try:
                stat = Path("/proc").joinpath(entry, "stat").read_text()
                fields = stat[stat.rfind(")") + 1:].split()
                state, process_group = fields[0], int(fields[2])
            except (OSError, ValueError, IndexError):
                continue
            if process_group == proc.pid and state != "Z":
                live = True
                break
        return live

    def _retire_if_gone(self, proc):
        if self.group_exists(proc):
            return False
        with self.lock:
            self.items = [(label, item) for label, item in self.items if item is not proc]
        return True

    def signal_group(self, proc, sig):
        # A PGID is only safe to signal while its owned group still has members.
        if not self.group_exists(proc):
            self._retire_if_gone(proc)
            return
        try:
            os.killpg(proc.pid, sig)
        except ProcessLookupError:
            self._retire_if_gone(proc)

    def stop(self, proc, grace=5):
        # Every child starts a new session. Its leader PID is the owned PGID.
        # A reaped leader can still have descendants; only retire after the
        # complete process group is confirmed gone.
        if self.group_exists(proc):
            self.signal_group(proc, signal.SIGTERM)
        else:
            self._retire_if_gone(proc)
        deadline = time.monotonic() + grace
        while time.monotonic() < deadline and self.group_exists(proc):
            if proc.poll() is None:
                try:
                    proc.wait(timeout=0.05)
                except subprocess.TimeoutExpired:
                    pass
            else:
                time.sleep(0.05)
        if self.group_exists(proc):
            self.signal_group(proc, signal.SIGKILL)
        if proc.poll() is None:
            proc.wait(timeout=3)
        deadline = time.monotonic() + 1
        while time.monotonic() < deadline and self.group_exists(proc):
            time.sleep(0.05)
        if self.group_exists(proc):
            raise RuntimeError(f"owned process group {proc.pid} survived TERM/KILL cleanup")
        self._retire_if_gone(proc)

    def run(self, args, env, label, cwd, timeout):
        with self.lock:
            if self.cancelled:
                raise KeyboardInterrupt("E2E process supervision cancelled")
            if self.closed:
                raise RuntimeError("cannot start an E2E child after shutdown began")
            proc = subprocess.Popen(args, cwd=cwd, env=env, stdout=self.log, stderr=subprocess.STDOUT if self.log else None, start_new_session=True)
            self.items.append((label, proc))
        deadline = time.monotonic() + timeout
        try:
            while proc.poll() is None:
                if self.closed or self.cancelled:
                    self.stop(proc)
                    raise KeyboardInterrupt("E2E process supervision cancelled")
                if time.monotonic() >= deadline:
                    self.stop(proc)
                    raise TimeoutError(f"{label} exceeded {timeout}s")
                time.sleep(0.1)
            if self.group_exists(proc):
                self.stop(proc)
            else:
                self._retire_if_gone(proc)
            if proc.returncode:
                raise RuntimeError(f"{label} exited with status {proc.returncode}")
        except BaseException:
            if self.group_exists(proc):
                self.stop(proc)
            else:
                self._retire_if_gone(proc)
            raise

    def close(self):
        with self.lock:
            self.cancelled = True
            self.closed = True
            children = list(reversed(self.items))
        # Signal every still-live owned group at once. Retired registrations
        # are absent, and each signal rechecks membership before using its PGID.
        for _, proc in children:
            if self.group_exists(proc):
                self.signal_group(proc, signal.SIGTERM)
            else:
                self._retire_if_gone(proc)
        deadline = time.monotonic() + 2.5
        while time.monotonic() < deadline and any(self.group_exists(proc) for _, proc in children):
            for _, proc in children:
                if proc.poll() is None:
                    try:
                        proc.wait(timeout=0.01)
                    except subprocess.TimeoutExpired:
                        pass
            time.sleep(0.02)
        for _, proc in children:
            if self.group_exists(proc):
                self.signal_group(proc, signal.SIGKILL)
            else:
                self._retire_if_gone(proc)
            if proc.poll() is None:
                proc.wait(timeout=1)
        deadline = time.monotonic() + 0.5
        while time.monotonic() < deadline and any(self.group_exists(proc) for _, proc in children):
            time.sleep(0.02)
        survivors = [f"{label} process group {proc.pid}" for label, proc in children if self.group_exists(proc)]
        if self.log:
            self.log.close()
            self.log = None
        if survivors:
            raise RuntimeError("owned process groups survived TERM/KILL cleanup: " + ", ".join(survivors))


def wait_http(url, process, children, expected=None, timeout=30):
    deadline = time.monotonic() + timeout
    last = "no response"
    while time.monotonic() < deadline:
        if children.cancelled:
            raise KeyboardInterrupt("E2E startup cancelled")
        if process.poll() is not None:
            raise RuntimeError(f"owned service exited before readiness (exit {process.returncode})")
        try:
            with urllib.request.urlopen(url, timeout=1) as response:
                body = response.read()
                if response.status == 200 and (expected is None or expected(body)):
                    if process.poll() is not None or children.cancelled:
                        raise RuntimeError("owned service exited at readiness boundary")
                    return
                last = f"unexpected HTTP {response.status}"
        except (OSError, urllib.error.URLError) as exc:
            last = str(exc)
        time.sleep(0.1)
    raise RuntimeError(f"service readiness timed out: {last}")


def psql_admin(inherited: dict[str, str], sql: str) -> str:
    """Run one bounded admin query without exposing connection credentials in argv."""
    value = inherited.get("TEST_DATABASE_ADMIN_URL", "")
    parsed = urllib.parse.urlsplit(value)
    if parsed.scheme not in ("postgres", "postgresql") or not parsed.hostname or not parsed.username or not parsed.path.strip("/") or parsed.fragment:
        raise RuntimeError("TEST_DATABASE_ADMIN_URL must be a postgres URL with host/database and no fragment")
    env = {key: val for key, val in inherited.items() if not key.startswith("PG") and key not in {"TEST_DATABASE_ADMIN_URL", "TEST_DATABASE_URL"}}
    env.update({
        "PGHOST": parsed.hostname,
        "PGPORT": str(parsed.port or 5432),
        "PGUSER": urllib.parse.unquote(parsed.username),
        "PGPASSWORD": urllib.parse.unquote(parsed.password or ""),
        "PGDATABASE": urllib.parse.unquote(parsed.path.lstrip("/")),
    })
    allowed = {"sslmode", "sslrootcert", "sslcert", "sslkey", "options", "application_name", "connect_timeout"}
    pairs = urllib.parse.parse_qsl(parsed.query, keep_blank_values=True)
    keys = [key for key, _ in pairs]
    if len(keys) != len(set(keys)) or set(keys) - allowed:
        raise RuntimeError("TEST_DATABASE_ADMIN_URL has duplicate or unsupported query parameters")
    for key, val in pairs:
        env["PG" + key.upper()] = val
    try:
        result = subprocess.run(
            [inherited.get("PSQL", "psql"), "-X", "-At", "-v", "ON_ERROR_STOP=1", "-c", sql],
            text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env, timeout=30,
        )
    except subprocess.TimeoutExpired as exc:
        raise RuntimeError("admin psql query exceeded 30-second deadline") from exc
    if result.returncode:
        raise RuntimeError(f"admin psql query failed with exit status {result.returncode}")
    return result.stdout.strip()


FOREIGN_SEED_EXPECTED = "Foreign cottage|1|false|Foreign gutter check|1|false|open|null|false|0|1|1|0|0"


def assert_foreign_seed_unchanged(inherited: dict[str, str], household_id: str, subject_id: str, item_id: str) -> None:
    for value in (household_id, subject_id, item_id):
        if not re.fullmatch(r"[0-9a-fA-F]{8}-(?:[0-9a-fA-F]{4}-){3}[0-9a-fA-F]{12}", value):
            raise RuntimeError("foreign seed identifiers must be UUIDs")
    h, s, i = household_id, subject_id, item_id
    sql = f"""SELECT concat_ws('|',
      (SELECT name FROM subjects WHERE household_id = '{h}' AND id = '{s}'),
      (SELECT version FROM subjects WHERE household_id = '{h}' AND id = '{s}'),
      (SELECT archived::text FROM subjects WHERE household_id = '{h}' AND id = '{s}'),
      (SELECT title FROM items WHERE household_id = '{h}' AND id = '{i}'),
      (SELECT version FROM items WHERE household_id = '{h}' AND id = '{i}'),
      (SELECT archived::text FROM items WHERE household_id = '{h}' AND id = '{i}'),
      (SELECT workflow_state FROM items WHERE household_id = '{h}' AND id = '{i}'),
      (SELECT coalesce(attention_on::text, 'null') FROM items WHERE household_id = '{h}' AND id = '{i}'),
      (SELECT done::text FROM items WHERE household_id = '{h}' AND id = '{i}'),
      (SELECT count(*) FROM item_completions WHERE household_id = '{h}'),
      (SELECT count(*) FROM subjects WHERE household_id = '{h}'),
      (SELECT count(*) FROM items WHERE household_id = '{h}'),
      (SELECT count(*) FROM household_memberships WHERE household_id = '{h}'),
      (SELECT count(*) FROM household_invitations WHERE household_id = '{h}'))"""
    actual = psql_admin(inherited, sql)
    if actual != FOREIGN_SEED_EXPECTED:
        raise RuntimeError(f"foreign household seed changed during E2E (expected {FOREIGN_SEED_EXPECTED!r}, got {actual!r}); authorization isolation assertion failed")


def run_playwright(root, env, children, *args, timeout=600):
    cmd = [str(root / "frontend/node_modules/.bin/playwright"), "test", *args]
    children.run(cmd, env, "Playwright", root / "frontend", timeout)


def run_screenshot(root: Path, app_path: Path, inherited: dict[str, str], children: Children, artifact_dir: Path, screenshot_path: Path | None = None, audit_dir: Path | None = None):
    env = {key: inherited[key] for key in ("PATH", "HOME", "TMPDIR", "LANG") if key in inherited}
    for key in ("PLAYWRIGHT_BROWSERS_PATH", "CI"):
        if key in inherited:
            env[key] = inherited[key]
    env.setdefault("PLAYWRIGHT_BROWSERS_PATH", str(Path.home() / ".cache/ms-playwright"))
    env.update({
        "TENDO_TEST_CLOCK_ACK": "isolated-e2e-only",
        "TENDO_TEST_CLOCK_NOW": "2026-05-14T08:00:00Z",
        "TENDO_PUBLIC_URL": "http://localhost",
        "TENDO_TRUSTED_PROXY_CIDRS": "",
        "TENDO_DB_TIMEOUT": "2",
        "TENDO_SHUTDOWN_TIMEOUT": "10",
        "TENDO_RESTART_POLICY": "no",
        "E2E_LOG_FILE": str(artifact_dir / "process.log"),
    })
    migration_env = env.copy()
    migration_env["DATABASE_URL"] = inherited["TEST_DATABASE_ADMIN_URL"]
    children.run([str(app_path), "migrate"], migration_env, "production migration", root / "backend", 150)

    host_port = port()
    app_origin = f"http://localhost:{host_port}"
    setup_token = generated_secret(32)
    env.update({
        "DATABASE_URL": inherited["TEST_DATABASE_URL"],
        "TENDO_PUBLIC_URL": app_origin,
        "TENDO_LISTEN_ADDR": f"127.0.0.1:{host_port}",
        "TENDO_SETUP_TOKEN": setup_token,
        "TENDO_OIDC_ISSUER": "",
        "TENDO_OIDC_CLIENT_ID": "",
        "TENDO_OIDC_CLIENT_SECRET": "",
        "TENDO_OIDC_CLIENT_SECRET_FILE": "",
    })
    app = children.start([str(app_path)], env, "Tendo app",)
    wait_http(f"http://127.0.0.1:{host_port}/health/ready", app, children)
    base = env.copy()
    base.update({
        "E2E_BASE_URL": app_origin,
        "E2E_SETUP_TOKEN": setup_token,
        "E2E_OWNER_PASSWORD": generated_secret(24),
        "PLAYWRIGHT_OUTPUT_DIR": str(artifact_dir / "artifacts"),
    })
    if audit_dir is not None:
        base["E2E_AUDIT_DIR"] = str(audit_dir.resolve())
        run_playwright(root, base, children, "e2e/ux-audit.spec.ts", timeout=1800)
    else:
        if screenshot_path is None:
            raise RuntimeError("screenshot path is required for README capture")
        base["E2E_SCREENSHOT_PATH"] = str(screenshot_path.resolve())
        run_playwright(root, base, children, "e2e/readme-screenshot.spec.ts")


def run_suite(root: Path, app_path: Path, provider_path: Path, inherited: dict[str, str], children: Children, artifact_dir: Path):
    env = {key: inherited[key] for key in ("PATH", "HOME", "TMPDIR", "LANG") if key in inherited}
    for key in ("PLAYWRIGHT_BROWSERS_PATH", "CI"):
        if key in inherited:
            env[key] = inherited[key]
    env.setdefault("PLAYWRIGHT_BROWSERS_PATH", str(Path.home() / ".cache/ms-playwright"))
    env.update({
        "TENDO_TEST_CLOCK_ACK": "isolated-e2e-only",
        "TENDO_TEST_CLOCK_NOW": "2026-01-15T22:59:59Z",
        "TENDO_PUBLIC_URL": "http://localhost",
        "TENDO_TRUSTED_PROXY_CIDRS": "",
        "TENDO_DB_TIMEOUT": "2",
        "TENDO_SHUTDOWN_TIMEOUT": "10",
        "TENDO_RESTART_POLICY": "no",
    })
    env["E2E_LOG_FILE"] = str(artifact_dir / "process.log")
    migration_env = env.copy()
    migration_env["DATABASE_URL"] = inherited["TEST_DATABASE_ADMIN_URL"]
    children.run([str(app_path), "migrate"], migration_env, "production migration", root / "backend", 150)
    seed = psql_admin(inherited, "WITH h AS (INSERT INTO households(name, timezone) VALUES ('Foreign household', 'UTC') RETURNING id), s AS (INSERT INTO subjects(household_id, type, name) SELECT id, 'home', 'Foreign cottage' FROM h RETURNING id, household_id), i AS (INSERT INTO items(household_id, subject_id, title) SELECT household_id, id, 'Foreign gutter check' FROM s RETURNING id, household_id, subject_id) SELECT household_id || '|' || subject_id || '|' || id FROM i;")
    foreign_ids = seed.split("|")
    if len(foreign_ids) != 3 or any(not re.fullmatch(r"[0-9a-fA-F]{8}-(?:[0-9a-fA-F]{4}-){3}[0-9a-fA-F]{12}", value) for value in foreign_ids):
        raise RuntimeError("foreign household seed did not return three UUIDs")
    foreign_household_id, foreign_subject_id, foreign_item_id = foreign_ids

    host_port, issuer_port = port(), port()
    app_origin = f"http://localhost:{host_port}"
    issuer = f"http://127.0.0.1:{issuer_port}"
    client, secret = "tendo-oidc-smoke", "tendo-oidc-smoke-secret"
    SENSITIVE_VALUES.add(secret)
    if "INTERRUPT_MARKERS" in inherited:
        env["INTERRUPT_MARKERS"] = inherited["INTERRUPT_MARKERS"]
    env.update({
        "DATABASE_URL": inherited["TEST_DATABASE_URL"],
        "TENDO_PUBLIC_URL": app_origin,
        "TENDO_LISTEN_ADDR": f"127.0.0.1:{host_port}",
        "TENDO_SETUP_TOKEN": generated_secret(32),
        "TENDO_TEST_CLOCK_NOW": "2026-01-15T22:59:59Z",
        "TENDO_TEST_CLOCK_ACK": "isolated-e2e-only",
        "TENDO_OIDC_ISSUER": issuer,
        "TENDO_OIDC_CLIENT_ID": client,
        "TENDO_OIDC_CLIENT_SECRET": secret,
        "TENDO_OIDC_DISPLAY_NAME": "OIDC Smoke Provider",
        "TENDO_OIDC_CLIENT_SECRET_FILE": "",
        "E2E_OIDC_ISSUER": issuer,
    })
    provider_env = {
        "PATH": env.get("PATH", ""),
        "HOME": env.get("HOME", "/tmp"),
        "LISTEN_ADDR": f"127.0.0.1:{issuer_port}",
        "ISSUER_URL": issuer,
        "CLIENT_ID": client,
        "CLIENT_SECRET": secret,
        "REDIRECT_URI": f"{app_origin}/api/v1/auth/oidc/callback",
        "SUBJECTS": "owner-subject,unknown-subject",
        "E2E_LOG_FILE": str(artifact_dir / "process.log"),
    }
    if "INTERRUPT_MARKERS" in inherited:
        provider_env["INTERRUPT_MARKERS"] = inherited["INTERRUPT_MARKERS"]
    provider = children.start([str(provider_path)], provider_env, "OIDC provider")
    wait_http(f"{issuer}/healthz", provider, children)
    app = children.start([str(app_path)], env, "Tendo app")
    wait_http(f"http://127.0.0.1:{host_port}/health/ready", app, children)

    base = env.copy()
    base.update({
        "E2E_BASE_URL": app_origin,
        "E2E_SETUP_TOKEN": env["TENDO_SETUP_TOKEN"],
        "E2E_OWNER_PASSWORD": generated_secret(24),
        "E2E_OIDC_ISSUER": issuer,
        "E2E_CLOCK_NOW": "2026-01-15T22:59:59Z",
        "E2E_CLOCK_FIXTURE_PATH": str(artifact_dir / "attention-clock.json"),
        "PLAYWRIGHT_OUTPUT_DIR": str(artifact_dir / "artifacts"),
        "E2E_FOREIGN_HOUSEHOLD_ID": foreign_household_id,
        "E2E_FOREIGN_SUBJECT_ID": foreign_subject_id,
        "E2E_FOREIGN_ITEM_ID": foreign_item_id,
    })
    run_playwright(root, base, children)
    before = base.copy()
    before.update({"E2E_CLOCK_PHASE": "before", "E2E_CLOCK_NOW": "2026-01-15T22:59:59Z"})
    run_playwright(root, before, children, "e2e/attention-clock.spec.ts")
    children.stop(app)
    env["TENDO_TEST_CLOCK_NOW"] = "2026-01-15T23:00:00Z"
    app = children.start([str(app_path)], env, "Tendo app after midnight")
    wait_http(f"http://127.0.0.1:{host_port}/health/ready", app, children)
    after = base.copy()
    after.update({"E2E_CLOCK_PHASE": "after", "E2E_CLOCK_NOW": "2026-01-15T23:00:00Z"})
    run_playwright(root, after, children, "e2e/attention-clock.spec.ts")
    assert_foreign_seed_unchanged(inherited, foreign_household_id, foreign_subject_id, foreign_item_id)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", type=Path, required=True)
    parser.add_argument("--app", type=Path, required=True)
    parser.add_argument("--provider", type=Path, required=True)
    parser.add_argument("--screenshot", type=Path)
    parser.add_argument("--audit-dir", type=Path)
    args = parser.parse_args()
    if args.screenshot and args.audit_dir:
        parser.error("--screenshot and --audit-dir are mutually exclusive")
    root = args.root.resolve()
    for key in ("TEST_DATABASE_ADMIN_URL", "TEST_DATABASE_URL", "TENDO_TEST_POSTGRES_ADMIN_URL"):
        register_database_url(os.environ.get(key, ""))
    children = Children()
    artifact_dir = root / "frontend/test-results" / f"native-{uuid.uuid4().hex}"
    artifact_dir.mkdir(parents=True, exist_ok=False)
    signal_seen = None
    previous_handlers = {}
    result_error = None
    cleanup_error_seen = None
    def handle_signal(signum, _frame):
        nonlocal signal_seen
        signal_seen = signum
        children.cancelled = True
    for signum in (signal.SIGTERM, signal.SIGINT):
        previous_handlers[signum] = signal.signal(signum, handle_signal)
    try:
        if args.screenshot or args.audit_dir:
            run_screenshot(root, args.app.resolve(), os.environ.copy(), children, artifact_dir, args.screenshot, args.audit_dir)
            if args.audit_dir:
                print(f"UX audit artifacts saved at {args.audit_dir.resolve()}")
            else:
                print(f"README screenshot saved at {args.screenshot.resolve()}")
        else:
            run_suite(root, args.app.resolve(), args.provider.resolve(), os.environ.copy(), children, artifact_dir)
            print(f"E2E artifacts retained at {artifact_dir}")
            print("Native production-binary E2E passed.")
    except BaseException as exc:
        result_error = exc
        raise
    finally:
        try:
            children.close()
        except BaseException as cleanup_error:
            cleanup_error_seen = cleanup_error
            if result_error is not None:
                print(f"secondary E2E child cleanup failure: {cleanup_error}", file=sys.stderr)
        finally:
            for signum, previous in previous_handlers.items():
                signal.signal(signum, previous)
        # Execution, cleanup, and cancellation failures all surface the redacted log before exiting nonzero.
        if result_error is not None or cleanup_error_seen is not None or signal_seen is not None:
            print_failure_log_tail(artifact_dir / "process.log")
            print(f"E2E failure artifacts retained at {artifact_dir}", file=sys.stderr)
        if result_error is None and cleanup_error_seen is not None:
            raise cleanup_error_seen
        if signal_seen is not None:
            # raise after cleanup rather than returning success on cancellation
            raise SystemExit(128 + signal_seen)


if __name__ == "__main__":
    try:
        main()
    except BaseException as exc:
        print(f"native E2E failed: {exc}", file=sys.stderr)
        raise SystemExit(1)
