#!/usr/bin/env bash
set -euo pipefail

LIBDVDCSS_VERSION="${LIBDVDCSS_VERSION:-1.6.0}"
LIBDVDCSS_SHA256="${LIBDVDCSS_SHA256:-7ea556c846b7bfc32d47b41cae56d1863a6b6d5f706bb162778d6f298490977c}"
LIBDVDCSS_URL="${LIBDVDCSS_URL:-https://download.videolan.org/libdvdcss/${LIBDVDCSS_VERSION}/libdvdcss-${LIBDVDCSS_VERSION}.tar.xz}"
PREFIX="${LIBDVDCSS_PREFIX:?Set LIBDVDCSS_PREFIX to the installation directory.}"
WORK="${LIBDVDCSS_WORK:-$(pwd)/.libdvdcss-work}"

for cmd in curl sha256sum tar meson ninja; do
    command -v "$cmd" >/dev/null 2>&1 || { echo "Missing libdvdcss build tool: $cmd" >&2; exit 1; }
done

rm -rf "$WORK" "$PREFIX"
mkdir -p "$WORK" "$PREFIX"
archive="$WORK/libdvdcss-${LIBDVDCSS_VERSION}.tar.xz"
source_dir="$WORK/libdvdcss-${LIBDVDCSS_VERSION}"
build_dir="$WORK/build"

curl --fail --location --retry 3 --proto '=https' --tlsv1.2 \
    -o "$archive" "$LIBDVDCSS_URL"
printf '%s  %s\n' "$LIBDVDCSS_SHA256" "$archive" | sha256sum --check --strict

tar -xJf "$archive" -C "$WORK"
test -f "$source_dir/COPYING" || { echo "libdvdcss source archive is missing COPYING" >&2; exit 1; }

meson setup "$build_dir" "$source_dir" \
    --prefix="$PREFIX" \
    --buildtype=release \
    --default-library=shared
meson compile -C "$build_dir"
meson install -C "$build_dir"

notice_dir="$PREFIX/share/mattrip/libdvdcss"
mkdir -p "$notice_dir"
cp "$archive" "$notice_dir/libdvdcss-${LIBDVDCSS_VERSION}-source.tar.xz"
cp "$source_dir/COPYING" "$notice_dir/COPYING"
cat > "$notice_dir/BUILD-INFO.txt" <<INFO
libdvdcss version: ${LIBDVDCSS_VERSION}
Source: ${LIBDVDCSS_URL}
Source SHA-256: ${LIBDVDCSS_SHA256}
Build system: Meson shared-library release build
INFO

case "$(uname -s)" in
    Linux*)
        find "$PREFIX" -type f -name 'libdvdcss.so*' -print -quit | grep -q . || {
            echo "libdvdcss Linux shared library was not produced" >&2
            exit 1
        }
        ;;
    MINGW*|MSYS*|CYGWIN*)
        find "$PREFIX" -type f -iname 'libdvdcss*.dll' -print -quit | grep -q . || {
            echo "libdvdcss Windows DLL was not produced" >&2
            exit 1
        }
        ;;
esac

echo "libdvdcss ${LIBDVDCSS_VERSION} built and staged under $PREFIX"
