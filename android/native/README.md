# Android native remux engine

MattRip Android/ChromeOS bundles FFmpeg/libav native libraries inside the APK/AAB and supports `arm64-v8a` plus `x86_64`.

## Runtime

- FFmpeg 9.0.1: LGPL-only configuration for probing/remuxing; GPL/nonfree disabled
- libudfread 1.1.2: UDF/ISO access
- libdvdnav 6.1.1 + libdvdread 6.1.3: DVD title discovery, using the same longest-title strategy proven by the 1.4.2 DVDNav Beta 1
- `libmattmux_jni.so`: MattRip JNI bridge; DVDNav/DVDRead are statically linked into this library

The DVDNav scanner stages only `VIDEO_TS.IFO`/`VTS_nn_0.IFO` metadata into app cache, asks libdvdnav/libdvdread for title count and title durations, selects the longest title, deletes the staging directory, then hands that global title number back to MattRip's existing safe IFO/cell/chapter remux planner.

This preserves the current 1.4.9 remux engine and Advanced Merger while replacing longest-title discovery with the tested DVDNav Beta behavior. No CSS decryption/circumvention is included.

Build with:

```bash
export ANDROID_NDK_HOME="$ANDROID_HOME/ndk/30.0.16248370"
bash android/native/build-ffmpeg-android.sh
```

The build creates source/provenance archives and license assets for FFmpeg, libudfread, libdvdnav and libdvdread and verifies both 64-bit ABIs.
