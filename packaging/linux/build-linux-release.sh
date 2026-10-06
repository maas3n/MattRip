#!/usr/bin/env bash
set -euo pipefail

APP_VERSION="${1:-1.3.0-dev5}"
DEB_VERSION="$APP_VERSION"
case "$DEB_VERSION" in
  *-alpha.*) DEB_VERSION="${DEB_VERSION/-alpha./~alpha.}" ;;
  *-beta.*)  DEB_VERSION="${DEB_VERSION/-beta./~beta.}" ;;
  *-rc.*)    DEB_VERSION="${DEB_VERSION/-rc./~rc.}" ;;
  *-dev*)    DEB_VERSION="${DEB_VERSION/-dev/~dev}" ;;
esac
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SRC="$ROOT/src"
DIST="$ROOT/dist/linux-release"
WORK="$ROOT/dist/linux-work"

# Pinned third-party tools for the Debian package. They are installed only
# below /usr/lib/mattrip and never replace distro executables in /usr/bin.
FFMPEG_TAG="autobuild-2026-09-08-23-15"
FFMPEG_ASSET="ffmpeg-N-126479-g08cd8df29d-linux64-gpl.tar.xz"
FFMPEG_SHA256="635a2d74de852064852e95db5a9c475a86d36e2b6390e3c1ba5e46b2c46dfce0"
FFMPEG_URL="https://github.com/BtbN/FFmpeg-Builds/releases/download/$FFMPEG_TAG/$FFMPEG_ASSET"
LIBDVDCSS_VERSION="1.6.0"

# MediaInfo's static CMake build normally auto-fetches these repositories from
# their moving master branches. Pin every checkout so rebuilding this release
# cannot silently pick up different source later.
MEDIAINFO_TAG="v26.05"
MEDIAINFO_REPO="https://github.com/MediaArea/MediaInfo.git"
MEDIAINFO_COMMIT="4728f24b666117a19d36515d95b9367fbb37aaf6"
MEDIAINFOLIB_REPO="https://github.com/MediaArea/MediaInfoLib.git"
MEDIAINFOLIB_COMMIT="8bfa658657da9e16470c9fb32035e0fa097c0112"
ZENLIB_REPO="https://github.com/MediaArea/ZenLib.git"
ZENLIB_COMMIT="2ddc277fe7ecfcbfe45616bb9cd9e23079113ecd"
ZLIB_REPO="https://github.com/MediaArea/zlib.git"
ZLIB_COMMIT="eaaf237c8cbc7310170c43202c6ec2cff64fff66"

if [[ "$(uname -s)" != "Linux" ]]; then echo "This packaging script must run on Linux." >&2; exit 1; fi
if [[ "$(uname -m)" != "x86_64" ]]; then echo "The bundled toolchain is currently pinned for amd64/x86_64 only." >&2; exit 1; fi
for cmd in go git tar dpkg-deb sha256sum curl cmake ninja meson; do command -v "$cmd" >/dev/null 2>&1 || { echo "Missing build tool: $cmd" >&2; exit 1; }; done

checkout_exact() {
  local repo="$1" commit="$2" dst="$3" label="$4"
  git init -q "$dst"
  git -C "$dst" remote add origin "$repo"
  git -C "$dst" fetch --quiet --depth 1 origin "$commit"
  git -C "$dst" checkout --quiet --detach FETCH_HEAD
  local actual
  actual="$(git -C "$dst" rev-parse HEAD)"
  [[ "$actual" == "$commit" ]] || { echo "$label commit verification failed: expected $commit, got $actual" >&2; exit 1; }
  echo "$label source pinned to $actual"
}

rm -rf "$DIST" "$WORK"
mkdir -p "$DIST" "$WORK/bin" "$WORK/tools"

pushd "$SRC" >/dev/null
export CGO_ENABLED=1
go test -tags cli ./...
go vet -tags cli ./...
go build -trimpath -ldflags "-s -w -X main.appVersion=$APP_VERSION" -o "$WORK/bin/mattrip-bin" .
go build -tags cli -trimpath -ldflags "-s -w -X main.appVersion=$APP_VERSION" -o "$WORK/bin/mattrip-cli-bin" .
popd >/dev/null

# The portable tarball remains small and uses the normal MattRip runtime tool
# discovery/fallback behavior. The .deb below is the self-contained installer.
PORTABLE="$WORK/MattRip-$APP_VERSION-Linux-amd64"
mkdir -p "$PORTABLE"
install -m 0755 "$WORK/bin/mattrip-bin" "$PORTABLE/mattrip"
install -m 0755 "$WORK/bin/mattrip-cli-bin" "$PORTABLE/mattrip-cli"
cat > "$PORTABLE/README-LINUX.txt" <<TXT
MattRip $APP_VERSION for Debian/Ubuntu Linux (amd64)

mattrip      Desktop GUI
mattrip-cli  Command-line interface

This portable archive checks ffmpeg, ffprobe, and mediainfo on PATH first.
System ffmpeg/ffprobe are used only when FFmpeg exposes the dvdvideo demuxer.
If system FFmpeg is missing or incompatible, MattRip can prepare its pinned,
SHA-256-verified FFmpeg fallback in the current user's cache.
MediaInfo is optional for the portable archive.

For a one-file installer with all required multimedia tools included, use the
MattRip .deb package. Its private tools never replace system multimedia tools.

CSS-protected DVD access is supported through the bundled libdvdcss runtime.
MattRip keeps libdvdcss private to its DVD tools and does not install it system-wide.
Use this functionality only where you are legally permitted to access the disc.
TXT

# Download and verify the exact GPL FFmpeg build used inside the self-contained
# .deb. dvdvideo requires a GPL-enabled FFmpeg build with libdvdnav/read.
FF_ARCHIVE="$WORK/tools/$FFMPEG_ASSET"
FF_EXTRACT="$WORK/tools/ffmpeg"
echo "Downloading pinned FFmpeg build for self-contained .deb..."
mkdir -p "$FF_EXTRACT"
if curl --fail --location --retry 3 --proto '=https' --tlsv1.2 -o "$FF_ARCHIVE" "$FFMPEG_URL"; then
  printf '%s  %s\n' "$FFMPEG_SHA256" "$FF_ARCHIVE" | sha256sum --check --strict
  tar -xJf "$FF_ARCHIVE" -C "$FF_EXTRACT"
else
  # Upstream prunes old daily builds. Recover the identical tools already
  # distributed in our immutable release, never an unverified newer build.
  PREVIOUS_DEB="$WORK/tools/MattMux-1.4.13-Linux-amd64.deb"
  curl --fail --location --retry 3 --proto '=https' --tlsv1.2 \
    -o "$PREVIOUS_DEB" "https://github.com/maas3n/MattMux/releases/download/v1.4.13/MattMux-1.4.13-Linux-amd64.deb"
  printf '%s  %s\n' '680fce81c1562cac5c454c8eea2c5ac9b6a7d4a2710c91c7cfaa5d042fe248a9' "$PREVIOUS_DEB" | sha256sum --check --strict
  dpkg-deb --extract "$PREVIOUS_DEB" "$WORK/tools/previous-release"
  cp -a "$WORK/tools/previous-release/usr/lib/mattmux/ffmpeg-bin" "$FF_EXTRACT/bin"
  cp "$WORK/tools/previous-release/usr/share/doc/mattmux/FFmpeg-LICENSE" "$FF_EXTRACT/LICENSE.txt"
fi
BUNDLED_FFMPEG="$(find "$FF_EXTRACT" -type f -name ffmpeg -perm -u+x | head -n1)"
BUNDLED_FFPROBE="$(find "$FF_EXTRACT" -type f -name ffprobe -perm -u+x | head -n1)"
[[ -n "$BUNDLED_FFMPEG" && -n "$BUNDLED_FFPROBE" ]] || { echo "FFmpeg archive did not contain ffmpeg/ffprobe" >&2; exit 1; }
"$BUNDLED_FFMPEG" -hide_banner -demuxers 2>/dev/null | grep -q 'dvdvideo' || { echo "Pinned FFmpeg lacks dvdvideo demuxer" >&2; exit 1; }

# Build the current pinned libdvdcss as a private shared runtime. libdvdread
# discovers it dynamically, so MattRip keeps the existing dvdvideo pipeline.
LIBDVDCSS_PREFIX="$WORK/tools/libdvdcss-install"
LIBDVDCSS_WORK="$WORK/tools/libdvdcss-build"
LIBDVDCSS_PREFIX="$LIBDVDCSS_PREFIX" LIBDVDCSS_WORK="$LIBDVDCSS_WORK" \
  bash "$ROOT/scripts/build-libdvdcss.sh"
test -e "$LIBDVDCSS_PREFIX/lib/libdvdcss.so.2" || { echo "Pinned libdvdcss runtime is missing" >&2; exit 1; }

# The portable archive carries the same private runtime beside the MattRip
# binaries. src/dvd_css_linux.go exposes that directory only to child media tools.
cp -a "$LIBDVDCSS_PREFIX"/lib/libdvdcss.so* "$PORTABLE/"
mkdir -p "$PORTABLE/licenses/libdvdcss"
cp "$LIBDVDCSS_PREFIX/share/mattrip/libdvdcss/COPYING" "$PORTABLE/licenses/libdvdcss/"
cp "$LIBDVDCSS_PREFIX/share/mattrip/libdvdcss/BUILD-INFO.txt" "$PORTABLE/licenses/libdvdcss/"
cp "$LIBDVDCSS_PREFIX/share/mattrip/libdvdcss/libdvdcss-$LIBDVDCSS_VERSION-source.tar.xz" "$PORTABLE/licenses/libdvdcss/"
tar -C "$WORK" -czf "$DIST/MattRip-$APP_VERSION-Linux-amd64.tar.gz" "$(basename "$PORTABLE")"

# Build MediaInfo with all four source repositories pinned to exact commits.
# Pre-populating the directories prevents CMake FetchContent from following
# moving master branches at build time.
MI_SRC="$WORK/tools/MediaInfo"
MILIB_SRC="$WORK/tools/MediaInfoLib"
ZEN_SRC="$WORK/tools/ZenLib"
ZLIB_SRC="$WORK/tools/zlib"
MI_BUILD="$WORK/tools/mediainfo-build"
MI_INSTALL="$WORK/tools/mediainfo-install"
echo "Preparing fully pinned MediaInfo $MEDIAINFO_TAG source set..."
checkout_exact "$MEDIAINFO_REPO" "$MEDIAINFO_COMMIT" "$MI_SRC" "MediaInfo CLI"
checkout_exact "$MEDIAINFOLIB_REPO" "$MEDIAINFOLIB_COMMIT" "$MILIB_SRC" "MediaInfoLib"
checkout_exact "$ZENLIB_REPO" "$ZENLIB_COMMIT" "$ZEN_SRC" "ZenLib"
checkout_exact "$ZLIB_REPO" "$ZLIB_COMMIT" "$ZLIB_SRC" "zlib"

cmake -G Ninja \
  -D CMAKE_PREFIX_PATH="$MI_INSTALL" \
  -D CMAKE_INSTALL_PREFIX="$MI_INSTALL" \
  -D CMAKE_BUILD_TYPE=Release \
  -D BUILD_ZENLIB=ON \
  -D BUILD_ZLIB=ON \
  -D ZLIB_BUILD_SHARED=OFF \
  -D ZLIB_BUILD_TESTING=OFF \
  -S "$MI_SRC/Project/CMake/CLI" \
  -B "$MI_BUILD"
cmake --build "$MI_BUILD" --parallel
cmake --install "$MI_BUILD"
BUNDLED_MEDIAINFO="$MI_INSTALL/bin/mediainfo"
[[ -x "$BUNDLED_MEDIAINFO" ]] || { echo "MediaInfo build did not produce a CLI binary" >&2; exit 1; }
"$BUNDLED_MEDIAINFO" --Version | head -n 4

DEBROOT="$WORK/deb-root"
mkdir -p \
  "$DEBROOT/DEBIAN" \
  "$DEBROOT/usr/bin" \
  "$DEBROOT/usr/lib/mattrip/app" \
  "$DEBROOT/usr/lib/mattrip/ffmpeg-bin" \
  "$DEBROOT/usr/lib/mattrip/mediainfo-bin" \
  "$DEBROOT/usr/share/applications" \
  "$DEBROOT/usr/share/doc/mattrip"

install -m 0755 "$WORK/bin/mattrip-bin" "$DEBROOT/usr/lib/mattrip/app/mattrip-bin"
install -m 0755 "$WORK/bin/mattrip-cli-bin" "$DEBROOT/usr/lib/mattrip/app/mattrip-cli-bin"
install -m 0755 "$BUNDLED_FFMPEG" "$DEBROOT/usr/lib/mattrip/ffmpeg-bin/ffmpeg"
install -m 0755 "$BUNDLED_FFPROBE" "$DEBROOT/usr/lib/mattrip/ffmpeg-bin/ffprobe"
install -m 0755 "$BUNDLED_MEDIAINFO" "$DEBROOT/usr/lib/mattrip/mediainfo-bin/mediainfo"
cp -a "$LIBDVDCSS_PREFIX"/lib/libdvdcss.so* "$DEBROOT/usr/lib/mattrip/ffmpeg-bin/"

# These launchers modify PATH only for the MattRip child process. They do not
# write to /etc/environment, shell profiles, alternatives, or any system PATH
# configuration. This makes the .deb self-contained while leaving any existing
# /usr/bin/ffmpeg, /usr/bin/ffprobe, and /usr/bin/mediainfo completely untouched.
cat > "$DEBROOT/usr/bin/mattrip" <<'LAUNCHER'
#!/bin/sh
set -eu
FFDIR=/usr/lib/mattrip/ffmpeg-bin
MIDIR=/usr/lib/mattrip/mediainfo-bin
PATH="$FFDIR:$MIDIR:$PATH"
LD_LIBRARY_PATH="$FFDIR${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
export PATH LD_LIBRARY_PATH
exec /usr/lib/mattrip/app/mattrip-bin "$@"
LAUNCHER
chmod 0755 "$DEBROOT/usr/bin/mattrip"

cat > "$DEBROOT/usr/bin/mattrip-cli" <<'LAUNCHER'
#!/bin/sh
set -eu
FFDIR=/usr/lib/mattrip/ffmpeg-bin
MIDIR=/usr/lib/mattrip/mediainfo-bin
PATH="$FFDIR:$MIDIR:$PATH"
LD_LIBRARY_PATH="$FFDIR${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
export PATH LD_LIBRARY_PATH
exec /usr/lib/mattrip/app/mattrip-cli-bin "$@"
LAUNCHER
chmod 0755 "$DEBROOT/usr/bin/mattrip-cli"

cat > "$DEBROOT/DEBIAN/control" <<CONTROL
Package: mattrip
Version: $DEB_VERSION
Section: video
Priority: optional
Architecture: amd64
Maintainer: MattRip project <noreply@github.com>
Depends: libc6 (>= 2.38), libstdc++6, libgcc-s1, ca-certificates, libgl1, libx11-6, libxcursor1, libxrandr2, libxinerama1, libxi6, libxkbcommon0, libwayland-client0
Homepage: https://github.com/maas3n/MattRip
Description: Self-contained lossless DVD title remuxer
 MattRip scans DVD-Video titles and remuxes the selected title to MKV without
 transcoding. This package installs the MattRip desktop GUI and CLI together
 with private FFmpeg, FFprobe and MediaInfo binaries under /usr/lib/mattrip.
 Existing distro multimedia tools and the user's system PATH are never replaced.
CONTROL

cat > "$DEBROOT/usr/share/applications/mattrip.desktop" <<DESKTOP
[Desktop Entry]
Type=Application
Name=MattRip
Comment=Lossless DVD title remuxing to Matroska
Exec=mattrip
Icon=video-x-generic
Terminal=false
Categories=AudioVideo;AudioVideoEditing;Utility;
Keywords=DVD;MKV;FFmpeg;Remux;
DESKTOP

cat > "$DEBROOT/usr/share/doc/mattrip/README.Debian" <<TXT
MattRip for Debian/Ubuntu
=========================

This .deb is the self-contained Linux installer for MattRip $APP_VERSION.
Install this one package; FFmpeg, FFprobe and MediaInfo are already included.

Commands installed by this package:
  /usr/bin/mattrip
  /usr/bin/mattrip-cli

Private bundled tools used only by MattRip:
  /usr/lib/mattrip/ffmpeg-bin/ffmpeg
  /usr/lib/mattrip/ffmpeg-bin/ffprobe
  /usr/lib/mattrip/mediainfo-bin/mediainfo

MattRip DOES NOT install or replace:
  /usr/bin/ffmpeg
  /usr/bin/ffprobe
  /usr/bin/mediainfo

It also does not modify /etc/environment, shell startup files, alternatives, or
any other system PATH configuration. The launcher prepends the private tool
directories only to the MattRip process, so an existing system FFmpeg,
FFprobe, or MediaInfo remains exactly as it was before MattRip was installed.

Use "mattrip-cli tools" to see the private paths MattRip resolves.
TXT

cat > "$DEBROOT/usr/share/doc/mattrip/THIRD-PARTY-NOTICES" <<TXT
Third-party software bundled with MattRip $APP_VERSION
=====================================================

FFmpeg / FFprobe
----------------
Build provider: BtbN/FFmpeg-Builds
Build tag: $FFMPEG_TAG
Asset: $FFMPEG_ASSET
SHA-256: $FFMPEG_SHA256
FFmpeg source revision represented by the build: 08cd8df29d
Build/source information: https://github.com/BtbN/FFmpeg-Builds
FFmpeg project source: https://github.com/FFmpeg/FFmpeg

The bundled build is GPL-enabled because FFmpeg's dvdvideo demuxer requires
libdvdnav and libdvdread with GPL support. See FFmpeg and the build provider for
the applicable copyright notices, license texts, build configuration and source.

libdvdcss
---------
Version: $LIBDVDCSS_VERSION
Source: https://download.videolan.org/libdvdcss/$LIBDVDCSS_VERSION/libdvdcss-$LIBDVDCSS_VERSION.tar.xz
Source SHA-256: 7ea556c846b7bfc32d47b41cae56d1863a6b6d5f706bb162778d6f298490977c
License: GPL-2.0-or-later (see libdvdcss-COPYING in this directory).

MattRip builds libdvdcss from the pinned upstream source and keeps the resulting
shared library private to MattRip. libdvdread loads it dynamically for
CSS-protected DVD access.

MediaInfo
---------
Version/tag: $MEDIAINFO_TAG
MediaInfo CLI commit: $MEDIAINFO_COMMIT
MediaInfoLib commit: $MEDIAINFOLIB_COMMIT
ZenLib commit: $ZENLIB_COMMIT
MediaArea zlib commit: $ZLIB_COMMIT
Source: https://github.com/MediaArea/MediaInfo
License: BSD-2-Clause (see MediaInfo-LICENSE in this directory).

MattRip keeps these programs private under /usr/lib/mattrip and does not claim
them as part of MattRip itself.
TXT
install -m 0644 "$MI_SRC/LICENSE" "$DEBROOT/usr/share/doc/mattrip/MediaInfo-LICENSE"
install -m 0644 "$LIBDVDCSS_PREFIX/share/mattrip/libdvdcss/COPYING" "$DEBROOT/usr/share/doc/mattrip/libdvdcss-COPYING"
install -m 0644 "$LIBDVDCSS_PREFIX/share/mattrip/libdvdcss/BUILD-INFO.txt" "$DEBROOT/usr/share/doc/mattrip/libdvdcss-BUILD-INFO.txt"
install -m 0644 "$LIBDVDCSS_PREFIX/share/mattrip/libdvdcss/libdvdcss-$LIBDVDCSS_VERSION-source.tar.xz" "$DEBROOT/usr/share/doc/mattrip/libdvdcss-$LIBDVDCSS_VERSION-source.tar.xz"

# Preserve any FFmpeg license/readme text distributed in the pinned build.
FF_LICENSE="$(find "$FF_EXTRACT" -type f \( -iname 'license*' -o -iname 'copying*' \) | head -n1 || true)"
if [[ -n "$FF_LICENSE" ]]; then install -m 0644 "$FF_LICENSE" "$DEBROOT/usr/share/doc/mattrip/FFmpeg-LICENSE"; fi

# Use an installer-style release filename while retaining a Debian-compliant
# package name/version in DEBIAN/control.
dpkg-deb --build --root-owner-group "$DEBROOT" "$DIST/MattRip-$APP_VERSION-Linux-amd64.deb" >/dev/null

# Source snapshot from the exact MattRip commit being built, plus the module
# metadata resolved by CI so the archive is immediately buildable.
SOURCE="$WORK/MattRip-$APP_VERSION-Source"
mkdir -p "$SOURCE"
git -C "$ROOT" archive HEAD | tar -x -C "$SOURCE"
if [[ -f "$SRC/go.mod" ]]; then cp "$SRC/go.mod" "$SOURCE/src/go.mod"; fi
if [[ -f "$SRC/go.sum" ]]; then cp "$SRC/go.sum" "$SOURCE/src/go.sum"; fi
tar -C "$WORK" -czf "$DIST/MattRip-$APP_VERSION-Source.tar.gz" "$(basename "$SOURCE")"

(
  cd "$DIST"
  sha256sum ./*.deb ./*.tar.gz > SHA256SUMS.txt
)

echo "Linux release artifacts:"
ls -lh "$DIST"
