"""Stage a complete immutable main snapshot before switching /releases/latest.

Each attempt uses a new draft. A failed upload leaves the previous public
release intact. Numbered releases must use --latest=false to preserve this channel.
"""
import hashlib
import json
import os
import re
from pathlib import Path
import subprocess


def command(*args):
    return subprocess.check_output(args, text=True).strip()


def verify_assets(local, remote):
    actual = {asset['name']: asset for asset in remote}
    if set(actual) != set(local):
        raise RuntimeError('Uploaded asset set differs from the complete local release')
    for name, path in local.items():
        asset = actual[name]
        with path.open('rb') as stream:
            digest = 'sha256:' + hashlib.file_digest(stream, 'sha256').hexdigest()
        if (asset['state'] != 'uploaded' or asset['size'] != path.stat().st_size
                or asset.get('digest') != digest):
            raise RuntimeError('Uploaded asset verification failed: ' + name)


def publish(root, repo, sha, run_number, attempt, run=command):
    def current_main():
        return run('git', 'ls-remote', 'origin', 'refs/heads/main').split()[0] == sha

    if not current_main():
        print('A newer main commit exists; leaving current downloads untouched.')
        return
    # A rerun of an older workflow can share the current main SHA. Do not let
    # its smaller versionCode replace a newer successfully published build.
    latest_tag = run('gh', 'release', 'view', '--repo', repo, '--json', 'tagName', '--jq', '.tagName')
    match = re.fullmatch(r'main-build-(\d+)-(\d+)', latest_tag)
    if match and int(match[1]) > int(run_number):
        print('A newer main build is already published; refusing a version downgrade.')
        return
    tag = f'main-build-{run_number}-{attempt}'
    local = {p.name: p for p in root.iterdir() if p.is_file()}
    required = {'MattRip-Windows-All-in-One.exe', 'MattRip-Linux-amd64Standalone',
                'MattRip-Android.apk', 'SHA256SUMS.txt'}
    if not required <= local.keys() or any(p.stat().st_size == 0 for p in local.values()):
        raise RuntimeError('Incomplete main release payload')
    # A distinct attempt tag avoids mutating either published releases or a
    # previous attempt's partial draft. A duplicate invocation fails closed.
    run('gh', 'release', 'create', tag, '--repo', repo, '--target', sha,
        '--draft', '--latest=false', '--title', f'MattRip main build {run_number}',
        '--notes', f'Current development snapshot from main commit {sha}. '
        'This is the main download channel, not a numbered stable release. '
        'All platforms, sources, notices and checksums are included.')
    run('gh', 'release', 'upload', tag, *[str(local[name]) for name in sorted(local)],
        '--repo', repo)
    release = json.loads(run('gh', 'api', f'repos/{repo}/releases/tags/{tag}'))
    if not release['draft'] or release['target_commitish'] != sha:
        raise RuntimeError('Refusing to change a published or mismatched release')
    verify_assets(local, release['assets'])
    # Builds may take an hour. Recheck after uploads as well as before staging.
    if not current_main():
        print(f'Newer main detected; {tag} remains an unpublished draft.')
        return
    # One API update publishes the already complete set and selects its redirect.
    run('gh', 'release', 'edit', tag, '--repo', repo, '--draft=false', '--latest')


if __name__ == '__main__':
    publish(Path('release-assets'), os.environ['GITHUB_REPOSITORY'],
            os.environ['GITHUB_SHA'], os.environ['GITHUB_RUN_NUMBER'],
            os.environ['GITHUB_RUN_ATTEMPT'])
