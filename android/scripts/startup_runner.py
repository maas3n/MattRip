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


def package_manager_ready():
    # Query one framework package instead of enumerating the complete package
    # database. This is enough to prove PackageManager is answering and works on
    # the API 26 image used by CI.
    return adb('shell', 'pm', 'path', 'android').startswith('package:')


def page_size_bytes():
    # Newer Android images provide getconf, but Android 8.0/API 26 does not.
    # Fall back to the kernel page size reported for the shell process itself.
    getconf_error = None
    try:
        value = adb('shell', 'getconf', 'PAGE_SIZE')
        size = int(value)
        if size > 0:
            return size
    except (subprocess.SubprocessError, OSError, ValueError) as exc:
        getconf_error = exc

    try:
        smaps = adb('shell', 'cat', '/proc/self/smaps')
        match = re.search(r'^KernelPageSize:\s+(\d+)\s+kB\s*$', smaps, re.MULTILINE)
        if match:
            return int(match.group(1)) * 1024
    except (subprocess.SubprocessError, OSError) as exc:
        raise RuntimeError(
            f'Unable to determine Android page size: getconf failed ({getconf_error!r}); '
            f'/proc/self/smaps failed ({exc!r})') from exc

    raise RuntimeError(
        f'Unable to determine Android page size: getconf failed ({getconf_error!r}) '
        'and /proc/self/smaps did not contain KernelPageSize')


def record_readiness(path, message):
    print(message, flush=True)
    if path is not None:
        with path.open('a', encoding='utf-8') as output:
            output.write(message + '\n')


def wait_ready(expected_page_size, diagnostics=None):
    deadline = time.monotonic() + 150
    previous = ''
    stable = 0
    sample = 0
    while time.monotonic() < deadline:
        sample += 1
        pid = ''
        booted = False
        package_ready = False
        error = ''
        try:
            pid = system_pid()
            booted = adb('shell', 'getprop', 'sys.boot_completed') == '1'
            # Ensure Android services are actually answering, not just adbd.
            package_ready = package_manager_ready()
        except (subprocess.SubprocessError, OSError) as exc:
            error = f'{type(exc).__name__}: {exc}'

        if not error:
            stable = stable + 1 if pid and pid == previous and booted and package_ready else 0
            previous = pid
        else:
            stable = 0

        record_readiness(
            diagnostics,
            f'readiness sample={sample} pid={pid or "-"} boot={int(booted)} '
            f'package={int(package_ready)} stable={stable} error={error or "-"}')

        if stable >= 5:
            # Keep page-size validation outside the readiness-probe exception path:
            # unsupported commands must report their real error, not masquerade as
            # an endless "framework not ready" condition.
            size = page_size_bytes()
            record_readiness(diagnostics, f'readiness page_size={size} expected={expected_page_size}')
            if size != expected_page_size:
                raise RuntimeError(f'Expected {expected_page_size}-byte pages, got {size}')
            return pid
        time.sleep(2)
    raise RuntimeError('Emulator framework did not become ready within 150 seconds')


def run_startup(apk, logs, page_size):
    for attempt in (1, 2):
        directory = logs / f'attempt-{attempt}'
        directory.mkdir(parents=True, exist_ok=True)
        before = wait_ready(page_size, directory / 'readiness.txt')
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
