#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
PREFIX="${1:?Pass the host libdvdcss 1.6.0 installation prefix}"
WORK="${2:?Pass the diagnostic directory}"
mkdir -p "$WORK"
extra=()
if [[ -n "${CSS_STREAM_HEADER:-}" ]]; then
    extra+=("-DCSS_STREAM_HEADER=\"$CSS_STREAM_HEADER\"")
fi
cc -std=c11 -Wall -Wextra -Werror -g -fsanitize=address,undefined \
    -fno-omit-frame-pointer "${extra[@]}" -I"$PREFIX/include" \
    "$ROOT/android/native/tests/dvdcss_stream_test.c" \
    -L"$PREFIX/lib" -Wl,-rpath,"$PREFIX/lib" -ldvdcss -o "$WORK/dvdcss-stream-test"
DVDCSS_CACHE=off "$WORK/dvdcss-stream-test"
