# Android native remux engine

MattRip Android/ChromeOS bundles FFmpeg/libav native libraries inside the APK/AAB and supports `arm64-v8a` plus `x86_64`.

## Runtime

- FFmpeg 9.0.1: LGPL-only configuration for probing/remuxing; GPL/nonfree disabled
- libudfread 1.1.2: UDF/ISO access
- libdvdnav 6.1.1 + libdvdread 6.1.3: DVD title discovery, using the same longest-title strategy proven by the 1.4.2 DVDNav Beta 1
- libdvdcss 1.6.0: optional CSS sector decryption for direct GitHub/development Android builds; statically linked into the JNI bridge when `MATTRIP_ANDROID_CSS=1`
- `libmattmux_jni.so`: MattRip JNI bridge; DVDNav/DVDRead and, when enabled, libdvdcss are statically linked into this library

The DVDNav scanner stages only `VIDEO_TS.IFO`/`VTS_nn_0.IFO` metadata into app cache, asks libdvdnav/libdvdread for title count and title durations, selects the longest title, deletes the staging directory, then hands that global title number back to MattRip's existing safe IFO/cell/chapter remux planner.

For CSS-capable builds, the existing SAF/UDF VOB reader supplies seek/read callbacks to libdvdcss and decrypts scrambled 2048-byte sectors before they enter the native libav remux/demux path. This keeps title selection, cell/chapter planning, progress and stream-copy behavior on the existing engine instead of adding a second DVD parser or remuxer. Android USB optical-drive transport is still a separate milestone.

Build with:

```bash
export ANDROID_NDK_HOME="$ANDROID_HOME/ndk/30.0.16248370"
bash android/native/build-ffmpeg-android.sh
```

Set `MATTRIP_ANDROID_CSS=0` to build the CSS-free variant used by the current Google Play workflow. The default direct/development build enables CSS support.

The build creates source/provenance archives and license assets for FFmpeg, libudfread, libdvdnav, libdvdread and libdvdcss when enabled, and verifies both 64-bit ABIs.
