#!/usr/bin/env python3
"""Provision the checked-out toolchain.json pins in a project-scoped user cache and run a command with them.

Usage:
  scripts/dev-exec.sh -- COMMAND [ARG...]   run COMMAND with the pinned Go, Node.js/npm, sqlc and Playwright browsers
  scripts/dev-exec.sh --versions            provision, then print the resolved tool versions and paths

Go and Node.js are installed by a SHA-256-verified, project-private mise used only as an installer: it runs with
MISE_NO_CONFIG=1 and private data/cache/state/config directories, so global or project mise configuration, shims and
shared installs are never read or changed. sqlc comes from its official release archive, verified before extraction.
Playwright's Chromium revision is derived from the playwright-core entry in frontend/package-lock.json. The command
replaces this process (exec), so its exit status and terminating signal are the wrapper's own.
"""
from __future__ import annotations

from contextlib import contextmanager
from dataclasses import dataclass
import fcntl
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import signal
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.request

ROOT = Path(__file__).resolve().parent.parent
MISE_URL = "https://github.com/jdx/mise/releases/download/v{version}/mise-v{version}-linux-x64.tar.gz"
SQLC_URL = "https://github.com/sqlc-dev/sqlc/releases/download/v{version}/sqlc_{version}_linux_amd64.tar.gz"
SEMVER = re.compile(r"^\d+\.\d+\.\d+$")
SHA256 = re.compile(r"^[0-9a-f]{64}$")
CALENDAR_VERSION = re.compile(r"^\d{4}\.\d{1,2}\.\d+$")


class ToolchainError(Exception):
    """A pin cannot be provisioned or verified; never fall back to another installation."""


class Interrupted(BaseException):
    """A termination signal arrived while provisioning; installer children are stopped before the lock is released."""

    def __init__(self, signum: int):
        super().__init__(signum)
        self.signum = signum


@dataclass(frozen=True)
class Pins:
    go: str
    node: str
    mise: str
    mise_sha256: str
    sqlc: str
    sqlc_sha256: str
    oapi_codegen: str
    playwright: str


@dataclass(frozen=True)
class Toolchain:
    cache: Path
    mise: Path
    go_root: Path
    node_bin: Path
    sqlc_dir: Path
    browsers: Path
    chromium_revision: str
    pins: Pins


def log(message: str) -> None:
    print(f"dev-exec: {message}", file=sys.stderr, flush=True)


def load_pins(path: Path) -> Pins:
    try:
        data = json.loads(path.read_text())
    except (OSError, ValueError) as exc:
        raise ToolchainError(f"cannot read {path}: {exc}") from exc

    def get(*keys: str) -> str:
        value = data
        for key in keys:
            if not isinstance(value, dict) or key not in value:
                raise ToolchainError(f"{path.name} is missing {'.'.join(keys)}")
            value = value[key]
        if not isinstance(value, str):
            raise ToolchainError(f"{path.name} {'.'.join(keys)} must be a string")
        return value

    pins = Pins(
        go=get("go"), node=get("node"), mise=get("mise", "version"), mise_sha256=get("mise", "linuxX64Sha256"),
        sqlc=get("sqlc", "version"), sqlc_sha256=get("sqlc", "linuxAmd64Sha256"), oapi_codegen=get("oapiCodegen"),
        playwright=get("playwright"),
    )
    for name in ("go", "node", "sqlc", "playwright"):
        if not SEMVER.match(getattr(pins, name)):
            raise ToolchainError(f"{path.name} {name} must be an exact X.Y.Z version, found {getattr(pins, name)!r}")
    if not CALENDAR_VERSION.match(pins.mise):
        raise ToolchainError(f"{path.name} mise.version must be an exact YYYY.M.N release, found {pins.mise!r}")
    if not re.match(r"^v\d+\.\d+\.\d+$", pins.oapi_codegen):
        raise ToolchainError(f"{path.name} oapiCodegen must be an exact vX.Y.Z version, found {pins.oapi_codegen!r}")
    for name in ("mise_sha256", "sqlc_sha256"):
        if not SHA256.match(getattr(pins, name)):
            raise ToolchainError(f"{path.name} {name} must be 64 lowercase hex characters")
    return pins


def cache_root(env: dict[str, str]) -> Path:
    if env.get("TENDO_TOOLCHAIN_CACHE"):
        root = Path(env["TENDO_TOOLCHAIN_CACHE"])
    else:
        base = env.get("XDG_CACHE_HOME") or str(Path(env.get("HOME") or Path.home()) / ".cache")
        root = Path(base) / "tendo" / "toolchain"
    if not root.is_absolute():
        raise ToolchainError(f"toolchain cache must be an absolute path, found {root}")
    return root


def require_supported_platform() -> None:
    if platform.system() != "Linux" or platform.machine().lower() not in ("x86_64", "amd64"):
        raise ToolchainError(f"verified mise/sqlc checksums exist only for linux x86_64, not {platform.system()} {platform.machine()}")


STOP_GRACE_SECONDS = 10.0


DEFERRED_SIGNALS = {signal.SIGINT, signal.SIGTERM, signal.SIGHUP}


def live_group_members(pgid: int) -> list[int]:
    """PIDs in process group `pgid` that can still run. Zombies are excluded: they execute nothing, and an
    adoptive parent that reaps slowly must not keep the wrapper (and its cache lock) waiting forever."""
    members = []
    for entry in os.scandir("/proc"):
        if not entry.name.isdigit():
            continue
        try:
            stat = Path(entry.path, "stat").read_text()
        except OSError:
            continue
        fields = stat[stat.rindex(")") + 2:].split()
        state, group = fields[0], int(fields[2])
        if group == pgid and state not in ("Z", "X"):
            members.append(int(entry.name))
    return members


def stop_group(proc: subprocess.Popen) -> None:
    """Stop an installer's whole process group, not only its leader: SIGTERM, wait up to the grace period for every
    member to exit, then SIGKILL until no member can run, and reap the leader. Termination signals are deferred
    meanwhile, so a repeated Ctrl-C cannot abandon cleanup; they are delivered once the group is gone."""
    pgid = proc.pid
    previous_mask = signal.pthread_sigmask(signal.SIG_BLOCK, DEFERRED_SIGNALS)
    try:
        proc.poll()
        if proc.returncode is not None and not live_group_members(pgid):
            return  # nothing left to stop; never signal a process group ID that may since have been reused
        try:
            os.killpg(pgid, signal.SIGTERM)
        except ProcessLookupError:
            pass
        deadline = time.monotonic() + STOP_GRACE_SECONDS
        while time.monotonic() < deadline:
            proc.poll()
            if proc.returncode is not None and not live_group_members(pgid):
                return
            time.sleep(0.05)
        # SIGKILL cannot be ignored, so this loop ends as soon as the kernel has torn the members down.
        while True:
            try:
                os.killpg(pgid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            proc.poll()
            if proc.returncode is not None and not live_group_members(pgid):
                return
            time.sleep(0.01)
    finally:
        signal.pthread_sigmask(signal.SIG_SETMASK, previous_mask)


def run(args: list[str], *, env: dict[str, str], cwd: Path | None = None, timeout: float = 1800) -> str:
    """Run an installer in its own process group and return its stdout.

    Termination signals stay blocked from before the spawn until `proc` is bound inside the cleanup scope, so no
    signal can unwind between fork and registration and leave an unowned installer running. The child restores the
    caller's mask before exec. Whatever way the call ends (success, nonzero status, timeout, or signal), the whole
    group is stopped before returning, so no descendant can outlive the caller's cache lock."""
    previous_mask = signal.pthread_sigmask(signal.SIG_BLOCK, DEFERRED_SIGNALS)
    try:
        try:
            proc = subprocess.Popen(args, env=env, cwd=cwd, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                    start_new_session=True,
                                    preexec_fn=lambda: signal.pthread_sigmask(signal.SIG_SETMASK, previous_mask))
        except OSError as exc:
            raise ToolchainError(f"cannot run {args[0]}: {exc}") from exc
        try:
            try:
                signal.pthread_sigmask(signal.SIG_SETMASK, previous_mask)  # a deferred signal is delivered here
                stdout, stderr = proc.communicate(timeout=timeout)
            finally:
                # Defer signals again so that cleanup below cannot be skipped by one arriving after communicate().
                signal.pthread_sigmask(signal.SIG_BLOCK, DEFERRED_SIGNALS)
        except subprocess.TimeoutExpired as exc:
            raise ToolchainError(f"{' '.join(args[:3])} exceeded {timeout:.0f}s") from exc
        finally:
            stop_group(proc)
    finally:
        signal.pthread_sigmask(signal.SIG_SETMASK, previous_mask)
    if proc.returncode != 0:
        detail = (stderr or stdout).strip().splitlines()[-5:]
        raise ToolchainError(f"{' '.join(args[:3])} failed ({proc.returncode}): {' | '.join(detail) or 'no output'}")
    return stdout.strip()


def file_sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with open(path, "rb") as handle:
        while chunk := handle.read(1 << 20):
            digest.update(chunk)
    return digest.hexdigest()


def download_verified(url: str, sha256: str, directory: Path) -> Path:
    """Download to a private file and return it only when its SHA-256 matches the pin."""
    directory.mkdir(parents=True, exist_ok=True)
    fd, name = tempfile.mkstemp(prefix=".download-", dir=directory)
    path = Path(name)
    digest = hashlib.sha256()
    try:
        with os.fdopen(fd, "wb") as out, urllib.request.urlopen(url, timeout=120) as response:
            while chunk := response.read(1 << 20):
                digest.update(chunk)
                out.write(chunk)
    except (OSError, ValueError) as exc:
        path.unlink(missing_ok=True)
        raise ToolchainError(f"download failed for {url}: {exc}") from exc
    except BaseException:
        path.unlink(missing_ok=True)
        raise
    if digest.hexdigest() != sha256:
        path.unlink(missing_ok=True)
        raise ToolchainError(f"SHA-256 mismatch for {url}: expected {sha256}, found {digest.hexdigest()}; archive discarded")
    return path


def extract_member(archive: Path, member: str, destination: Path) -> None:
    """Extract one regular file atomically; links, devices and path tricks are refused."""
    try:
        with tarfile.open(archive, "r:gz") as tar:
            info = tar.getmember(member)
            if not info.isfile():
                raise ToolchainError(f"{archive.name}: {member} is not a regular file")
            source = tar.extractfile(info)
            destination.parent.mkdir(parents=True, exist_ok=True)
            partial = destination.with_name(destination.name + ".partial")
            with source, open(partial, "wb") as out:
                shutil.copyfileobj(source, out)
    except (KeyError, tarfile.TarError, OSError) as exc:
        destination.with_name(destination.name + ".partial").unlink(missing_ok=True)
        raise ToolchainError(f"cannot extract {member} from {archive.name}: {exc}") from exc
    partial.chmod(0o755)
    os.replace(partial, destination)


def release_path(cache: Path, name: str, version: str, archive_sha256: str) -> Path:
    """Cache identity includes the archive digest, so a re-pinned checksum never reuses an older binary."""
    return cache / name / f"{version}-{archive_sha256[:16]}" / name


def ensure_release_binary(name: str, url: str, sha256: str, member: str, destination: Path, version_args: list[str],
                          accept, env: dict[str, str]) -> None:
    """Install one binary from a SHA-256-pinned archive. A cached binary is executed only after its recorded
    extracted-file digest matches, and it must report the pinned version."""
    record = destination.with_name(destination.name + ".sha256")

    def verified() -> bool:
        try:
            if not destination.is_file() or file_sha256(destination) != record.read_text().strip():
                return False
        except OSError:
            return False
        try:
            return accept(run([str(destination), *version_args], env=env))
        except ToolchainError:
            return False

    if verified():
        return
    log(f"installing {name} from {url}")
    destination.unlink(missing_ok=True)
    record.unlink(missing_ok=True)
    archive = download_verified(url, sha256, destination.parent)
    try:
        extract_member(archive, member, destination)
    finally:
        archive.unlink(missing_ok=True)
    record.write_text(file_sha256(destination) + "\n")
    if not verified():
        destination.unlink(missing_ok=True)
        record.unlink(missing_ok=True)
        raise ToolchainError(f"{name} at {destination} does not report the pinned version")


PROVISIONING_DROPPED = ("NODE_OPTIONS", "NODE_PATH", "GOFLAGS", "GOENV")


def provisioning_env(cache: Path, base: dict[str, str]) -> dict[str, str]:
    """Environment for mise/npm/Playwright installers: no inherited mise or npm configuration, no Node preloads,
    and every mise directory - including the system and shared install directories - private to the cache."""
    env = {key: value for key, value in base.items()
           if not key.startswith("MISE_") and not key.lower().startswith("npm_config_") and key not in PROVISIONING_DROPPED}
    state = cache / "mise"
    config = cache / "config"
    for directory in (state / "config", state / "system", config):
        directory.mkdir(parents=True, exist_ok=True)
    for name in ("npmrc", "npmrc-global", "no-default-packages"):
        (config / name).touch()
    env.update({
        "MISE_NO_CONFIG": "1",
        "MISE_DATA_DIR": str(state / "data"),
        "MISE_INSTALLS_DIR": str(state / "data" / "installs"),
        "MISE_CACHE_DIR": str(state / "cache"),
        "MISE_STATE_DIR": str(state / "state"),
        "MISE_CONFIG_DIR": str(state / "config"),
        "MISE_SYSTEM_DIR": str(state / "system"),
        "MISE_SYSTEM_CONFIG_DIR": str(state / "system"),
        "MISE_SYSTEM_DATA_DIR": str(state / "system"),
        "MISE_SYSTEM_INSTALLS_DIR": str(state / "system" / "installs"),
        "MISE_SHARED_INSTALL_DIRS": "",
        "MISE_YES": "1",
        "MISE_GO_SKIP_CHECKSUM": "false",
        "MISE_GO_DOWNLOAD_MIRROR": "https://dl.google.com/go",
        "MISE_NODE_VERIFY": "true",
        "MISE_NODE_MIRROR_URL": "https://nodejs.org/dist/",
        "MISE_NODE_COREPACK": "false",
        "MISE_NODE_NPM_SHIM": "false",
        "MISE_GO_DEFAULT_PACKAGES_FILE": str(config / "no-default-packages"),
        "MISE_NODE_DEFAULT_PACKAGES_FILE": str(config / "no-default-packages"),
        "GOTOOLCHAIN": "local",
        "npm_config_userconfig": str(config / "npmrc"),
        "npm_config_globalconfig": str(config / "npmrc-global"),
        "npm_config_global": "false",
        "npm_config_location": "project",
        "npm_config_registry": "https://registry.npmjs.org/",
        "npm_config_cache": str(cache / "npm"),
        "npm_config_ignore_scripts": "true",
        "npm_config_audit": "false",
        "npm_config_fund": "false",
        "npm_config_update_notifier": "false",
    })
    return env


def ensure_runtime(mise: Path, env: dict[str, str], tool: str, version: str, version_args: list[str], expected: str,
                   intact=lambda _where: True) -> Path:
    """Return the private mise install directory for tool@version after checking the executable reports it.
    An install outside MISE_INSTALLS_DIR is refused before anything in it runs; one that fails `intact` is reinstalled."""
    spec = f"{tool}@{version}"
    installs = Path(env["MISE_INSTALLS_DIR"]).resolve()

    def located() -> Path | None:
        try:
            where = Path(run([str(mise), "where", spec], env=env)).resolve()
        except ToolchainError:
            return None
        if not where.is_relative_to(installs):
            raise ToolchainError(f"mise resolved {spec} to {where}, outside the private install directory {installs}")
        executable = where / "bin" / tool
        try:
            reported = run([str(executable), *version_args], env=env)
        except ToolchainError as exc:
            raise ToolchainError(f"{spec} at {where} is not runnable: {exc}") from exc
        if expected not in reported.split():
            raise ToolchainError(f"{spec} at {where} reports {reported!r}, expected {expected}")
        return where

    where = located()
    if where is None or not intact(where):
        log(f"installing {spec} with project-private mise")
        run([str(mise), "install", *([] if where is None else ["--force"]), spec], env=env)
        where = located()
    if where is None:
        raise ToolchainError(f"mise did not provide {spec}")
    if not intact(where):
        raise ToolchainError(f"{spec} at {where} is not a plain upstream install")
    return where


def npm_without_mise_hook(where: Path) -> bool:
    """mise's optional npm shim calls `mise reshim`, which could reach a host mise; upstream npm is a symlink."""
    return (where / "bin" / "npm").is_symlink()


def lock_entry(lockfile: Path, package: str) -> dict:
    try:
        entry = json.loads(lockfile.read_text())["packages"][f"node_modules/{package}"]
    except (OSError, ValueError, KeyError, TypeError) as exc:
        raise ToolchainError(f"{lockfile} has no node_modules/{package} entry") from exc
    if not entry.get("version") or not entry.get("integrity"):
        raise ToolchainError(f"{lockfile} node_modules/{package} lacks version/integrity")
    return entry


def chromium_revision(browsers_json: Path) -> str:
    try:
        browsers = json.loads(browsers_json.read_text())["browsers"]
        revisions = {item["name"]: item["revision"] for item in browsers}
    except (OSError, ValueError, KeyError, TypeError) as exc:
        raise ToolchainError(f"cannot read Chromium revision from {browsers_json}") from exc
    chromium, shell = revisions.get("chromium"), revisions.get("chromium-headless-shell")
    if not chromium or chromium != shell:
        raise ToolchainError(f"{browsers_json} has inconsistent Chromium revisions {chromium!r}/{shell!r}")
    return chromium


def browsers_installed(browsers: Path, revision: str) -> bool:
    return all((browsers / f"{name}-{revision}" / "INSTALLATION_COMPLETE").is_file() for name in ("chromium", "chromium_headless_shell"))


def ensure_playwright(cache: Path, node_bin: Path, pins: Pins, repo: Path, base: dict[str, str]) -> tuple[Path, str]:
    """`base` must already be a provisioning environment (see provisioning_env)."""
    lockfile = repo / "frontend" / "package-lock.json"
    core = lock_entry(lockfile, "playwright-core")
    test = lock_entry(lockfile, "@playwright/test")
    if core["version"] != pins.playwright or test["version"] != pins.playwright:
        raise ToolchainError(f"frontend lockfile Playwright {test['version']}/{core['version']} != toolchain.json {pins.playwright}")
    package_dir = cache / "playwright-core" / pins.playwright
    installed = package_dir / "node_modules" / "playwright-core"
    env = dict(base, PATH=os.pathsep.join([str(node_bin), base.get("PATH", "")]))

    def matches_lock() -> bool:
        try:
            installed_lock = lock_entry(package_dir / "package-lock.json", "playwright-core")
            version = json.loads((installed / "package.json").read_text())["version"]
        except (ToolchainError, OSError, ValueError, KeyError):
            return False
        return installed_lock["integrity"] == core["integrity"] and version == core["version"]

    if not matches_lock():
        log(f"installing playwright-core {core['version']} (frontend lockfile integrity)")
        shutil.rmtree(package_dir, ignore_errors=True)
        package_dir.mkdir(parents=True)
        (package_dir / "package.json").write_text('{"private": true}\n')
        run([str(node_bin / "npm"), "install", "--ignore-scripts", "--no-audit", "--no-fund", "--save-exact",
             "--no-global", f"playwright-core@{core['version']}"], env=env, cwd=package_dir)
        if not matches_lock():
            raise ToolchainError(f"installed playwright-core {core['version']} does not match the frontend lockfile integrity")
    revision = chromium_revision(installed / "browsers.json")
    browsers = cache / "ms-playwright"
    if not browsers_installed(browsers, revision):
        log(f"installing Playwright Chromium revision {revision}")
        run([str(node_bin / "node"), str(installed / "cli.js"), "install", "chromium"], env=dict(env, PLAYWRIGHT_BROWSERS_PATH=str(browsers)))
        if not browsers_installed(browsers, revision):
            raise ToolchainError(f"Playwright did not install Chromium revision {revision} into {browsers}")
    return browsers, revision


@contextmanager
def provisioning_lock(cache: Path):
    """Serialize provisioning per cache. Installer children are reaped (see run) before this lock is released."""
    cache.mkdir(parents=True, exist_ok=True)
    with open(cache / ".lock", "w") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        yield


def provision(repo: Path, base: dict[str, str]) -> Toolchain:
    pins = load_pins(repo / "toolchain.json")
    cache = cache_root(base)
    require_supported_platform()
    with provisioning_lock(cache):
        env = provisioning_env(cache, base)
        mise = release_path(cache, "mise", pins.mise, pins.mise_sha256)
        ensure_release_binary("mise", MISE_URL.format(version=pins.mise), pins.mise_sha256, "mise/bin/mise", mise,
                              ["--version"], lambda out: out.split()[:1] == [pins.mise], env)
        go_root = ensure_runtime(mise, env, "go", pins.go, ["version"], f"go{pins.go}")
        node_root = ensure_runtime(mise, env, "node", pins.node, ["--version"], f"v{pins.node}", npm_without_mise_hook)
        sqlc = release_path(cache, "sqlc", pins.sqlc, pins.sqlc_sha256)
        ensure_release_binary("sqlc", SQLC_URL.format(version=pins.sqlc), pins.sqlc_sha256, "sqlc", sqlc,
                              ["version"], lambda out: out == f"v{pins.sqlc}", env)
        browsers, revision = ensure_playwright(cache, node_root / "bin", pins, repo, env)
    return Toolchain(cache, mise, go_root, node_root / "bin", sqlc.parent, browsers, revision, pins)


def child_env(toolchain: Toolchain, base: dict[str, str]) -> dict[str, str]:
    env = dict(base)
    tool_dirs = [toolchain.go_root / "bin", toolchain.node_bin, toolchain.sqlc_dir]
    env["PATH"] = os.pathsep.join([*(str(path) for path in tool_dirs), base.get("PATH", os.defpath)])
    env["GOROOT"] = str(toolchain.go_root)
    env["GOTOOLCHAIN"] = "local"
    env["GOENV"] = "off"  # persisted `go env -w` settings must not change pinned builds; explicit GO* variables still apply
    env.setdefault("GOCACHE", str(toolchain.cache / "go" / "build"))
    env.setdefault("GOMODCACHE", str(toolchain.cache / "go" / "mod"))
    env["PLAYWRIGHT_BROWSERS_PATH"] = str(toolchain.browsers)
    env["TENDO_TOOLCHAIN_CACHE"] = str(toolchain.cache)
    expected = {"go": toolchain.go_root / "bin" / "go", "gofmt": toolchain.go_root / "bin" / "gofmt",
                "node": toolchain.node_bin / "node", "npm": toolchain.node_bin / "npm", "npx": toolchain.node_bin / "npx",
                "sqlc": toolchain.sqlc_dir / "sqlc"}
    for name, path in expected.items():
        resolved = shutil.which(name, path=env["PATH"])
        if resolved is None or Path(resolved) != path:
            raise ToolchainError(f"{name} resolves to {resolved}, expected pinned {path}")
    return env


def versions(toolchain: Toolchain, env: dict[str, str]) -> str:
    def out(*args: str) -> str:
        return run(list(args), env=env, cwd=ROOT).splitlines()[0]

    def tool(name: str, *args: str) -> str:
        return f"{out(name, *args)} ({shutil.which(name, path=env['PATH'])})"

    rows = (
        ("cache", str(toolchain.cache)),
        ("mise (installer only)", f"{out(str(toolchain.mise), '--version')} ({toolchain.mise})"),
        ("go", tool("go", "version")),
        ("node", tool("node", "--version")),
        ("npm", tool("npm", "--version")),
        ("sqlc", tool("sqlc", "version")),
        ("oapi-codegen", f"{toolchain.pins.oapi_codegen} (go run ...@version from api/Makefile)"),
        ("playwright", f"{toolchain.pins.playwright}, Chromium revision {toolchain.chromium_revision} in {toolchain.browsers}"),
    )
    return "\n".join(f"{name}: {value}" for name, value in rows)


def main(argv: list[str]) -> int:
    if argv == ["--versions"]:
        command = None
    elif len(argv) >= 2 and argv[0] == "--":
        command = argv[1:]
    else:
        print(__doc__.strip().split("\n\n")[1], file=sys.stderr)
        return 2
    base = dict(os.environ)

    first_signal: list[int] = []

    def interrupted(signum, _frame):
        # Only the first signal interrupts provisioning; later ones (including those deferred while installers are
        # being stopped) must not unwind cleanup a second time.
        if not first_signal:
            first_signal.append(signum)
            raise Interrupted(signum)

    handled = (signal.SIGINT, signal.SIGTERM, signal.SIGHUP)
    previous = {signum: signal.signal(signum, interrupted) for signum in handled}
    try:
        toolchain = provision(ROOT, base)
        env = child_env(toolchain, base)
        if command is None:
            print(versions(toolchain, env))
            return 0
    except ToolchainError as exc:
        log(f"error: {exc}")
        return 1
    except Interrupted as exc:
        log(f"interrupted by signal {exc.signum}; installer processes stopped")
        for signum in handled:
            signal.signal(signum, signal.SIG_IGN)
        signal.signal(exc.signum, signal.SIG_DFL)
        os.kill(os.getpid(), exc.signum)
        return 128 + exc.signum
    finally:
        if not first_signal:
            for signum, handler in previous.items():
                signal.signal(signum, handler)
    try:
        os.execvpe(command[0], command, env)
    except OSError as exc:
        log(f"cannot execute {command[0]}: {exc}")
        return 127


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
