#!/usr/bin/env python3
"""Run native production-binary Playwright E2E against isolated PostgreSQL."""
from __future__ import annotations

import argparse
import contextlib
import os
import shutil
import signal
import socket
import subprocess
import sys
import time
import uuid
from pathlib import Path

SCRIPTS_DIR = str(Path(__file__).resolve().parent)
if SCRIPTS_DIR not in sys.path:
    sys.path.insert(0, SCRIPTS_DIR)

import e2e_db_fixtures as db_fixtures
import e2e_native_diagnostics as diagnostics
import e2e_orca_session as orca_session

SENSITIVE_VALUES = diagnostics.SENSITIVE_VALUES
EXPECTED_DIAGNOSTICS_FINGERPRINT = diagnostics.EXPECTED_DIAGNOSTICS_FINGERPRINT
register_sensitive_value = diagnostics.register_sensitive_value
generated_secret = diagnostics.generated_secret
register_database_url = diagnostics.register_database_url
CANCELLED = False
CANCEL_STARTED: float | None = None
LIVE_TREES: list["ChildTree"] = []
DEFERRED = 0
STOP_GRACE = 20
# The PostgreSQL wrapper SIGKILLs the runner 25 s after forwarding a cancellation. Stopping children (at most
# CANCEL_STOP_GRACE, then KILL_BUDGET of SIGKILL) and staging diagnostics finish within CANCEL_TOTAL_BUDGET of it.
CANCEL_STOP_GRACE = 6
KILL_BUDGET = 2
CANCEL_TOTAL_BUDGET = 22


@contextlib.contextmanager
def deferred_cancellation():
    """Inside this block a cancellation is only recorded; it never interrupts the code with KeyboardInterrupt."""
    global DEFERRED
    DEFERRED += 1
    try:
        yield
    finally:
        DEFERRED -= 1


def port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


def process_table():
    """pid -> (state, parent pid, process group) for every visible process."""
    table = {}
    for entry in os.scandir("/proc"):
        if not entry.name.isdigit():
            continue
        try:
            stat = Path(entry.path, "stat").read_text()
        except OSError:
            continue
        fields = stat[stat.rindex(")") + 2:].split()
        table[int(entry.name)] = (fields[0], int(fields[1]), int(fields[2]))
    return table


def signal_group(pgid, signum):
    try:
        os.killpg(pgid, signum)
    except (ProcessLookupError, PermissionError):
        pass


class ChildTree:
    """A child in its own session plus every process group its descendants open. Playwright starts web servers and
    browsers detached, so they would outlive a Playwright that dies or ignores its stop signal unless their groups
    are recorded and stopped too. Every started tree stays in LIVE_TREES until stopped; main stops leftovers."""

    def __init__(self, args, env, cwd, log, stop_signal):
        if CANCELLED:
            raise KeyboardInterrupt("E2E run cancelled")
        self.stop_signal = stop_signal
        with deferred_cancellation():
            self.proc = subprocess.Popen(args, cwd=cwd, env=env, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
            self.groups = {self.proc.pid}
            LIVE_TREES.append(self)

    def live_groups(self):
        """Record the groups of current descendants while the parent links exist, then return the recorded groups
        that still have a runnable member. An emptied group is forgotten, so a reused group ID is never signalled."""
        table = process_table()
        if self.proc.poll() is None:
            children = {}
            for pid, (_, parent, _) in table.items():
                children.setdefault(parent, []).append(pid)
            pending = [self.proc.pid]
            while pending:
                for pid in children.get(pending.pop(), ()):
                    self.groups.add(table[pid][2])
                    pending.append(pid)
        live = {pgid for state, _, pgid in table.values() if pgid in self.groups and state not in ("Z", "X")}
        self.groups = live | ({self.proc.pid} if self.proc.returncode is None else set())
        return live

    def wait(self, label, timeout):
        deadline = time.monotonic() + timeout
        try:
            while self.proc.poll() is None:
                if CANCELLED:
                    raise KeyboardInterrupt(f"{label} cancelled")
                if time.monotonic() >= deadline:
                    raise TimeoutError(f"{label} exceeded {timeout}s")
                self.live_groups()
                time.sleep(0.5)
        finally:
            self.stop()
        return self.proc.returncode

    def stop(self):
        """Send the stop signal once, wait for the leader and every recorded group, then SIGKILL survivors. After a
        cancellation the graceful wait ends CANCEL_STOP_GRACE after the signal (at least one second)."""
        if self.proc.poll() is None:
            signal_group(self.proc.pid, self.stop_signal)
        started = time.monotonic()
        orphans_signalled = False
        while (live := self.live_groups()) or self.proc.poll() is None:
            if self.proc.returncode is not None and not orphans_signalled:
                for pgid in live:  # the leader is gone, so nothing else will stop the groups it opened
                    signal_group(pgid, signal.SIGTERM)
                orphans_signalled = True
            grace = STOP_GRACE if CANCEL_STARTED is None else max(1.0, CANCEL_STARTED + CANCEL_STOP_GRACE - started)
            if time.monotonic() - started >= grace:
                break
            time.sleep(0.05)
        kill_deadline = time.monotonic() + KILL_BUDGET
        while (live := self.live_groups()) or self.proc.poll() is None:
            if time.monotonic() >= kill_deadline:
                print(f"E2E process groups {sorted(live)} survived SIGKILL", file=sys.stderr)
                break
            for pgid in live:
                signal_group(pgid, signal.SIGKILL)
            time.sleep(0.01)
        if self in LIVE_TREES:
            LIVE_TREES.remove(self)


def run_process(args, env, label, cwd, log, timeout, stop_signal=signal.SIGTERM):
    result = ChildTree(args, env, cwd, log, stop_signal).wait(label, timeout)
    if result:
        raise RuntimeError(f"{label} exited with status {result}")


def playwright(root, env, log, project, timeout=600):
    report_dir = Path(env.get("E2E_ARTIFACT_DIR", root / "frontend/test-results"))
    invocation = uuid.uuid4().hex
    env["PLAYWRIGHT_HTML_OUTPUT_DIR"] = str(report_dir / f"html-report-{invocation}")
    env["PLAYWRIGHT_JSON_OUTPUT_FILE"] = str(report_dir / f"report-{invocation}.json")
    cmd = [str(root / "frontend/node_modules/.bin/playwright"), "test", f"--project={project}"]
    # Playwright stops its webServer processes only on SIGINT or a normal exit; SIGTERM kills Node without cleanup.
    run_process(cmd, env, "Playwright", root / "frontend", log, timeout, signal.SIGINT)


def process_env(inherited, clock):
    env = {key: inherited[key] for key in ("PATH", "HOME", "TMPDIR", "LANG", "CI", "PLAYWRIGHT_BROWSERS_PATH", "TENDO_E2E_READ_DELAY_MS") if key in inherited}
    env.setdefault("PLAYWRIGHT_BROWSERS_PATH", str(Path.home() / ".cache/ms-playwright"))
    env.update({
        "TENDO_TEST_CLOCK_ACK": "isolated-e2e-only",
        "TENDO_TEST_CLOCK_NOW": clock,
        "TENDO_PUBLIC_URL": "http://localhost",
        "TENDO_TRUSTED_PROXY_CIDRS": "",
        "TENDO_DB_TIMEOUT": "2",
        "TENDO_SHUTDOWN_TIMEOUT": "10",
    })
    return env


def run_migration(root, app_path, inherited, env, log):
    migration_env = env.copy()
    migration_env["DATABASE_URL"] = inherited["TEST_DATABASE_ADMIN_URL"]
    run_process([str(app_path), "migrate"], migration_env, "production migration", root / "backend", log, 150)


def run_screen_reader(root, env, log, audit_dir):
    audit_dir = audit_dir.resolve()
    audit_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
    os.chmod(audit_dir, 0o700)
    for name in ("transcript.json", "transcript.md", "manifest.json", "orca-debug.out"):
        (audit_dir / name).unlink(missing_ok=True)
    if os.environ.get("TENDO_SR_ORCA_NO_START") == "1":
        raise RuntimeError("Orca is required for screen-reader audit but TENDO_SR_ORCA_NO_START=1 disabled it")
    runtime = audit_dir / "runtime"
    debug = audit_dir / "orca-debug.out"
    if runtime.is_symlink() or (runtime.exists() and not runtime.is_dir()):
        raise RuntimeError(f"screen-reader runtime path is not a real directory: {runtime}")
    if runtime.exists():
        shutil.rmtree(runtime)
    runtime.mkdir(mode=0o700)
    os.chmod(runtime, 0o700)
    env["E2E_SR_RUN_DIR"] = str(runtime)
    env["E2E_SR_DEBUG_FILE"] = str(debug)
    with debug.open("w", encoding="utf-8"):
        pass
    os.chmod(debug, 0o600)
    logs = runtime / "logs"
    session = root / "scripts/sr-orca-session.sh"
    readiness_seconds = orca_session.readiness_timeout(os.environ)
    env["TENDO_SR_READY_TIMEOUT_SECONDS"] = str(readiness_seconds)
    session_tree = ChildTree([str(session), str(runtime), str(debug), str(logs)], env, None, log, signal.SIGTERM)
    try:
        try:
            orca_session.wait_until_ready(session_tree.proc, logs / "ready", readiness_seconds + 15, lambda: CANCELLED)
            bus_address = orca_session.validate_bus_address((logs / "bus-address").read_text(encoding="utf-8").removesuffix("\n"))
            env.update({
                "HOME": str(runtime / "home"), "XDG_RUNTIME_DIR": str(runtime / "xdg-runtime"),
                "XDG_CONFIG_HOME": str(runtime / "xdg-config"), "XDG_DATA_HOME": str(runtime / "xdg-data"),
                "TENDO_SR_ORCA_NO_START": os.environ.get("TENDO_SR_ORCA_NO_START", "0"),
                "DISPLAY": (runtime / "display").read_text(encoding="utf-8").strip(),
                "DBUS_SESSION_BUS_ADDRESS": bus_address, "E2E_SR_AUDIT_DIR": str(audit_dir),
                "GIT_COMMIT": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip(),
                "GIT_DIRTY": "true" if subprocess.check_output(["git", "status", "--porcelain"], cwd=root, text=True).strip() else "false",
            })
            orca_session.verify_transport(env)
            playwright(root, env, log, "screen-reader", 1800)
        finally:
            session_tree.stop()
    finally:
        with deferred_cancellation():
            orca_session.cleanup_private_runtime(runtime)


def run_opt_in(root, app_path, inherited, log, artifact_dir, screenshot_path=None, audit_dir=None, sr_audit_dir=None, visual=False, design_capture_dir=None):
    env = process_env(inherited, "2026-05-14T08:00:00Z")
    env.update({"DATABASE_URL": inherited["TEST_DATABASE_URL"], "TENDO_TEST_CLOCK_NOW": "2026-05-14T08:00:00Z"})
    run_migration(root, app_path, inherited, env, log)
    app_port = port()
    app_origin = f"http://localhost:{app_port}"
    setup_token = generated_secret(32)
    SENSITIVE_VALUES.add(setup_token)
    env.update({
        "TENDO_TEST_CLOCK_NOW": "2026-05-14T08:00:00Z", "TENDO_PUBLIC_URL": app_origin,
        "TENDO_LISTEN_ADDR": f"127.0.0.1:{app_port}",
        "TENDO_SETUP_TOKEN": setup_token, "TENDO_OIDC_ISSUER": "", "TENDO_OIDC_CLIENT_ID": "",
        "TENDO_OIDC_CLIENT_SECRET": "", "TENDO_OIDC_CLIENT_SECRET_FILE": "",
        "E2E_BASE_URL": app_origin, "E2E_APP_BINARY": str(app_path), "E2E_APP_PORT": str(app_port),
        "E2E_SETUP_TOKEN": setup_token, "E2E_OWNER_PASSWORD": generated_secret(24),
        "PLAYWRIGHT_OUTPUT_DIR": str(artifact_dir / "artifacts"), "E2E_ARTIFACT_DIR": str(artifact_dir),
    })
    if sr_audit_dir is not None:
        sr_audit_dir = sr_audit_dir.resolve()
        env.update({"E2E_SR_AUDIT_DIR": str(sr_audit_dir), "E2E_SR_DEBUG_FILE": str(sr_audit_dir / "orca-debug.out"), "E2E_SR_RUN_DIR": str(sr_audit_dir / "runtime"), "E2E_TEST_CLOCK_NOW": env["TENDO_TEST_CLOCK_NOW"], "TENDO_SR_AUDIT_DIR": str(sr_audit_dir)})
        command = ["bash", "scripts/dev-exec.sh", "--", "bash", "scripts/e2e-smoke.sh"]
        env["TENDO_SR_COMMAND"] = " ".join(f"{key}={value}" for key, value in sorted(os.environ.items()) if key in {"TENDO_SR_AUDIT_DIR", "TENDO_SR_ITEM_PROBE"} and value) + " " + " ".join(command)
        env["TENDO_SR_COMMAND"] = env["TENDO_SR_COMMAND"].strip()
        run_screen_reader(root, env, log, sr_audit_dir)
    elif audit_dir is not None:
        env["E2E_AUDIT_DIR"] = str(audit_dir.resolve())
        playwright(root, env, log, "ux-audit", 3600)
    elif design_capture_dir is not None:
        env["E2E_DESIGN_CAPTURE_DIR"] = str(design_capture_dir.resolve())
        playwright(root, env, log, "design-capture", 3600)
    elif visual:
        env["TENDO_VISUAL_MODE"] = "1"
        env["TENDO_VISUAL_UPDATE"] = os.environ.get("TENDO_VISUAL_UPDATE", "0")
        env["FONTCONFIG_FILE"] = str(root / "frontend/e2e/visual/fonts/fonts.conf")
        env["FC_CONFIG_DIR"] = str(root / "frontend/e2e/visual/fonts")
        playwright(root, env, log, "visual", 3600)
    else:
        if screenshot_path is None:
            raise RuntimeError("screenshot path is required for README capture")
        env["E2E_SCREENSHOT_PATH"] = str(screenshot_path.resolve())
        playwright(root, env, log, "readme-screenshot")


def run_suite(root, app_path, provider_path, inherited, log, artifact_dir):
    env = process_env(inherited, "2026-01-15T22:59:59Z")
    run_migration(root, app_path, inherited, env, log)
    fixture_env = db_fixtures.seed_suite_fixtures(inherited, register_sensitive_value)
    # After a cancellation the wrapper drops the whole isolated database, so the fixture cleanup is skipped.
    with db_fixtures.large_paging_household_cleanup(inherited, fixture_env["E2E_LARGE_HOUSEHOLD_ID"], lambda: CANCELLED):
        run_seeded_suite(root, app_path, provider_path, inherited, log, artifact_dir, env, fixture_env)


def run_seeded_suite(root, app_path, provider_path, inherited, log, artifact_dir, env, fixture_env):
    host_port, issuer_port = port(), port()
    app_origin = f"http://localhost:{host_port}"
    issuer = f"http://127.0.0.1:{issuer_port}"
    client, secret = "tendo-oidc-smoke", "tendo-oidc-smoke-secret"
    SENSITIVE_VALUES.add(secret)
    env.update({
        "DATABASE_URL": inherited["TEST_DATABASE_URL"], "TENDO_PUBLIC_URL": app_origin,
        "TENDO_LISTEN_ADDR": f"127.0.0.1:{host_port}", "TENDO_SETUP_TOKEN": generated_secret(32),
        "TENDO_TEST_CLOCK_NOW": "2026-01-15T22:59:59Z", "TENDO_OIDC_ISSUER": issuer,
        "TENDO_OIDC_CLIENT_ID": client, "TENDO_OIDC_CLIENT_SECRET": secret,
        "TENDO_OIDC_DISPLAY_NAME": "OIDC Smoke Provider", "TENDO_OIDC_CLIENT_SECRET_FILE": "",
        "E2E_OIDC_ISSUER": issuer, "E2E_BASE_URL": app_origin,
        "E2E_APP_BINARY": str(app_path), "E2E_APP_PORT": str(host_port),
        "E2E_OIDC_PROVIDER_BINARY": str(provider_path), "E2E_OIDC_LISTEN_ADDR": f"127.0.0.1:{issuer_port}",
        "E2E_OIDC_CLIENT_ID": client, "E2E_OIDC_CLIENT_SECRET": secret,
        "E2E_OIDC_REDIRECT_URI": f"{app_origin}/api/v1/auth/oidc/callback",
        "E2E_OIDC_SUBJECTS": "owner-subject,unknown-subject,second-unknown-subject,invited-subject",
        "E2E_OIDC_SUBJECT_EMAILS": "owner-subject=owner@example.test:true,second-unknown-subject=owner@example.test:true,unknown-subject=owner@example.test:string",
    })
    base = env.copy()
    base.update({
        "E2E_SETUP_TOKEN": env["TENDO_SETUP_TOKEN"], "E2E_OWNER_PASSWORD": generated_secret(24),
        "E2E_CLOCK_NOW": "2026-01-15T22:59:59Z", "E2E_CLOCK_FIXTURE_PATH": str(artifact_dir / "attention-clock.json"),
        "PLAYWRIGHT_OUTPUT_DIR": str(artifact_dir / "artifacts"), "E2E_ARTIFACT_DIR": str(artifact_dir), **fixture_env,
    })
    playwright(root, base, log, "e2e")
    playwright(root, base, log, "cross-browser-firefox", 600)
    playwright(root, base, log, "cross-browser-webkit", 600)
    before = base.copy()
    before.update({"E2E_CLOCK_PHASE": "before", "E2E_CLOCK_NOW": "2026-01-15T22:59:59Z"})
    playwright(root, before, log, "clock")
    # A new Playwright invocation starts a new app process on the same database after midnight.
    after = base.copy()
    after.update({"TENDO_TEST_CLOCK_NOW": "2026-01-15T23:00:00Z", "E2E_CLOCK_PHASE": "after", "E2E_CLOCK_NOW": "2026-01-15T23:00:00Z"})
    playwright(root, after, log, "clock")
    db_fixtures.verify_suite_fixtures(inherited, fixture_env)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", type=Path, required=True)
    parser.add_argument("--app", type=Path, required=True)
    parser.add_argument("--provider", type=Path, required=True)
    parser.add_argument("--screenshot", type=Path)
    parser.add_argument("--audit-dir", type=Path)
    parser.add_argument("--sr-audit-dir", type=Path)
    parser.add_argument("--design-capture-dir", type=Path)
    parser.add_argument("--visual", action="store_true")
    args = parser.parse_args()
    if sum(value is not None for value in (args.screenshot, args.audit_dir, args.sr_audit_dir, args.design_capture_dir)) + int(args.visual) > 1:
        parser.error("opt-in modes are mutually exclusive")
    root = args.root.resolve()
    for key in ("TEST_DATABASE_ADMIN_URL", "TEST_DATABASE_URL", "TENDO_TEST_POSTGRES_ADMIN_URL"):
        register_database_url(os.environ.get(key, ""))
    test_results_dir = root / "frontend/test-results"
    test_results_dir.mkdir(parents=True, exist_ok=True)
    upload_destination = Path(os.environ["E2E_CI_UPLOAD_DIR"]) if os.environ.get("E2E_CI_UPLOAD_DIR") else None
    recipient_file = Path(os.environ.get("E2E_DIAGNOSTICS_RECIPIENT", str(root / "ci/e2e-artifact-recipient.asc")))
    if upload_destination is not None:
        diagnostics.validate_upload_destination(upload_destination)
        if not shutil.which("gpg") or not diagnostics.verify_diagnostics_recipient(recipient_file, EXPECTED_DIAGNOSTICS_FINGERPRINT):
            print("Encrypted E2E diagnostics preflight failed; failure marker will be staged.", file=sys.stderr)
    artifact_dir = test_results_dir / f"native-{uuid.uuid4().hex}"
    artifact_dir.mkdir(parents=True, exist_ok=False)
    diagnostics._cancel_deadline = None
    signal_seen = None
    previous_handlers = {}
    result_error = None
    def handle_signal(signum, _frame):
        # While a child runs, its wait loop polls CANCELLED and stops it within budget; other runner work (database
        # fixtures) is interrupted directly. Repeated signals and cleanup paths are never interrupted.
        global CANCELLED, CANCEL_STARTED
        nonlocal signal_seen
        signal_seen = signum
        if CANCELLED:
            return
        CANCELLED, CANCEL_STARTED = True, time.monotonic()
        diagnostics._cancel_deadline = CANCEL_STARTED + CANCEL_TOTAL_BUDGET
        if not LIVE_TREES and not DEFERRED:
            raise KeyboardInterrupt("E2E run cancelled")
    for signum in (signal.SIGTERM, signal.SIGINT):
        previous_handlers[signum] = signal.signal(signum, handle_signal)
    try:
        with (artifact_dir / "process.log").open("a", encoding="utf-8") as log:
            try:
                if args.screenshot or args.audit_dir or args.sr_audit_dir or args.design_capture_dir or args.visual:
                    run_opt_in(root, args.app.resolve(), os.environ.copy(), log, artifact_dir, args.screenshot, args.audit_dir, args.sr_audit_dir, args.visual, args.design_capture_dir)
                    if args.sr_audit_dir:
                        print(f"Screen-reader audit artifacts saved at {args.sr_audit_dir.resolve()}")
                    elif args.audit_dir:
                        print(f"UX audit artifacts saved at {args.audit_dir.resolve()}")
                    elif args.design_capture_dir:
                        print(f"Design capture artifacts saved at {args.design_capture_dir.resolve()}")
                    elif args.visual:
                        print("Visual regression suite passed.")
                    else:
                        print(f"README screenshot saved at {args.screenshot.resolve()}")
                else:
                    run_suite(root, args.app.resolve(), args.provider.resolve(), os.environ.copy(), log, artifact_dir)
                    print(f"E2E artifacts retained at {artifact_dir}")
                    print("Native production-binary E2E passed.")
            except BaseException as exc:
                result_error = exc
                raise
            finally:
                with deferred_cancellation():
                    for tree in list(LIVE_TREES):
                        tree.stop()
                    if result_error is not None or signal_seen is not None:
                        diagnostics.write_redacted_process_log(artifact_dir / "process.log")
                        if upload_destination is not None:
                            diagnostics.stage_ci_upload(upload_destination, artifact_dir)
                        diagnostics.print_failure_log_tail(artifact_dir / "process.log")
                        print(f"E2E failure artifacts retained at {artifact_dir}", file=sys.stderr)
                if signal_seen is not None:
                    raise SystemExit(128 + signal_seen)
    finally:
        for signum, previous in previous_handlers.items():
            signal.signal(signum, previous)
        diagnostics._cancel_deadline = None


if __name__ == "__main__":
    try:
        main()
    except BaseException as exc:
        print(f"native E2E failed: {exc}", file=sys.stderr)
        raise SystemExit(1)
