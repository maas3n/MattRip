#!/usr/bin/env python3
"""Install and actually launch a built APK on the connected CI emulator."""
from pathlib import Path
import re
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
    xml = adb("shell", "cat", "/sdcard/mattmux-startup.xml")
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
    for label in ("DVD Remux", "Advanced Merger", "BATCH", "CLI"):
        tree = screen("before-" + label.replace(" ", "-"))
        node = next((n for n in tree.iter("node") if n.get("text", "").casefold() == label.casefold()), None)
        if node is None:
            raise RuntimeError(f"Missing tab after launch: {label}")
        x1, y1, x2, y2 = map(int, re.findall(r"\d+", node.attrib["bounds"]))
        adb("shell", "input", "tap", str((x1 + x2) // 2), str((y1 + y2) // 2))
        time.sleep(1)
        adb("shell", "pidof", package)
    screen("final")
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
