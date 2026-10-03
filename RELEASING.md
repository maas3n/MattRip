# Releasing MattRip

MattRip uses one long-lived source branch, `main`, and one product version namespace across Windows, Linux, and Android/ChromeOS.

## Unified release model

Every new public release uses exactly one product tag and one GitHub Release:

- stable: `vMAJOR.MINOR.PATCH`
- preview: `vMAJOR.MINOR.PATCH-alpha.N`, `-beta.N`, or `-rc.N`

Do not create new platform-specific version tags such as `-linux`, `-chromeos`, `-windows`, or separate `devN` release lines. Historical platform-specific tags remain valid historical pointers and are not rewritten.

A unified release contains the platform assets that are ready from the same tagged commit. Typical assets are:

- `MattRip-<version>-Windows-Setup.exe`
- `MattRip-<version>-Windows-All-in-One.exe`
- `MattRip-<version>-Windows-Portable.zip`
- `MattRip-<version>-Linux-amd64.deb`
- `MattRip-<version>-Linux-amd64.tar.gz`
- `MattRip-<version>-Linux-amd64Standalone`
- `MattRip-<version>-Source.tar.gz`
- `MattRip-<version>-Android.apk` — universal APK for Android phones/tablets and Chromebooks with Android app support
- third-party source/provenance/license files
- per-platform checksum manifests
- one combined `SHA256SUMS.txt`

`MattRip-<version>-Android.apk` is the single persistently signed universal APK for both Android phones/tablets and Chromebooks with Android app support.

GitHub also exposes source ZIP/tar archives automatically for the release tag.

## Release principles

1. **Published release tags are immutable.** Never force-move an existing version tag.
2. **Published release assets are immutable.** Fix a released problem in a new version.
3. **All platform payloads come from the same tag/commit.** Windows, Linux, and Android/ChromeOS must not publish different source commits under the same product version.
4. **One tag creates one GitHub Release.** Platform workflows may build independently, but `.github/workflows/release.yml` is the only workflow that publishes GitHub Releases.
5. Generate and verify SHA-256 checksums for release payloads and verify bundled runtime dependencies before publishing.
6. Keep historical development provenance in Git history; obsolete platform-specific public release entries may remain retired after the unified-release cleanup.

## Before tagging

- Merge the intended source into `main`.
- Confirm Windows, Linux, and Android/ChromeOS CI is green.
- Confirm pinned third-party versions/checksums and licensing/provenance documentation are current.
- Choose a MattRip version/tag that is unused in this fork. Inherited MattMux tags are provenance and must not be reused for MattRip releases.
- Decide whether the release is stable or a shared preview.
- Do not reuse a tag that already exists or already has a GitHub Release.

## Publishing

Create the tag from the exact `main` commit to publish and push it:

```bash
git switch main
git pull --ff-only
git tag vX.Y.Z
git push origin vX.Y.Z
```

For a preview:

```bash
git tag vX.Y.Z-alpha.1
git push origin vX.Y.Z-alpha.1
```

The **Unified release** workflow then:

1. validates the unified tag format;
2. derives one product version plus the Android `versionCode`;
3. builds the Windows payload;
4. builds the Linux payload;
5. builds one signed universal `MattRip-<version>-Android.apk` for both Android and ChromeOS;
6. verifies each platform payload;
7. downloads all platform artifacts into one release job;
8. creates a combined `SHA256SUMS.txt`; and
9. publishes one GitHub Release for that tag.

The workflow refuses to overwrite an existing GitHub Release.

The same workflow can be run manually for an **existing** unified tag by using `workflow_dispatch` and supplying that tag. Manual dispatch does not invent or move tags.

## Android / ChromeOS versionCode

The unified workflow derives a monotonically ordered Android versionCode from the product version:

- alpha builds sort before beta builds;
- beta builds sort before release candidates;
- release candidates sort before the stable release;
- the next patch/minor/major version sorts after the previous stable release.

Android/ChromeOS purchases remain disabled until production device validation, signing, and purchase-verification readiness are complete. The separate Play bundle workflow is distribution tooling; it does not create GitHub Releases.

MattRip uses the separate application ID `io.github.maas3n.mattrip`, so it is a different Android app from MattMux and is not an in-place upgrade path for MattMux APKs. Establish and preserve MattRip's own release-signing identity before the first public APK, and verify its certificate before publishing.

## Historical releases

The repository was forked from the verified MattMux 1.4.19 source baseline. Any inherited MattMux tags, platform-specific tags, and old release-line history are pre-fork provenance, not MattRip releases. Leave them immutable and do not reuse them for MattRip. New MattRip releases use only new, unused unified tags.

## Emergency fixes

If a published unified release is defective, leave its tag and assets unchanged, fix the problem on `main`, and publish the next unused MattRip product version. Never rebuild an old release in place.
