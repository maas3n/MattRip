#!/usr/bin/env python3
"""Fail publication when any package, source, notice, or checksum is missing."""
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
version = sys.argv[2]
required = [
    f"MattRip-{version}-Windows-Setup.exe",
    f"MattRip-{version}-Windows-All-in-One.exe",
    f"MattRip-{version}-Windows-Portable.zip",
    f"MattRip-{version}-Linux-amd64.deb",
    f"MattRip-{version}-Linux-amd64.tar.gz",
    f"MattRip-{version}-Linux-amd64Standalone",
    f"MattRip-{version}-Linux-Library-Sources.tar.gz",
    f"MattRip-{version}-Android.apk",
    f"MattRip-{version}-Source.tar.gz",
    f"MattRip-{version}-THIRD-PARTY.md",
    "DVDNAV_COPYING.txt", "DVDREAD_COPYING.txt", "LIBUDFREAD_COPYING.txt",
    "libdvdnav-6.1.1-source.tar.gz", "libdvdread-6.1.3-source.tar.gz",
    "libudfread-1.1.2-source.tar.gz", "FFmpeg-9.0.1-source.tar.xz",
    "MediaInfoLib-source.tar.gz", "ZenLib-source.tar.gz",
    "MediaInfoLib-License.html", "ZenLib-License.txt", "NDK-NOTICE.txt",
    "SHA256SUMS-Windows.txt", "SHA256SUMS-Linux.txt", "SHA256SUMS-Android.txt",
    "SHA256SUMS.txt",
]
missing = [name for name in required if not (root / name).is_file() or (root / name).stat().st_size == 0]
if missing:
    raise SystemExit("Incomplete cross-platform release: " + ", ".join(missing))
print(f"Complete release: {len(required)} required assets present")
