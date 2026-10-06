#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
WORK="${SCRIPT_DIR}/.work"
JNI_ROOT="${REPO_ROOT}/android/app/src/main/jniLibs"
ASSET_ROOT="${REPO_ROOT}/android/app/src/main/assets/ffmpeg"
ANDROID_API="${ANDROID_API:-26}"
: "${ANDROID_NDK_HOME:?Set ANDROID_NDK_HOME to the Android NDK directory.}"

DVDREAD_VERSION=6.1.3
DVDNAV_VERSION=6.1.1
DVDCSS_VERSION=1.6.0
DVDCSS_SHA256=7ea556c846b7bfc32d47b41cae56d1863a6b6d5f706bb162778d6f298490977c
MATTRIP_ANDROID_CSS="${MATTRIP_ANDROID_CSS:-1}"
case "$MATTRIP_ANDROID_CSS" in
  0|1) ;;
  *) echo "MATTRIP_ANDROID_CSS must be 0 or 1" >&2; exit 1 ;;
esac
READ_SRC="$WORK/libdvdread"
NAV_SRC="$WORK/libdvdnav"
CSS_SRC="$WORK/libdvdcss-${DVDCSS_VERSION}"
CSS_ARCHIVE="$WORK/libdvdcss-${DVDCSS_VERSION}.tar.xz"
rm -rf "$READ_SRC" "$NAV_SRC" "$CSS_SRC"
git clone --depth 1 --branch "$DVDREAD_VERSION" https://code.videolan.org/videolan/libdvdread.git "$READ_SRC"
git clone --depth 1 --branch "$DVDNAV_VERSION" https://code.videolan.org/videolan/libdvdnav.git "$NAV_SRC"
if [[ "$MATTRIP_ANDROID_CSS" == "1" ]]; then
    curl --fail --location --retry 3 --proto '=https' --tlsv1.2 \
      -o "$CSS_ARCHIVE" "https://download.videolan.org/libdvdcss/${DVDCSS_VERSION}/libdvdcss-${DVDCSS_VERSION}.tar.xz"
    printf '%s  %s\n' "$DVDCSS_SHA256" "$CSS_ARCHIVE" | sha256sum --check --strict
    tar -xJf "$CSS_ARCHIVE" -C "$WORK"
    test -f "$CSS_SRC/COPYING"
fi
READ_COMMIT="$(git -C "$READ_SRC" rev-parse HEAD)"
NAV_COMMIT="$(git -C "$NAV_SRC" rev-parse HEAD)"
git -C "$READ_SRC" archive --format=tar --prefix="libdvdread-${DVDREAD_VERSION}/" HEAD | gzip -n > "$WORK/libdvdread-${DVDREAD_VERSION}-source.tar.gz"
git -C "$NAV_SRC" archive --format=tar --prefix="libdvdnav-${DVDNAV_VERSION}/" HEAD | gzip -n > "$WORK/libdvdnav-${DVDNAV_VERSION}-source.tar.gz"
cp "$READ_SRC/COPYING" "$ASSET_ROOT/DVDREAD_COPYING.txt"
cp "$NAV_SRC/COPYING" "$ASSET_ROOT/DVDNAV_COPYING.txt"
if [[ "$MATTRIP_ANDROID_CSS" == "1" ]]; then
    cp "$CSS_SRC/COPYING" "$ASSET_ROOT/LIBDVDCSS_COPYING.txt"
fi
(cd "$READ_SRC" && autoreconf -fi)
(cd "$NAV_SRC" && autoreconf -fi)

case "$(uname -s)-$(uname -m)" in
  Linux-x86_64) HOST_TAG=linux-x86_64 ;;
  Darwin-x86_64|Darwin-arm64) HOST_TAG=darwin-x86_64 ;;
  *) echo "Unsupported build host" >&2; exit 1 ;;
esac
TOOLCHAIN="$ANDROID_NDK_HOME/toolchains/llvm/prebuilt/$HOST_TAG"
AR="$TOOLCHAIN/bin/llvm-ar"
RANLIB="$TOOLCHAIN/bin/llvm-ranlib"
STRIP="$TOOLCHAIN/bin/llvm-strip"
NM="$TOOLCHAIN/bin/llvm-nm"

build_one() {
    local abi="$1" target="$2"
    local cc="$TOOLCHAIN/bin/${target}${ANDROID_API}-clang"
    local prefix="$WORK/install-${abi}"
    local jni="$JNI_ROOT/${abi}"
    local read_build="$WORK/build-dvdread-${abi}"
    local nav_build="$WORK/build-dvdnav-${abi}"
    local css_build="$WORK/build-dvdcss-${abi}"
    rm -rf "$read_build" "$nav_build" "$css_build"
    mkdir -p "$read_build" "$nav_build"
    if [[ "$MATTRIP_ANDROID_CSS" == "1" ]]; then
        command -v meson >/dev/null 2>&1 || { echo "Meson is required for libdvdcss ${DVDCSS_VERSION}" >&2; exit 1; }
        mkdir -p "$css_build"
        local css_cpu_family css_cpu
        case "$abi" in
            arm64-v8a) css_cpu_family=aarch64; css_cpu=aarch64 ;;
            x86_64) css_cpu_family=x86_64; css_cpu=x86_64 ;;
            *) echo "Unsupported libdvdcss Android ABI: $abi" >&2; exit 1 ;;
        esac
        local css_cross="$css_build/android-cross.ini"
        cat > "$css_cross" <<EOF
[binaries]
c = '$cc'
ar = '$AR'
strip = '$STRIP'

[host_machine]
system = 'android'
cpu_family = '$css_cpu_family'
cpu = '$css_cpu'
endian = 'little'

[properties]
needs_exe_wrapper = true

[built-in options]
c_args = ['-O2', '-fPIC']
c_link_args = ['-Wl,-z,max-page-size=16384']
EOF
        meson setup "$css_build/out" "$CSS_SRC" \
          --cross-file "$css_cross" \
          --prefix "$prefix" --libdir lib \
          --buildtype release --default-library static \
          -Db_staticpic=true -Denable_docs=false -Denable_examples=false
        meson compile -C "$css_build/out"
        meson install -C "$css_build/out"
        test -s "$prefix/lib/libdvdcss.a"
        test -f "$prefix/include/dvdcss/dvdcss.h"
    fi

    test -d "$prefix/include" && test -d "$jni"

    (
        cd "$read_build"
        if [[ "$MATTRIP_ANDROID_CSS" == "1" ]]; then
            PKG_CONFIG_PATH="$prefix/lib/pkgconfig" \
              CSS_CFLAGS="-I$prefix/include" CSS_LIBS="-L$prefix/lib -ldvdcss" \
              CC="$cc" AR="$AR" RANLIB="$RANLIB" STRIP="$STRIP" \
              CFLAGS='-O2 -fPIC' LDFLAGS='-Wl,-z,max-page-size=16384' \
              "$READ_SRC/configure" --host="$target" --prefix="$prefix" \
              --enable-static --disable-shared --with-libdvdcss
        else
            CC="$cc" AR="$AR" RANLIB="$RANLIB" STRIP="$STRIP" \
              CFLAGS='-O2 -fPIC' LDFLAGS='-Wl,-z,max-page-size=16384' \
              "$READ_SRC/configure" --host="$target" --prefix="$prefix" --enable-static --disable-shared
        fi
        make -j2
        make install
    )

    (
        cd "$nav_build"
        CC="$cc" AR="$AR" RANLIB="$RANLIB" STRIP="$STRIP" \
          DVDREAD_CFLAGS="-I$prefix/include" DVDREAD_LIBS="-L$prefix/lib -ldvdread" \
          CFLAGS='-O2 -fPIC' LDFLAGS='-Wl,-z,max-page-size=16384' \
          "$NAV_SRC/configure" --host="$target" --prefix="$prefix" --enable-static --disable-shared
        make -j2
        make install
    )

    local css_define=()
    local css_lib=()
    if [[ "$MATTRIP_ANDROID_CSS" == "1" ]]; then
        css_define=(-DMATTMUX_DVDCSS=1)
        css_lib=(-ldvdcss)
    fi

    "$cc" \
        -shared -fPIC -O2 -DMATTMUX_DVDNAV=1 "${css_define[@]}" \
        -I"$prefix/include" -I"$prefix/include/udfread" \
        "$SCRIPT_DIR/mattmux_jni.c" "$SCRIPT_DIR/udf_source.c" \
        -L"$jni" -L"$prefix/lib" -Wl,--no-as-needed \
        -lavformat -lavcodec -lavutil -ludfread \
        -ldvdnav -ldvdread "${css_lib[@]}" -ldl -lm \
        -Wl,-z,max-page-size=16384 -llog -Wl,--no-undefined \
        -Wl,-soname,libmattmux_jni.so -o "$jni/libmattmux_jni.so"

    while read -r dep; do
        case "$dep" in
            libavutil.so.*) patchelf --replace-needed "$dep" libavutil.so "$jni/libmattmux_jni.so" ;;
            libavcodec.so.*) patchelf --replace-needed "$dep" libavcodec.so "$jni/libmattmux_jni.so" ;;
            libavformat.so.*) patchelf --replace-needed "$dep" libavformat.so "$jni/libmattmux_jni.so" ;;
        esac
    done < <(patchelf --print-needed "$jni/libmattmux_jni.so")

    # Capture the complete symbol table before checking it. With pipefail,
    # grep -q can close the pipe early and make llvm-nm fail with exit 74.
    "$NM" "$jni/libmattmux_jni.so" > "$nav_build/jni-symbols.txt"
    grep -q 'dvdnav_get_number_of_titles' "$nav_build/jni-symbols.txt"
    grep -q 'dvdnav_describe_title_chapters' "$nav_build/jni-symbols.txt"
    grep -q 'DVDOpen' "$nav_build/jni-symbols.txt"
    # libdvdcss is linked statically when enabled; no standalone JNI dependency
    # should be required at runtime.
    patchelf --print-needed "$jni/libmattmux_jni.so" | grep -Eiq 'dvdcss' && {
        echo 'libdvdcss must remain statically linked into the JNI runtime' >&2; exit 1;
    } || true
    if [[ "$MATTRIP_ANDROID_CSS" == "1" ]]; then
        grep -q 'dvdcss_open_stream' "$nav_build/jni-symbols.txt"
        grep -q 'dvdcss_read' "$nav_build/jni-symbols.txt"
    fi
    "$STRIP" --strip-unneeded "$jni/libmattmux_jni.so"
}

build_one arm64-v8a aarch64-linux-android
build_one x86_64 x86_64-linux-android

cat >> "$ASSET_ROOT/ffmpeg-build-info.txt" <<EOF

DVD TITLE DISCOVERY
Title discovery: libdvdnav ${DVDNAV_VERSION} + libdvdread ${DVDREAD_VERSION}
Integration: GPL DVD libraries statically linked into libmattmux_jni.so for title discovery
libdvdread commit: ${READ_COMMIT}
libdvdnav commit: ${NAV_COMMIT}
CSS support: $(if [[ "$MATTRIP_ANDROID_CSS" == "1" ]]; then printf 'libdvdcss %s (GPL; statically linked into JNI)' "$DVDCSS_VERSION"; else printf 'not included'; fi)
EOF

# The unified release workflow uploads every file already present in
# dist/android-release. Stage the GPL DVD source archives and notices here so
# future Android/ChromeOS releases automatically publish the corresponding
# source and license material alongside the APK.
RELEASE_STAGE="${REPO_ROOT}/dist/android-release"
mkdir -p "$RELEASE_STAGE"
cp "$WORK/libdvdread-${DVDREAD_VERSION}-source.tar.gz" "$RELEASE_STAGE/"
cp "$WORK/libdvdnav-${DVDNAV_VERSION}-source.tar.gz" "$RELEASE_STAGE/"
cp "$ASSET_ROOT/DVDREAD_COPYING.txt" "$RELEASE_STAGE/"
cp "$ASSET_ROOT/DVDNAV_COPYING.txt" "$RELEASE_STAGE/"
if [[ "$MATTRIP_ANDROID_CSS" == "1" ]]; then
    cp "$CSS_ARCHIVE" "$RELEASE_STAGE/libdvdcss-${DVDCSS_VERSION}-source.tar.xz"
    cp "$ASSET_ROOT/LIBDVDCSS_COPYING.txt" "$RELEASE_STAGE/"
fi
test -s "$RELEASE_STAGE/libdvdread-${DVDREAD_VERSION}-source.tar.gz"
test -s "$RELEASE_STAGE/libdvdnav-${DVDNAV_VERSION}-source.tar.gz"
test -s "$RELEASE_STAGE/DVDREAD_COPYING.txt"
test -s "$RELEASE_STAGE/DVDNAV_COPYING.txt"
if [[ "$MATTRIP_ANDROID_CSS" == "1" ]]; then
    test -s "$RELEASE_STAGE/libdvdcss-${DVDCSS_VERSION}-source.tar.xz"
    test -s "$RELEASE_STAGE/LIBDVDCSS_COPYING.txt"
fi

echo "libdvdnav/libdvdread Android title scanner built successfully (CSS=${MATTRIP_ANDROID_CSS})."
