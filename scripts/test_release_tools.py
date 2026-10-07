import hashlib
import json
from pathlib import Path
import subprocess
import tempfile
import unittest

from check_signing_environment import missing_settings
from publish_main import publish, verify_assets
from release_version import android_version_code


class ReleaseTests(unittest.TestCase):
    def test_channel_transitions_increase_version_codes(self):
        # Historical v1.6.0-alpha.1 -> main -> numbered -> main.
        codes = [10_600_001] + [android_version_code(n) for n in (3, 4, 5)]
        self.assertEqual(codes, sorted(set(codes)))
        for bad in (0, -1, 1_100_000_001):
            with self.assertRaises(ValueError):
                android_version_code(bad)

    def test_missing_signing_configuration_is_reported_without_values(self):
        self.assertEqual(len(missing_settings({})), 5)
        missing = missing_settings({'KEYSTORE_B64': 'private-value'})
        self.assertEqual(len(missing), 4)
        self.assertNotIn('private-value', str(missing))

    def fixture(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        root = Path(tmp.name)
        for name in ('MattRip-Windows-All-in-One.exe', 'MattRip-Linux-amd64Standalone',
                     'MattRip-Android.apk', 'SHA256SUMS.txt', 'dependency-source.tar.gz'):
            (root / name).write_bytes(name.encode())
        assets = [{'name': p.name, 'size': p.stat().st_size, 'state': 'uploaded',
                   'digest': 'sha256:' + hashlib.sha256(p.read_bytes()).hexdigest()}
                  for p in root.iterdir()]
        return root, assets

    def test_incomplete_or_corrupt_remote_assets_fail_closed(self):
        root, assets = self.fixture()
        local = {p.name: p for p in root.iterdir()}
        for bad in [assets[:-1], [dict(a, digest=None) for a in assets],
                    [dict(a, size=1) for a in assets]]:
            with self.assertRaises(RuntimeError):
                verify_assets(local, bad)

    def simulate(self, fail_at=None, stale=False, corrupt=False):
        root, assets = self.fixture()
        calls = []

        def run(*args):
            calls.append(args)
            if args[0] == 'git':
                reads = sum(c[0] == 'git' for c in calls)
                return ('newer' if stale and reads > 1 else 'sha') + '\trefs/heads/main'
            if fail_at and args[1:3] == ('release', fail_at):
                raise subprocess.CalledProcessError(1, args)
            if args[1] == 'api':
                if '--paginate' in args:
                    return json.dumps([[{
                        'id': 123,
                        'tag_name': 'beta-build-3-1',
                        'draft': True,
                        'target_commitish': 'sha',
                    }]])
                return json.dumps({
                    'id': 123,
                    'tag_name': 'beta-build-3-1',
                    'draft': True,
                    'target_commitish': 'sha',
                    'assets': assets[:-1] if corrupt else assets,
                })
            return ''

        try:
            publish(root, 'owner/repo', 'sha', '3', '1', run=run)
        except (subprocess.CalledProcessError, RuntimeError):
            if not (fail_at or corrupt):
                raise
        return calls

    def test_failed_upload_never_switches_public_release(self):
        calls = self.simulate(fail_at='upload')
        self.assertFalse(any(c[1:3] == ('release', 'edit') for c in calls))
        self.assertFalse(any('--clobber' in c for c in calls))

    def test_failed_remote_verification_never_publishes(self):
        calls = self.simulate(corrupt=True)
        self.assertFalse(any(c[1:3] == ('release', 'edit') for c in calls))

    def test_new_main_during_upload_leaves_private_draft(self):
        calls = self.simulate(stale=True)
        self.assertFalse(any(c[1:3] == ('release', 'edit') for c in calls))

    def test_draft_verification_resolves_release_id_not_tag_endpoint(self):
        calls = self.simulate()
        api_calls = [c for c in calls if len(c) > 1 and c[1] == 'api']
        self.assertTrue(any('--paginate' in c and '--slurp' in c for c in api_calls))
        self.assertTrue(any('repos/owner/repo/releases/123' in c for c in api_calls))
        self.assertFalse(any(
            any('/releases/tags/' in arg for arg in c)
            for c in api_calls
        ))

    def test_complete_payload_publishes_once_after_verification(self):
        calls = self.simulate()
        self.assertEqual(calls[-1][1:3], ('release', 'edit'))
        self.assertIn('--draft=false', calls[-1])
        self.assertIn('--prerelease', calls[-1])
        self.assertIn('--latest=false', calls[-1])
        self.assertNotIn('--latest', calls[-1])
        upload = next(c for c in calls if c[1:3] == ('release', 'upload'))
        self.assertTrue(any(c.endswith('/dependency-source.tar.gz') for c in upload))
        create = next(c for c in calls if c[1:3] == ('release', 'create'))
        self.assertIn('--draft', create)
        self.assertIn('--prerelease', create)
        self.assertIn('--latest=false', create)


if __name__ == '__main__':
    unittest.main()
