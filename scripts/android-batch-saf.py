#!/usr/bin/env python3
"""Exercise a released APK through DocumentsUI and verify two different movies."""
from pathlib import Path
import re
import subprocess
import sys
import time
import xml.etree.ElementTree as ET

apk, work = Path(sys.argv[1]).resolve(), Path(sys.argv[2]).resolve()
work.mkdir(parents=True, exist_ok=True)
logs = work / "diagnostics"
logs.mkdir(exist_ok=True)
package = "io.github.maas3n.mattmux"
remote = "/sdcard/Documents/MattMuxBatchRegression"
sequence = 0


def command(*args):
    return subprocess.check_output(args, text=True, errors="replace", stderr=subprocess.STDOUT)


def adb(*args):
    return command("adb", *args)


def screen():
    global sequence
    sequence += 1
    adb("shell", "uiautomator", "dump", "/sdcard/mattmux-batch.xml")
    xml = adb("shell", "cat", "/sdcard/mattmux-batch.xml")
    (logs / f"screen-{sequence:03d}.xml").write_text(xml)
    return ET.fromstring(xml)


def click_node(node):
    x1, y1, x2, y2 = map(int, re.findall(r"\d+", node.attrib["bounds"]))
    adb("shell", "input", "tap", str((x1 + x2) // 2), str((y1 + y2) // 2))
    time.sleep(1)


def click(label):
    for attempt in range(4):
        tree = screen()
        node = next((n for n in tree.iter("node") if n.get("text", "").casefold() == label.casefold()), None)
        if node is not None:
            click_node(node)
            return
        if attempt == 0:
            adb("shell", "input", "swipe", "500", "650", "500", "1450", "300")
        else:
            adb("shell", "input", "swipe", "500", "1450", "500", "650", "300")
    raise AssertionError(f"Missing UI control: {label}")


def choose_directory(button, folder):
    click(button)
    tree = screen()
    documents_crumb = next((n for n in tree.iter("node") if n.get("resource-id", "").endswith("breadcrumb_text") and n.get("text") == "Documents"), None)
    if documents_crumb is not None:
        click_node(documents_crumb)
    else:
        root_crumb = next((n for n in tree.iter("node") if n.get("resource-id", "").endswith("breadcrumb_text") and "sdk_gphone" in n.get("text", "")), None)
        if root_crumb is not None:
            click_node(root_crumb)
        else:
            drawer = next((n for n in tree.iter("node") if n.get("content-desc", "").casefold() in ("show roots", "show navigation drawer")), None)
            if drawer is None:
                raise AssertionError("Cannot navigate to local storage in DocumentsUI")
            click_node(drawer)
            tree = screen()
            storage = next((n for n in tree.iter("node") if "sdk_gphone" in n.get("text", "") or n.get("text", "").casefold() in ("internal storage", "pixel 2")), None)
            if storage is None:
                raise AssertionError("Missing local storage root in DocumentsUI")
            click_node(storage)
        click("Documents")
    for part in ("MattMuxBatchRegression", folder):
        click(part)
    click("Use this folder")
    click("Allow")


def run_batch():
    click("ONECLICK BATCH")
    for attempt in range(30):
        tree = screen()
        statuses = [n.get("text", "") for n in tree.iter("node") if n.get("text", "").startswith("Batch complete:") or n.get("text", "").startswith("Batch failed:")]
        if statuses:
            print(statuses[0], flush=True)
            assert statuses == ["Batch complete: 2 movie(s) remuxed."], statuses
            return
        adb("shell", "input", "swipe", "500", "1450", "500", "650", "300")
        time.sleep(1)
    raise AssertionError("Batch did not report successful completion")


def media_hash(path, kind):
    return command("ffmpeg", "-v", "error", "-i", str(path), "-map", f"0:{kind}:0", "-f", "hash", "-hash", "sha256", "-").strip()


try:
    # Each independently authored disc has its own distinct audio and video.
    for name, color, tone in (("Alpha", "red", 440), ("Beta", "blue", 880)):
        vob = work / f"{name}.vob"
        command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", f"color={color}:size=720x576:rate=25",
                "-f", "lavfi", "-i", f"sine=frequency={tone}:sample_rate=48000", "-t", "6", "-target", "pal-dvd", "-y", str(vob))
        dvd = work / name
        xml = work / f"{name}.xml"
        xml.write_text(f'<dvdauthor dest="{dvd}"><vmgm><menus><video format="pal" /></menus></vmgm><titleset><titles><video format="pal" /><audio lang="en" /><pgc><vob file="{vob}" chapters="0,3" /></pgc></titles></titleset></dvdauthor>')
        command("dvdauthor", "-x", str(xml))
    command("genisoimage", "-quiet", "-dvd-video", "-udf", "-o", str(work / "Alpha.iso"), str(work / "Alpha"))
    print(adb("install", "-r", str(apk)), flush=True)
    # Delete only this test's own fixture tree on the disposable CI emulator.
    adb("shell", "rm", "-rf", remote)
    adb("shell", "mkdir", "-p", remote + "/Media", remote + "/Output")
    adb("push", str(work / "Alpha.iso"), remote + "/Media/Alpha.iso")
    adb("push", str(work / "Beta"), remote + "/Media/Beta")
    adb("shell", "am", "force-stop", package)
    adb("shell", "am", "start", "-W", "-n", package + "/.MainActivity")
    time.sleep(2)
    click("BATCH")
    choose_directory("CHOOSE MOVIE FOLDER", "Media")
    run_batch()
    choose_directory("CHOOSE OUTPUT FOLDER (OPTIONAL)", "Output")
    run_batch()
    adb("pull", remote, str(work / "pulled"))
    pulled = work / "pulled"
    expected = {"Media/Alpha.mkv": "Alpha", "Media/Beta/Beta.mkv": "Beta", "Output/Alpha.mkv": "Alpha", "Output/Beta.mkv": "Beta"}
    actual = {str(p.relative_to(pulled)) for p in pulled.rglob("*.mkv")}
    assert actual == set(expected), f"Wrong/missing/duplicate output paths: {actual}"
    hashes = {}
    for relative, name in expected.items():
        received = tuple(media_hash(pulled / relative, kind) for kind in ("v", "a"))
        reference = tuple(media_hash(work / f"{name}.vob", kind) for kind in ("v", "a"))
        assert received == reference, f"{relative}: contains the wrong or incomplete movie"
        hashes[name] = received
    assert hashes["Alpha"] != hashes["Beta"], "Batch duplicated a movie"
    print("Real Android SAF Batch: distinct ISO and VIDEO_TS movies, default and explicit output folders PASS", flush=True)
finally:
    (logs / "logcat.txt").write_text(adb("logcat", "-d", "-v", "threadtime"))
    with (logs / "screen.png").open("wb") as out:
        subprocess.run(["adb", "exec-out", "screencap", "-p"], stdout=out, check=False)
    print(adb("shell", "find", remote, "-type", "f"), flush=True)
