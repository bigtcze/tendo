import importlib.util
import pathlib
import signal
import subprocess
import os
import sys
import tempfile
import time
import unittest
from unittest import mock

MODULE = pathlib.Path(__file__).with_name("e2e-native-runner.py")
spec = importlib.util.spec_from_file_location("e2e_native_runner", MODULE)
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class ChildProcessTests(unittest.TestCase):
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


if __name__ == "__main__":
    unittest.main()
