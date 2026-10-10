#!/usr/bin/env python3
"""Behavior tests for scripts/dev_toolchain.py using synthetic manifests, archives and executables (no network)."""
from __future__ import annotations

import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import tarfile
import tempfile
import textwrap
import time
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parent.parent
spec = importlib.util.spec_from_file_location("dev_toolchain", ROOT / "scripts/dev_toolchain.py")
dt = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = dt
spec.loader.exec_module(dt)


def manifest(**overrides) -> dict:
    data = json.loads((ROOT / "toolchain.json").read_text())
    for dotted, value in overrides.items():
        target = data
        *parents, leaf = dotted.split(".")
        for key in parents:
            target = target[key]
        if value is None:
            del target[leaf]
        else:
            target[leaf] = value
    return data


def executable(path: Path, body: str) -> Path:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text("#!/bin/sh\n" + textwrap.dedent(body))
    path.chmod(0o755)
    return path


def tarball(path: Path, members: dict[str, str], symlink: str | None = None) -> str:
    with tarfile.open(path, "w:gz") as tar:
        for name, content in members.items():
            data = content.encode()
            info = tarfile.TarInfo(name)
            info.size, info.mode = len(data), 0o644
            tar.addfile(info, io.BytesIO(data))
        if symlink:
            info = tarfile.TarInfo(symlink)
            info.type, info.linkname = tarfile.SYMTYPE, "/etc/passwd"
            tar.addfile(info)
    return hashlib.sha256(path.read_bytes()).hexdigest()


class TempDirTest(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory(prefix="tendo-dev-toolchain-")
        self.tmp = Path(self._tmp.name)
        self.env = {"PATH": os.defpath, "HOME": str(self.tmp / "home")}

    def tearDown(self):
        self._tmp.cleanup()


class ManifestTests(TempDirTest):
    def write(self, data: dict) -> Path:
        path = self.tmp / "toolchain.json"
        path.write_text(json.dumps(data))
        return path

    def test_repository_manifest_is_complete_and_exact(self):
        pins = dt.load_pins(ROOT / "toolchain.json")
        data = json.loads((ROOT / "toolchain.json").read_text())
        self.assertEqual((pins.go, pins.node, pins.sqlc, pins.playwright), (data["go"], data["node"], data["sqlc"]["version"], data["playwright"]))
        self.assertEqual((pins.mise, pins.mise_sha256), (data["mise"]["version"], data["mise"]["linuxX64Sha256"]))
        self.assertEqual(pins.sqlc_sha256, data["sqlc"]["linuxAmd64Sha256"])

    def test_synthetic_candidate_versions_are_used_verbatim(self):
        pins = dt.load_pins(self.write(manifest(go="1.28.0", node="26.1.3", **{"sqlc.version": "1.32.0"})))
        self.assertEqual((pins.go, pins.node, pins.sqlc), ("1.28.0", "26.1.3", "1.32.0"))

    def test_ranges_aliases_missing_keys_and_bad_digests_are_rejected(self):
        cases = {
            "go minor only": manifest(go="1.27"),
            "node range": manifest(node="^24.21.0"),
            "node alias": manifest(node="lts"),
            "playwright prefix": manifest(playwright="v1.64.0"),
            "oapi without v": manifest(oapiCodegen="2.8.0"),
            "mise latest": manifest(**{"mise.version": "latest"}),
            "mise missing": manifest(mise=None),
            "sqlc digest short": manifest(**{"sqlc.linuxAmd64Sha256": "abc"}),
            "mise digest uppercase": manifest(**{"mise.linuxX64Sha256": "A" * 64}),
            "go not string": manifest(go=1.27),
        }
        for name, data in cases.items():
            with self.subTest(name), self.assertRaises(dt.ToolchainError):
                dt.load_pins(self.write(data))

    def test_unreadable_manifest_is_a_toolchain_error(self):
        (self.tmp / "toolchain.json").write_text("{not json")
        with self.assertRaises(dt.ToolchainError):
            dt.load_pins(self.tmp / "toolchain.json")


class CacheAndIsolationTests(TempDirTest):
    def test_cache_root_precedence_and_absolute_requirement(self):
        self.assertEqual(dt.cache_root({"HOME": "/h"}), Path("/h/.cache/tendo/toolchain"))
        self.assertEqual(dt.cache_root({"HOME": "/h", "XDG_CACHE_HOME": "/x"}), Path("/x/tendo/toolchain"))
        self.assertEqual(dt.cache_root({"HOME": "/h", "TENDO_TOOLCHAIN_CACHE": "/p/c"}), Path("/p/c"))
        with self.assertRaises(dt.ToolchainError):
            dt.cache_root({"TENDO_TOOLCHAIN_CACHE": "relative/cache"})

    def test_installers_run_without_user_config_and_with_private_state(self):
        cache = self.tmp / "cache"
        env = dt.provisioning_env(cache, {
            "PATH": "/bin", "MISE_GLOBAL_CONFIG_FILE": "/home/u/.config/mise/config.toml", "MISE_DATA_DIR": "/home/u/.local/share/mise",
            "MISE_SHARED_INSTALL_DIRS": "/opt/mise", "MISE_NODE_VERIFY": "false", "MISE_GO_SKIP_CHECKSUM": "true", "GOTOOLCHAIN": "auto",
            "NPM_CONFIG_PREFIX": "/usr/local", "npm_config_global": "true", "NODE_OPTIONS": "--require /tmp/evil.js", "GOFLAGS": "-mod=mod"})
        for key in ("MISE_GLOBAL_CONFIG_FILE", "NPM_CONFIG_PREFIX", "NODE_OPTIONS", "GOFLAGS"):
            self.assertNotIn(key, env)
        self.assertEqual(env["MISE_NO_CONFIG"], "1")
        self.assertEqual(env["MISE_SHARED_INSTALL_DIRS"], "")
        private = [key for key in env if key.startswith("MISE_") and key.endswith(("_DIR", "_FILE"))] + ["npm_config_userconfig", "npm_config_globalconfig", "npm_config_cache"]
        self.assertIn("MISE_SYSTEM_INSTALLS_DIR", private)
        for key in private:
            self.assertTrue(Path(env[key]).is_relative_to(cache), key)
        for key, value in {"MISE_NODE_VERIFY": "true", "MISE_GO_SKIP_CHECKSUM": "false", "MISE_NODE_NPM_SHIM": "false",
                           "GOTOOLCHAIN": "local", "npm_config_global": "false", "npm_config_ignore_scripts": "true"}.items():
            self.assertEqual(env[key], value, key)

    def test_real_npm_ignores_hostile_user_npm_configuration(self):
        npm = shutil.which("npm")
        if npm is None:
            self.skipTest("npm not on PATH; run through scripts/dev-exec.sh")
        home = self.tmp / "home"
        home.mkdir()
        (home / ".npmrc").write_text("prefix=/hostile-prefix\nregistry=https://hostile.invalid/\n")
        base = {"PATH": os.environ["PATH"], "HOME": str(home), "npm_config_prefix": "/hostile-prefix2"}
        env = dt.provisioning_env(self.tmp / "cache", base)
        get = lambda key: subprocess.run([npm, "config", "get", key], env=env, cwd=self.tmp, capture_output=True, text=True, check=True).stdout.strip()
        self.assertEqual(get("registry"), "https://registry.npmjs.org/")
        self.assertNotIn("hostile", get("prefix"))
        self.assertEqual(get("global"), "false")


class VerifiedReleaseTests(TempDirTest):
    def fake_sqlc_archive(self, reported: str) -> tuple[str, str]:
        archive = self.tmp / f"sqlc-{reported}.tar.gz"
        digest = tarball(archive, {"sqlc": f"#!/bin/sh\necho {reported}\n"})
        return archive.as_uri(), digest

    def install(self, url: str, digest: str) -> Path:
        destination = dt.release_path(self.tmp / "cache", "sqlc", "1.31.1", digest)
        dt.ensure_release_binary("sqlc", url, digest, "sqlc", destination, ["version"], lambda out: out == "v1.31.1", self.env)
        return destination

    def test_checksum_mismatch_discards_archive_and_installs_nothing(self):
        url, _ = self.fake_sqlc_archive("v1.31.1")
        with self.assertRaisesRegex(dt.ToolchainError, "SHA-256 mismatch"):
            self.install(url, "0" * 64)
        self.assertEqual(list(dt.release_path(self.tmp / "cache", "sqlc", "1.31.1", "0" * 64).parent.iterdir()), [])

    def test_verified_archive_installs_and_cached_binary_is_reused_offline(self):
        url, digest = self.fake_sqlc_archive("v1.31.1")
        binary = self.install(url, digest)
        self.assertEqual(subprocess.run([binary], capture_output=True, text=True).stdout.strip(), "v1.31.1")
        self.assertEqual(sorted(p.name for p in binary.parent.iterdir()), ["sqlc", "sqlc.sha256"])
        self.install((self.tmp / "missing.tar.gz").as_uri(), digest)

    def test_tampered_cached_binary_is_never_executed_and_is_replaced(self):
        url, digest = self.fake_sqlc_archive("v1.31.1")
        binary = self.install(url, digest)
        executable(binary, f"touch {self.tmp}/tampered-ran; echo v1.31.1\n")
        self.install(url, digest)
        self.assertFalse((self.tmp / "tampered-ran").exists())
        self.assertEqual(subprocess.run([binary], capture_output=True, text=True).stdout.strip(), "v1.31.1")

    def test_changed_archive_checksum_for_same_version_does_not_reuse_old_binary(self):
        old_url, old_digest = self.fake_sqlc_archive("v1.31.1")
        old = self.install(old_url, old_digest)
        archive = self.tmp / "rebuilt.tar.gz"
        new_digest = tarball(archive, {"sqlc": "#!/bin/sh\n# rebuilt release\necho v1.31.1\n"})
        new = self.install(archive.as_uri(), new_digest)
        self.assertNotEqual(old, new)
        self.assertIn("rebuilt release", new.read_text())

    def test_wrong_reported_version_is_removed(self):
        url, digest = self.fake_sqlc_archive("v1.30.0")
        with self.assertRaisesRegex(dt.ToolchainError, "does not report the pinned version"):
            self.install(url, digest)
        self.assertEqual(list(dt.release_path(self.tmp / "cache", "sqlc", "1.31.1", digest).parent.iterdir()), [])

    def test_non_regular_archive_members_are_refused(self):
        archive = self.tmp / "evil.tar.gz"
        tarball(archive, {}, symlink="sqlc")
        with self.assertRaisesRegex(dt.ToolchainError, "not a regular file"):
            dt.extract_member(archive, "sqlc", self.tmp / "out/sqlc")
        self.assertFalse((self.tmp / "out/sqlc").exists())


class MiseRuntimeTests(TempDirTest):
    def setUp(self):
        super().setUp()
        self.env = dict(self.env, MISE_INSTALLS_DIR=str(self.tmp / "installs"))

    def fake_mise(self, installed_version: str, reported_version: str, where_override: str = "") -> Path:
        installs = self.tmp / "installs"
        return executable(self.tmp / "mise", f"""\
            printf '%s\\n' "$*" >> {self.tmp}/mise.log
            dir={installs}/go/{installed_version}
            case "$1" in
              where) {f'echo {where_override}; exit 0' if where_override else ':'}
                     [ -d "$dir" ] && [ "$2" = go@{installed_version} ] && echo "$dir" && exit 0; echo "$2 not installed" >&2; exit 1 ;;
              install) mkdir -p "$dir/bin"; printf '#!/bin/sh\\necho go version go{reported_version} linux/amd64\\n' > "$dir/bin/go"; chmod +x "$dir/bin/go"
                       if [ "$2" = --force ]; then touch "$dir/plain"; fi ;;
            esac
            """)

    def test_install_outside_private_directory_is_refused_before_running_it(self):
        shared = self.tmp / "usr-local-share-mise/installs/go/1.27.2"
        executable(shared / "bin/go", f"touch {self.tmp}/shared-ran; echo go version go1.27.2 linux/amd64\n")
        mise = self.fake_mise("1.27.2", "1.27.2", where_override=str(shared))
        with self.assertRaisesRegex(dt.ToolchainError, "outside the private install directory"):
            dt.ensure_runtime(mise, self.env, "go", "1.27.2", ["version"], "go1.27.2")
        self.assertFalse((self.tmp / "shared-ran").exists())

    def test_install_failing_integrity_predicate_is_force_reinstalled(self):
        mise = self.fake_mise("1.27.2", "1.27.2")
        dt.ensure_runtime(mise, self.env, "go", "1.27.2", ["version"], "go1.27.2")
        dt.ensure_runtime(mise, self.env, "go", "1.27.2", ["version"], "go1.27.2", lambda where: (where / "plain").exists())
        self.assertIn("install --force go@1.27.2", (self.tmp / "mise.log").read_text())

    def test_npm_shim_detection(self):
        node = self.tmp / "installs/node/24.21.0"
        executable(node / "bin/npm", "mise reshim\n")
        self.assertFalse(dt.npm_without_mise_hook(node))
        (node / "bin/npm").unlink()
        (node / "bin/npm").symlink_to("../lib/node_modules/npm/bin/npm-cli.js")
        self.assertTrue(dt.npm_without_mise_hook(node))

    def test_missing_exact_version_is_installed_then_verified(self):
        mise = self.fake_mise("1.27.2", "1.27.2")
        where = dt.ensure_runtime(mise, self.env, "go", "1.27.2", ["version"], "go1.27.2")
        self.assertEqual(where, self.tmp / "installs/go/1.27.2")
        self.assertEqual((self.tmp / "mise.log").read_text().splitlines(), ["where go@1.27.2", "install go@1.27.2", "where go@1.27.2"])
        dt.ensure_runtime(mise, self.env, "go", "1.27.2", ["version"], "go1.27.2")
        self.assertEqual((self.tmp / "mise.log").read_text().count("install"), 1)

    def test_install_reporting_another_version_fails_without_fallback(self):
        mise = self.fake_mise("1.27.2", "1.27.1")
        with self.assertRaisesRegex(dt.ToolchainError, "reports .*go1.27.1.*expected go1.27.2"):
            dt.ensure_runtime(mise, self.env, "go", "1.27.2", ["version"], "go1.27.2")

    def test_failed_install_is_reported(self):
        mise = executable(self.tmp / "mise", "[ \"$1\" = install ] && echo 'checksum verification failed' >&2; exit 1\n")
        with self.assertRaisesRegex(dt.ToolchainError, "checksum verification failed"):
            dt.ensure_runtime(mise, self.env, "node", "24.21.0", ["--version"], "v24.21.0")


class ExecutableResolutionTests(TempDirTest):
    def toolchain(self, *, gofmt: bool = True) -> "dt.Toolchain":
        go_root, node_bin, sqlc_dir = self.tmp / "go", self.tmp / "node/bin", self.tmp / "sqlc"
        for path in [go_root / "bin/go", node_bin / "node", node_bin / "npm", node_bin / "npx", sqlc_dir / "sqlc"] + ([go_root / "bin/gofmt"] if gofmt else []):
            executable(path, "exit 0\n")
        pins = dt.load_pins(ROOT / "toolchain.json")
        return dt.Toolchain(self.tmp / "cache", self.tmp / "mise", go_root, node_bin, sqlc_dir, self.tmp / "browsers", "1248", pins)

    def system_path(self) -> str:
        system = self.tmp / "system-bin"
        for name in ("go", "gofmt", "node", "npm", "npx", "sqlc"):
            executable(system / name, "echo incompatible system tool; exit 99\n")
        return f"{system}{os.pathsep}{os.defpath}"

    def test_pinned_tools_shadow_incompatible_system_binaries(self):
        env = dt.child_env(self.toolchain(), {"PATH": self.system_path(), "GOTOOLCHAIN": "auto", "PLAYWRIGHT_BROWSERS_PATH": "/shared"})
        for name, expected in (("go", self.tmp / "go/bin/go"), ("npm", self.tmp / "node/bin/npm"), ("sqlc", self.tmp / "sqlc/sqlc")):
            self.assertEqual(subprocess.run(["sh", "-c", f"command -v {name}"], env=env, capture_output=True, text=True).stdout.strip(), str(expected))
        self.assertEqual((env["GOROOT"], env["GOTOOLCHAIN"], env["PLAYWRIGHT_BROWSERS_PATH"]), (str(self.tmp / "go"), "local", str(self.tmp / "browsers")))
        self.assertEqual(env["GOMODCACHE"], str(self.tmp / "cache/go/mod"))

    def test_incomplete_pinned_install_does_not_fall_back_to_system_tool(self):
        with self.assertRaisesRegex(dt.ToolchainError, "gofmt resolves to .*system-bin/gofmt"):
            dt.child_env(self.toolchain(gofmt=False), {"PATH": self.system_path()})


class PlaywrightTests(TempDirTest):
    def repo(self, test_version="1.64.0", core_version="1.64.0", integrity="sha512-frontend") -> Path:
        repo = self.tmp / "repo"
        (repo / "frontend").mkdir(parents=True, exist_ok=True)
        packages = {"node_modules/@playwright/test": {"version": test_version, "integrity": "sha512-test"},
                    "node_modules/playwright-core": {"version": core_version, "integrity": integrity}}
        (repo / "frontend/package-lock.json").write_text(json.dumps({"lockfileVersion": 3, "packages": packages}))
        return repo

    def fake_node(self, integrity: str, chromium="1248", shell="1248") -> Path:
        node_bin = self.tmp / "node/bin"
        browsers = json.dumps({"browsers": [{"name": "chromium", "revision": chromium}, {"name": "chromium-headless-shell", "revision": shell}]})
        lock = json.dumps({"packages": {"node_modules/playwright-core": {"version": "1.64.0", "integrity": integrity}}})
        executable(node_bin / "npm", f"""\
            echo "npm $*" >> {self.tmp}/calls.log
            mkdir -p node_modules/playwright-core
            echo '{{"version": "1.64.0"}}' > node_modules/playwright-core/package.json
            echo '{browsers}' > node_modules/playwright-core/browsers.json
            echo '{lock}' > package-lock.json
            """)
        executable(node_bin / "node", f"""\
            echo "node $* $PLAYWRIGHT_BROWSERS_PATH" >> {self.tmp}/calls.log
            for d in chromium-{chromium} chromium_headless_shell-{chromium}; do mkdir -p "$PLAYWRIGHT_BROWSERS_PATH/$d"; touch "$PLAYWRIGHT_BROWSERS_PATH/$d/INSTALLATION_COMPLETE"; done
            """)
        return node_bin

    def provision(self, repo: Path, node_bin: Path):
        return dt.ensure_playwright(self.tmp / "cache", node_bin, dt.load_pins(ROOT / "toolchain.json"), repo, self.env)

    def test_lockfile_pinned_core_installs_matching_chromium_revision_once(self):
        browsers, revision = self.provision(self.repo(), self.fake_node("sha512-frontend"))
        self.assertEqual((browsers, revision), (self.tmp / "cache/ms-playwright", "1248"))
        self.assertTrue((browsers / "chromium_headless_shell-1248/INSTALLATION_COMPLETE").is_file())
        calls = (self.tmp / "calls.log").read_text()
        self.assertIn("--ignore-scripts", calls)
        self.assertIn("playwright-core@1.64.0", calls)
        self.assertIn("install chromium " + str(browsers), calls)
        self.provision(self.repo(), self.fake_node("sha512-frontend"))
        self.assertEqual((self.tmp / "calls.log").read_text(), calls, "cached package and browsers must be reused")

    def test_registry_integrity_differing_from_frontend_lockfile_fails(self):
        with self.assertRaisesRegex(dt.ToolchainError, "does not match the frontend lockfile integrity"):
            self.provision(self.repo(), self.fake_node("sha512-other"))

    def test_lockfile_and_manifest_disagreement_fails_before_install(self):
        for repo in (self.repo(test_version="1.65.0"), self.repo(core_version="1.63.0")):
            with self.subTest(repo=json.loads((repo / "frontend/package-lock.json").read_text())), self.assertRaisesRegex(dt.ToolchainError, "!= toolchain.json"):
                self.provision(repo, self.fake_node("sha512-frontend"))
        self.assertFalse((self.tmp / "calls.log").exists())

    def test_inconsistent_chromium_revisions_fail(self):
        with self.assertRaisesRegex(dt.ToolchainError, "inconsistent Chromium revisions"):
            self.provision(self.repo(), self.fake_node("sha512-frontend", shell="1247"))

    def test_repository_lockfile_pins_manifest_playwright(self):
        pins = dt.load_pins(ROOT / "toolchain.json")
        for package in ("playwright-core", "@playwright/test", "playwright"):
            self.assertEqual(dt.lock_entry(ROOT / "frontend/package-lock.json", package)["version"], pins.playwright)


class CommandExecutionTests(TempDirTest):
    STUB = textwrap.dedent("""\
        import importlib.util, sys
        spec = importlib.util.spec_from_file_location("dt", sys.argv[1])
        dt = importlib.util.module_from_spec(spec); sys.modules["dt"] = dt; spec.loader.exec_module(dt)
        def fail(*_):
            raise dt.ToolchainError("node@24.21.0 checksum mismatch")
        dt.provision = (lambda *_: object()) if sys.argv[2] == "ok" else fail
        dt.child_env = lambda _t, base: dict(base, TENDO_PINNED="yes")
        sys.exit(dt.main(sys.argv[3:]))
        """)

    def wrapper(self, mode: str, *args: str) -> subprocess.CompletedProcess:
        stub = self.tmp / "stub.py"
        stub.write_text(self.STUB)
        return subprocess.run([sys.executable, str(stub), str(ROOT / "scripts/dev_toolchain.py"), mode, *args],
                              capture_output=True, text=True, timeout=30)

    def test_child_exit_status_environment_and_signal_propagate(self):
        self.assertEqual(self.wrapper("ok", "--", "sh", "-c", "exit 7").returncode, 7)
        self.assertEqual(self.wrapper("ok", "--", "sh", "-c", "printf %s \"$TENDO_PINNED\"").stdout, "yes")
        self.assertEqual(self.wrapper("ok", "--", "sh", "-c", "kill -TERM $$").returncode, -signal.SIGTERM)

    def test_provisioning_failure_never_runs_the_command(self):
        result = self.wrapper("fail", "--", "sh", "-c", f"touch {self.tmp}/ran")
        self.assertEqual(result.returncode, 1)
        self.assertIn("checksum mismatch", result.stderr)
        self.assertFalse((self.tmp / "ran").exists())

    def test_usage_and_missing_command(self):
        self.assertEqual(self.wrapper("ok").returncode, 2)
        self.assertEqual(self.wrapper("ok", "sh").returncode, 2)
        self.assertEqual(self.wrapper("ok", "--", str(self.tmp / "no-such-command")).returncode, 127)

    def test_signal_during_provisioning_stops_installer_before_lock_release(self):
        """A second wrapper must not provision while the first one's installer may still be running."""
        self.interrupt_provisioning(repeat=False)

    def test_repeated_signal_during_cleanup_does_not_abandon_installer(self):
        self.interrupt_provisioning(repeat=True)

    def interrupt_provisioning(self, *, repeat: bool):
        cache = self.tmp / "cache"
        script = textwrap.dedent(f"""\
            import importlib.util, sys
            spec = importlib.util.spec_from_file_location("dt", {str(ROOT / 'scripts/dev_toolchain.py')!r})
            dt = importlib.util.module_from_spec(spec); sys.modules["dt"] = dt; spec.loader.exec_module(dt)
            def provision(_repo, base):
                with dt.provisioning_lock(dt.cache_root(base)):
                    dt.run(["sh", "-c", "trap '' TERM; echo $$ > {self.tmp}/installer.pid; sleep 20; touch {self.tmp}/installer-finished"], env=base)
            dt.provision = provision
            dt.STOP_GRACE_SECONDS = float(sys.argv[1])
            sys.exit(dt.main(["--", "true"]))
            """)
        stub = self.tmp / "interrupt.py"
        stub.write_text(script)
        env = dict(os.environ, TENDO_TOOLCHAIN_CACHE=str(cache))
        first = subprocess.Popen([sys.executable, str(stub), "1.0" if repeat else "0.5"], env=env, stderr=subprocess.PIPE, text=True)
        pidfile = self.tmp / "installer.pid"
        for _ in range(500):
            if pidfile.exists() and pidfile.read_text().strip():
                break
            time.sleep(0.01)
        installer = int(pidfile.read_text())
        started = time.monotonic()
        first.send_signal(signal.SIGTERM)
        if repeat:
            time.sleep(0.3)  # inside the TERM grace period, while the installer still ignores TERM
            first.send_signal(signal.SIGTERM)
            first.send_signal(signal.SIGINT)
        _, stderr = first.communicate(timeout=30)
        self.assertLess(time.monotonic() - started, 10, "TERM-ignoring installer must be escalated to SIGKILL")
        self.assertEqual(first.returncode, -signal.SIGTERM, stderr)
        self.assertIn("installer processes stopped", stderr)
        with self.assertRaises(ProcessLookupError):
            os.kill(installer, 0)
        self.assertFalse((self.tmp / "installer-finished").exists(), "installer outlived the wrapper")

    def test_installer_descendant_ignoring_term_is_killed_even_after_leader_exits(self):
        pidfile = self.tmp / "descendant.pid"
        script = f"(trap '' TERM; echo $(exec sh -c 'echo $PPID') > {pidfile}; sleep 30) & while [ ! -s {pidfile} ]; do sleep 0.01; done; sleep 30"
        with mock.patch.object(dt, "STOP_GRACE_SECONDS", 0.3), mock.patch.object(dt.subprocess.Popen, "communicate", side_effect=KeyboardInterrupt):
            original = dt.subprocess.Popen.__init__

            def start_then_wait(proc, *args, **kwargs):
                original(proc, *args, **kwargs)
                for _ in range(500):
                    if pidfile.exists() and pidfile.read_text().strip():
                        return
                    time.sleep(0.01)

            with mock.patch.object(dt.subprocess.Popen, "__init__", start_then_wait), self.assertRaises(KeyboardInterrupt):
                dt.run(["sh", "-c", script], env=dict(os.environ))
        descendant = int(pidfile.read_text())
        with self.assertRaises(ProcessLookupError):
            os.kill(descendant, 0)

    def test_zombie_group_members_do_not_count_as_running(self):
        child = subprocess.Popen(["true"], start_new_session=True)
        try:
            for _ in range(500):
                if Path(f"/proc/{child.pid}/stat").read_text().split(") ", 1)[1].startswith("Z"):
                    break
                time.sleep(0.01)
            os.killpg(child.pid, 0)  # the zombie still owns its process group
            self.assertEqual(dt.live_group_members(child.pid), [])
        finally:
            child.wait()
        sleeper = subprocess.Popen(["sleep", "30"], start_new_session=True)
        try:
            self.assertEqual(dt.live_group_members(sleeper.pid), [sleeper.pid])
        finally:
            sleeper.kill()
            sleeper.wait()

    def test_shell_entrypoint_is_valid(self):
        subprocess.run(["bash", "-n", str(ROOT / "scripts/dev-exec.sh")], check=True)


if __name__ == "__main__":
    unittest.main()
