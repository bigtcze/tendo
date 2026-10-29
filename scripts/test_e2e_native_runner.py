import base64
import contextlib
import io
import json
import importlib.util
import pathlib
import signal
import subprocess
import os
import sys
import tempfile
import threading
import time
import unittest
from unittest import mock

MODULE = pathlib.Path(__file__).with_name("e2e-native-runner.py")
spec = importlib.util.spec_from_file_location("e2e_native_runner", MODULE)
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class AdminPsqlTests(unittest.TestCase):
    def test_admin_credentials_are_environment_only_and_url_options_are_parsed(self):
        inherited = {
            "PATH": "/bin", "HOME": "/home/e2e", "TEST_DATABASE_ADMIN_URL":
                "postgresql://migrator:p%40ss%3Aword@db.example:5544/testdb?sslmode=verify-full&sslrootcert=%2Fcerts%2Froot.pem",
            "PSQL": "/custom/psql",
        }
        completed = subprocess.CompletedProcess([], 0, "ok\n", "")
        with mock.patch.object(runner.db_fixtures.subprocess, "run", return_value=completed) as run:
            self.assertEqual(runner.db_fixtures.psql_admin(inherited, "SELECT 1"), "ok")
        args = run.call_args.args[0]
        env = run.call_args.kwargs["env"]
        self.assertEqual(args[0], "/custom/psql")
        self.assertNotIn(inherited["TEST_DATABASE_ADMIN_URL"], args)
        self.assertNotIn("p@ss:word", args)
        self.assertEqual(args, ["/custom/psql", "-X", "-At", "-v", "ON_ERROR_STOP=1", "-c", "SELECT 1"])
        self.assertEqual({key: env[key] for key in ("PATH", "HOME", "PGHOST", "PGPORT", "PGUSER", "PGPASSWORD", "PGDATABASE", "PGSSLMODE", "PGSSLROOTCERT")}, {
            "PATH": "/bin", "HOME": "/home/e2e", "PGHOST": "db.example", "PGPORT": "5544",
            "PGUSER": "migrator", "PGPASSWORD": "p@ss:word", "PGDATABASE": "testdb",
            "PGSSLMODE": "verify-full", "PGSSLROOTCERT": "/certs/root.pem",
        })
        self.assertEqual(run.call_args.kwargs["timeout"], 30)

    def test_admin_psql_failure_does_not_echo_password(self):
        password = "secret-without-leak"
        inherited = {"TEST_DATABASE_ADMIN_URL": f"postgresql://migrator:{password}@localhost/testdb"}
        completed = subprocess.CompletedProcess([], 2, "", f"connection failed for {password}")
        with mock.patch.object(runner.db_fixtures.subprocess, "run", return_value=completed) as run:
            with self.assertRaises(RuntimeError) as raised:
                runner.db_fixtures.psql_admin(inherited, "SELECT 1")
        self.assertNotIn(password, str(raised.exception))
        self.assertNotIn(password, run.call_args.args[0])


class ChildProcessTests(unittest.TestCase):
    def test_default_playwright_discovery_excludes_screen_reader_spec_without_sr_env(self):
        root = pathlib.Path(__file__).resolve().parents[1]
        env = os.environ.copy()
        env.pop("E2E_SR_AUDIT_DIR", None)
        env.pop("E2E_SR_DEBUG_FILE", None)
        env.update({"E2E_BASE_URL": "http://127.0.0.1:1", "E2E_SETUP_TOKEN": "test-token", "E2E_OWNER_PASSWORD": "test-password", "E2E_EXISTING_PASSWORD": "existing-password", "E2E_CONFLICT_HOUSEHOLD_ID": "01890000-0000-7000-8000-000000000004", "E2E_LARGE_HOUSEHOLD_ID": "01890000-0000-7000-8000-000000000005", "E2E_LARGE_OWNER_LOGIN": "large_paging_owner_e2e", "E2E_CLOCK_NOW": "2026-05-14T08:00:00Z", "E2E_FOREIGN_HOUSEHOLD_ID": "01890000-0000-7000-8000-000000000001", "E2E_FOREIGN_SUBJECT_ID": "01890000-0000-7000-8000-000000000002", "E2E_FOREIGN_ITEM_ID": "01890000-0000-7000-8000-000000000003"})
        result = subprocess.run(
            [str(root / "frontend/node_modules/.bin/playwright"), "test", "--list"],
            cwd=root / "frontend", env=env, text=True, capture_output=True, timeout=30,
        )
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertNotIn("screen-reader.spec.ts", result.stdout + result.stderr)

    def test_cleanup_terminates_only_owned_process_group_and_reaps_ignoring_child(self):
        with tempfile.TemporaryDirectory() as tmp:
            pidfile = pathlib.Path(tmp) / "child.pid"
            child = runner.Children()
            proc = child.start([sys.executable, "-c", f"import signal,time,pathlib; signal.signal(signal.SIGTERM, signal.SIG_IGN); pathlib.Path({str(pidfile)!r}).write_text(str(__import__('os').getpid())); time.sleep(60)"], {}, "ignore-term")
            deadline = time.monotonic() + 3
            while not pidfile.exists() and time.monotonic() < deadline:
                time.sleep(.01)
            self.assertTrue(pidfile.exists())
            child.close()
            self.assertIsNotNone(proc.poll())

    def test_stop_kills_descendant_group_after_leader_exits(self):
        with tempfile.TemporaryDirectory() as tmp:
            pidfile = pathlib.Path(tmp) / "descendant.pid"
            child = runner.Children()
            proc = child.start([sys.executable, "-c", f"import subprocess,sys,time,pathlib; p=subprocess.Popen([sys.executable,'-c',\"import signal,time,os; signal.signal(signal.SIGTERM,signal.SIG_IGN); print(os.getpid(),flush=True); time.sleep(60)\"],stdout=subprocess.PIPE,text=True,start_new_session=False); pathlib.Path({str(pidfile)!r}).write_text(p.stdout.readline().strip()); raise SystemExit(0)"], {}, "leader-exits")
            proc.wait(timeout=3)
            descendant = int(pidfile.read_text())
            child.close()
            try:
                os.kill(descendant, 0)
            except ProcessLookupError:
                return
            stat = pathlib.Path(f"/proc/{descendant}/stat")
            self.assertTrue(not stat.exists() or stat.read_text().split()[2] == "Z")

    def test_term_ignoring_child_is_killed_after_bounded_grace(self):
        with tempfile.TemporaryDirectory() as tmp:
            marker = pathlib.Path(tmp) / "terminated"
            children = runner.Children()
            proc = children.start([sys.executable, "-c", f"import signal,time,pathlib; signal.signal(signal.SIGTERM, signal.SIG_IGN); time.sleep(30)"], {}, "ignore-term")
            started = time.monotonic()
            children.close()
            self.assertIsNotNone(proc.poll())
            self.assertLess(time.monotonic() - started, 8)

    def test_playwright_browser_path_and_ci_are_explicitly_preserved(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            env = {"PATH": "/bin", "PLAYWRIGHT_BROWSERS_PATH": str(root / "browser-cache"), "CI": "true", "E2E_LOG_FILE": str(root / "e2e.log")}
            children = runner.Children()
            output = root / "e2e.log"
            output.write_text("")
            with mock.patch.object(runner.subprocess, "Popen") as popen:
                fake = mock.Mock(pid=123, returncode=0)
                fake.poll.side_effect = [None, 0, 0]
                popen.return_value = fake
                with mock.patch.object(runner.time, "sleep"):
                    children.run(["playwright"], env, "playwright", root, 1)
                child_env = popen.call_args.kwargs["env"]
                self.assertEqual(child_env["PLAYWRIGHT_BROWSERS_PATH"], env["PLAYWRIGHT_BROWSERS_PATH"])
                self.assertEqual(child_env["CI"], "true")
                self.assertEqual(popen.call_args.kwargs["start_new_session"], True)
                self.assertEqual(children.items, [])

    def test_completed_run_registration_is_retired_before_close(self):
        children = runner.Children()
        proc = mock.Mock(pid=123, returncode=0)
        proc.poll.side_effect = [None, 0]
        with mock.patch.object(runner.subprocess, "Popen", return_value=proc), \
             mock.patch.object(children, "group_exists", return_value=False), \
             mock.patch.object(runner.os, "killpg") as killpg, \
             mock.patch.object(runner.time, "sleep"):
            children.run(["done"], {}, "completed", pathlib.Path("."), 1)
            self.assertEqual(children.items, [])
            children.close()
        killpg.assert_not_called()

    def test_stopped_registration_is_not_signalled_again_by_close(self):
        children = runner.Children()
        proc = mock.Mock(pid=123, returncode=0)
        children.items.append(("stopped", proc))
        with mock.patch.object(children, "group_exists", side_effect=[True, True, False, False, False, False]), \
             mock.patch.object(runner.os, "killpg") as killpg, \
             mock.patch.object(runner.time, "monotonic", return_value=0), \
             mock.patch.object(runner.time, "sleep"): 
            children.stop(proc, grace=0)
            self.assertEqual(children.items, [])
            children.close()
        killpg.assert_called_once_with(proc.pid, signal.SIGTERM)

    def test_close_signals_group_when_leader_exited_but_descendant_remains(self):
        children = runner.Children()
        proc = mock.Mock(pid=123, returncode=0)
        children.items.append(("leader-exited", proc))
        with mock.patch.object(children, "group_exists", side_effect=[True, True, True, True, False]), \
             mock.patch.object(runner.os, "killpg") as killpg, \
             mock.patch.object(runner.time, "monotonic", side_effect=[0, 10, 10, 11]), \
             mock.patch.object(runner.time, "sleep"): 
            children.close()
        self.assertEqual(killpg.call_args_list, [
            mock.call(proc.pid, signal.SIGTERM),
            mock.call(proc.pid, signal.SIGKILL),
        ])

    def test_app_readiness_fails_fast_if_owned_process_exits(self):
        proc = subprocess.Popen([sys.executable, "-c", "raise SystemExit(7)"])
        proc.wait(timeout=3)
        with self.assertRaisesRegex(RuntimeError, "owned service exited"):
            runner.wait_http("http://127.0.0.1:1/healthz", proc, runner.Children(), timeout=.2)


    def test_migration_is_owned_supervised_child_and_failures_always_close_children(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=pathlib.Path(tmp); backend=root/"backend"; backend.mkdir()
            app=root/"app"; app.write_text("fake")
            provider=root/"provider"; provider.write_text("fake")
            with mock.patch.dict("os.environ",{"TEST_DATABASE_ADMIN_URL":"postgresql://m:secret@db/postgres","TEST_DATABASE_URL":"postgresql://r:secret@db/app","PLAYWRIGHT_BROWSERS_PATH":"/browser-cache","CI":"true"},clear=True), mock.patch.object(runner,"Children") as children_cls, mock.patch.object(sys,"argv",["runner","--root",str(root),"--app",str(app),"--provider",str(provider)]):
                children=children_cls.return_value
                def fail_migration(command, env, label, cwd, timeout):
                    self.assertEqual(command,[str(app.resolve()),"migrate"])
                    self.assertEqual(cwd,backend)
                    self.assertEqual(env["DATABASE_URL"],"postgresql://m:secret@db/postgres")
                    self.assertEqual(timeout,150)
                    raise RuntimeError("migration child failed")
                children.run.side_effect=fail_migration
                with self.assertRaisesRegex(RuntimeError,"migration child failed"):
                    runner.main()
                children.close.assert_called_once()


    def test_screenshot_argument_selects_opt_in_runner_mode(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            app = root / "app"
            provider = root / "provider"
            app.write_text("fake")
            provider.write_text("fake")
            screenshot = root / "home.png"
            with (mock.patch.dict("os.environ", {}, clear=True),
                  mock.patch.object(runner, "Children") as children_cls,
                  mock.patch.object(runner, "run_screenshot") as capture,
                  mock.patch.object(runner, "run_suite") as suite,
                  mock.patch.object(sys, "argv", ["runner", "--root", str(root), "--app", str(app), "--provider", str(provider), "--screenshot", str(screenshot)])):
                runner.main()
            capture.assert_called_once()
            self.assertEqual(capture.call_args.args[1], app.resolve())
            self.assertEqual(capture.call_args.args[-3], screenshot)
            self.assertIsNone(capture.call_args.args[-2])
            self.assertIsNone(capture.call_args.args[-1])
            suite.assert_not_called()
            children_cls.return_value.close.assert_called_once()

    def test_audit_dir_dispatches_to_screenshot_runner(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            app = root / "app"; provider = root / "provider"; audit = root / "audit"
            app.write_text("fake"); provider.write_text("fake")
            with (mock.patch.dict("os.environ", {}, clear=True),
                  mock.patch.object(runner, "Children") as children_cls,
                  mock.patch.object(runner, "run_screenshot") as capture,
                  mock.patch.object(runner, "run_suite") as suite,
                  mock.patch.object(sys, "argv", ["runner", "--root", str(root), "--app", str(app), "--provider", str(provider), "--audit-dir", str(audit)])):
                runner.main()
            capture.assert_called_once()
            self.assertIsNone(capture.call_args.args[-3])
            self.assertEqual(capture.call_args.args[-2], audit)
            self.assertIsNone(capture.call_args.args[-1])
            suite.assert_not_called()
            children_cls.return_value.close.assert_called_once()

    def test_screen_reader_no_orca_toggle_fails_fast_after_invalidating_results(self):
        with tempfile.TemporaryDirectory() as tmp:
            audit = pathlib.Path(tmp) / "audit"; audit.mkdir()
            for name in ("transcript.json", "transcript.md", "manifest.json", "orca-debug.out"):
                (audit / name).write_text("previous passing evidence")
            children = mock.Mock()
            with mock.patch.dict("os.environ", {"TENDO_SR_ORCA_NO_START": "1"}):
                with self.assertRaisesRegex(RuntimeError, "Orca is required.*disabled"):
                    runner.run_screen_reader(pathlib.Path(tmp), {}, children, pathlib.Path(tmp), audit)
            self.assertEqual(sorted(p.name for p in audit.iterdir()), [])
            children.start.assert_not_called()

    def test_screen_reader_ignores_caller_runtime_and_rejects_symlinked_runtime(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            outside = root / "outside"; outside.mkdir(); (outside / "sentinel").write_text("keep")
            audit = root / "audit"; audit.mkdir()
            children = mock.Mock(); children.start.side_effect = RuntimeError("stop before session")
            env = {"E2E_SR_RUN_DIR": str(outside), "E2E_SR_DEBUG_FILE": str(outside / "sentinel")}
            with self.assertRaisesRegex(RuntimeError, "stop before session"):
                runner.run_screen_reader(root, env, children, root, audit)
            self.assertEqual((outside / "sentinel").read_text(), "keep")
            self.assertEqual(env["E2E_SR_RUN_DIR"], str(audit.resolve() / "runtime"))
            (audit / "runtime").rmdir(); (audit / "runtime").symlink_to(outside, target_is_directory=True)
            children.start.reset_mock()
            with self.assertRaisesRegex(RuntimeError, "not a real directory"):
                runner.run_screen_reader(root, {}, children, root, audit)
            self.assertEqual((outside / "sentinel").read_text(), "keep")
            children.start.assert_not_called()

    def test_screen_reader_run_owns_private_runtime_and_stops_session(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp).resolve(); audit = root / "audit"; audit.mkdir()
            runtime = audit / "runtime"; logs = runtime / "logs"
            logs.mkdir(parents=True); (runtime / "display").write_text(":123"); (logs / "ready").touch(); (logs / "bus-address").write_text("unix:path=private")
            debug = audit / "orca-debug.out"; debug.write_text("stale debug")
            for name in ("transcript.json", "transcript.md", "manifest.json"):
                (audit / name).write_text("stale result")
            env = {"E2E_SR_RUN_DIR": str(runtime), "E2E_SR_DEBUG_FILE": str(debug)}
            children = mock.Mock(); proc = mock.Mock(); children.start.return_value = proc
            def start_session(*args):
                runtime.mkdir(mode=0o700, exist_ok=True)
                new_logs = runtime / "logs"; new_logs.mkdir()
                (runtime / "display").write_text(":123"); (new_logs / "ready").touch(); (new_logs / "bus-address").write_text("unix:path=private")
                return proc
            children.start.side_effect = start_session
            with mock.patch.object(runner, "run_playwright") as playwright, mock.patch.object(runner.subprocess, "check_output", side_effect=["commit\n", ""]), mock.patch.object(pathlib.Path, "iterdir", return_value=[]):
                runner.run_screen_reader(root, env, children, root / "artifacts", audit)
            children.start.assert_called_once_with([str(root / "scripts/sr-orca-session.sh"), str(runtime), str(debug), str(logs)], env, "private Orca session")
            children.stop.assert_called_once_with(proc, grace=5)
            self.assertEqual(list(audit.glob("transcript.*")), [])
            self.assertFalse((audit / "manifest.json").exists())
            self.assertEqual(debug.read_text(), "")
            self.assertEqual(runtime.stat().st_mode & 0o777, 0o700)
            self.assertEqual(audit.stat().st_mode & 0o777, 0o700)
            self.assertEqual(debug.stat().st_mode & 0o777, 0o600)
            self.assertEqual(env["HOME"], str(runtime / "home"))
            self.assertEqual(env["DISPLAY"], ":123")
            self.assertEqual(env["DBUS_SESSION_BUS_ADDRESS"], "unix:path=private")
            self.assertEqual(playwright.call_args.args[1]["E2E_SR_HEADFUL"], "1")
            self.assertEqual(playwright.call_args.args[1]["DISPLAY"], ":123")
            self.assertEqual(playwright.call_args.args[1]["XDG_RUNTIME_DIR"], str(runtime / "xdg-runtime"))

    def test_screen_reader_argument_selects_opt_in_mode(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            app = root / "app"; provider = root / "provider"; audit = root / "sr-audit"
            app.write_text("fake"); provider.write_text("fake")
            with (mock.patch.dict("os.environ", {}, clear=True),
                  mock.patch.object(runner, "Children") as children_cls,
                  mock.patch.object(runner, "run_screenshot") as capture,
                  mock.patch.object(runner, "run_suite") as suite,
                  mock.patch.object(sys, "argv", ["runner", "--root", str(root), "--app", str(app), "--provider", str(provider), "--sr-audit-dir", str(audit)])):
                runner.main()
            capture.assert_called_once()
            self.assertEqual(capture.call_args.args[-1], audit)
            suite.assert_not_called()
            children_cls.return_value.close.assert_called_once()

    def test_all_three_opt_in_modes_are_mutually_exclusive(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp); app = root / "app"; provider = root / "provider"
            app.write_text("fake"); provider.write_text("fake")
            argv = ["runner", "--root", str(root), "--app", str(app), "--provider", str(provider), "--screenshot", str(root / "home.png"), "--audit-dir", str(root / "audit"), "--sr-audit-dir", str(root / "sr-audit")]
            with mock.patch.object(sys, "argv", argv), self.assertRaises(SystemExit) as raised:
                runner.main()
            self.assertEqual(raised.exception.code, 2)

    def test_screenshot_and_audit_arguments_are_mutually_exclusive(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp); app = root / "app"; provider = root / "provider"
            app.write_text("fake"); provider.write_text("fake")
            with (mock.patch.object(sys, "argv", ["runner", "--root", str(root), "--app", str(app), "--provider", str(provider), "--screenshot", str(root / "home.png"), "--audit-dir", str(root / "audit")]),
                  self.assertRaises(SystemExit) as raised):
                runner.main()
            self.assertEqual(raised.exception.code, 2)

    def test_run_playwright_uses_distinct_report_paths_without_mutating_caller_env(self):
        root = pathlib.Path("/repo")
        env = {"E2E_ARTIFACT_DIR": "/repo/frontend/test-results/native-test", "KEEP": "value"}
        children = mock.Mock()
        runner.run_playwright(root, env, children, "e2e/journey.spec.ts")
        first = children.run.call_args.args[1]
        runner.run_playwright(root, env, children, "e2e/attention-clock.spec.ts")
        second = children.run.call_args.args[1]
        self.assertNotEqual(first["PLAYWRIGHT_HTML_OUTPUT_DIR"], second["PLAYWRIGHT_HTML_OUTPUT_DIR"])
        self.assertNotEqual(first["PLAYWRIGHT_JSON_OUTPUT_FILE"], second["PLAYWRIGHT_JSON_OUTPUT_FILE"])
        self.assertEqual(env, {"E2E_ARTIFACT_DIR": "/repo/frontend/test-results/native-test", "KEEP": "value"})

    def test_run_playwright_forwards_default_and_audit_timeouts(self):
        root = pathlib.Path("/repo")
        children = mock.Mock()
        runner.run_playwright(root, {}, children, "e2e/readme-screenshot.spec.ts")
        self.assertEqual(children.run.call_args.args[-1], 600)
        runner.run_playwright(root, {}, children, "e2e/ux-audit.spec.ts", timeout=1800)
        self.assertEqual(children.run.call_args.args[-1], 1800)

    def test_existing_account_seed_password_is_registered_before_psql_and_uuid_checked(self):
        runner.SENSITIVE_VALUES.clear()
        household_id = "01890000-0000-7000-8000-000000000004"
        conflict_user_id = "01890000-0000-7000-8000-000000000005"
        query_results = iter(["", f"{household_id}|{conflict_user_id}"])

        def psql_after_registration(_inherited, sql):
            self.assertIn(runner.db_fixtures.EXISTING_ACCOUNT_PASSWORD, runner.SENSITIVE_VALUES)
            return next(query_results)

        with mock.patch.object(runner.db_fixtures, "psql_admin", side_effect=psql_after_registration) as query:
            self.assertEqual(
                runner.db_fixtures.seed_existing_invitation_accounts(
                    {}, runner.register_sensitive_value
                ),
                household_id,
            )
        self.assertEqual(query.call_count, 2)
        self.assertIn("existing_free_e2e", query.call_args_list[0].args[1])
        self.assertIn(runner.db_fixtures.EXISTING_ACCOUNT_PASSWORD_HASH, query.call_args_list[0].args[1])
        seed_query = query.call_args_list[1].args[1]
        self.assertIn("INSERT INTO households", seed_query)
        self.assertIn("INSERT INTO user_accounts", seed_query)
        self.assertIn("INSERT INTO household_memberships", seed_query)
        self.assertIn("INSERT INTO local_credentials", seed_query)
        self.assertIn("default_household_id", seed_query)
        with mock.patch.object(runner.db_fixtures, "psql_admin", side_effect=["", "invalid|not-a-uuid"]):
            with self.assertRaisesRegex(RuntimeError, "UUIDs"):
                runner.db_fixtures.seed_existing_invitation_accounts(
                    {}, runner.register_sensitive_value
                )
        with mock.patch.object(runner.db_fixtures, "psql_admin", side_effect=["", f"{household_id}|invalid"]):
            with self.assertRaisesRegex(RuntimeError, "UUIDs"):
                runner.db_fixtures.seed_existing_invitation_accounts(
                    {}, runner.register_sensitive_value
                )

    def test_large_paging_seed_uses_set_based_fixtures_and_validates_ids(self):
        household_id = "01890000-0000-7000-8000-000000000004"
        owner_id = "01890000-0000-7000-8000-000000000005"
        runner.SENSITIVE_VALUES.clear()
        with mock.patch.object(runner.db_fixtures, "psql_admin", return_value=f"{household_id}|{owner_id}|1001|1001|1001") as query:
            self.assertEqual(
                runner.db_fixtures.seed_large_paging_household({}, runner.register_sensitive_value),
                (household_id, "large_paging_owner_e2e"),
            )
        sql = query.call_args.args[1]
        self.assertIn("generate_series(1, 1001)", sql)
        self.assertIn("INSERT INTO household_invitations", sql)
        self.assertIn("decode(md5(", sql)
        self.assertIn("FROM member_rows", sql)
        self.assertIn("FROM subjects", sql)
        self.assertIn("FROM invitations", sql)
        self.assertIn("token_hash", sql)
        self.assertIn(runner.db_fixtures.EXISTING_ACCOUNT_PASSWORD_HASH, sql)
        self.assertIn(runner.db_fixtures.EXISTING_ACCOUNT_PASSWORD, runner.SENSITIVE_VALUES)
        with mock.patch.object(runner.db_fixtures, "psql_admin", return_value=f"{household_id}|invalid|1001|1001|1001"):
            with self.assertRaisesRegex(RuntimeError, "fixture counts"):
                runner.db_fixtures.seed_large_paging_household({}, runner.register_sensitive_value)

    def test_large_paging_fixture_postcheck_requires_exact_counts(self):
        household_id = "01890000-0000-7000-8000-000000000004"
        with mock.patch.object(runner.db_fixtures, "psql_admin", return_value="1002|1001|1001|1001|1001|0"):
            runner.db_fixtures.assert_large_paging_household({}, household_id, "large_paging_owner_e2e")
        with mock.patch.object(runner.db_fixtures, "psql_admin", return_value="1002|1001|1000|1001|1001|0"):
            with self.assertRaisesRegex(RuntimeError, "state mismatch"):
                runner.db_fixtures.assert_large_paging_household({}, household_id, "large_paging_owner_e2e")
        with self.assertRaisesRegex(RuntimeError, "identifiers"):
            runner.db_fixtures.assert_large_paging_household({}, "invalid", "wrong")

    def test_large_paging_cleanup_removes_only_the_fixture(self):
        household_id = "01890000-0000-7000-8000-000000000004"
        members = "\n".join(f"01890000-0000-7000-8000-{n:012d}" for n in range(1002))
        with mock.patch.object(runner.db_fixtures, "psql_admin", side_effect=[members, "1", "1", "1002"]) as query:
            runner.db_fixtures.cleanup_large_paging_household({}, household_id)
        selection, owner, household, accounts = (call.args[1] for call in query.call_args_list)
        self.assertIn(f"m.household_id='{household_id}'", selection)
        self.assertIn("'^large_paging_member_[0-9]{4}$'", selection)
        self.assertNotIn("LIKE", selection)
        self.assertIn("DELETE FROM households", household)
        self.assertIn("id = ANY(ARRAY['01890000-0000-7000-8000-000000000000',", accounts)
        self.assertNotIn("login", accounts)
        self.assertTrue(all("SELECT count(*)" in sql for sql in (owner, household, accounts)))
        # psql prints "UPDATE 1"/"DELETE 1" after bare DML; counts must come from the CTE, not line counting.
        with mock.patch.object(runner.db_fixtures, "psql_admin", side_effect=[members, "1", "1", "1001"]):
            with self.assertRaisesRegex(RuntimeError, "cleanup mismatch"):
                runner.db_fixtures.cleanup_large_paging_household({}, household_id)
        with mock.patch.object(runner.db_fixtures, "psql_admin", return_value="01890000-0000-7000-8000-000000000001") as query:
            with self.assertRaisesRegex(RuntimeError, "found 1 fixture accounts"):
                runner.db_fixtures.cleanup_large_paging_household({}, household_id)
        self.assertEqual(query.call_count, 1, "nothing is deleted when the fixture membership is not exact")

    def test_large_paging_cleanup_runs_after_success_and_any_failure(self):
        fixtures = runner.db_fixtures
        with mock.patch.object(fixtures, "cleanup_large_paging_household") as cleanup:
            with fixtures.large_paging_household_cleanup({}, "l"):
                pass
        cleanup.assert_called_once_with({}, "l")
        for failure in (RuntimeError("browser failed"), KeyboardInterrupt()):
            with mock.patch.object(fixtures, "cleanup_large_paging_household", side_effect=RuntimeError("cleanup failed")) as cleanup:
                with self.assertRaises(type(failure)) as raised, contextlib.redirect_stderr(io.StringIO()) as stderr:
                    with fixtures.large_paging_household_cleanup({}, "l"):
                        raise failure
            self.assertIs(raised.exception, failure, "the primary failure is preserved")
            self.assertIn("cleanup also failed", stderr.getvalue())
            cleanup.assert_called_once_with({}, "l")

    def test_run_suite_cleans_large_fixture_when_the_seeded_suite_fails(self):
        inherited = {"TEST_DATABASE_ADMIN_URL": "postgres://x@h/d"}
        with mock.patch.object(runner.Children, "run"), \
                mock.patch.object(runner.db_fixtures, "seed_suite_fixtures", return_value={"E2E_LARGE_HOUSEHOLD_ID": "l"}), \
                mock.patch.object(runner, "run_seeded_suite", side_effect=RuntimeError("Playwright exited")), \
                mock.patch.object(runner.db_fixtures, "cleanup_large_paging_household") as cleanup:
            with self.assertRaisesRegex(RuntimeError, "Playwright exited"), contextlib.redirect_stderr(io.StringIO()):
                runner.run_suite(pathlib.Path("/r"), pathlib.Path("/app"), pathlib.Path("/p"), inherited, runner.Children(), pathlib.Path("/a"))
        cleanup.assert_called_once_with(inherited, "l")

    def test_suite_fixture_verification_checks_every_fixture(self):
        fixture_env = {
            "E2E_FOREIGN_HOUSEHOLD_ID": "f", "E2E_FOREIGN_SUBJECT_ID": "s", "E2E_FOREIGN_ITEM_ID": "i",
            "E2E_CONFLICT_HOUSEHOLD_ID": "c", "E2E_LARGE_HOUSEHOLD_ID": "l", "E2E_LARGE_OWNER_LOGIN": "o",
        }
        fixtures = runner.db_fixtures
        with mock.patch.object(fixtures, "assert_foreign_seed_unchanged") as foreign, \
                mock.patch.object(fixtures, "assert_existing_invitation_accounts") as existing, \
                mock.patch.object(fixtures, "assert_large_paging_household") as large:
            fixtures.verify_suite_fixtures({}, fixture_env)
        foreign.assert_called_once_with({}, "f", "s", "i")
        existing.assert_called_once_with({}, "c")
        large.assert_called_once_with({}, "l", "o")

    def test_foreign_household_seed_returns_validated_uuid_tuple(self):
        ids = (
            "01890000-0000-7000-8000-000000000001",
            "01890000-0000-7000-8000-000000000002",
            "01890000-0000-7000-8000-000000000003",
        )
        with mock.patch.object(runner.db_fixtures, "psql_admin", return_value="|".join(ids)):
            self.assertEqual(runner.db_fixtures.seed_foreign_household({}), ids)
        with mock.patch.object(runner.db_fixtures, "psql_admin", return_value="invalid|" + "|".join(ids[1:])):
            with self.assertRaisesRegex(RuntimeError, "three UUIDs"):
                runner.db_fixtures.seed_foreign_household({})

    def test_existing_invitation_membership_postcheck_accepts_exact_state(self):
        owner = "01890000-0000-7000-8000-000000000001"
        conflict = "01890000-0000-7000-8000-000000000004"
        expected = f"1|1|{owner}|{owner}|1|1|{conflict}|1"
        with mock.patch.object(runner.db_fixtures, "psql_admin", side_effect=[expected, owner]) as query:
            runner.db_fixtures.assert_existing_invitation_accounts({}, conflict)
        self.assertEqual(query.call_count, 2)
        with mock.patch.object(runner.db_fixtures, "psql_admin", side_effect=["wrong-state", owner]):
            with self.assertRaisesRegex(RuntimeError, "state mismatch"):
                runner.db_fixtures.assert_existing_invitation_accounts({}, conflict)
        with self.assertRaisesRegex(RuntimeError, "UUID"):
            runner.db_fixtures.assert_existing_invitation_accounts({}, "invalid")

    def test_foreign_seed_check_accepts_only_exact_unchanged_snapshot(self):
        ids = ("01890000-0000-7000-8000-000000000001", "01890000-0000-7000-8000-000000000002", "01890000-0000-7000-8000-000000000003")
        with mock.patch.object(runner.db_fixtures, "psql_admin", return_value=runner.db_fixtures.FOREIGN_SEED_EXPECTED) as query:
            runner.db_fixtures.assert_foreign_seed_unchanged({}, *ids)
        self.assertIn(ids[0], query.call_args.args[1])
        changed = runner.db_fixtures.FOREIGN_SEED_EXPECTED.replace("Foreign gutter check|1|", "Hijack|2|")
        with mock.patch.object(runner.db_fixtures, "psql_admin", return_value=changed):
            with self.assertRaisesRegex(RuntimeError, "Hijack"):
                runner.db_fixtures.assert_foreign_seed_unchanged({}, *ids)
        with self.assertRaisesRegex(RuntimeError, "UUIDs"):
            runner.db_fixtures.assert_foreign_seed_unchanged({}, "x'; DROP TABLE items; --", ids[1], ids[2])


class FailureLogTests(unittest.TestCase):
    def test_recipient_fingerprint_is_checked(self):
        with tempfile.TemporaryDirectory() as temporary:
            home = pathlib.Path(temporary) / "home"
            home.mkdir()
            result = subprocess.run(["gpg", "--batch", "--homedir", str(home), "--passphrase", "", "--quick-generate-key", "Encdiag test", "ed25519", "sign", "0"], capture_output=True)
            if result.returncode:
                self.skipTest("gpg key generation unavailable")
            exported = subprocess.run(["gpg", "--batch", "--homedir", str(home), "--armor", "--export"], capture_output=True, check=True)
            cert = pathlib.Path(temporary) / "recipient.asc"; cert.write_bytes(exported.stdout)
            listing = subprocess.run(["gpg", "--batch", "--homedir", str(home), "--with-colons", "--fingerprint"], capture_output=True, text=True, check=True)
            fingerprint = next(line.split(":")[9] for line in listing.stdout.splitlines() if line.startswith("fpr:"))
            self.assertTrue(runner.verify_diagnostics_recipient(cert, fingerprint))
            self.assertFalse(runner.verify_diagnostics_recipient(cert, runner.EXPECTED_DIAGNOSTICS_FINGERPRINT))

    def test_full_redacted_process_log_is_written_without_registered_secret(self):
        token = runner.generated_secret(32)
        with tempfile.TemporaryDirectory() as tmp:
            log = pathlib.Path(tmp) / "process.log"
            log.write_text(f"early password={token}\nlate useful failure detail\n")
            redacted = runner.write_redacted_process_log(log)
            self.assertEqual(redacted, pathlib.Path(tmp) / "process.redacted.log")
            content = redacted.read_text()
        self.assertEqual(content, "early password=[redacted]\nlate useful failure detail\n")
        self.assertNotIn(token, content)

    def test_failure_log_tail_is_bounded_and_redacts_generated_credentials(self):
        token = runner.generated_secret(32)
        with tempfile.TemporaryDirectory() as tmp:
            log = pathlib.Path(tmp) / "process.log"
            log.write_text("\n".join([f"line {n}" for n in range(300)] + [f"  1) failed fill password={token}", "  1 failed"]) + "\n")
            with mock.patch.object(runner.sys, "stderr", new_callable=__import__("io").StringIO) as err:
                runner.print_failure_log_tail(log, lines=5)
            out = err.getvalue()
        self.assertNotIn(token, out)
        self.assertIn("password=[redacted]", out)
        self.assertIn("  1 failed", out)
        self.assertNotIn("line 296", out)
        self.assertIn("line 297", out)

    def test_registered_secret_redaction_in_neutral_text(self):
        runner.SENSITIVE_VALUES.clear()
        token = runner.generated_secret(24)
        out = runner.redact(f"event detail: {token}")
        self.assertNotIn(token, out)
        self.assertEqual(out, "event detail: [redacted]")

    def test_structural_redaction_covers_cookies_oidc_invites_db_urls_json_and_ansi(self):
        runner.register_database_url("postgresql://tendo_app:p%40ss-W0rd@127.0.0.1:5432/tendo_e2e")
        lines = [
            "\x1b[31m    Cookie: tendo_session=abcSESSION123; other=1\x1b[39m",
            "set-cookie: tendo_session=zzzSESSION999; HttpOnly",
            "  - navigated to \"http://localhost:1234/api/v1/auth/oidc/callback?code=CODE777&state=STATE888\"",
            "  expect(page).toHaveURL(http://127.0.0.1:9/authorize?nonce=NONCE555&client_id=x)",
            "Received: \"http://localhost:1234/invite#INVITEsecretfragment0123456789abcdefghijklmn\"",
            "headers: { Cookie: `tendo_session=${'TOKENVALUE42'}` }",
            "dial postgresql://tendo_app:p%40ss-W0rd@127.0.0.1:5432/tendo_e2e failed",
            'report {"password":"json-password-value"}',
            "auth failed for password p@ss-W0rd",
            "  1) e2e/journey.spec.ts:12:3 > n5 invite > Expected: 3 Received: 4",
        ]
        out = "\n".join(runner.redact(line) for line in lines)
        for leaked in ("abcSESSION123", "zzzSESSION999", "CODE777", "STATE888", "NONCE555", "INVITEsecretfragment", "TOKENVALUE42", "json-password-value", "p%40ss-W0rd", "p@ss-W0rd", "\x1b["):
            self.assertNotIn(leaked, out)
        self.assertIn("e2e/journey.spec.ts:12:3 > n5 invite > Expected: 3 Received: 4", out)
        self.assertIn("/api/v1/auth/oidc/callback?code=[redacted]", out)
        for line in ('password="!KeepMePrivate$"', "password='!KeepMePrivate$'", "password=!KeepMePrivate$"):
            with self.subTest(line=line):
                self.assertNotIn("!KeepMePrivate$", runner.redact(line))
        self.assertIn("Received string: \"#[redacted]!\"", runner.redact('Received string: "#AbCdEfGhIjKlMnOpQrStUvWxYz0123456789ABCDE!"'))
        readable = runner.redact("e2e/journey.spec.ts:12:3 report-0123456789abcdef0123456789abcdef")
        self.assertIn("e2e/journey.spec.ts:12:3", readable)
        self.assertIn("report-0123456789abcdef0123456789abcdef", readable)

    def test_registered_secret_encodings_are_redacted(self):
        runner.SENSITIVE_VALUES.clear()
        secret = "Owner secret /+"
        runner.register_sensitive_value(secret)
        variants = {
            __import__("urllib.parse", fromlist=["quote"]).quote(secret, safe=""),
            __import__("urllib.parse", fromlist=["quote"]).quote_plus(secret),
            json.dumps(secret)[1:-1],
            json.dumps(secret)[1:-1].replace("/", "\\/"),
            base64.b64encode(secret.encode()).decode(),
            base64.b64encode(secret.encode()).decode().rstrip("="),
            base64.urlsafe_b64encode(secret.encode()).decode(),
            base64.urlsafe_b64encode(secret.encode()).decode().rstrip("="),
        }
        for variant in variants:
            with self.subTest(variant=variant):
                self.assertNotIn(variant, runner.redact(f"failure {variant}"))

    def test_cookie_object_diagnostics_are_redacted(self):
        diagnostics = '{"name":"tendo_session","value":"cookie-secret"} {"name":"other-cookie","value":"other-secret"}'
        out = runner.redact(diagnostics)
        self.assertNotIn("cookie-secret", out)
        self.assertNotIn("other-secret", out)

    def test_predictable_encodings_are_redacted_in_every_staged_text_channel(self):
        with tempfile.TemporaryDirectory() as tmp:
            results = pathlib.Path(tmp) / "test-results"
            source = results / "native-run"
            source.mkdir(parents=True)
            secret = "secret/with+symbols"
            runner.SENSITIVE_VALUES.clear()
            runner.register_sensitive_value(secret)
            percent = __import__("urllib.parse", fromlist=["quote"]).quote(secret, safe="")
            encoded = base64.b64encode(secret.encode()).decode()
            escaped = json.dumps({"detail": secret})[1:-1].replace("/", "\\/")
            (source / "process.redacted.log").write_text(f"process {percent} {encoded} {escaped}\n")
            artifacts = source / "artifacts" / "test-failed-7"
            artifacts.mkdir(parents=True)
            (artifacts / "error-context.md").write_text(f"context {percent} {encoded} {escaped}\n")
            (artifacts / "report-7.json").write_text(json.dumps({"errors": [{"message": f"{percent} {encoded}", "stack": escaped, "snippet": f"cookie={{\"name\":\"tendo_session\",\"value\":\"{secret}\"}}"}]}))
            with mock.patch.dict("os.environ", {"E2E_CI_UPLOAD_DIR": str(results / "upload"), "GITHUB_OUTPUT": str(results / "github-output")}):
                staged = results / "upload"
                self.assertTrue(runner.stage_ci_upload(staged, source))
                self.assertEqual((results / "github-output").read_text(), "ci_upload=ready\n")
            for path in staged.iterdir():
                if path.is_file() and path.name not in {"STAGED_OK", "diagnostics.tar.gpg"}:
                    contents = path.read_text()
                    for leaked in (secret, percent, encoded):
                        self.assertNotIn(leaked, contents)

    def test_preexisting_upload_paths_abort_before_children_without_deletion(self):
        for kind in ("file", "directory", "symlink", "dangling"):
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as tmp:
                root = pathlib.Path(tmp); app = root / "app"; provider = root / "provider"
                app.write_text("fake"); provider.write_text("fake")
                results = root / "frontend/test-results"
                results.mkdir(parents=True)
                destination = pathlib.Path(tmp) / "upload"
                outside = pathlib.Path(tmp) / "outside"
                if kind == "file":
                    destination.write_text("sentinel")
                elif kind == "directory":
                    destination.mkdir(); (destination / "sentinel").write_text("outside")
                elif kind == "symlink":
                    outside.write_text("sentinel"); destination.symlink_to(outside)
                else:
                    destination.symlink_to(pathlib.Path(tmp) / "missing")
                with (mock.patch.dict("os.environ", {"E2E_CI_UPLOAD_DIR": str(destination)}, clear=True),
                      mock.patch.object(runner, "Children") as children,
                      mock.patch.object(sys, "argv", ["runner", "--root", str(root), "--app", str(app), "--provider", str(provider)])):
                    with self.assertRaisesRegex(RuntimeError, "already exists"):
                        runner.main()
                children.assert_not_called()
                if kind == "file":
                    self.assertEqual(destination.read_text(), "sentinel")
                elif kind == "directory":
                    self.assertEqual((destination / "sentinel").read_text(), "outside")
                elif kind == "symlink":
                    self.assertEqual(outside.read_text(), "sentinel")
                else:
                    self.assertTrue(destination.is_symlink())

    def test_symlinked_upload_parent_aborts_before_children(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp); app = root / "app"; provider = root / "provider"
            app.write_text("fake"); provider.write_text("fake")
            actual = root / "actual"; actual.mkdir()
            parent_link = root / "parent-link"; parent_link.symlink_to(actual, target_is_directory=True)
            destination = parent_link / "upload"
            with (mock.patch.dict("os.environ", {"E2E_CI_UPLOAD_DIR": str(destination)}, clear=True),
                  mock.patch.object(runner, "Children") as children,
                  mock.patch.object(sys, "argv", ["runner", "--root", str(root), "--app", str(app), "--provider", str(provider)])):
                with self.assertRaisesRegex(RuntimeError, "real directory"):
                    runner.main()
            children.assert_not_called()
            self.assertFalse((actual / "upload").exists())

    def test_ci_upload_staging_is_allowlisted_and_sanitized(self):
        with tempfile.TemporaryDirectory() as tmp:
            results = pathlib.Path(tmp) / "test-results"
            source = results / "native-fake"
            source.mkdir(parents=True)
            runner.SENSITIVE_VALUES.clear()
            runner.register_sensitive_value("owner-secret-123")
            runner.register_sensitive_value("database-secret-456")
            (source / "process.redacted.log").write_text("owner-secret-123 should be hidden\n{\"password\":\"database-secret-456\"}\n")
            artifacts = source / "artifacts" / "test-failed-1"
            artifacts.mkdir(parents=True)
            (artifacts / "trace.zip").write_bytes(b"raw trace")
            (artifacts / "resource.json").write_text('{"password":"owner-secret-123"}')
            (artifacts / "error-context.md").write_text("Invite http://localhost/invite#database-secret-456\n")
            (artifacts / "report-1.json").write_text(json.dumps({"errors": [{"message": f"percent={__import__('urllib.parse', fromlist=['quote']).quote('owner-secret-123', safe='')} base64={base64.b64encode(b'owner-secret-123').decode()}", "stack": f"nested={{\"detail\":\"x\\/y\"}} cookie={{\"name\":\"tendo_session\",\"value\":\"database-secret-456\"}}", "snippet": "password=database-secret-456"}], "attachments": [{"name": "stdout", "body": "owner-secret-123"}]}))
            (artifacts / "test-failed-1.png").write_bytes(b"png")
            (source / "html-report").mkdir()
            (source / "html-report" / "index.html").write_text("secret")
            staged = results / "ci-upload"
            with mock.patch.dict("os.environ", {"GITHUB_OUTPUT": str(results / "github-output")}):
                self.assertTrue(runner.stage_ci_upload(staged, source))
            files = {p.relative_to(staged).as_posix() for p in staged.rglob("*") if p.is_file()}
            self.assertEqual(files, {"process.redacted.log", "report-1.json", "test-failed-1-error-context.md", "STAGED_OK", "diagnostics.tar.gpg", "diagnostics.manifest.json"})
            combined = "\n".join(p.read_text() for p in staged.rglob("*") if p.is_file() and p.name != "diagnostics.tar.gpg")
            self.assertNotIn("owner-secret-123", combined)
            self.assertNotIn("database-secret-456", combined)
            self.assertNotIn("stdout", combined)
            self.assertNotIn(base64.b64encode(b"owner-secret-123").decode(), combined)
            self.assertNotIn('"body"', combined)
            self.assertIn('x\\\\/y', combined)
            self.assertFalse(any(p.suffix == ".zip" or p.name == "index.html" for p in staged.rglob("*")))
            self.assertTrue((staged / "STAGED_OK").is_file())

    def test_ci_upload_skips_symlinked_sources(self):
        with tempfile.TemporaryDirectory() as tmp:
            results = pathlib.Path(tmp) / "test-results"
            source = results / "native-run"
            source.mkdir(parents=True)
            (source / "process.redacted.log").write_text("safe\n")
            destination = results / "ci-upload"
            external = pathlib.Path(tmp) / "outside.json"
            external.write_text('{"message":"should not copy"}')
            (source / "report-evil.json").symlink_to(external)
            staged = results / "upload"
            self.assertTrue(runner.stage_ci_upload(staged, source))
            names = {path.name for path in staged.iterdir()}
            self.assertIn("STAGED_OK", names)
            self.assertNotIn("report-evil.json", names)

    def test_upload_symlink_destination_is_rejected_without_touching_target(self):
        with tempfile.TemporaryDirectory() as tmp:
            results = pathlib.Path(tmp) / "test-results"
            results.mkdir()
            source = results / "native-run"
            source.mkdir()
            (source / "process.redacted.log").write_text("safe\n")
            destination = results / "upload"
            outside = pathlib.Path(tmp) / "outside"
            outside.mkdir()
            (outside / "sentinel").write_text("safe")
            destination.symlink_to(outside, target_is_directory=True)
            with self.assertRaisesRegex(RuntimeError, "already exists"):
                runner.validate_upload_destination(destination)
            self.assertTrue(destination.is_symlink())
            self.assertEqual((outside / "sentinel").read_text(), "safe")

    def test_ci_upload_rename_collision_never_emits_ready(self):
        with tempfile.TemporaryDirectory() as tmp:
            results = pathlib.Path(tmp) / "test-results"
            source = results / "native-run"
            source.mkdir(parents=True)
            (source / "process.redacted.log").write_text("safe\n")
            destination = results / "upload"
            output = results / "github-output"
            def collide(_src, dst):
                pathlib.Path(dst).mkdir()
                raise FileExistsError("collision")
            with mock.patch.dict("os.environ", {"GITHUB_OUTPUT": str(output)}), mock.patch.object(runner.os, "rename", side_effect=collide):
                self.assertFalse(runner.stage_ci_upload(destination, source))
            self.assertFalse(output.exists())

    def test_ci_upload_write_failure_publishes_failure_marker_not_partial_staging(self):
        with tempfile.TemporaryDirectory() as tmp:
            results = pathlib.Path(tmp) / "test-results"
            source = results / "native-run"
            source.mkdir(parents=True)
            (source / "process.redacted.log").write_text("safe\n")
            (source / "report-1.json").write_text("{}")
            original_write_text = pathlib.Path.write_text
            def fail_report(path, data, *args, **kwargs):
                if path.name == "report-1.json":
                    raise OSError("disk full")
                return original_write_text(path, data, *args, **kwargs)
            output = results / "github-output"
            with mock.patch.dict("os.environ", {"GITHUB_OUTPUT": str(output)}), mock.patch.object(pathlib.Path, "write_text", fail_report):
                self.assertTrue(runner.stage_ci_upload(results / "upload", source))
            staged = results / "upload"
            self.assertEqual(set(path.name for path in staged.iterdir()), {"SANITIZE_FAILED", "diagnostics.tar.gpg", "diagnostics.manifest.json"})
            self.assertEqual(output.read_text(), "ci_upload=ready\n")

    def test_github_output_unset_does_not_crash_after_publication(self):
        with tempfile.TemporaryDirectory() as tmp:
            results = pathlib.Path(tmp)
            source = results / "native-run"
            source.mkdir()
            (source / "process.redacted.log").write_text("safe\n")
            with mock.patch.dict("os.environ", {}, clear=True):
                self.assertTrue(runner.stage_ci_upload(results / "upload", source))

    def test_ci_upload_sanitizer_failure_leaves_only_marker(self):
        with tempfile.TemporaryDirectory() as tmp:
            results = pathlib.Path(tmp) / "test-results"
            source = results / "native-fake"
            source.mkdir(parents=True)
            (source / "process.redacted.log").write_text("safe\n")
            (source / "report-1.json").write_text("{}")
            destination = results / "upload"
            output = results / "github-output"
            with mock.patch.dict("os.environ", {"GITHUB_OUTPUT": str(output)}), mock.patch.object(runner, "sanitize_json_value", side_effect=ValueError("invalid")):
                self.assertTrue(runner.stage_ci_upload(destination, source))
            self.assertEqual(set(p.name for p in destination.iterdir()), {"SANITIZE_FAILED", "diagnostics.tar.gpg", "diagnostics.manifest.json"})
            self.assertEqual(output.read_text(), "ci_upload=ready\n")

    def test_routine_access_lines_do_not_push_playwright_report_out_of_tail(self):
        with tempfile.TemporaryDirectory() as tmp:
            log = pathlib.Path(tmp) / "process.log"
            report = ["  ✘  27 e2e/journey.spec.ts:1250:3 › n12 bounded pages", "    Expected: 25", "    Received: 24"]
            access = [f"2026/10/23 01:55:14 INFO http request request_id={n} method=POST route=/api/v1/x status=201 duration=5ms" for n in range(500)]
            failure = ["2026/10/23 01:55:15 INFO http request request_id=z method=GET route=/api/v1/x status=500 duration=5ms"]
            log.write_text("\n".join(report + access + failure) + "\n")
            with mock.patch.object(runner.sys, "stderr", new_callable=__import__("io").StringIO) as err:
                runner.print_failure_log_tail(log, lines=10)
            out = err.getvalue()
        self.assertIn("n12 bounded pages", out)
        self.assertIn("Received: 24", out)
        self.assertIn("status=500", out)
        self.assertIn("500 successful access-log lines omitted", out)

    def test_missing_failure_log_is_reported_not_raised(self):
        with mock.patch.object(runner.sys, "stderr", new_callable=__import__("io").StringIO) as err:
            runner.print_failure_log_tail(pathlib.Path("/nonexistent/tendo/process.log"))
        self.assertIn("E2E process log unavailable", err.getvalue())


class FailureReportingLifecycleTests(unittest.TestCase):
    def _main(self, argv_extra=(), suite=None, close=None, env=None):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp); app = root / "app"; provider = root / "provider"
            app.write_text("fake"); provider.write_text("fake")
            with (mock.patch.dict("os.environ", env or {}, clear=True),
                  mock.patch.object(runner, "Children") as children_cls,
                  mock.patch.object(runner, "run_suite", side_effect=suite) as _suite,
                  mock.patch.object(runner, "run_screenshot", side_effect=suite) as _shot,
                  mock.patch.object(runner, "print_failure_log_tail") as tail,
                  mock.patch.object(sys, "argv", ["runner", "--root", str(root), "--app", str(app), "--provider", str(provider), *argv_extra])):
                if close is not None:
                    children_cls.return_value.close.side_effect = close
                error = None
                try:
                    runner.main()
                except BaseException as exc:
                    error = exc
                return error, tail.call_count

    def test_interruption_before_staging_leaves_no_upload_directory(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp); app = root / "app"; provider = root / "provider"
            app.write_text("fake"); provider.write_text("fake")
            results = root / "frontend/test-results"
            results.mkdir(parents=True)
            destination = root / "runner-temp/upload"
            destination.parent.mkdir()
            with (mock.patch.dict("os.environ", {"E2E_CI_UPLOAD_DIR": str(destination)}, clear=True),
                  mock.patch.object(runner, "Children"),
                  mock.patch.object(runner, "run_suite", side_effect=KeyboardInterrupt()),
                  mock.patch.object(runner, "write_redacted_process_log", return_value=None),
                  mock.patch.object(runner, "print_failure_log_tail"),
                  mock.patch.object(runner, "stage_ci_upload") as stage,
                  mock.patch.object(sys, "argv", ["runner", "--root", str(root), "--app", str(app), "--provider", str(provider)])):
                with self.assertRaises(KeyboardInterrupt):
                    runner.main()
            self.assertFalse(destination.exists())
            stage.assert_called_once_with(destination, mock.ANY)

    def test_success_does_not_write_redacted_log(self):
        with mock.patch.object(runner, "write_redacted_process_log") as write_redacted:
            error, tails = self._main()
        self.assertIsNone(error); self.assertEqual(tails, 0)
        write_redacted.assert_not_called()

    def test_success_prints_no_failure_log(self):
        error, tails = self._main()
        self.assertIsNone(error); self.assertEqual(tails, 0)

    def test_suite_failure_writes_full_redacted_log(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp); app = root / "app"; provider = root / "provider"
            app.write_text("fake"); provider.write_text("fake")
            def fail_suite(_root, _app, _provider, _inherited, _children, artifact_dir):
                artifact_dir.joinpath("process.log").write_text("password=registered-secret\nfull failure details\n")
                raise RuntimeError("Playwright failed")

            output = root / "github-output"
            upload_dir = root / "runner-temp/upload"
            upload_dir.parent.mkdir()
            with (mock.patch.dict("os.environ", {"E2E_CI_UPLOAD_DIR": str(upload_dir), "GITHUB_OUTPUT": str(output)}, clear=True),
                  mock.patch.object(runner, "Children"),
                  mock.patch.object(runner, "run_suite", side_effect=fail_suite),
                  mock.patch.object(runner, "print_failure_log_tail"),
                  mock.patch.object(sys, "argv", ["runner", "--root", str(root), "--app", str(app), "--provider", str(provider)])):
                with self.assertRaisesRegex(RuntimeError, "Playwright failed"):
                    runner.main()
            self.assertIn("process.redacted.log", {path.name for path in upload_dir.iterdir()})
            self.assertEqual(output.read_text(), "ci_upload=ready\n")

    def test_suite_failure_prints_log_and_keeps_primary_error(self):
        error, tails = self._main(suite=RuntimeError("Playwright exited with status 1"))
        self.assertIsInstance(error, RuntimeError); self.assertIn("Playwright", str(error)); self.assertEqual(tails, 1)

    def test_screenshot_and_audit_failures_print_log(self):
        for extra in (["--screenshot", "/tmp/never.png"], ["--audit-dir", "/tmp/never-audit"]):
            error, tails = self._main(argv_extra=extra, suite=RuntimeError("capture failed"))
            self.assertIsInstance(error, RuntimeError); self.assertEqual(tails, 1)

    def test_cleanup_only_failure_prints_log_and_fails(self):
        with mock.patch.object(runner, "write_redacted_process_log") as write_redacted:
            error, tails = self._main(close=RuntimeError("process group survived"))
        self.assertIsInstance(error, RuntimeError); self.assertIn("survived", str(error)); self.assertEqual(tails, 1)
        write_redacted.assert_called_once()

    def test_cleanup_failure_after_suite_failure_keeps_primary_error(self):
        error, tails = self._main(suite=RuntimeError("primary"), close=RuntimeError("secondary"))
        self.assertEqual(str(error), "primary"); self.assertEqual(tails, 1)

    def test_missing_process_log_does_not_replace_primary_error(self):
        error, tails = self._main(suite=RuntimeError("primary failure"))
        self.assertIsInstance(error, RuntimeError)
        self.assertEqual(str(error), "primary failure")
        self.assertEqual(tails, 1)

    def test_cancellation_after_suite_returns_prints_log_and_exits_nonzero(self):
        def cancelled(*_args, **_kwargs):
            signal.getsignal(signal.SIGTERM)(signal.SIGTERM, None)
        with mock.patch.object(runner, "write_redacted_process_log") as write_redacted:
            error, tails = self._main(suite=cancelled)
        self.assertIsInstance(error, SystemExit); self.assertEqual(error.code, 128 + signal.SIGTERM); self.assertEqual(tails, 1)
        write_redacted.assert_called_once()

    def test_database_credentials_are_registered_before_children_start(self):
        runner.SENSITIVE_VALUES.clear()
        self._main(env={"TEST_DATABASE_URL": "postgresql://r:runtime%2Fpw@db/app", "TEST_DATABASE_ADMIN_URL": "postgresql://m:adminpw@db/postgres"})
        self.assertNotIn("runtime/pw", runner.redact("x runtime/pw y"))
        self.assertNotIn("adminpw", runner.redact("x adminpw y"))

class EncryptedDiagnosticsTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        cls.root = pathlib.Path(cls.temp.name)
        cls.home = cls.root / "private-home"; cls.home.mkdir(mode=0o700)
        env = {**os.environ, "GNUPGHOME": str(cls.home)}
        subprocess.run(["gpg", "--batch", "--passphrase", "", "--quick-gen-key", "Encdiag Test <encdiag@example.invalid>", "ed25519", "cert", "0"], env=env, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        listing = subprocess.run(["gpg", "--batch", "--with-colons", "--fingerprint"], env=env, check=True, capture_output=True, text=True)
        cls.fingerprint = next(line.split(":")[9] for line in listing.stdout.splitlines() if line.startswith("fpr:"))
        subprocess.run(["gpg", "--batch", "--passphrase", "", "--quick-add-key", cls.fingerprint, "cv25519", "encr", "0"], env=env, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        cls.certificate = cls.root / "recipient.asc"
        with cls.certificate.open("wb") as output:
            subprocess.run(["gpg", "--batch", "--armor", "--export", cls.fingerprint], env=env, check=True, stdout=output)

    @classmethod
    def tearDownClass(cls):
        cls.temp.cleanup()

    def setUp(self):
        runner._cancel_deadline = None
        self.tempdir = tempfile.TemporaryDirectory()
        self.root = pathlib.Path(self.tempdir.name)
        self.source = self.root / "native-uuid"; self.source.mkdir()
        (self.source / "process.log").write_text("RAW CREDENTIAL\n")
        (self.source / "process.redacted.log").write_text("redacted details\n")
        (self.source / "attention-clock.json").write_text('{"clock":1}')
        report = self.source / "report-uuid.json"; report.write_text('{"safe":true}')
        html = self.source / "html-report-uuid"; html.mkdir(); (html / "index.html").write_text("report")
        artifacts = self.source / "artifacts" / "test-failed"; artifacts.mkdir(parents=True)
        (artifacts / "trace.zip").write_bytes(b"trace bytes")
        (artifacts / "failure.png").write_bytes(b"png bytes")
        (artifacts / "error-context.md").write_text("safe context")
        self.destination = self.root / "upload"; self.destination.mkdir()

    def tearDown(self):
        self.tempdir.cleanup()
        runner._cancel_deadline = None

    def _encrypt(self, recipient=None, expected=None):
        recipient = recipient or self.certificate
        expected = expected or self.fingerprint
        return runner.stage_encrypted_diagnostics(self.destination, self.source, recipient, expected)

    def _decrypt(self, home, ciphertext=None):
        return subprocess.run(["gpg", "--batch", "--yes", "--homedir", str(home), "--decrypt", str(ciphertext or self.destination / "diagnostics.tar.gpg")], stdout=subprocess.PIPE, stderr=subprocess.PIPE)

    def test_real_round_trip_preserves_files_and_manifest_hashes(self):
        self.assertTrue(self._encrypt())
        imported = subprocess.run(["gpg", "--batch", "--homedir", str(self.home), "--import", str(self.certificate)], capture_output=True)
        self.assertEqual(imported.returncode, 0)
        decrypted = self._decrypt(self.home)
        self.assertEqual(decrypted.returncode, 0, decrypted.stderr)
        tar_path = self.root / "unpacked.tar"; tar_path.write_bytes(decrypted.stdout)
        unpack = self.root / "unpacked"; unpack.mkdir()
        with __import__("tarfile").open(tar_path) as archive:
            archive.extractall(unpack)
        manifest = json.loads((unpack / "MANIFEST.json").read_text())
        expected_paths = {p.relative_to(self.source).as_posix() for p in self.source.rglob("*") if p.is_file() and p.name != "process.log"}
        self.assertEqual({entry["path"] for entry in manifest}, expected_paths)
        self.assertNotIn("process.log", {entry["path"] for entry in manifest})
        for entry in manifest:
            contents = (unpack / entry["path"]).read_bytes()
            self.assertEqual(len(contents), entry["size"])
            self.assertEqual(__import__("hashlib").sha256(contents).hexdigest(), entry["sha256"])
        self.assertFalse((unpack / "process.log").exists())
        public = json.loads((self.destination / "diagnostics.manifest.json").read_text())
        self.assertEqual(public["recipient_fingerprint"], self.fingerprint)
        ciphertext = (self.destination / "diagnostics.tar.gpg").read_bytes()
        self.assertEqual(public["ciphertext_size"], len(ciphertext))
        self.assertEqual(public["ciphertext_sha256"], __import__("hashlib").sha256(ciphertext).hexdigest())

    def test_wrong_recipient_and_invalid_inputs_make_fixed_marker_without_plaintext(self):
        scenarios = [(self.certificate, "0" * 40, "RECIPIENT_INVALID"), (self.root / "missing.asc", self.fingerprint, "RECIPIENT_INVALID")]
        for recipient, fingerprint, marker in scenarios:
            with self.subTest(marker=marker):
                self.assertFalse(runner.stage_encrypted_diagnostics(self.destination, self.source, recipient, fingerprint))
                self.assertEqual((self.destination / "ENCRYPTED_DIAGNOSTICS_FAILED").read_text().strip(), marker)
                self.assertFalse((self.destination / "diagnostics.tar.gpg").exists())
                self.assertFalse(any(p.suffix == ".tar" for p in self.destination.iterdir()))
                (self.destination / "ENCRYPTED_DIAGNOSTICS_FAILED").unlink()

    def test_gpg_missing_failure_timeout_and_limits_make_marker(self):
        runner._cancel_deadline = None
        with mock.patch.object(runner, "verify_diagnostics_recipient", return_value=True), mock.patch.object(runner.shutil, "which", return_value=None):
            self.assertFalse(self._encrypt())
        self.assertEqual((self.destination / "ENCRYPTED_DIAGNOSTICS_FAILED").read_text().strip(), "GPG_UNAVAILABLE")
        (self.destination / "ENCRYPTED_DIAGNOSTICS_FAILED").unlink()
        runner._cancel_deadline = time.monotonic() + 0.1
        fake_process = mock.Mock(returncode=None)
        fake_process.poll.return_value = None
        with mock.patch.object(runner, "verify_diagnostics_recipient", return_value=True), mock.patch.object(runner.subprocess, "Popen", return_value=fake_process):
            self.assertFalse(self._encrypt())
        self.assertEqual((self.destination / "ENCRYPTED_DIAGNOSTICS_FAILED").read_text().strip(), "TIMEOUT")
        (self.destination / "ENCRYPTED_DIAGNOSTICS_FAILED").unlink()
        runner._cancel_deadline = None
        with mock.patch.object(runner, "verify_diagnostics_recipient", return_value=True), mock.patch.object(runner, "MAX_DIAGNOSTIC_FILE_SIZE", 1):
            self.assertFalse(self._encrypt())
        self.assertEqual((self.destination / "ENCRYPTED_DIAGNOSTICS_FAILED").read_text().strip(), "SIZE_LIMIT")
        (self.destination / "ENCRYPTED_DIAGNOSTICS_FAILED").unlink()
        with mock.patch.object(runner, "verify_diagnostics_recipient", return_value=True), mock.patch.object(runner, "MAX_DIAGNOSTIC_TOTAL_SIZE", 1):
            self.assertFalse(self._encrypt())
        self.assertEqual((self.destination / "ENCRYPTED_DIAGNOSTICS_FAILED").read_text().strip(), "SIZE_LIMIT")
        (self.destination / "ENCRYPTED_DIAGNOSTICS_FAILED").unlink()
        with mock.patch.object(runner, "verify_diagnostics_recipient", return_value=True), mock.patch.object(runner, "MAX_DIAGNOSTIC_FILES", 1):
            self.assertFalse(self._encrypt())
        self.assertEqual((self.destination / "ENCRYPTED_DIAGNOSTICS_FAILED").read_text().strip(), "SIZE_LIMIT")

    def test_symlinked_files_are_skipped_and_stage_sanitization_is_independent(self):
        external = self.root / "outside.txt"; external.write_text("outside")
        (self.source / "artifacts" / "test-failed" / "outside-link").symlink_to(external)
        self.assertTrue(self._encrypt())
        with mock.patch.dict("os.environ", {}, clear=True):
            staged = self.root / "stage"
            self.assertTrue(runner.stage_ci_upload(staged, self.source))
        self.assertTrue((staged / "diagnostics.tar.gpg").exists())
        self.assertTrue((staged / "process.redacted.log").exists())
        (self.destination / "diagnostics.tar.gpg").unlink(missing_ok=True)
        (self.destination / "diagnostics.manifest.json").unlink(missing_ok=True)

    def test_stage_missing_redacted_log_keeps_encrypted_pair(self):
        (self.source / "process.redacted.log").unlink()
        staged = self.root / "missing-log-upload"
        self.assertTrue(runner.stage_ci_upload(staged, self.source))
        self.assertTrue((staged / "SANITIZE_FAILED").exists())
        self.assertTrue((staged / "diagnostics.tar.gpg").exists() or (staged / "ENCRYPTED_DIAGNOSTICS_FAILED").exists())

    def test_process_log_is_excluded_from_bundle(self):
        self.assertTrue(self._encrypt())
        imported_home = self.root / "imported"; imported_home.mkdir(mode=0o700)
        subprocess.run(["gpg", "--batch", "--homedir", str(imported_home), "--import", str(self.certificate)], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        decrypted = self._decrypt(self.home)
        tar_path = self.root / "process-exclusion.tar"; tar_path.write_bytes(decrypted.stdout)
        with __import__("tarfile").open(tar_path) as archive:
            self.assertNotIn("process.log", archive.getnames())

    def test_encryption_failure_retains_sanitized_text_and_sanitizer_failure_keeps_encryption(self):
        source = self.source
        def fail_encryption(destination, *_args, **_kwargs):
            (destination / "ENCRYPTED_DIAGNOSTICS_FAILED").write_text("GPG_FAILED\\n")
            return False
        with mock.patch.object(runner, "stage_encrypted_diagnostics", side_effect=fail_encryption):
            staged = self.root / "text-upload"
            self.assertTrue(runner.stage_ci_upload(staged, source))
        self.assertTrue((staged / "process.redacted.log").exists())
        self.assertTrue((staged / "ENCRYPTED_DIAGNOSTICS_FAILED").exists())
        staged2 = self.root / "sanitizer-upload"
        with mock.patch.object(runner, "sanitize_json_value", side_effect=ValueError("secret path")):
            self.assertTrue(runner.stage_ci_upload(staged2, source))
        self.assertTrue((staged2 / "SANITIZE_FAILED").exists())
        self.assertTrue((staged2 / "diagnostics.tar.gpg").exists() or (staged2 / "ENCRYPTED_DIAGNOSTICS_FAILED").exists())
        self.assertNotIn("secret path", (staged2 / "SANITIZE_FAILED").read_text())
        missing_log = self.root / "missing-redacted-log"; (source / "process.redacted.log").unlink()
        self.assertTrue(runner.stage_ci_upload(missing_log, source))
        self.assertTrue((missing_log / "SANITIZE_FAILED").exists())
        self.assertTrue((missing_log / "diagnostics.tar.gpg").exists() or (missing_log / "ENCRYPTED_DIAGNOSTICS_FAILED").exists())

    def test_public_only_and_wrong_homes_cannot_decrypt_and_ciphertext_corruption_fails(self):
        self.assertTrue(self._encrypt())
        pubhome = self.root / "pubhome"; pubhome.mkdir(mode=0o700)
        subprocess.run(["gpg", "--batch", "--homedir", str(pubhome), "--import", str(self.certificate)], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        self.assertNotEqual(self._decrypt(pubhome).returncode, 0)
        wronghome = self.root / "wronghome"; wronghome.mkdir(mode=0o700)
        wrong_env = {**os.environ, "GNUPGHOME": str(wronghome)}
        subprocess.run(["gpg", "--batch", "--passphrase", "", "--quick-gen-key", "Wrong key <wrong@example.invalid>", "ed25519", "cert", "0"], env=wrong_env, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        self.assertNotEqual(self._decrypt(wronghome).returncode, 0)
        original = (self.destination / "diagnostics.tar.gpg").read_bytes()
        for payload in (original[:-8], original[:-1] + bytes([original[-1] ^ 1])):
            tampered = self.root / "tampered.gpg"; tampered.write_bytes(payload)
            self.assertNotEqual(self._decrypt(self.home, tampered).returncode, 0)

    def test_gpg_nonzero_after_partial_output_leaves_no_final_ciphertext(self):
        real_popen = runner.subprocess.Popen
        runner._cancel_deadline = None
        class FailedGpg:
            returncode = 2
            def poll(self):
                return self.returncode
            def wait(self, **_kwargs):
                return self.returncode
            def kill(self):
                self.returncode = -9
        def partial(command, **_kwargs):
            pathlib.Path(command[command.index("--output") + 1]).write_bytes(b"partial")
            return FailedGpg()
        with mock.patch.object(runner, "verify_diagnostics_recipient", return_value=True), mock.patch.object(runner.subprocess, "Popen", side_effect=partial):
            self.assertFalse(self._encrypt())
        self.assertEqual((self.destination / "ENCRYPTED_DIAGNOSTICS_FAILED").read_text().strip(), "GPG_FAILED")
        self.assertFalse((self.destination / "diagnostics.tar.gpg").exists())

    def test_sigterm_during_encryption_cleans_private_tar_and_publishes_failure_marker(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary); source = root / "native-run"; source.mkdir()
            (source / "process.redacted.log").write_text("safe log\\n")
            artifacts = source / "artifacts"; artifacts.mkdir(); (artifacts / "trace.zip").write_bytes(b"trace")
            upload = root / "upload"
            fakebin = root / "bin"; fakebin.mkdir()
            fakegpg = fakebin / "gpg"
            started_marker = root / "gpg-started"
            fakegpg.write_text("#!/usr/bin/python3\nimport os, pathlib, time\npathlib.Path(os.environ['GPG_STARTED']).touch()\ntime.sleep(30)\n")
            fakegpg.chmod(0o755)
            module_path = str(MODULE)
            child_code = f'''import importlib.util, pathlib, sys
spec = importlib.util.spec_from_file_location("runner", {module_path!r})
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
module.verify_diagnostics_recipient = lambda *_: True
def fail_suite(root, app, provider, inherited, children, artifacts):
    (artifacts / "process.log").write_text("failure\\n")
    raise RuntimeError("expected test failure")
module.run_suite = fail_suite
sys.argv = ["runner", "--root", {str(root)!r}, "--app", {str(root / 'app')!r}, "--provider", {str(root / 'provider')!r}]
try:
    module.main()
except RuntimeError:
    pass
'''
            env = {**os.environ, "PATH": str(fakebin) + os.pathsep + os.environ.get("PATH", ""),
                   "E2E_CI_UPLOAD_DIR": str(upload), "E2E_DIAGNOSTICS_RECIPIENT": str(self.certificate),
                   "GPG_STARTED": str(started_marker)}
            (root / "app").touch(); (root / "provider").touch()
            proc = subprocess.Popen([sys.executable, "-c", child_code], env=env)
            deadline = time.monotonic() + 8
            while time.monotonic() < deadline and not started_marker.exists() and proc.poll() is None:
                time.sleep(.02)
            if not started_marker.exists():
                self.fail(proc.communicate(timeout=1)[0] or "child did not begin GPG encryption")
            started = time.monotonic(); proc.send_signal(signal.SIGTERM)
            self.assertEqual(proc.wait(timeout=18), 128 + signal.SIGTERM)
            self.assertLess(time.monotonic() - started, 17)
            self.assertEqual(list(root.rglob("tendo-encrypted-diag-*")), [])
            self.assertFalse(list(root.rglob("diagnostics.tar")))
            self.assertTrue((upload / "ENCRYPTED_DIAGNOSTICS_FAILED").is_file())
            self.assertEqual((upload / "ENCRYPTED_DIAGNOSTICS_FAILED").read_text().strip(), "TIMEOUT")
            self.assertFalse(list(upload.glob("*.tar")))

    def test_mid_staging_cancel_shortens_gpg_wait_to_failure_marker(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = pathlib.Path(temporary); source = root / "native"; source.mkdir()
            artifacts = source / "artifacts"; artifacts.mkdir(); (artifacts / "trace.zip").write_bytes(b"trace")
            destination = root / "upload"; destination.mkdir()
            fakebin = root / "bin"; fakebin.mkdir()
            fakegpg = fakebin / "gpg"; fakegpg.write_text("#!/bin/sh\nexec sleep 30\n"); fakegpg.chmod(0o755)
            started_marker = root / "gpg-started"
            fakegpg.write_text(f"#!/usr/bin/python3\nimport pathlib, time\npathlib.Path({str(started_marker)!r}).touch()\ntime.sleep(30)\n")
            fakegpg.chmod(0o755)
            original_which = runner.shutil.which
            with mock.patch.object(runner, "verify_diagnostics_recipient", return_value=True), mock.patch.object(runner.shutil, "which", side_effect=lambda name: str(fakegpg) if name == "gpg" else original_which(name)):
                def cancel_when_gpg_starts():
                    deadline = time.monotonic() + 5
                    while time.monotonic() < deadline and not started_marker.exists(): time.sleep(.01)
                    runner._cancel_deadline = time.monotonic() + runner.CANCELLED_STAGING_BUDGET
                timer = threading.Thread(target=cancel_when_gpg_starts)
                timer.start(); started = time.monotonic()
                self.assertFalse(runner.stage_encrypted_diagnostics(destination, source, self.certificate))
                timer.join()
            self.assertLess(time.monotonic() - started, 17)
            self.assertEqual((destination / "ENCRYPTED_DIAGNOSTICS_FAILED").read_text().strip(), "TIMEOUT")
            self.assertFalse(list(root.rglob("diagnostics.tar")))

    def test_workflow_is_secret_free_and_upload_gated(self):
        workflow = (pathlib.Path(__file__).resolve().parents[1] / ".github/workflows/required.yml").read_text()
        e2e = workflow.split("  e2e:\n", 1)[1].split("\n  deployment:", 1)[0]
        self.assertNotIn("secrets.", e2e)
        upload = e2e.split("- name: Upload E2E failure diagnostics", 1)[1]
        self.assertIn("steps.e2e.outputs.ci_upload == 'ready'", upload)
        self.assertIn("${{ runner.temp }}/e2e-ci-upload-${{ github.run_id }}-${{ github.run_attempt }}/", upload)
        self.assertNotIn("frontend/test-results", upload)


if __name__ == "__main__":
    unittest.main()
