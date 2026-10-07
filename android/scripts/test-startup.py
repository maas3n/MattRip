#!/usr/bin/env python3
"""Install and actually launch a built APK on the connected CI emulator."""
from pathlib import Path
import re
from startup_ui import center_of, find_tab, foreground_anr_title, pixel_launcher_anr_close_button
import subprocess
import sys
import time
import xml.etree.ElementTree as ET

apk = Path(sys.argv[1]).resolve()
logs = Path(sys.argv[2])
logs.mkdir(parents=True, exist_ok=True)
package = "io.github.maas3n.mattrip"


def adb(*args):
    return subprocess.check_output(["adb", *args], text=True, errors="replace", stderr=subprocess.STDOUT, timeout=180)


def screen(name):
    adb("shell", "uiautomator", "dump", "/sdcard/mattrip-startup.xml")
    xml = adb("shell", "cat", "/sdcard/mattrip-startup.xml")
    (logs / (name + ".xml")).write_text(xml)
    return ET.fromstring(xml)


try:
    print(adb("install", "-r", str(apk)))
    adb("shell", "am", "force-stop", package)
    # Some API 26 images refuse log clearing. Preserve their logs instead;
    # successful installation, launch, survival and tab navigation are required.
    launch = adb("shell", "am", "start", "-W", "-n", package + "/io.github.maas3n.mattmux.MainActivity")
    (logs / "launch.txt").write_text(launch)
    if "Status: ok" not in launch or "Error:" in launch:
        raise RuntimeError("Activity failed to launch: " + launch)
    time.sleep(3)
    if not adb("shell", "pidof", package).strip():
        raise RuntimeError("MattRip exited after launch")
    for label in ("REMUX/DEMUX", "ADVANCED", "BATCH", "CLI"):
        safe_label = re.sub(r"[^A-Za-z0-9._-]+", "-", label).strip("-")
        # The 16 KB emulator has occasionally crashed system_server on cold
        # boot, leaving a Pixel Launcher ANR dialog over a *healthy* MattRip.
        # Only clear this known external dialog; do not bypass app failures.
        for attempt in range(12):
            if not adb("shell", "pidof", package).strip():
                raise RuntimeError(f"MattRip exited while looking for tab: {label}")
            tree = screen(f"before-{safe_label}-{attempt}")
            node = find_tab(tree, label)
            title = foreground_anr_title(tree)
            if title is not None:
                close = pixel_launcher_anr_close_button(tree)
                if close is None:
                    raise RuntimeError(f"Unexpected ANR dialog instead of MattRip tab {label}: {title}")
                print("Closing unrelated Pixel Launcher ANR over MattRip", flush=True)
                x, y = center_of(close)
                adb("shell", "input", "tap", str(x), str(y))
            elif node is not None:
                break
            else:
                print(f"Waiting for MattRip tab {label} ({attempt + 1}/12)", flush=True)
            time.sleep(2)
        else:
            raise RuntimeError(f"Missing tab after launch: {label} (after 12 UI checks)")
        x, y = center_of(node)
        adb("shell", "input", "tap", str(x), str(y))
        time.sleep(1)
        if not adb("shell", "pidof", package).strip():
            raise RuntimeError(f"MattRip exited after opening tab: {label}")
    final = screen("final")
    if foreground_anr_title(final) is not None:
        raise RuntimeError("Unexpected ANR dialog after tab navigation: " + foreground_anr_title(final))
    print("APK installed, activity stayed alive, and all four tabs opened.")
    if "DemuxSmokeInstrumentation" in adb("shell", "pm", "list", "instrumentation"):
        result = adb("shell", "am", "instrument", "-w", package + "/io.github.maas3n.mattmux.DemuxSmokeInstrumentation")
        (logs / "demux-smoke.txt").write_text(result)
        if "MATTRIP_DEMUX_SMOKE_PASS" not in result:
            raise RuntimeError("Packaged MediaInfo/demux validation failed: " + result)
        print("Bundled MediaInfo and native demux passed on Android.")
finally:
    logcat = adb("logcat", "-b", "main", "-b", "system", "-b", "crash", "-d", "-v", "threadtime")
    (logs / "logcat.txt").write_text(logcat)
    # Keep the crash reason visible in Actions logs as well as the artifact.
    for line in logcat.splitlines():
        if any(word in line for word in ("AndroidRuntime", "FATAL", "Fatal signal", "mattrip", "mattmux", "DEBUG   :")):
            print(line, flush=True)
    with (logs / "screen.png").open("wb") as out:
        subprocess.run(["adb", "exec-out", "screencap", "-p"], stdout=out, check=False)
