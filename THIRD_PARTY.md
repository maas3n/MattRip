# Third-party software

MattRip relies on FFmpeg/FFprobe and MediaInfo on desktop platforms. Their source or binary distributions are not committed to the normal repository history. Self-contained release packages download/build the pinned versions during CI, verify them, and then bundle the resulting runtime privately with MattRip.

## Inherited MattMux Windows 1.2.0 provenance

### FFmpeg / FFprobe

- Provider: BtbN/FFmpeg-Builds
- Release: `autobuild-2026-09-08-23-15`
- Asset: `ffmpeg-N-126479-g08cd8df29d-win64-gpl-shared.zip`
- Checksum manifest: `checksums.sha256`
- Trusted manifest SHA-256: `f64be162403094773397bfcc299a4a059507028afa7563591fd05c17d56b3214`
- Verified archive SHA-256: `3139da8c0e3d201d16d849d2d6da2744b2b715f8d71184c4196db43da07b9607`

MattRip verifies the pinned checksum manifest first, then verifies the FFmpeg archive against the expected value before using it.

### MediaInfo CLI

- Version: `26.05`
- Asset: `MediaInfo_CLI_26.05_Windows_x64.zip`
- Trusted archive SHA-256: `f7f80620ce6d14f4995f0de6f98e3ef18ad29496db01899571152ee3311229f9`

The Windows Setup EXE, Portable ZIP, and All-in-One EXE bundle these verified runtime tools for normal offline-capable use.

## Inherited MattMux Linux 1.3.0-dev5 provenance

### FFmpeg / FFprobe

- Provider: BtbN/FFmpeg-Builds
- Release: `autobuild-2026-09-08-23-15`
- Asset: `ffmpeg-N-126479-g08cd8df29d-linux64-gpl.tar.xz`
- Trusted archive SHA-256: `635a2d74de852064852e95db5a9c475a86d36e2b6390e3c1ba5e46b2c46dfce0`

The self-contained `.deb` and single-file standalone build keep FFmpeg and FFprobe private to MattRip. They do not install or replace `/usr/bin/ffmpeg` or `/usr/bin/ffprobe` and do not modify the global system `PATH`.

### MediaInfo CLI

Linux MediaInfo is built from an exact pinned source set rather than from moving branches:

- MediaInfo CLI tag: `v26.05`
- MediaInfo CLI commit: `4728f24b666117a19d36515d95b9367fbb37aaf6`
- MediaInfoLib commit: `8bfa658657da9e16470c9fb32035e0fa097c0112`
- ZenLib commit: `2ddc277fe7ecfcbfe45616bb9cd9e23079113ecd`
- MediaArea zlib commit: `eaaf237c8cbc7310170c43202c6ec2cff64fff66`

The self-contained Linux packages keep the resulting MediaInfo binary private to MattRip and do not replace `/usr/bin/mediainfo`.

### libdvdcss

Self-contained Windows and Linux packages build and bundle a private libdvdcss runtime for libdvdread to discover dynamically when reading CSS-protected DVD sources.

- Version: `1.6.0`
- Upstream source: VideoLAN libdvdcss 1.6.0 release tarball
- Trusted source SHA-256: `7ea556c846b7bfc32d47b41cae56d1863a6b6d5f706bb162778d6f298490977c`
- License: GPL-2.0-or-later
- Integration: private shared library loaded by libdvdread; MattRip does not replace the existing FFmpeg `dvdvideo` title/remux/demux path

The release packages retain the exact libdvdcss source archive, COPYING file, and build metadata. Linux keeps the library in MattRip's private runtime search path; Windows keeps `libdvdcss-2.dll` beside the private FFmpeg tools. Direct GitHub/development Android builds use the same pinned libdvdcss source, statically linked into the JNI bridge, and decrypt scrambled sectors through libdvdcss stream callbacks before the existing SAF/UDF native reader passes them to libav. The Google Play AAB workflow deliberately builds with `MATTRIP_ANDROID_CSS=0` pending a separate Play distribution-policy review.

## Inherited MattMux Android / ChromeOS Alpha 4 provenance

The Android/ChromeOS app uses native FFmpeg libraries, libudfread, and the DVD title-discovery pair proven by the 1.4.2 DVDNav Beta 1.

- FFmpeg version: `9.0.1` (LGPL-only FFmpeg configuration)
- FFmpeg source SHA-256: `cf38e0e28c7e5605942c4a77755349b0145804a397af37eb1fb4c77cb237f635`
- libudfread version: `1.1.2`
- libdvdnav version: `6.1.1` (GPL; statically linked into the JNI bridge)
- libdvdread version: `6.1.3` (GPL; statically linked into the JNI bridge)
- Current target ABIs: `arm64-v8a`, `x86_64`
- Direct GitHub/development CSS path: libdvdcss 1.6.0, statically linked into `libmattmux_jni.so`
- Google Play CSS path: disabled with `MATTRIP_ANDROID_CSS=0` pending separate policy review

Direct CSS-capable Android releases include the exact libdvdcss source/provenance and license material alongside the other native dependency sources.

## Licensing

MattRip itself is licensed under MIT. Third-party projects keep their own licenses and copyright notices.

The Windows and Linux FFmpeg distributions currently used by MattRip are GPL-enabled builds because the desktop DVD workflow depends on FFmpeg's `dvdvideo` support with the relevant DVD libraries. The Android FFmpeg build is handled separately under its own build configuration and licensing requirements.

Anyone redistributing MattRip together with third-party binaries should review and satisfy the corresponding FFmpeg, BtbN/FFmpeg-Builds, MediaInfo, MediaInfoLib, ZenLib, zlib, libudfread, libdvdcss, and other applicable license/source-distribution obligations.

## Preserved desktop FFmpeg build inputs

BtbN can remove old daily-build archives. Packaging can recover the same FFmpeg
binaries originally shipped in MattMux 1.4.13 from these immutable inputs, verified
before extraction:

- Linux: `MattMux-1.4.13-Linux-amd64.deb`, SHA-256
  `680fce81c1562cac5c454c8eea2c5ac9b6a7d4a2710c91c7cfaa5d042fe248a9`.
- Windows: `MattMux-1.4.13-Windows-Portable.zip`, SHA-256
  `9a8127fb60684e86a3550d30a9f74ad9494a654218ad671d4c4527b0503d6bc9`.

These contain the existing `N-126479-g08cd8df29d` FFmpeg build; recovery does not
substitute a newer FFmpeg revision. The Linux standalone also includes private
GUI library notices and records their binary/source package versions under
`licenses/library-packages.json`. Exact source packages accompany each new
release in `MattRip-VERSION-Linux-Library-Sources.tar.gz`.

## Android MediaInfo metadata

The DVD Remux tab bundles MediaInfoLib and ZenLib in `libmediainfo_jni.so` on both
Android ABIs. Pinned sources: MediaInfoLib
`8bfa658657da9e16470c9fb32035e0fa097c0112` and ZenLib
`2ddc277fe7ecfcbfe45616bb9cd9e23079113ecd`. Their BSD-style notices are included
as `MediaInfoLib-License.html` and `ZenLib-License.txt`; exact source archives are
published with each release. Built statically into the JNI bridge with Android's
system zlib. The NDK static C++ runtime notices accompany it as `NDK-NOTICE.txt`;
the exact NDK revision is recorded in `ffmpeg-build-info.txt`. No runtime
downloads are required for MKV metadata.
