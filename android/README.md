# MattRip for Android / ChromeOS

This directory contains the experimental Android/ChromeOS frontend for MattRip. Android/ChromeOS follows the unified MattRip development line. No public MattRip APK has been released yet; future releases will use `MattRip-<version>-Android.apk`.

Use `MattRip-<version>-Android.apk` on Android phones/tablets and on Chromebooks with Android app support. The same APK supports both Android and ChromeOS; no separate ChromeOS APK is required.

## Experimental status

Implemented:

- ChromeOS-compatible, resizable Android activity
- Storage Access Framework input/output pickers
- DVD-folder / `VIDEO_TS` input through document-tree providers
- read-only UDF ISO input through libudfread
- one IFO/title/cell planner for folder and ISO sources
- longest-title selection
- native libavformat/libavcodec/libavutil stream-copy remuxing to MKV
- chapter planning and MKV chapter output
- temporary `.partial` output with commit/abort handling
- cancellation and native progress callbacks
- arm64-v8a and x86_64 native runtimes
- LGPL-only FFmpeg 9.0.1 runtime built from pinned source
- separately linked LGPL libudfread 1.1.2
- Android unit tests, native host parity tests, APK/AAB package verification, and 16 KB page-size checks in CI

Current limitations / remaining gates:

- CSS or other DVD copy protection is not bypassed
- interleaved multi-angle discs are unsupported
- still/shuffle/multi-PGC semantics are not fully supported
- ISO input requires a seekable storage provider
- output providers must support random-access writing and rename
- ISO9660-only images and streaming-only providers are unsupported
- real-device Android and physical Chromebook testing remain release gates
- production Play purchase verification and rollout remain separate from the experimental GitHub APK
- MattRip uses a separate Android application ID from MattMux; establish MattRip's own persistent signing identity before its first public APK

Run and release status should be checked in CI; source implementation alone is not proof of a tested APK. Native test details are in [`native/tests/README.md`](native/tests/README.md).

## Billing

The project contains a Google Play Billing integration and the non-consumable product ID `mattrip_pro`, but purchases are deliberately disabled in the current experimental build through `BuildConfig.ENABLE_BILLING_PURCHASES = false`.

Do not enable charging merely because the native remux engine now exists. Enable production purchases only after the remux path has passed real Chromebook/device testing and the production purchase-verification/signing plan is ready. See [`PLAY_CONSOLE.md`](PLAY_CONSOLE.md).

## Native runtime and licensing

The Android commercial build must remain separate from the GPL-enabled desktop FFmpeg packages. `native/build-ffmpeg-android.sh` builds FFmpeg 9.0.1 as LGPL-only shared libraries and rejects GPL/nonfree configuration plus prohibited DVD-library dependencies. libudfread is linked separately as an LGPL shared library.

Generated native libraries and FFmpeg provenance/license assets are build outputs and are intentionally not committed:

```text
app/src/main/jniLibs/
├── arm64-v8a/
│   ├── libavutil.so
│   ├── libavcodec.so
│   ├── libavformat.so
│   ├── libudfread.so
│   └── libmattmux_jni.so
└── x86_64/
    ├── libavutil.so
    ├── libavcodec.so
    ├── libavformat.so
    ├── libudfread.so
    └── libmattmux_jni.so
```

See [`native/FFMPEG_LGPL_POLICY.md`](native/FFMPEG_LGPL_POLICY.md) and the repository-level [`THIRD_PARTY.md`](../THIRD_PARTY.md) for provenance and licensing details.

## Build

Requirements used by CI:

- JDK 17
- Gradle 9.6.0
- Android SDK platform/build-tools 36
- Android NDK `30.0.16248370`
- `curl`, `xz`, `patchelf`, Autotools and the normal native build toolchain

From the repository root:

```bash
export ANDROID_NDK_HOME="$ANDROID_HOME/ndk/30.0.16248370"
bash android/native/build-ffmpeg-android.sh
gradle -p android :app:testDebugUnitTest :app:lintDebug :app:assembleDebug :app:bundleRelease
```

## Google Play

The intended one-time product ID is:

```text
mattrip_pro
```

Price and availability belong in Play Console rather than in the APK. Follow [`PLAY_CONSOLE.md`](PLAY_CONSOLE.md) before enabling purchases or publishing a production Play build.

## In-app CLI stream selection and ISO output

`mattrip-cli remux --streams 0,2 --title 1 SOURCE` copies only the absolute stream indexes shown by `metadata --title 1 SOURCE`. Both `--streams 0,2` and `--streams=0,2` are accepted; omitting the option copies all streams. Unknown indexes fail before creating output. `--no-chapters` remains independent of stream selection.

For a single unmounted `Movie.iso`, omitting `--output` creates `Movie.mkv` beside the ISO when MattRip has read/write access to its parent through a granted document tree. A standalone file grant may not include parent access: grant the containing folder or pass `--output OUTPUT_ROOT`. Provider document IDs are treated as opaque; MattRip verifies the parent through the provider instead of guessing a filesystem path. An explicit output folder overrides this default and retains the ISO-derived filename.

Existing MKVs are never overwritten. The CLI reserves a new final-name document before writing, so it is visible while remuxing; cancellation/failure removes that newly created document. Providers that cannot supply the requested name or a writable seekable output fail cleanly.
