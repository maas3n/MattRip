# Changelog

## Unreleased

- Fork MattRip from the verified MattMux 1.4.19 baseline and establish separate Windows, Linux, Android, CLI, package, cache, installer, and release identities without changing media-engine behavior.
- Preserve historical MattMux release artifacts only where they are checksum-verified build inputs; historical entries below remain MattMux history rather than being relabeled as MattRip releases.
- Retire inherited one-off MattMux publish/hotfix workflows from the active fork workflow set.

- Keep the Windows All-in-One chooser and its message loop on the same OS
  thread to prevent the launcher becoming unresponsive. Test all three buttons.
- Add an isolated Mesa software renderer to the Linux standalone, with exact
  dependency sources and notices. Prefer a working system OpenGL driver and
  automatically fall back when graphics initialization fails.
- Validate actual Linux GUI startup on a clean runtime without host graphics
  client libraries, in addition to checking library loading.

- Android: correct libdvdnav's one-based title numbering in title discovery and
  planning. Batch, Remux, CLI and Advanced Merger now select the actual longest
  title and keep its duration/chapters aligned with its video cells.
- Add production JNI regressions for single-title DVDs and multi-title DVDs
  with the main movie first/last, including staged IFO folder/ISO scans and
  compressed-packet/chapter parity. Test mixed batch sources with opaque provider IDs.
- Preserve emulator diagnostics containing non-UTF-8 bytes without failing a
  successful startup test.


- Bundle private Wayland, X11, keyboard, and OpenGL dispatch libraries and their dependencies in the Linux standalone, so users do not need to install missing GUI packages manually.
- Check GUI library resolution in the standalone self-test and add a clean Ubuntu runtime check to Linux CI and release validation.
- Fix the Android startup crash caused by legacy TabHost indicator layouts missing from the Material theme; supply explicit tab views and verify opening all four tabs on emulators.
- Recover pinned desktop FFmpeg tools from checksum-verified MattMux 1.4.13 packages when upstream daily-build archives have expired.

## 1.4.8 — 2026-09-13

- Fix Windows native-tab repainting so switching among **DVD Remux**, **Advanced Merger**, and **BATCH** no longer leaves stale controls or apparent duplicate buttons behind.
- Rename the desktop BATCH action from **ONECLICK BATCH MUX BUTTON** to **ONECLICK BATCH** on Windows and Linux, including the matching instruction text.
- Remove the legacy MattMux desktop `scanTitles()` / `probeDuration()` naming and wrapper structure from Windows/Linux GUI, CLI, and BATCH title discovery; candidate titles are discovered through FFprobe's `dvdvideo` demuxer backed by `libdvdread`/`libdvdnav`, with an automatic `-preindex 1` retry when needed.
- Keep the separate native IFO chapter parser only for chapter metadata; it is not used for DVD title discovery.
- Keep Android/ChromeOS product behavior unchanged from 1.4.7.

## 1.4.7 — 2026-09-12

- Add a desktop **BATCH** tab on Windows and Linux with a **ONECLICK BATCH MUX BUTTON** for collections laid out as `Movie Title/VIDEO_TS/VIDEO_TS.IFO`.
- Discover each immediate movie-title folder, scan DVD titles through FFmpeg `dvdvideo` backed by `libdvdread`/`libdvdnav`, and automatically choose the longest readable title without restoring MattMux's removed `ReadDVDTitleCount()` path.
- Remux each selected longest title with all streams and chapters using lossless stream copy and always apply `-analyzeduration 100M -probesize 100M -fflags +genpts` on the DVD input.
- Default each completed MKV to its corresponding movie-title folder, with an optional common output folder in the BATCH UI.
- Add Linux CLI batch mode: `mattmux-cli --batch [--log FILE] MOVIES_ROOT [OUTPUT_ROOT]`; `--log` is optional and batch output defaults to each movie folder when `OUTPUT_ROOT` is omitted.
- Add a Windows `mattmux-cli.exe` batch executable with the same `--batch` / optional `--log` interface and package it with Setup and Portable distributions.
- Keep Android/ChromeOS product behavior unchanged from 1.4.6.

## 1.4.6 — 2026-09-12

- Keep the 1.4.5 Windows/Linux **Scan Titles** implementation unchanged: title discovery remains entirely on FFmpeg `dvdvideo` with `libdvdread`/`libdvdnav`, and MattMux's old `ReadDVDTitleCount()` parser stays removed.
- Make Windows/Linux DVD remux inputs always use `-analyzeduration 100M -probesize 100M -fflags +genpts`.
- Remove `-preindex 1` from the Windows/Linux **Start Remux** command so remuxing starts immediately instead of pre-indexing the DVD when chapter preservation is enabled; chapter mapping remains controlled by the existing Preserve chapters option.
- Apply the same `-analyzeduration 100M -probesize 100M -fflags +genpts` input policy to every Windows/Linux Advanced Merger media input and separate chapter input.
- Android/ChromeOS behavior is unchanged from 1.4.5.

## 1.4.5 — 2026-09-12

- Hotfix Windows and Linux **Scan Titles** so both DVD folders/`VIDEO_TS` sources and ISO images are discovered through FFmpeg's `dvdvideo` demuxer, which uses `libdvdread` for DVD structure parsing and `libdvdnav` for navigation.
- Remove MattMux's desktop `ReadDVDTitleCount()`/TT_SRPT title-count parser from the scan path; candidate title numbers 1–99 are now validated by the same `dvdvideo`/`libdvdread`/`libdvdnav` path on both desktop platforms.
- Keep the native desktop IFO chapter parser unchanged for chapter metadata; this hotfix only changes **Scan Titles** title discovery.
- Android/ChromeOS behavior is unchanged from 1.4.4.

## 1.4.4 — 2026-09-12

- Update **CHOOSE MOVIE FILES** so movie/container inputs expose every probed stream in **Select Streams**, including video, audio, subtitle, attachment/data streams and an embedded chapter-set checkbox when chapters are present.
- Keep **CHOOSE AUDIO FILES FROM MKV or RAW** and **CHOOSE SUBTITLE FILES FROM MKV or RAW** category-filtered so only the requested stream type is added from containers or raw/elementary inputs.
- Allow chapters to come from a selected movie's embedded chapter set or from **CHOOSE CHAPTER FILE FROM MKV or RAW** using either an MKV containing chapters or a valid `FFMETADATA1` file; a dedicated chapter source overrides movie chapter selections.
- Preserve chapter titles and stream metadata during desktop muxing, including attachment filenames, while keeping explicit stream maps and lossless stream copy.
- Extend Android/ChromeOS native merger probing and muxing to the same all-stream movie behavior, embedded chapters, MKV/FFMETADATA1 chapter overrides, metadata copying, and attachment/data stream handling.
- Add cross-platform regression coverage for embedded movie chapters, chapter-title preservation, all-stream movie imports, raw media inputs, package builds, and the exact Windows/Linux/Android release paths.

## 1.4.3 — 2026-09-12

- Add an **Advanced Merger** tab on Windows, Linux, Android, and ChromeOS with multiple-file selection, category-filtered video/audio/subtitle tracks, optional FFMETADATA1 chapters, output-folder and filename selection, and lossless muxing to MKV.
- Accept FFmpeg-supported containers and elementary media inputs while exposing only the stream category requested by the corresponding Movie, Audio, or Subtitle input button.
- Use explicit stream maps on desktop and equivalent native libav packet mapping on Android/ChromeOS, with cancellable jobs and no-overwrite output finalization.
- Add real-FFmpeg integration coverage for mixed containers, raw H.264/MPEG-2/VOB, external AC-3/SRT inputs, chapter inclusion, cancellation, and stream-copy behavior.
- Add native Windows tabs and Android/ChromeOS stream-copy merging through JNI.
- Android/ChromeOS stages selected inputs and output in private temporary storage; select both IDX and SUB when adding VobSub subtitles.
- Keep the Android/ChromeOS tab interface clear of status and navigation bars across phones, navigation modes, rotation, and resizable ChromeOS windows.

## 1.4.2 — 2026-09-11

- Publish the signed universal APK under both `MattMux-1.4.2-Android.apk` for Android phones/tablets and `MattMux-1.4.2-ChromeOS.apk` for Chromebooks.
- Verify the two Android/ChromeOS APK assets are byte-for-byte identical before publishing.
- Include both APK filenames in the Android and combined SHA-256 manifests.

## 1.4.1 — 2026-09-11

- Preserve completed desktop and Android remuxes when final publication fails, and add Linux no-overwrite publication fallbacks for filesystems without hard links.
- Use consistent 100M probe/analyze limits for desktop metadata probing and remuxing.
- Clean Linux CLI partial outputs on Ctrl+C/SIGTERM and add a process-level interrupt regression test.
- Correct Debian preview ordering and declare the measured `libc6 (>= 2.38)` runtime floor.
- Give additional DVD titles distinct `-title-NN` desktop output filenames.
- Sign GitHub Android APKs with persistent distribution credentials starting with 1.4.1 and reject debug certificates; users coming from debug-signed v1.4.0 may need to uninstall/reinstall.
- Carry DVD IFO language mappings and the selected PGC subtitle palette into Android native stream metadata.
- Align Android/release documentation and remove tracked Python bytecode.

MattMux uses one `main` source branch and one unified product-version namespace. Published builds are identified by immutable unified release tags.

## Android / ChromeOS 1.2.0 Alpha 4 — `v1.2.0-chromeos-alpha4`

- Added the Android/ChromeOS native remux path for DVD folders and read-only UDF ISO input.
- Added one IFO/title/cell planner for folder and ISO sources, longest-title selection, chapter planning, native stream-copy remuxing, cancellation, and progress reporting.
- Added arm64-v8a and x86_64 LGPL FFmpeg 9.0.1 + libudfread 1.1.2 runtimes with package/provenance verification.
- Added Android unit tests, host/native parity tests, APK/AAB validation, and 16 KB page-size checks.
- Google Play Billing integration is present, but purchases remain disabled in this alpha.

## Linux 1.3.0-dev5 — `v1.3.0-dev5`

- Added Linux GUI and CLI builds from the shared desktop source.
- Added portable amd64 tarball, self-contained `.deb`, source archive, and one-file standalone executable.
- Bundled FFmpeg/FFprobe and MediaInfo privately for the self-contained packages without replacing system multimedia tools.
- Pinned FFmpeg and MediaInfo-related sources/checksums for reproducible release construction.
- Added release-audit tests and safe no-overwrite output finalization.

## Windows 1.2.0 — `v1.2.0`

- Added a **Preserve chapters in the output MKV** checkbox; enabled by default and persisted.
- Added dependency-free native Go DVD IFO chapter detection for `VIDEO_TS` sources.
- Added automatic FFprobe `dvdvideo`/preindex fallback for ISO and unusual DVD authoring.
- Metadata window now lists detected chapter number, start timestamp, and duration.
- Chapter-preserving remuxes explicitly use FFmpeg `dvdvideo -preindex 1` and `-map_chapters 0`.
- Disabling chapter preservation explicitly uses `-map_chapters -1`.
- Added verified self-contained Setup, Portable and All-in-One packages.
- Added unique operation-owned temporary MKVs, validation/sync, and atomic no-overwrite finalization.

## Windows 1.1.0

- Rebuilt the Windows UI with clearer source/destination/title sections.
- Added DVD-folder and ISO-specific pickers plus drag-and-drop.
- Added cancellation for scans, downloads, metadata reads, and remuxes.
- Added longest-title auto-selection after scanning.
- Added stale-title protection when the source path changes after a scan.
- Added verified, pinned FFmpeg and MediaInfo downloads.
- Added safe ZIP extraction and protected output-folder checks.
- Added FFmpeg progress reporting.
- Kept partial-MKV then rename behavior for safer output.
- Added metadata viewer, persistent output settings, and local operation logs.
- Added DPI-aware external manifest and a dedicated sidecar app icon.
