import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch
import startup_runner as runner

CRASH = '''>>> system_server <<<
artInstanceOfFromCode
ProcessServiceRecord.getRunningServiceAt
OomAdjusterModernImpl
'''


class RecoveryTests(unittest.TestCase):
    def test_only_confirmed_framework_death_is_retryable(self):
        self.assertTrue(runner.retryable_system_crash('557', '890', CRASH))
        self.assertTrue(runner.retryable_system_crash('557', '', CRASH))
        for before, after, text in [('', '890', CRASH), ('557', '557', CRASH),
                                   ('557', '890', 'uiautomator exit 137'),
                                   ('557', '890', 'Missing tab after launch')]:
            self.assertFalse(runner.retryable_system_crash(before, after, text))

    def test_app_failures_cannot_be_retried(self):
        for failure in ['Process: io.github.maas3n.mattrip, PID: 12',
                        '>>> io.github.maas3n.mattrip <<<',
                        "MattRip isn't responding", 'ANR in io.github.maas3n.mattrip',
                        'Unexpected ANR dialog instead of MattRip tab']:
            self.assertFalse(runner.retryable_system_crash('557', '890', CRASH + failure))

    def test_page_size_uses_getconf_when_available(self):
        with patch.object(runner, 'adb', return_value='16384') as adb:
            self.assertEqual(runner.page_size_bytes(), 16384)
        adb.assert_called_once_with('shell', 'getconf', 'PAGE_SIZE')

    def test_page_size_falls_back_to_smaps_when_getconf_is_missing(self):
        missing = subprocess.CalledProcessError(127, ['adb', 'shell', 'getconf', 'PAGE_SIZE'])
        with patch.object(runner, 'adb', side_effect=[missing, 'Size: 12 kB\nKernelPageSize:        4 kB\n']):
            self.assertEqual(runner.page_size_bytes(), 4096)

    def test_api26_readiness_survives_missing_getconf_and_writes_diagnostics(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        diagnostics = Path(temporary.name) / 'readiness.txt'

        def fake_adb(*args):
            if args == ('shell', 'getprop', 'sys.boot_completed'):
                return '1'
            if args == ('shell', 'pm', 'path', 'android'):
                return 'package:/system/framework/framework-res.apk'
            if args == ('shell', 'getconf', 'PAGE_SIZE'):
                raise subprocess.CalledProcessError(127, args)
            if args == ('shell', 'cat', '/proc/self/smaps'):
                return 'KernelPageSize:        4 kB\n'
            raise AssertionError(args)

        with patch.object(runner, 'system_pid', return_value='557'), \
             patch.object(runner, 'adb', side_effect=fake_adb), \
             patch.object(runner.time, 'monotonic', side_effect=[0, 1, 2, 3, 4, 5, 6]), \
             patch.object(runner.time, 'sleep'):
            self.assertEqual(runner.wait_ready(4096, diagnostics), '557')

        text = diagnostics.read_text()
        self.assertIn('stable=5', text)
        self.assertIn('page_size=4096 expected=4096', text)

    def test_page_size_mismatch_is_reported_directly(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        diagnostics = Path(temporary.name) / 'readiness.txt'

        def fake_adb(*args):
            if args == ('shell', 'getprop', 'sys.boot_completed'):
                return '1'
            if args == ('shell', 'pm', 'path', 'android'):
                return 'package:/system/framework/framework-res.apk'
            raise AssertionError(args)

        with patch.object(runner, 'system_pid', return_value='557'), \
             patch.object(runner, 'adb', side_effect=fake_adb), \
             patch.object(runner, 'page_size_bytes', return_value=16384), \
             patch.object(runner.time, 'monotonic', side_effect=[0, 1, 2, 3, 4, 5, 6]), \
             patch.object(runner.time, 'sleep'):
            with self.assertRaisesRegex(RuntimeError, 'Expected 4096-byte pages, got 16384'):
                runner.wait_ready(4096, diagnostics)

    def exercise(self, results):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        calls = []
        def execute(args, **kwargs):
            calls.append(args)
            if args[0] == 'adb':
                return subprocess.CompletedProcess(args, 0)
            code, output = results.pop(0)
            return subprocess.CompletedProcess(args, code, output)
        return temporary.name, calls, execute

    def test_reboots_and_replays_full_test_once(self):
        directory, calls, execute = self.exercise([(1, CRASH), (0, 'all tests passed')])
        with patch.object(runner, 'wait_ready', side_effect=['557', '600']), \
             patch.object(runner, 'system_pid', side_effect=['890', '600']), \
             patch.object(runner, 'adb') as adb, patch.object(runner.subprocess, 'run', side_effect=execute):
            runner.run_startup(Path('same.apk'), Path(directory), 16384)
        adb.assert_called_once_with('reboot')
        self.assertEqual(sum('same.apk' in call for call in calls), 2)
        self.assertTrue((Path(directory) / 'attempt-1/test-output.txt').is_file())

    def test_repeated_system_failure_stays_red(self):
        directory, calls, execute = self.exercise([(1, CRASH), (1, CRASH)])
        with patch.object(runner, 'wait_ready', side_effect=['557', '600']), \
             patch.object(runner, 'system_pid', side_effect=['890', '900']), \
             patch.object(runner, 'adb') as adb, patch.object(runner.subprocess, 'run', side_effect=execute):
            with self.assertRaises(RuntimeError):
                runner.run_startup(Path('same.apk'), Path(directory), 16384)
        adb.assert_called_once_with('reboot')

    def test_app_failure_never_reboots(self):
        directory, calls, execute = self.exercise([(1, "MattRip isn't responding")])
        with patch.object(runner, 'wait_ready', return_value='557'), \
             patch.object(runner, 'system_pid', return_value='890'), \
             patch.object(runner, 'adb') as adb, patch.object(runner.subprocess, 'run', side_effect=execute):
            with self.assertRaises(RuntimeError):
                runner.run_startup(Path('same.apk'), Path(directory), 16384)
        adb.assert_not_called()


if __name__ == '__main__':
    unittest.main()
