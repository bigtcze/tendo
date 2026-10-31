#!/usr/bin/env python3
"""Publish the AT-SPI bus address for clients that start before the launcher does."""
from __future__ import annotations

import ctypes
import ctypes.util
import os
import sys


def main() -> int:
    value = os.environ.get("AT_SPI_BUS")
    if not value:
        print("AT_SPI_BUS must contain the private accessibility bus address", file=sys.stderr)
        return 2
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
    x11.XSync.argtypes = [ctypes.c_void_p, ctypes.c_int]
    display = x11.XOpenDisplay(os.environ.get("DISPLAY", "").encode())
    if not display:
        raise RuntimeError(f"cannot open DISPLAY={os.environ.get('DISPLAY', '')!r}")
    try:
        atom = x11.XInternAtom(display, b"AT_SPI_BUS", 0)
        string_type = x11.XInternAtom(display, b"STRING", 0)
        data = ctypes.create_string_buffer(value.encode())
        x11.XChangeProperty(display, x11.XDefaultRootWindow(display), atom, string_type, 8, 0, data, len(value.encode()))
        x11.XSync(display, 0)
    finally:
        x11.XCloseDisplay(display)
    return 0


if __name__ == "__main__":
    sys.exit(main())
