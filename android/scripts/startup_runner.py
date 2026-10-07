"""Repeat the entire startup test once, only for the observed emulator ART crash."""
import argparse
from pathlib import Path
import re
import subprocess
import sys
import time

PACKAGE = 'io.github.maas3n.mattrip'


def retryable_system_crash(before_pid, after_pid, diagnostics):
    # A missing tab, UIAutomator failure, app crash or app ANR alone never earns
    # a retry. Require a changed/dead system_server AND this exact platform fault.
    app_failure = re.search(
        r'Process:\s*io\.github\.maas3n\.mattrip(?:[,:\s]|$)'
        r'|>>>\s*io\.github\.maas3n\.mattrip(?:[:\s]|$)'
        r'|ANR in io\.github\.maas3n\.mattrip'
        r'|MattRip (?:isn.t|is not) responding|Unexpected ANR dialog', diagnostics)
    return bool(before_pid and before_pid != after_pid and not app_failure
                and '>>> system_server <<<' in diagnostics
                and 'artInstanceOfFromCode' in diagnostics
                and 'ProcessServiceRecord.getRunningServiceAt' in diagnostics
                and 'OomAdjusterModernImpl' in diagnostics)


def adb(*args):
    return subprocess.check_output(['adb', *args], text=True, stderr=subprocess.STDOUT, timeout=20).strip()


def system_pid():
    try:
        return adb('shell', 'pidof', 'system_server')
    except (subprocess.SubprocessError, OSError):
        return ''


def wait_ready(expected_page_size):
    deadline = time.monotonic() + 150
    previous = ''
    stable = 0
    while time.monotonic() < deadline:
        try:
            pid = system_pid()
            booted = adb('shell', 'getprop', 'sys.boot_completed') == '1'
            # Ensure Android services are actually answering, not just adbd.
            package_ready = 'package:android' in adb('shell', 'pm', 'list', 'packages')
            stable = stable + 1 if pid and pid == previous and booted and package_ready else 0
            previous = pid
            if stable >= 5:
                size = adb('shell', 'getconf', 'PAGE_SIZE')
                if size != str(expected_page_size):
                    raise RuntimeError(f'Expected {expected_page_size}-byte pages, got {size}')
                return pid
        except subprocess.SubprocessError:
            stable = 0
        time.sleep(2)
    raise RuntimeError('Emulator framework did not become ready within 150 seconds')


def run_startup(apk, logs, page_size):
    for attempt in (1, 2):
        directory = logs / f'attempt-{attempt}'
        directory.mkdir(parents=True, exist_ok=True)
        before = wait_ready(page_size)
        result = subprocess.run(
            [sys.executable, str(Path(__file__).with_name('test-startup.py')), str(apk), str(directory)],
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        print(result.stdout, flush=True)
        (directory / 'test-output.txt').write_text(result.stdout)
        after = system_pid()
        (directory / 'system-server-pids.txt').write_text(f'before={before}\nafter={after}\n')
        if result.returncode == 0:
            if before != after:
                raise RuntimeError('system_server restarted during an otherwise successful test')
            return
        log = directory / 'logcat.txt'
        diagnostics = result.stdout + (log.read_text() if log.exists() else '')
        if attempt == 2 or not retryable_system_crash(before, after, diagnostics):
            raise RuntimeError(f'Startup validation failed (attempt {attempt}, exit {result.returncode}); see {directory}')
        print('::warning::Confirmed emulator system_server ART crash; preserving diagnostics and rebooting for one complete retest.', flush=True)
        adb('reboot')
        subprocess.run(['adb', 'wait-for-device'], check=True, timeout=150)
    raise AssertionError('Unreachable')


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('apk', type=Path)
    parser.add_argument('logs', type=Path)
    parser.add_argument('--page-size', type=int, required=True)
    args = parser.parse_args()
    run_startup(args.apk, args.logs, args.page_size)
