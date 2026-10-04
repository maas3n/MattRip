#!/usr/bin/env bash
set -euo pipefail

artifact="${1:?Usage: verify-android-signing-cert.sh ARTIFACT EXPECTED_SHA256 [APKSIGNER]}"
expected_raw="${2:?Usage: verify-android-signing-cert.sh ARTIFACT EXPECTED_SHA256 [APKSIGNER]}"
apksigner="${3:-}"

normalize_sha256() {
  printf '%s' "$1" | tr -d '[:space:]:' | tr '[:upper:]' '[:lower:]'
}

expected="$(normalize_sha256 "$expected_raw")"
if [[ ! "$expected" =~ ^[0-9a-f]{64}$ ]]; then
  echo "Expected certificate SHA-256 must contain exactly 64 hex characters." >&2
  exit 1
fi

case "$artifact" in
  *.apk)
    test -n "$apksigner" || {
      echo "APK verification requires the apksigner path as the third argument." >&2
      exit 1
    }
    actual_raw="$("$apksigner" verify --print-certs "$artifact"       | sed -n 's/^Signer #1 certificate SHA-256 digest: //p'       | head -n1)"
    ;;
  *.aab)
    actual_raw="$(keytool -printcert -jarfile "$artifact"       | sed -n 's/^[[:space:]]*SHA256:[[:space:]]*//p'       | head -n1)"
    ;;
  *)
    echo "Unsupported Android artifact type: $artifact" >&2
    exit 1
    ;;
esac

actual="$(normalize_sha256 "$actual_raw")"
if [[ ! "$actual" =~ ^[0-9a-f]{64}$ ]]; then
  echo "Could not read a SHA-256 signing certificate fingerprint from $artifact" >&2
  exit 1
fi

if [[ "$actual" != "$expected" ]]; then
  echo "Android artifact signing certificate mismatch: $artifact" >&2
  echo "Expected: $expected" >&2
  echo "Actual:   $actual" >&2
  exit 1
fi

echo "Android artifact certificate verified: $artifact"
echo "SHA-256: $actual"
