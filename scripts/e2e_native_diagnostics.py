from __future__ import annotations

import base64
import hashlib
import io
import json
import os
import re
import secrets
import shutil
import subprocess
import sys
import tarfile
import tempfile
import time
import urllib.parse
from pathlib import Path

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
    (re.compile(
        r"(?i)([?&#](?:code|state|nonce|token|id_token|access_token|refresh_token|session_state|client_secret|password)=)"
        r"[^&#\s'\"`)]+"
    ), r"\1[redacted]"),
    (re.compile(r"(?i)(/invite#)[^\s'\"`)]+"), r"\1[redacted]"),
    (re.compile(r"(?i)\b((?:postgres(?:ql)?|https?)://[^:/@\s'\"`]+:)[^@\s'\"`]+@"), r"\1[redacted]@"),
    (re.compile(r'(?i)(["\'](?:password|passwd|secret|setup[_ -]?token|session[_ -]?token|sessionToken)["\']\s*:\s*)["\'][^"\']*["\']'), r'\1"[redacted]"'),
    (re.compile(
        r'(?is)(["\']name["\']\s*:\s*["\'](?:tendo_session[^"\']*|[^"\']*cookie[^"\']*)["\']'
        r'[^{}]*?["\']value["\']\s*:\s*)["\'][^"\']*["\']'
    ), r'\1"[redacted]"'),
    (re.compile(r'(?i)(["\']value["\']\s*:\s*)["\'][^"\']*["\'](?=\s*[,}])'), r'\1"[redacted]"'),
    (re.compile(r"(?i)(?<![A-Za-z0-9_-])#([A-Za-z0-9_-]{20,})"), r"#[redacted]"),
    (re.compile(
        r"(?<![A-Za-z0-9_-])(?=[A-Za-z0-9_+/=-]{24,}(?![A-Za-z0-9_+/=-]))"
        r"(?=[A-Za-z0-9_+/-]*[A-Z])(?=[A-Za-z0-9_+/-]*[a-z])(?=[A-Za-z0-9_+/-]*[0-9])"
        r"[A-Za-z0-9_+/=-]{24,}"
    ), "[redacted]"),
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
                    context = redact(path.read_text(encoding="utf-8", errors="replace"))
                    (temporary / f"{path.parent.name}-error-context.md").write_text(context, encoding="utf-8")
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


