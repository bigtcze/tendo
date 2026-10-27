#!/usr/bin/env python3
"""Run native production-binary Playwright E2E under bounded child supervision."""
from __future__ import annotations

import argparse
import base64
import hashlib
import io
import os
import re
import secrets
import shutil
import json
import tempfile
import signal
import socket
import subprocess
import sys
import threading
import tarfile
import time
import uuid
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

# Direct script execution and spec_from_file_location tests use different sys.path roots.
SCRIPTS_DIR = str(Path(__file__).resolve().parent)
if SCRIPTS_DIR not in sys.path:
    sys.path.insert(0, SCRIPTS_DIR)

import e2e_db_fixtures as db_fixtures


# Generated credentials are registered here so failure output can be redacted before printing.
SENSITIVE_VALUES: set[str] = set()
FAILURE_LOG_TAIL_LINES = 200
EXPECTED_DIAGNOSTICS_FINGERPRINT = "249E445F0DB60EC10F16136EA1E1C31A529EE4CA"
MAX_DIAGNOSTIC_FILES = 10_000
MAX_DIAGNOSTIC_FILE_SIZE = 256 * 1024 * 1024
MAX_DIAGNOSTIC_TOTAL_SIZE = 512 * 1024 * 1024
MAX_DIAGNOSTIC_CIPHERTEXT_SIZE = 550 * 1024 * 1024
CANCELLED_STAGING_BUDGET = 15
_cancel_deadline: float | None = None
ROUTINE_ACCESS_LINE = re.compile(r" INFO http request .* status=[23]\d\d ")


def register_sensitive_value(value: str) -> None:
    if not value:
        return
    variants = {
        value,
        urllib.parse.quote(value, safe=""),
        urllib.parse.quote_plus(value),
        json.dumps(value)[1:-1],
        json.dumps(value)[1:-1].replace("/", "\\/"),
    }
    for encoded in (base64.b64encode(value.encode()), base64.urlsafe_b64encode(value.encode())):
        variants.add(encoded.decode())
        variants.add(encoded.decode().rstrip("="))
    SENSITIVE_VALUES.update(variants)


def generated_secret(nbytes: int) -> str:
    value = base64.b64encode(secrets.token_bytes(nbytes)).decode("ascii")
    register_sensitive_value(value)
    return value


def register_database_url(value: str):
    """Register a database URL plus its encoded and decoded password for failure-log redaction."""
    if not value:
        return
    register_sensitive_value(value)
    password = urllib.parse.urlsplit(value).password
    if password:
        register_sensitive_value(password)
        register_sensitive_value(urllib.parse.unquote(password))


ANSI_ESCAPE = re.compile(r"\x1b\[[0-9;?]*[ -/]*[@-~]")
# Structural redaction for secrets the runner never sees by value: browser/API cookies, auth headers,
# OIDC callback parameters, invitation fragments, and any credential-bearing URL.
STRUCTURAL_PATTERNS = (
    (re.compile(r"(?i)\b(set-cookie|cookie|authorization|proxy-authorization|x-csrf-token)(\s*[:=]\s*)[^\r\n]*"), r"\1\2[redacted]"),
    (re.compile(r"(?i)\b(tendo_session[A-Za-z0-9_-]*)=[^;\s'\"`,)]+"), r"\1=[redacted]"),
    (re.compile(r"(?i)([?&#](?:code|state|nonce|token|id_token|access_token|refresh_token|session_state|client_secret|password)=)[^&#\s'\"`)]+"), r"\1[redacted]"),
    (re.compile(r"(?i)(/invite#)[^\s'\"`)]+"), r"\1[redacted]"),
    (re.compile(r"(?i)\b((?:postgres(?:ql)?|https?)://[^:/@\s'\"`]+:)[^@\s'\"`]+@"), r"\1[redacted]@"),
    (re.compile(r'(?i)(["\'](?:password|passwd|secret|setup[_ -]?token|session[_ -]?token|sessionToken)["\']\s*:\s*)["\'][^"\']*["\']'), r'\1"[redacted]"'),
    (re.compile(r'(?is)(["\']name["\']\s*:\s*["\'](?:tendo_session[^"\']*|[^"\']*cookie[^"\']*)["\'][^{}]*?["\']value["\']\s*:\s*)["\'][^"\']*["\']'), r'\1"[redacted]"'),
    (re.compile(r'(?i)(["\']value["\']\s*:\s*)["\'][^"\']*["\'](?=\s*[,}])'), r'\1"[redacted]"'),
    (re.compile(r"(?i)(?<![A-Za-z0-9_-])#([A-Za-z0-9_-]{20,})"), r"#[redacted]"),
    (re.compile(r"(?<![A-Za-z0-9_-])(?=[A-Za-z0-9_+/=-]{24,}(?![A-Za-z0-9_+/=-]))(?=[A-Za-z0-9_+/-]*[A-Z])(?=[A-Za-z0-9_+/-]*[a-z])(?=[A-Za-z0-9_+/-]*[0-9])[A-Za-z0-9_+/=-]{24,}"), "[redacted]"),
    (re.compile(r"(?i)\b(password|passwd|secret|setup[_ -]?token|session[_ -]?token|sessionToken)(\s*[:=]\s*)['\"`]?[^\s'\"`,;)]+"), r"\1\2[redacted]"),
)


def redact(text: str) -> str:
    text = ANSI_ESCAPE.sub("", text)
    for value in sorted(SENSITIVE_VALUES, key=len, reverse=True):
        text = text.replace(value, "[redacted]")
    for pattern, replacement in STRUCTURAL_PATTERNS:
        text = pattern.sub(replacement, text)
    return text


def write_redacted_process_log(log_path: Path) -> Path | None:
    """Write the full process output after applying the same credential redaction used for CI logs."""
    try:
        text = log_path.read_text(encoding="utf-8", errors="replace")
        redacted_path = log_path.with_name("process.redacted.log")
        redacted_path.write_text(redact(text), encoding="utf-8")
        return redacted_path
    except OSError as exc:
        print(f"Redacted E2E process log unavailable: {exc}", file=sys.stderr)
        return None


def sanitize_json_value(value):
    if isinstance(value, dict):
        sanitized = {}
        for key, item in value.items():
            lowered = key.lower()
            # Raw output streams and inline attachment bodies (base64, unredactable) are dropped.
            if lowered in {"stdout", "stderr", "body"}:
                continue
            if lowered in {"password", "passwd", "secret", "client_secret", "setup_token", "cookie", "set-cookie", "authorization"}:
                sanitized[key] = "[redacted]"
            else:
                sanitized[key] = sanitize_json_value(item)
        return sanitized
    if isinstance(value, list):
        return [
            sanitize_json_value(item)
            for item in value
            if not (isinstance(item, dict) and str(item.get("name", "")).lower() in {"stdout", "stderr"})
        ]
    if isinstance(value, str):
        return redact(value)
    return value


def validate_upload_destination(destination: Path) -> None:
    parent = destination.parent
    if parent.is_symlink() or not parent.is_dir():
        raise RuntimeError(f"E2E CI upload parent must be an existing real directory: {parent}")
    if os.path.lexists(destination):
        raise RuntimeError(f"E2E CI upload destination already exists: {destination}")


def verify_diagnostics_recipient(recipient_file: Path, expected_fpr: str, gpg: str = "gpg") -> bool:
    """Check exactly one expected primary fingerprint from the public certificate."""
    try:
        with tempfile.TemporaryDirectory(prefix="tendo-gpg-check-") as temporary:
            home = Path(temporary) / "home"
            home.mkdir(mode=0o700)
            result = subprocess.run(
                [gpg, "--no-options", "--batch", "--no-tty", "--homedir", str(home),
                 "--with-colons", "--import-options", "show-only", "--import", str(recipient_file)],
                stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, timeout=10, check=False,
            )
        records = [line.split(":") for line in result.stdout.decode(errors="replace").splitlines()]
        fingerprints = [parts[9].upper() for parts in records if parts[0] == "fpr" and len(parts) > 9]
        primaries = [parts for parts in records if parts[0] in {"pub", "sec"}]
        if len(primaries) != 1:
            return False
        primary = primaries[0]
        now = int(time.time())
        expiry = int(primary[6]) if primary and len(primary) > 6 and primary[6] else 0
        return (result.returncode == 0 and fingerprints and fingerprints[0] == expected_fpr.upper()
                and primary is not None and primary[1] not in {"e", "r", "d"} and (not expiry or expiry > now))
    except (OSError, subprocess.SubprocessError):
        return False


def _effective_staging_deadline(deadline: float) -> float:
    return min(deadline, _cancel_deadline) if _cancel_deadline is not None else deadline


def stage_encrypted_diagnostics(destination_dir: Path, source_dir: Path, recipient_file: Path,
                                expected_fpr: str = EXPECTED_DIAGNOSTICS_FINGERPRINT) -> bool:
    """Encrypt an allowlisted per-run artifact tree; publish only complete ciphertext and manifest."""
    ciphertext = destination_dir / "diagnostics.tar.gpg"
    manifest_path = destination_dir / "diagnostics.manifest.json"
    reason = "ENCRYPTION_FAILED"
    try:
        if not verify_diagnostics_recipient(recipient_file, expected_fpr):
            reason = "RECIPIENT_INVALID"
            raise RuntimeError
        deadline = _effective_staging_deadline(time.monotonic() + 120)
        source_root = source_dir.resolve(strict=True)
        items = []
        total_size = 0
        for current, dirs, files in os.walk(source_root, followlinks=False):
            current_path = Path(current)
            dirs[:] = sorted(name for name in dirs if not (current_path / name).is_symlink())
            for name in sorted(files):
                path = current_path / name
                if path.is_symlink() or not path.is_file() or name == "process.log":
                    continue
                relative_candidate = path.relative_to(source_root).as_posix()
                if not (relative_candidate.startswith("html-report-") or relative_candidate.startswith("artifacts/")
                        or ("/html-report-" in relative_candidate) or ("/artifacts/" in relative_candidate)
                        or ("/" not in relative_candidate and ((name.startswith("report-") and path.suffix == ".json")
                            or name in {"attention-clock.json", "process.redacted.log"}))):
                    continue
                deadline = _effective_staging_deadline(deadline)
                if time.monotonic() > deadline:
                    reason = "TIMEOUT"
                    raise RuntimeError
                resolved = path.resolve(strict=True)
                relative = resolved.relative_to(source_root).as_posix()
                if relative.startswith("../") or relative == "..":
                    reason = "SOURCE_INVALID"
                    raise RuntimeError
                size = path.stat().st_size
                if size > MAX_DIAGNOSTIC_FILE_SIZE:
                    reason = "SIZE_LIMIT"
                    raise RuntimeError
                total_size += size
                items.append((relative, path, size))
                if len(items) > MAX_DIAGNOSTIC_FILES or total_size > MAX_DIAGNOSTIC_TOTAL_SIZE:
                    reason = "SIZE_LIMIT"
                    raise RuntimeError
        with tempfile.TemporaryDirectory(prefix="tendo-encrypted-diag-", dir=destination_dir.parent) as private:
            private_dir = Path(private)
            os.chmod(private_dir, 0o700)
            tar_path = private_dir / "diagnostics.tar"
            encrypted_path = private_dir / "diagnostics.tar.gpg"
            entries = []
            with tarfile.open(tar_path, "w", format=tarfile.PAX_FORMAT) as archive:
                for relative, path, size in items:
                    deadline = _effective_staging_deadline(deadline)
                    if time.monotonic() > deadline:
                        reason = "TIMEOUT"
                        raise RuntimeError
                    hasher = hashlib.sha256()
                    with path.open("rb") as source:
                        for block in iter(lambda: source.read(1024 * 1024), b""):
                            hasher.update(block)
                    digest = hasher.hexdigest()
                    entries.append({"path": relative, "size": size, "sha256": digest})
                    info = archive.gettarinfo(str(path), arcname=relative)
                    info.uid = info.gid = 0
                    info.uname = info.gname = ""
                    info.mtime = 0
                    with path.open("rb") as stream:
                        archive.addfile(info, stream)
                manifest_data = json.dumps(entries, sort_keys=True, separators=(",", ":")).encode()
                info = tarfile.TarInfo("MANIFEST.json")
                info.size = len(manifest_data); info.uid = info.gid = 0; info.mtime = 0
                archive.addfile(info, io.BytesIO(manifest_data))
            gpg = shutil.which("gpg")
            if not gpg:
                reason = "GPG_UNAVAILABLE"
                raise RuntimeError
            home = private_dir / "gnupg"
            home.mkdir(mode=0o700)
            command = [gpg, "--no-options", "--batch", "--no-tty", "--homedir", str(home),
                       "--trust-model", "always", "--compress-algo", "none", "--cipher-algo", "AES256",
                       "--recipient-file", str(recipient_file), "--output", str(encrypted_path), "--encrypt"]
            process = subprocess.Popen([*command, str(tar_path)], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            while process.poll() is None:
                deadline = _effective_staging_deadline(deadline)
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    process.kill()
                    process.wait()
                    reason = "TIMEOUT"
                    raise RuntimeError
                try:
                    process.wait(timeout=min(0.1, remaining))
                except subprocess.TimeoutExpired:
                    continue
            if process.returncode:
                reason = "GPG_FAILED"
                raise RuntimeError
            size = encrypted_path.stat().st_size
            if size > MAX_DIAGNOSTIC_CIPHERTEXT_SIZE:
                reason = "SIZE_LIMIT"
                raise RuntimeError
            hasher = hashlib.sha256()
            with encrypted_path.open("rb") as source:
                for block in iter(lambda: source.read(1024 * 1024), b""):
                    hasher.update(block)
            digest = hasher.hexdigest()
            public_manifest = {"format_version": 1, "recipient_fingerprint": expected_fpr.upper(),
                               "ciphertext_sha256": digest, "ciphertext_size": size}
            for key, variable in (("github_run_id", "GITHUB_RUN_ID"), ("github_run_attempt", "GITHUB_RUN_ATTEMPT"), ("github_sha", "GITHUB_SHA")):
                if os.environ.get(variable): public_manifest[key] = os.environ[variable]
            staged_ciphertext = destination_dir / ".diagnostics.tar.gpg.tmp"
            shutil.copyfile(encrypted_path, staged_ciphertext)
            os.replace(staged_ciphertext, ciphertext)
            staged_manifest = destination_dir / ".diagnostics.manifest.json.tmp"
            staged_manifest.write_text(json.dumps(public_manifest, sort_keys=True, indent=2) + "\n", encoding="utf-8")
            os.replace(staged_manifest, manifest_path)
        return True
    except BaseException:
        ciphertext.unlink(missing_ok=True)
        manifest_path.unlink(missing_ok=True)
        (destination_dir / ".diagnostics.tar.gpg.tmp").unlink(missing_ok=True)
        (destination_dir / ".diagnostics.manifest.json.tmp").unlink(missing_ok=True)
        try:
            (destination_dir / "ENCRYPTED_DIAGNOSTICS_FAILED").write_text(reason + "\n", encoding="ascii")
        except OSError:
            pass
        return False


def write_ci_upload_ready() -> None:
    output_path = os.environ.get("GITHUB_OUTPUT")
    if output_path:
        with open(output_path, "a", encoding="utf-8") as output:
            output.write("ci_upload=ready\n")


def stage_ci_upload(destination: Path, source_dir: Path) -> bool:
    """Sanitize to a new sibling and publish only by non-overwriting rename."""
    temporary = None
    try:
        temporary = Path(tempfile.mkdtemp(prefix=f".{destination.name}-", dir=destination.parent))
        recipient = Path(os.environ.get("E2E_DIAGNOSTICS_RECIPIENT", str(Path(__file__).resolve().parents[1] / "ci/e2e-artifact-recipient.asc")))
        encrypted_ok = stage_encrypted_diagnostics(temporary, source_dir, recipient)
        if not encrypted_ok and not (temporary / "ENCRYPTED_DIAGNOSTICS_FAILED").exists():
            (temporary / "ENCRYPTED_DIAGNOSTICS_FAILED").write_text("ENCRYPTION_FAILED\n", encoding="ascii")
        redacted_log = source_dir / "process.redacted.log"
        if redacted_log.is_symlink() or not redacted_log.is_file():
            raise FileNotFoundError("redacted process log missing or symlinked")
        (temporary / "process.redacted.log").write_text(redact(redacted_log.read_text(encoding="utf-8", errors="replace")), encoding="utf-8")
        for current, dirs, files in os.walk(source_dir, followlinks=False):
            current_path = Path(current)
            dirs[:] = [name for name in dirs if not (current_path / name).is_symlink()]
            for name in files:
                path = current_path / name
                if path.is_symlink() or not path.is_file():
                    continue
                if path.name.startswith("report-") and path.suffix == ".json":
                    payload = json.loads(path.read_text(encoding="utf-8"))
                    (temporary / path.name).write_text(json.dumps(sanitize_json_value(payload), ensure_ascii=False, indent=2), encoding="utf-8")
                elif path.name == "error-context.md":
                    (temporary / f"{path.parent.name}-error-context.md").write_text(redact(path.read_text(encoding="utf-8", errors="replace")), encoding="utf-8")
        (temporary / "STAGED_OK").write_text("ok\n", encoding="utf-8")
    except BaseException as exc:
        print("E2E artifact sanitization failed; only safe artifacts will be staged.", file=sys.stderr)
        if temporary is not None:
            try:
                for child in list(temporary.iterdir()):
                    if child.name not in {"diagnostics.tar.gpg", "diagnostics.manifest.json", "ENCRYPTED_DIAGNOSTICS_FAILED"}:
                        shutil.rmtree(child) if child.is_dir() else child.unlink()
            except OSError as cleanup_error:
                print(f"E2E artifact temporary cleanup failed: {cleanup_error}", file=sys.stderr)
                return False
        try:
            if temporary is None:
                temporary = Path(tempfile.mkdtemp(prefix=f".{destination.name}-", dir=destination.parent))
            (temporary / "SANITIZE_FAILED").write_text("E2E artifact sanitization failed\n", encoding="utf-8")
        except OSError as marker_error:
            print(f"E2E artifact failure marker staging failed: {marker_error}", file=sys.stderr)
            if temporary is not None:
                try:
                    shutil.rmtree(temporary)
                except OSError as cleanup_error:
                    print(f"E2E artifact temporary cleanup failed: {cleanup_error}", file=sys.stderr)
            return False
    try:
        if os.path.lexists(destination):
            raise FileExistsError(f"E2E CI upload destination appeared during staging: {destination}")
        os.rename(temporary, destination)
    except OSError as rename_error:
        print(f"E2E artifact publication failed: {rename_error}", file=sys.stderr)
        try:
            shutil.rmtree(temporary)
        except OSError as cleanup_error:
            print(f"E2E artifact temporary cleanup failed: {cleanup_error}", file=sys.stderr)
        return False
    try:
        write_ci_upload_ready()
    except OSError as output_error:
        print(f"E2E CI upload output could not be written: {output_error}", file=sys.stderr)
        try:
            if destination.is_symlink():
                destination.unlink()
            elif destination.exists():
                shutil.rmtree(destination)
        except OSError as cleanup_error:
            print(f"E2E CI upload cleanup failed: {cleanup_error}", file=sys.stderr)
        return False
    return True


def print_failure_log_tail(log_path: Path, lines: int = FAILURE_LOG_TAIL_LINES):
    """CI keeps no runner files; surface Playwright/app output so a failure is diagnosable from the job log."""
    try:
        all_lines = log_path.read_text(encoding="utf-8", errors="replace").splitlines()
    except OSError as exc:
        print(f"E2E process log unavailable: {exc}", file=sys.stderr)
        return
    # Successful per-request access lines would otherwise push Playwright's failure report out of the tail.
    kept = [line for line in all_lines if not ROUTINE_ACCESS_LINE.search(line)]
    tail = kept[-lines:]
    omitted = len(all_lines) - len(kept)
    print(f"---- last {len(tail)} lines of {log_path} ({omitted} successful access-log lines omitted; credentials redacted) ----", file=sys.stderr)
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


def run_playwright(root, env, children, *args, timeout=600):
    env = env.copy()
    report_dir = Path(env.get("E2E_ARTIFACT_DIR", root / "frontend/test-results"))
    invocation = uuid.uuid4().hex
    env["PLAYWRIGHT_HTML_OUTPUT_DIR"] = str(report_dir / f"html-report-{invocation}")
    env["PLAYWRIGHT_JSON_OUTPUT_FILE"] = str(report_dir / f"report-{invocation}.json")
    cmd = [str(root / "frontend/node_modules/.bin/playwright"), "test", *args]
    children.run(cmd, env, "Playwright", root / "frontend", timeout)


def run_screen_reader(root: Path, env: dict[str, str], children: Children, artifact_dir: Path, audit_dir: Path):
    audit_dir = audit_dir.resolve()
    audit_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
    os.chmod(audit_dir, 0o700)
    # Invalidate previous results first so no failed invocation can leave earlier evidence looking current.
    for name in ("transcript.json", "transcript.md", "manifest.json", "orca-debug.out"):
        (audit_dir / name).unlink(missing_ok=True)
    if os.environ.get("TENDO_SR_ORCA_NO_START") == "1":
        raise RuntimeError("Orca is required for screen-reader audit but TENDO_SR_ORCA_NO_START=1 disabled it")
    # Runtime and debug paths are always derived from the canonical audit directory; caller-supplied
    # paths are never trusted for destructive cleanup.
    runtime = audit_dir / "runtime"
    debug = audit_dir / "orca-debug.out"
    if runtime.is_symlink() or (runtime.exists() and not runtime.is_dir()):
        raise RuntimeError(f"screen-reader runtime path is not a real directory: {runtime}")
    if runtime.exists():
        import shutil
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
    proc = children.start([str(session), str(runtime), str(debug), str(logs)], env, "private Orca session")
    try:
        ready = logs / "ready"
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline and not ready.exists():
            if proc.poll() is not None:
                raise RuntimeError("Orca session exited before startup (check host prerequisites)")
            time.sleep(.1)
        if not ready.exists():
            raise RuntimeError("Orca session did not become ready within 15 seconds")
        env.update({"HOME": str(runtime / "home"), "XDG_RUNTIME_DIR": str(runtime / "xdg-runtime"), "XDG_CONFIG_HOME": str(runtime / "xdg-config"), "XDG_DATA_HOME": str(runtime / "xdg-data"), "TENDO_SR_ORCA_NO_START": os.environ.get("TENDO_SR_ORCA_NO_START", "0"), "DISPLAY": (runtime / "display").read_text(encoding="utf-8").strip(), "DBUS_SESSION_BUS_ADDRESS": (logs / "bus-address").read_text(encoding="utf-8").strip(), "E2E_SR_DEBUG_FILE": str(debug), "E2E_SR_AUDIT_DIR": str(audit_dir.resolve()), "E2E_SR_HEADFUL": "1", "GIT_COMMIT": subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip(), "GIT_DIRTY": "true" if subprocess.check_output(["git", "status", "--porcelain"], cwd=root, text=True).strip() else "false"})
        # The helper owns the display choice and publishes it to its ready file for the browser process.
        display_file = runtime / "display"
        if display_file.exists():
            env["DISPLAY"] = display_file.read_text(encoding="utf-8").strip()
        env["PLAYWRIGHT_OUTPUT_DIR"] = str(artifact_dir / "artifacts")
        run_playwright(root, env, children, "e2e/screen-reader.spec.ts", timeout=1800)
    finally:
        children.stop(proc, grace=5)
        # speech-dispatcher may daemonize into a separate session; stop only those processes
        # whose command line points at this run's private runtime.
        for entry in Path("/proc").iterdir():
            if not entry.name.isdigit():
                continue
            try:
                command = (entry / "cmdline").read_bytes().decode(errors="replace")
            except OSError:
                continue
            if str(runtime) in command:
                try:
                    os.kill(int(entry.name), signal.SIGTERM)
                except (ProcessLookupError, PermissionError):
                    pass


def run_screenshot(root: Path, app_path: Path, inherited: dict[str, str], children: Children, artifact_dir: Path, screenshot_path: Path | None = None, audit_dir: Path | None = None, sr_audit_dir: Path | None = None):
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
    SENSITIVE_VALUES.add(setup_token)
    base.update({
        "E2E_BASE_URL": app_origin,
        "E2E_SETUP_TOKEN": setup_token,
        "E2E_OWNER_PASSWORD": generated_secret(24),
        "PLAYWRIGHT_OUTPUT_DIR": str(artifact_dir / "artifacts"),
        "E2E_ARTIFACT_DIR": str(artifact_dir),
    })
    if sum(value is not None for value in (screenshot_path, audit_dir, sr_audit_dir)) > 1:
        raise RuntimeError("screenshot, UX audit and screen-reader audit modes are mutually exclusive")
    if sr_audit_dir is not None:
        base["E2E_SR_AUDIT_DIR"] = str(sr_audit_dir.resolve())
        base["E2E_SR_DEBUG_FILE"] = str(sr_audit_dir.resolve() / "orca-debug.out")
        base["E2E_SR_RUN_DIR"] = str(sr_audit_dir.resolve() / "runtime")
        base["E2E_TEST_CLOCK_NOW"] = env["TENDO_TEST_CLOCK_NOW"]
        base["TENDO_SR_AUDIT_DIR"] = str(sr_audit_dir.resolve())
        exact_command = ["bash", "scripts/dev-exec.sh", "--", "bash", "scripts/e2e-smoke.sh"]
        base["TENDO_SR_COMMAND"] = " ".join(f"{key}={value}" for key, value in sorted(os.environ.items()) if key in {"TENDO_SR_AUDIT_DIR", "TENDO_SR_ITEM_PROBE"} and value) + " " + " ".join(exact_command)
        base["TENDO_SR_COMMAND"] = base["TENDO_SR_COMMAND"].strip()
        run_screen_reader(root, base, children, artifact_dir, sr_audit_dir)
    elif audit_dir is not None:
        base["E2E_AUDIT_DIR"] = str(audit_dir.resolve())
        run_playwright(root, base, children, "e2e/ux-audit.spec.ts", timeout=1800)
    else:
        if screenshot_path is None:
            raise RuntimeError("screenshot path is required for README capture")
        base["E2E_SCREENSHOT_PATH"] = str(screenshot_path.resolve())
        run_playwright(root, base, children, "e2e/readme-screenshot.spec.ts")


def run_suite(root: Path, app_path: Path, provider_path: Path, inherited: dict[str, str], children: Children, artifact_dir: Path):
    env = {key: inherited[key] for key in ("PATH", "HOME", "TMPDIR", "LANG") if key in inherited}
    for key in ("PLAYWRIGHT_BROWSERS_PATH", "CI", "TENDO_E2E_READ_DELAY_MS"):
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
    foreign_household_id, foreign_subject_id, foreign_item_id = db_fixtures.seed_foreign_household(inherited)
    conflict_household_id = db_fixtures.seed_existing_invitation_accounts(inherited, register_sensitive_value)

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
        "E2E_ARTIFACT_DIR": str(artifact_dir),
        "E2E_FOREIGN_HOUSEHOLD_ID": foreign_household_id,
        "E2E_FOREIGN_SUBJECT_ID": foreign_subject_id,
        "E2E_FOREIGN_ITEM_ID": foreign_item_id,
        "E2E_EXISTING_PASSWORD": db_fixtures.EXISTING_ACCOUNT_PASSWORD,
        "E2E_CONFLICT_HOUSEHOLD_ID": conflict_household_id,
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
    db_fixtures.assert_foreign_seed_unchanged(inherited, foreign_household_id, foreign_subject_id, foreign_item_id)
    db_fixtures.assert_existing_invitation_accounts(inherited, conflict_household_id)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", type=Path, required=True)
    parser.add_argument("--app", type=Path, required=True)
    parser.add_argument("--provider", type=Path, required=True)
    parser.add_argument("--screenshot", type=Path)
    parser.add_argument("--audit-dir", type=Path)
    parser.add_argument("--sr-audit-dir", type=Path)
    args = parser.parse_args()
    selected = [args.screenshot, args.audit_dir, args.sr_audit_dir]
    if sum(value is not None for value in selected) > 1:
        parser.error("--screenshot, --audit-dir and --sr-audit-dir are mutually exclusive")
    root = args.root.resolve()
    for key in ("TEST_DATABASE_ADMIN_URL", "TEST_DATABASE_URL", "TENDO_TEST_POSTGRES_ADMIN_URL"):
        register_database_url(os.environ.get(key, ""))
    test_results_dir = root / "frontend/test-results"
    test_results_dir.mkdir(parents=True, exist_ok=True)
    upload_destination = Path(os.environ["E2E_CI_UPLOAD_DIR"]) if os.environ.get("E2E_CI_UPLOAD_DIR") else None
    recipient_file = Path(os.environ.get("E2E_DIAGNOSTICS_RECIPIENT", str(root / "ci/e2e-artifact-recipient.asc")))
    if upload_destination is not None:
        validate_upload_destination(upload_destination)
        if not shutil.which("gpg") or not verify_diagnostics_recipient(recipient_file, EXPECTED_DIAGNOSTICS_FINGERPRINT):
            print("Encrypted E2E diagnostics preflight failed; failure marker will be staged.", file=sys.stderr)
    children = Children()
    artifact_dir = test_results_dir / f"native-{uuid.uuid4().hex}"
    artifact_dir.mkdir(parents=True, exist_ok=False)
    global _cancel_deadline
    _cancel_deadline = None
    signal_seen = None
    previous_handlers = {}
    result_error = None
    cleanup_error_seen = None
    def handle_signal(signum, _frame):
        nonlocal signal_seen
        global _cancel_deadline
        signal_seen = signum
        children.cancelled = True
        _cancel_deadline = time.monotonic() + CANCELLED_STAGING_BUDGET
    for signum in (signal.SIGTERM, signal.SIGINT):
        previous_handlers[signum] = signal.signal(signum, handle_signal)
    try:
        if args.screenshot or args.audit_dir or args.sr_audit_dir:
            run_screenshot(root, args.app.resolve(), os.environ.copy(), children, artifact_dir, args.screenshot, args.audit_dir, args.sr_audit_dir)
            if args.sr_audit_dir:
                print(f"Screen-reader audit artifacts saved at {args.sr_audit_dir.resolve()}")
            elif args.audit_dir:
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
            try:
                children.close()
            except BaseException as cleanup_error:
                cleanup_error_seen = cleanup_error
                if result_error is not None:
                    print(f"secondary E2E child cleanup failure: {cleanup_error}", file=sys.stderr)
            if result_error is not None or cleanup_error_seen is not None or signal_seen is not None:
                if signal_seen is not None:
                    _cancel_deadline = time.monotonic() + CANCELLED_STAGING_BUDGET
                write_redacted_process_log(artifact_dir / "process.log")
                if upload_destination is not None:
                    stage_ci_upload(upload_destination, artifact_dir)
                print_failure_log_tail(artifact_dir / "process.log")
                print(f"E2E failure artifacts retained at {artifact_dir}", file=sys.stderr)
            if result_error is None and cleanup_error_seen is not None:
                raise cleanup_error_seen
            if signal_seen is not None:
                raise SystemExit(128 + signal_seen)
        finally:
            for signum, previous in previous_handlers.items():
                signal.signal(signum, previous)
            _cancel_deadline = None


if __name__ == "__main__":
    try:
        main()
    except BaseException as exc:
        print(f"native E2E failed: {exc}", file=sys.stderr)
        raise SystemExit(1)
