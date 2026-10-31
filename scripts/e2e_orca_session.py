"""Readiness and private-runtime cleanup for the isolated Orca session."""
from __future__ import annotations

import os
import re
import signal
import subprocess
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


def validate_bus_address(address: str) -> str:
    if not re.fullmatch(r"unix:[^\s]*", address) or not re.search(r"(?:^|,)guid=[0-9a-fA-F]{32}$", address):
        raise RuntimeError("private D-Bus session address is invalid")
    return address


def verify_transport(env: dict[str, str]) -> None:
    dbus_send = ["dbus-send", "--session", "--print-reply", "--reply-timeout=2000"]
    probes = (
        ("session D-Bus", [*dbus_send, "--dest=org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus.GetId"]),
        ("AT-SPI bus", [*dbus_send, "--dest=org.a11y.Bus", "/org/a11y/bus", "org.a11y.Bus.GetAddress"]),
    )
    for name, command in probes:
        try:
            subprocess.run(command, env=env, check=True, capture_output=True, text=True, timeout=3)
        except (OSError, subprocess.SubprocessError) as exc:
            raise RuntimeError(f"{name} transport readiness probe failed") from exc


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
