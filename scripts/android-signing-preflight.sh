#!/usr/bin/env bash
set -euo pipefail

label="${1:-Android signing key}"

: "${MATTRIP_SIGNING_KEYSTORE:?MATTRIP_SIGNING_KEYSTORE is required}"
: "${MATTRIP_SIGNING_STORE_PASSWORD:?MATTRIP_SIGNING_STORE_PASSWORD is required}"
: "${MATTRIP_SIGNING_KEY_ALIAS:?MATTRIP_SIGNING_KEY_ALIAS is required}"
: "${MATTRIP_SIGNING_KEY_PASSWORD:?MATTRIP_SIGNING_KEY_PASSWORD is required}"
: "${MATTRIP_EXPECTED_CERT_SHA256:?MATTRIP_EXPECTED_CERT_SHA256 is required}"

normalize_sha256() {
  printf '%s' "$1" | tr -d '[:space:]:' | tr '[:upper:]' '[:lower:]'
}

expected="$(normalize_sha256 "$MATTRIP_EXPECTED_CERT_SHA256")"
if [[ ! "$expected" =~ ^[0-9a-f]{64}$ ]]; then
  echo "$label: expected certificate SHA-256 must contain exactly 64 hex characters." >&2
  exit 1
fi

test -s "$MATTRIP_SIGNING_KEYSTORE" || {
  echo "$label: keystore is missing or empty: $MATTRIP_SIGNING_KEYSTORE" >&2
  exit 1
}

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

keytool -list   -keystore "$MATTRIP_SIGNING_KEYSTORE"   -storepass "$MATTRIP_SIGNING_STORE_PASSWORD"   -alias "$MATTRIP_SIGNING_KEY_ALIAS" >/dev/null

keytool -exportcert   -keystore "$MATTRIP_SIGNING_KEYSTORE"   -storepass "$MATTRIP_SIGNING_STORE_PASSWORD"   -alias "$MATTRIP_SIGNING_KEY_ALIAS"   -file "$tmp/cert.der" >/dev/null

actual="$(sha256sum "$tmp/cert.der" | awk '{print $1}')"
if [[ "$actual" != "$expected" ]]; then
  echo "$label: certificate SHA-256 mismatch." >&2
  echo "Expected: $expected" >&2
  echo "Actual:   $actual" >&2
  exit 1
fi

printf 'MattRip signing preflight\n' > "$tmp/payload.txt"
(
  cd "$tmp"
  jar --create --file probe.jar payload.txt
)

jarsigner   -keystore "$MATTRIP_SIGNING_KEYSTORE"   -storepass "$MATTRIP_SIGNING_STORE_PASSWORD"   -keypass "$MATTRIP_SIGNING_KEY_PASSWORD"   "$tmp/probe.jar"   "$MATTRIP_SIGNING_KEY_ALIAS" >/dev/null

jarsigner -verify "$tmp/probe.jar" >/dev/null

echo "$label: keystore, alias, private-key password and certificate fingerprint verified."
echo "$label SHA-256: $actual"
