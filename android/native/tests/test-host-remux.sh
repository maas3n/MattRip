#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
UDF_SOURCE="${1:?Pass pinned libudfread source directory}"
TEST_WORK="${2:?Pass output diagnostic directory}"
mkdir -p "$TEST_WORK"
TEST_WORK="$(cd "$TEST_WORK" && pwd)"
JDK="${JAVA_HOME:-/usr/lib/jvm/java-17-openjdk-amd64}"
css_flags=()
css_libs=()
if [[ -n "${CSS_LIBDVDCSS_PREFIX:-}" ]]; then
  css_flags=(-DMATTMUX_DVDCSS=1 "-I$CSS_LIBDVDCSS_PREFIX/include")
  css_libs=("-L$CSS_LIBDVDCSS_PREFIX/lib" "-Wl,-rpath,$CSS_LIBDVDCSS_PREFIX/lib" -ldvdcss)
fi
cc -shared -fPIC -std=c11 -D_POSIX_C_SOURCE=200809L -DHAVE_UNISTD_H=1 -DHAVE_FCNTL_H=1 \
  "${css_flags[@]}" \
  -Wall -Wextra -Werror=implicit-function-declaration \
  -I"${UDF_SOURCE}/src" -I"${ROOT}/android/native" -I"${JDK}/include" -I"${JDK}/include/linux" \
  "${UDF_SOURCE}/src/udfread.c" "${UDF_SOURCE}/src/ecma167.c" "${UDF_SOURCE}/src/default_blockinput.c" \
  "${ROOT}/android/native/mattmux_jni.c" "${ROOT}/android/native/udf_source.c" \
  "${ROOT}/android/native/tests/host_fd.c" \
  -lavformat -lavcodec -lavutil "${css_libs[@]}" -o "${TEST_WORK}/libmattmux_host_test.so"
ffmpeg -v error -f lavfi -i 'testsrc2=size=720x576:rate=25' \
  -f lavfi -i 'sine=frequency=440:sample_rate=48000' -t 2 -target pal-dvd \
  -y "${TEST_WORK}/input.vob"
python3 "${ROOT}/android/native/tests/make_remux_iso.py" "${TEST_WORK}"
javac -d "${TEST_WORK}/classes" "${ROOT}/android/native/tests/java/io/github/maas3n/mattmux/AndroidNativeRemuxEngine.java"
java -cp "${TEST_WORK}/classes" io.github.maas3n.mattmux.AndroidNativeRemuxEngine \
  "${TEST_WORK}/libmattmux_host_test.so" "${TEST_WORK}"
python3 "${ROOT}/android/native/tests/remux_fingerprint.py" "${TEST_WORK}/folder.mkv" "${TEST_WORK}/iso.mkv" "${TEST_WORK}/input.vob"
ffprobe -v error -show_entries stream=codec_type -of json "${TEST_WORK}/selected.mkv" | python3 -c 'import json,sys; streams=json.load(sys.stdin)["streams"]; assert len(streams)==1 and streams[0]["codec_type"]=="video", streams'

# Exercise the production JNI with omitted PES timestamps and reordered frames.
# The original fixture remains a separate baseline; do not replace its coverage.
SPARSE_WORK="${TEST_WORK}/sparse"
mkdir -p "$SPARSE_WORK"
ffmpeg -v error -f lavfi -i 'testsrc2=size=720x576:rate=25' \
  -f lavfi -i 'sine=frequency=440:sample_rate=48000' -t 2 -target pal-dvd -bf 2 \
  -y "${SPARSE_WORK}/reference.vob"
python3 "${ROOT}/android/native/tests/sparse_timestamps.py" make \
  "${SPARSE_WORK}/reference.vob" "${SPARSE_WORK}/input.vob"
python3 "${ROOT}/android/native/tests/make_remux_iso.py" "$SPARSE_WORK"
java -cp "${TEST_WORK}/classes" io.github.maas3n.mattmux.AndroidNativeRemuxEngine \
  "${TEST_WORK}/libmattmux_host_test.so" "$SPARSE_WORK"
python3 "${ROOT}/android/native/tests/remux_fingerprint.py" \
  "${SPARSE_WORK}/folder.mkv" "${SPARSE_WORK}/iso.mkv" "${SPARSE_WORK}/input.vob"
python3 "${ROOT}/android/native/tests/sparse_timestamps.py" verify \
  "${SPARSE_WORK}/reference.vob" "${SPARSE_WORK}/folder.mkv"

# Exercise the Advanced Merger through its production JNI surface.
bash "${ROOT}/android/native/tests/test-advanced-merger.sh" "${TEST_WORK}"
