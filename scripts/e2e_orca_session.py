"""Readiness and private-runtime cleanup for the isolated Orca session."""
from __future__ import annotations

import os
import signal
import time
from pathlib import Path


def readiness_timeout(environ: dict[str, str]) -> int:
    value = environ.get("TENDO_SR_READY_TIMEOUT_SECONDS", "30")
    if not value.isdecimal() or int(value) <= 0:
        raise ValueError("TENDO_SR_READY_TIMEOUT_SECONDS must be a positive integer")
    return int(value)


def wait_until_ready(proc, ready: Path, timeout: int) -> None:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline and not ready.exists():
        if proc.poll() is not None:
            raise RuntimeError("Orca session exited before startup (check host prerequisites)")
        time.sleep(.1)
    if not ready.exists():
        raise RuntimeError(f"Orca session did not become ready within {timeout} seconds")


def cleanup_private_runtime(runtime: Path) -> None:
    # speech-dispatcher may daemonize into a separate session; stop only those
    # processes whose command line points at this run's private runtime.
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
