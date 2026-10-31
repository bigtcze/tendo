#!/usr/bin/env python3
"""Capture the accessibility bus advertisement visible to the browser process."""
from __future__ import annotations

import ctypes
import ctypes.util
import os
import subprocess
import sys
import time


def set_x_property(name: str, value: str) -> None:
    library = ctypes.util.find_library("X11")
    if not library:
        raise RuntimeError("libX11 not found")
    x11 = ctypes.CDLL(library)
    x11.XOpenDisplay.argtypes = [ctypes.c_char_p]
    x11.XOpenDisplay.restype = ctypes.c_void_p
    x11.XDefaultRootWindow.argtypes = [ctypes.c_void_p]
    x11.XDefaultRootWindow.restype = ctypes.c_ulong
    x11.XInternAtom.argtypes = [ctypes.c_void_p, ctypes.c_char_p, ctypes.c_int]
    x11.XInternAtom.restype = ctypes.c_ulong
    x11.XChangeProperty.argtypes = [ctypes.c_void_p, ctypes.c_ulong, ctypes.c_ulong, ctypes.c_ulong, ctypes.c_int, ctypes.c_int, ctypes.c_void_p, ctypes.c_int]
    display = x11.XOpenDisplay(os.environ.get("DISPLAY", "").encode())
    if not display:
        raise RuntimeError(f"cannot open DISPLAY={os.environ.get('DISPLAY', '')!r}")
    try:
        atom = x11.XInternAtom(display, name.encode(), 0)
        string_type = x11.XInternAtom(display, b"STRING", 0)
        data = ctypes.create_string_buffer(value.encode())
        x11.XChangeProperty(display, x11.XDefaultRootWindow(display), atom, string_type, 8, 0, data, len(value.encode()))
        x11.XSync(display, 0)
    finally:
        x11.XCloseDisplay(display)


def x_property(name: str) -> str:
    library = ctypes.util.find_library("X11")
    if not library:
        return "unavailable: libX11 not found"
    x11 = ctypes.CDLL(library)
    x11.XOpenDisplay.argtypes = [ctypes.c_char_p]
    x11.XOpenDisplay.restype = ctypes.c_void_p
    x11.XDefaultRootWindow.argtypes = [ctypes.c_void_p]
    x11.XDefaultRootWindow.restype = ctypes.c_ulong
    x11.XInternAtom.argtypes = [ctypes.c_void_p, ctypes.c_char_p, ctypes.c_int]
    x11.XInternAtom.restype = ctypes.c_ulong
    x11.XFree.argtypes = [ctypes.c_void_p]
    x11.XGetWindowProperty.argtypes = [
        ctypes.c_void_p, ctypes.c_ulong, ctypes.c_ulong, ctypes.c_long,
        ctypes.c_long, ctypes.c_int, ctypes.c_ulong,
        ctypes.POINTER(ctypes.c_ulong), ctypes.POINTER(ctypes.c_int),
        ctypes.POINTER(ctypes.c_ulong), ctypes.POINTER(ctypes.c_ulong),
        ctypes.POINTER(ctypes.POINTER(ctypes.c_ubyte)),
    ]
    display = x11.XOpenDisplay(os.environ.get("DISPLAY", "").encode())
    if not display:
        return f"unavailable: cannot open DISPLAY={os.environ.get('DISPLAY', '')!r}"
    try:
        atom = x11.XInternAtom(display, name.encode(), 0)
        if not atom:
            return "unset"
        actual_type, actual_format = ctypes.c_ulong(), ctypes.c_int()
        item_count, bytes_after = ctypes.c_ulong(), ctypes.c_ulong()
        value = ctypes.POINTER(ctypes.c_ubyte)()
        result = x11.XGetWindowProperty(
            display, x11.XDefaultRootWindow(display), atom, 0, 1024, 0, 0,
            ctypes.byref(actual_type), ctypes.byref(actual_format),
            ctypes.byref(item_count), ctypes.byref(bytes_after), ctypes.byref(value),
        )
        if result != 0:
            return f"unavailable: XGetWindowProperty status={result}"
        if not value:
            return "unset"
        try:
            return ctypes.string_at(value, item_count.value * max(actual_format.value // 8, 1)).decode(errors="replace")
        finally:
            x11.XFree(value)
    finally:
        x11.XCloseDisplay(display)


def main() -> int:
    if os.environ.get("AT_SPI_BUS"):
        set_x_property("AT_SPI_BUS", os.environ["AT_SPI_BUS"])
    print(f"DISPLAY={os.environ.get('DISPLAY', '')}")
    for attempt in range(20):
        advertised = x_property("AT_SPI_BUS")
        if advertised != "unset":
            break
        if attempt < 19:
            time.sleep(0.1)
    print(f"AT_SPI_BUS={advertised}")
    print(f"_AT_SPI_BUS={x_property('_AT_SPI_BUS')}")
    if advertised != "unset":
        print(f"AT_SPI_BUS advertised value intentionally omitted from log (length={len(advertised)})")
    try:
        result = subprocess.run(
            ["dbus-send", "--bus=" + os.environ["DBUS_SESSION_BUS_ADDRESS"], "--print-reply", "--reply-timeout=2000",
             "--dest=org.a11y.Bus", "/org/a11y/bus", "org.a11y.Bus.GetAddress"],
            capture_output=True, text=True, check=False,
        )
        print(f"org.a11y.Bus.GetAddress status={result.returncode} output={result.stdout.strip() or result.stderr.strip()}")
    except OSError as error:
        print(f"org.a11y.Bus.GetAddress unavailable: {error}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
