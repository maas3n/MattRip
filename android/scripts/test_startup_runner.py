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
