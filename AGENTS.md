# MattRip cross-platform release requirements

- Keep Android, Windows and Linux improvements aligned. For every media feature
  or bug fix, inspect all three implementations, update applicable counterparts,
  and test their common behavior. Document unavoidable platform differences.
- Treat every desktop DVD input as a `DVDSource`. VIDEO_TS folders, ISO images
  and physical optical drives must resolve through the shared source layer before
  probing/remux/demux/Advanced Merger. Do not add a drive-only remux engine or a
  separate title scanner. Windows optical drives are drive-letter sources; Linux
  optical drives are block-device sources such as `/dev/sr0`.
- Never implement a MattRip-written IFO parser. DVD titles, navigation, planning
  and chapters must come from libdvdnav/libdvdread, directly on Android or via
  FFmpeg/FFprobe dvdvideo on desktop. Do not add parser fallbacks.
- Desktop CSS support must extend that same dvdvideo/libdvdread path by making a
  pinned private libdvdcss runtime available dynamically. Do not add a second
  CSS-specific title scanner or remux engine. Preserve the exact libdvdcss source,
  checksum, license, and build provenance in self-contained release packages.
- Do not mark Android CSS as supported merely because libdvdcss can be linked to
  the DVDNav scanner. Android remux/demux reads VOB/UDF payload sectors directly;
  CSS-capable builds must decrypt that payload path, verify the statically linked
  libdvdcss symbols/notices in CI, and keep the existing unencrypted native parity
  regressions green. Real encrypted folder/ISO validation remains a pre-release
  manual gate; Android USB optical-drive transport is a separate feature.
- BATCH accepts DVD folders and unmounted ISOs. Blank output means an MKV beside
  the ISO or beside VIDEO_TS in the movie folder, never inside VIDEO_TS. An
  explicit output directory overrides this. Never overwrite existing outputs;
  record an individual failure and continue the remaining batch items.
- Maintain scan, metadata, remux, --batch, --title, --no-chapters and --streams
  in the desktop standalone CLI and the in-app CLI. Keep the Windows/Linux
  command parser and batch discovery shared. Android uses SAF content URIs.
- Android production signing uses two distinct identities. Direct GitHub APKs use
  MattRip's long-lived app-signing key; Play AAB uploads use a separate upload
  key. Pin both public certificate SHA-256 fingerprints in CI, require signed
  production builds, and never commit private keystores or passwords. Configure
  Play App Signing to use the MattRip app-signing identity so Play-delivered
  installs remain compatible with direct MattRip APK updates.
- A release is complete only when built from the same tag with Windows Setup,
  All-in-One and Portable; Linux DEB, tarball and Standalone; Android APK;
  exact source, dependency source/license notices, and verified checksums.
- Run the shared desktop behavior tests on Windows and Linux. Run authored DVD
  folder/ISO integration tests and Android native tests before release. Run
  scripts/check-release-assets.py before publishing. Do not move published tags
  or silently rebuild just one package variant.

- DVD input policy: apply `-analyzeduration 100M -probesize 100M -fflags +genpts`
  to each DVD VIDEO_TS/folder/ISO input in probing, remux, demux, batch, CLI and
  Advanced Merger (native equivalent: 100000000 microseconds, 100000000 bytes,
  AVFMT_FLAG_GENPTS). In mixed jobs the 100M probe overrides belong only to
  the DVD input, not accompanying MKV/MP4/raw/subtitle/chapter inputs.
  Preserve existing GENPTS handling: the user explicitly corrected its removal.
  Do not remove GENPTS while scoping the DVD probe settings. Test mixed inputs.
- `-safe 0` only relaxes concat-demuxer filename restrictions. It is not a
  timestamp/corruption recovery option and must not be passed to `dvdvideo`,
  `concat:` protocol inputs, or ordinary media readers.

- DVD demux must read the selected title directly, never through a temporary MKV.
  MKV demux remains supported and uses container timestamps without explicitly
  adding the DVD option bundle (including GENPTS). This demux-specific rule does
  not remove existing GENPTS from raw-media merging or any DVD path.
