# MattRip

**MattRip** is a cross-platform media remuxing, demuxing, merging, batch-processing, and CLI tool for **Windows, Linux, Android, and ChromeOS**. It focuses on copying existing media streams without transcoding.

MattRip can remux DVD-Video titles to MKV, extract individual streams from DVD or MKV sources, combine selected streams from multiple files, and batch-remux DVD libraries. Windows, Linux, and Android/ChromeOS are developed together from the single `main` branch and use one shared product version.

> The current MattRip baseline does **not yet** add CSS handling or physical-drive input. Those are planned fork features; this rebrand/cleanup change does not alter media access behavior.

## Development status

MattRip is under active development and does not have a public MattRip release yet. The fork starts from the verified MattMux 1.4.19 source baseline at commit `a794b451e454d7e0343cf5ba00ab200080e8e60b` and keeps the existing cross-platform behavior while MattRip-specific features are developed.

The first cleanup work intentionally separates MattRip's product identity from MattMux without changing the media engine behavior.


## What MattRip can do

### DVD Remux

The main **DVD Remux** tab accepts:

- DVD folders and `VIDEO_TS`
- unmounted DVD ISO images
- MKV files

For DVD sources, MattRip discovers DVD titles, automatically selects the longest readable title, and lets you inspect metadata before processing. Video, audio, and subtitle streams are individually selectable. All detected streams are selected by default.

Choose **Start Remux** to create an MKV using stream copy. Chapter preservation is optional.

Windows and Linux use FFmpeg's `dvdvideo` input backed by `libdvdread`/`libdvdnav`. Android/ChromeOS uses its native FFmpeg/libav, libdvdnav, libdvdread, and libudfread path. MattRip does not use its own DVD IFO parser.

### Demux

The **Demux** action in the DVD Remux tab extracts selected streams without re-encoding. It works with DVD folders, DVD ISOs, and MKV sources on Windows, Linux, and Android/ChromeOS.

Depending on the selected streams, MattRip can export:

- MPEG-2 video as `.mpeg2` or video-only `.VOB`
- H.264 as `.h264`
- HEVC/H.265 as `.h265`
- audio in its codec format, including AC-3 and DTS
- SRT and ASS text subtitles
- PGS subtitles as `.sup`
- DVD/VobSub subtitles as paired `.idx` + `.sub`
- chapters as `Chapters.txt` in simple OGM chapter format

The VOB option creates a video-only MPEG program stream; it does not recreate DVD menus or DVD-Video structure. Unsupported selected codecs are reported rather than transcoded.

Each demux operation creates a new output folder. DVD folders and ISOs are read directly from the selected title rather than being remuxed to a temporary MKV first. DVD inputs use the `100M / 100M / +genpts` input policy (`-analyzeduration 100M -probesize 100M -fflags +genpts` on desktop, with the native libav equivalent on Android/ChromeOS). MKV demux keeps its container-timestamp path instead of inheriting the DVD-specific 100M overrides.

Android/ChromeOS temporarily holds exported files while saving them through the Storage Access Framework; MKV inputs also require an app-private input copy. Direct DVD demux keeps stream selection, chapters, subtitle metadata, cancellation, extraction progress, and the DVD clock-reset/discontinuity handling used by the native reader.

### Advanced Merger

The **Advanced Merger** combines selected streams from multiple sources into one MKV without transcoding.

Inputs include normal containers such as MKV, MP4 and AVI, DVD ISOs, and supported elementary streams such as H.264, MPEG-2/VOB, AAC, AC-3, MP3, DTS, SRT, WebVTT and SUP.

- **CHOOSE MOVIE FILES** exposes discovered video, audio, subtitle, attachment/data streams and embedded chapters.
- **CHOOSE AUDIO FILES FROM MKV or RAW** exposes audio streams.
- **CHOOSE SUBTITLE FILES FROM MKV or RAW** exposes subtitle streams.
- **CHOOSE CHAPTER FILE FROM MKV or RAW** accepts chapters from MKV or valid `FFMETADATA1`.

Streams are explicitly selectable. Embedded chapter titles are preserved, and a dedicated chapter source overrides selected embedded chapters.

Advanced Merger also has a **DEMUX** action. Tick or untick the rows you want, then demux only the selected video, audio, and subtitle streams; a checked embedded chapter row exports `Chapters.txt`. Multiple inputs can be demuxed in one operation, with source-prefixed output names to avoid collisions. MPEG-2 video can be exported either as elementary `.mpeg2` or as video-only `.VOB`, matching the main Demux workflow.

DVD ISO input uses the longest DVD title and keeps that title through stream selection and muxing. Windows/Linux Advanced Merger DEMUX reads DVD selections directly through `dvdvideo`. Android/ChromeOS maps the selected staged merger rows back to the original DVD stream indexes and performs DEMUX directly from the original DVD source rather than from the temporary MKV used for the MUX workflow. Android/ChromeOS stages Storage Access Framework documents as needed; paired VobSub input requires both the matching `.idx` and `.sub` files.

See [`docs/ADVANCED_MERGER.md`](docs/ADVANCED_MERGER.md) for details.

### BATCH

**ONECLICK BATCH** scans a collection and losslessly remuxes discovered DVD movies to MKV. It supports `Movie/VIDEO_TS` folders and unmounted `.iso` files, automatically selects the longest readable title, preserves streams and chapters, and continues to later items when one item fails.

Example collection:

```text
Movies/
├── Alien.iso
├── Movie One/
│   └── VIDEO_TS/
│       ├── VIDEO_TS.IFO
│       └── ...
└── Movie Two/
    └── VIDEO_TS/
        ├── VIDEO_TS.IFO
        └── ...
```

Without an explicit output root, an ISO such as `Alien.iso` produces `Alien.mkv` beside the ISO, while `Movie One/VIDEO_TS` produces `Movie One/Movie One.mkv`. An explicit output root collects completed MKVs in that destination. Existing MKVs are not overwritten.

Windows and Linux also expose batch processing through the packaged CLI. Android/ChromeOS provides BATCH through its Storage Access Framework-based app implementation.

### CLI

Windows and Linux share the core CLI commands:

```text
scan
metadata
remux
--batch
```

Single-disc remuxing supports options including `--title`, `--streams 0,2`, `--no-chapters`, and `--output`. Windows Setup/Portable packages include `mattrip-cli.exe`; Linux packages include `mattrip-cli`, and the Linux Standalone accepts CLI commands directly or after `--cli`.

Example:

```bash
mattrip-cli remux --title 1 --streams 0,2 /path/to/DVD-or.iso
mattrip-cli --batch --log=/path/to/mattrip-batch.log /path/to/Movies /path/to/output
```

Android/ChromeOS also includes an in-app CLI with `scan`, `metadata`, `remux`, and `--batch` using Storage Access Framework content URIs. Android remux supports explicit `--streams` selection and `--no-chapters`.

BATCH and CLI are DVD-oriented workflows; MKV source selection and raw-stream extraction belong to the GUI DVD Remux/Demux and Advanced Merger workflows.

## Track selection and metadata

Use **Show Metadata** to inspect a selected source and choose exactly which video, audio, and subtitle streams to process.

For example:

```text
Video
☑ #0  MPEG-2 Video   720×576

Audio
☑ #1  AC-3 5.1      English
☐ #2  AC-3 2.0      Commentary

Subtitles
☑ #3  DVD Subtitle  English
☐ #4  DVD Subtitle  Norwegian
```

All tracks start selected. At least one media stream must remain selected for remuxing or demuxing. Chapter preservation/extraction is controlled separately.

Changing the source or DVD title clears the previous selection so stream indexes cannot accidentally carry over to another title. MKV metadata in the DVD Remux tab is displayed using MediaInfo.

## Platform support

### Windows

- Windows x64
- All-in-One single-file GUI
- normal Setup package
- Portable ZIP
- packaged `mattrip-cli.exe` in Setup/Portable
- bundled FFmpeg, FFprobe and MediaInfo
- current binaries are not Authenticode-signed

### Linux

- amd64 / x86_64
- one-file Standalone executable
- self-contained `.deb` and tarball packages
- GUI and packaged CLI
- published binaries currently require **glibc 2.38 or newer**
- Standalone includes private graphics/runtime dependencies while still relying on the host glibc, display session and GPU driver environment

### Android / ChromeOS

- Android 8.0 / API 26 or newer
- arm64-v8a and x86_64 native runtimes
- one persistently signed universal APK for Android phones/tablets and Chromebooks with Android app support
- native stream-copy DVD remux, direct DVD/MKV demux, Advanced Merger MUX/DEMUX, BATCH and in-app CLI paths
- Storage Access Framework input/output
- DVD folder and read-only UDF ISO support
- CI coverage includes API 26, API 35 and API 35 with 16 KB pages

Android/ChromeOS remains marked **experimental** while real-device/physical-Chromebook validation and production Play rollout remain separate gates. The v1.4.0 APK was debug-signed; v1.4.1 and later GitHub APKs use persistent distribution signing, so upgrading directly from v1.4.0 may require uninstalling it first.

See [`android/README.md`](android/README.md) for Android-specific implementation details and limitations.

## Important limitations

- The current MattRip baseline does not decrypt CSS or other protected DVD content; CSS-capable DVD access is planned fork work, not part of this cleanup.
- Interleaved multi-angle DVD titles are unsupported on the current Android path.
- Still/shuffle/multi-PGC DVD semantics are not fully supported on Android.
- Android ISO input requires a seekable storage provider.
- Android operations that stage media require sufficient temporary free space.
- Output capabilities can depend on the selected Android document provider.
- Remuxing and demuxing are stream-copy operations; unsupported formats are not silently transcoded.

## Quick start

There are no public MattRip binaries yet. Build MattRip from source using the platform instructions below while the fork is in development. Published MattMux 1.4.19 binaries remain MattMux artifacts and are not relabeled as MattRip.

## Build from source

Everything is built from `main`.

### Windows

```powershell
powershell -ExecutionPolicy Bypass -File .\src\build.ps1
```

### Linux

```bash
bash packaging/linux/build-linux-release.sh dev
bash packaging/linux/build-linux-standalone.sh dev
```

### Android / ChromeOS

Build the native runtime first, then run the Android tests/lint/package build:

```bash
export ANDROID_NDK_HOME="$ANDROID_HOME/ndk/30.0.16248370"
bash android/native/build-ffmpeg-android.sh
gradle -p android :app:testDebugUnitTest :app:lintDebug :app:assembleDebug
```

GitHub Actions uses the same native build step before Gradle. See [`android/native/README.md`](android/native/README.md) for the full native build process.

## Development and release model

`main` is the only long-lived source branch. Windows, Linux, and Android/ChromeOS changes are integrated into the same trunk and tested together.

New public versions use one shared tag:

- stable: `vMAJOR.MINOR.PATCH`
- preview: `vMAJOR.MINOR.PATCH-alpha.N`, `-beta.N`, or `-rc.N`

A release is built from one source commit and publishes the applicable Windows, Linux and Android/ChromeOS packages together. Published tags and release assets are treated as immutable; fixes are shipped as a new version.

See [`RELEASING.md`](RELEASING.md) for the full release policy.

## Recent development

The current feature set grew substantially after the early 1.4.x releases. Notable additions and fixes include cross-platform Advanced Merger expansion, Advanced Merger selected-stream DEMUX with MPEG-2/VOB choice, one-click BATCH, shared Windows/Linux DVD CLI commands, Android BATCH and in-app CLI support, DVD ISO handling, native Android libdvdnav title selection, MKV input in the DVD Remux tab, direct DVD demux without a temporary MKV, DVD clock-reset/progress handling, DVD subtitle extraction, and stronger Windows/Linux/Android parity coverage.

MattRip inherited this feature set from MattMux 1.4.19. For the pre-fork version history, see [MattMux Releases](https://github.com/maas3n/MattMux/releases).

## Third-party runtime tools

Desktop builds use FFmpeg/FFprobe and MediaInfo CLI. Android/ChromeOS uses native FFmpeg/libav plus DVD/UDF libraries and MediaInfo where required by the feature path.

Exact pinned versions, hashes, source revisions, licensing notes, and provenance are documented in [`THIRD_PARTY.md`](THIRD_PARTY.md).

## License

MattRip is licensed under the [MIT License](LICENSE). Third-party components remain governed by their own licenses.
