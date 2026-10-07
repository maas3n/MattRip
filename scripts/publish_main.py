"""Stage a complete immutable beta snapshot without replacing the stable latest release.

Each attempt uses a new draft. A failed upload leaves every previous public
release intact. Main snapshots are published as prereleases with --latest=false.
"""
import hashlib
import json
import os
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


def release_by_tag(repo, tag, run=command):
    # GitHub's /releases/tags/{tag} endpoint can return 404 for a newly-created
    # draft whose Git tag has not been published yet. Enumerate releases,
    # resolve the exact draft by tag_name, then verify it through its numeric ID.
    pages = json.loads(run(
        'gh', 'api', '--paginate', '--slurp',
        f'repos/{repo}/releases?per_page=100'))
    matches = [
        release
        for page in pages
        for release in page
        if release.get('tag_name') == tag
    ]
    if len(matches) != 1:
        raise RuntimeError(
            f'Expected exactly one draft release for {tag}, found {len(matches)}')
    release_id = matches[0].get('id')
    if not release_id:
        raise RuntimeError(f'Draft release for {tag} did not expose a release ID')
    return json.loads(run('gh', 'api', f'repos/{repo}/releases/{release_id}'))


def publish(root, repo, sha, run_number, attempt, run=command):
    def current_main():
        return run('git', 'ls-remote', 'origin', 'refs/heads/main').split()[0] == sha

    if not current_main():
        print('A newer main commit exists; leaving current downloads untouched.')
        return
    tag = f'beta-build-{run_number}-{attempt}'
    local = {p.name: p for p in root.iterdir() if p.is_file()}
    required = {'MattRip-Windows-All-in-One.exe', 'MattRip-Linux-amd64Standalone',
                'MattRip-Android.apk', 'SHA256SUMS.txt'}
    if not required <= local.keys() or any(p.stat().st_size == 0 for p in local.values()):
        raise RuntimeError('Incomplete main release payload')
    # A distinct attempt tag avoids mutating either published releases or a
    # previous attempt's partial draft. A duplicate invocation fails closed.
    run('gh', 'release', 'create', tag, '--repo', repo, '--target', sha,
        '--draft', '--prerelease', '--latest=false',
        '--title', f'MattRip Beta build {run_number}',
        '--notes', f'Beta development snapshot from main commit {sha}. '
        'This is a prerelease for testing and does not replace the latest stable release. '
        'All platforms, sources, notices and checksums are included.')
    run('gh', 'release', 'upload', tag, *[str(local[name]) for name in sorted(local)],
        '--repo', repo)
    release = release_by_tag(repo, tag, run=run)
    if not release['draft'] or release['target_commitish'] != sha:
        raise RuntimeError('Refusing to change a published or mismatched release')
    verify_assets(local, release['assets'])
    # Builds may take an hour. Recheck after uploads as well as before staging.
    if not current_main():
        print(f'Newer main detected; {tag} remains an unpublished draft.')
        return
    # Publish only after the complete uploaded payload has been verified.
    # Keep the stable /releases/latest channel untouched.
    run('gh', 'release', 'edit', tag, '--repo', repo,
        '--draft=false', '--prerelease', '--latest=false')


if __name__ == '__main__':
    publish(Path('release-assets'), os.environ['GITHUB_REPOSITORY'],
            os.environ['GITHUB_SHA'], os.environ['GITHUB_RUN_NUMBER'],
            os.environ['GITHUB_RUN_ATTEMPT'])
