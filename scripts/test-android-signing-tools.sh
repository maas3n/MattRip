#!/usr/bin/env bash
set -euo pipefail

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

keystore="$tmp/test-signing.jks"
store_password='test-store-password-123'
key_password='test-key-password-456'
alias='mattrip-test'

keytool -genkeypair   -keystore "$keystore"   -storetype JKS   -storepass "$store_password"   -alias "$alias"   -keypass "$key_password"   -keyalg RSA -keysize 2048 -sigalg SHA256withRSA   -validity 3650   -dname "CN=MattRip CI Signing Test, O=MattRip"

keytool -exportcert   -keystore "$keystore"   -storepass "$store_password"   -alias "$alias"   -file "$tmp/cert.der" >/dev/null
expected="$(sha256sum "$tmp/cert.der" | awk '{print $1}')"

MATTRIP_SIGNING_KEYSTORE="$keystore" MATTRIP_SIGNING_STORE_PASSWORD="$store_password" MATTRIP_SIGNING_KEY_ALIAS="$alias" MATTRIP_SIGNING_KEY_PASSWORD="$key_password" MATTRIP_EXPECTED_CERT_SHA256="$expected"   bash scripts/android-signing-preflight.sh "CI throwaway signing key"

if MATTRIP_SIGNING_KEYSTORE="$keystore"    MATTRIP_SIGNING_STORE_PASSWORD="$store_password"    MATTRIP_SIGNING_KEY_ALIAS="$alias"    MATTRIP_SIGNING_KEY_PASSWORD='wrong-private-key-password'    MATTRIP_EXPECTED_CERT_SHA256="$expected"      bash scripts/android-signing-preflight.sh "CI wrong password"; then
  echo "Signing preflight accepted a wrong private-key password." >&2
  exit 1
fi

if MATTRIP_SIGNING_KEYSTORE="$keystore"    MATTRIP_SIGNING_STORE_PASSWORD="$store_password"    MATTRIP_SIGNING_KEY_ALIAS="$alias"    MATTRIP_SIGNING_KEY_PASSWORD="$key_password"    MATTRIP_EXPECTED_CERT_SHA256='0000000000000000000000000000000000000000000000000000000000000000'      bash scripts/android-signing-preflight.sh "CI wrong fingerprint"; then
  echo "Signing preflight accepted a wrong certificate fingerprint." >&2
  exit 1
fi

printf 'MattRip signing artifact test\n' > "$tmp/payload.txt"
(
  cd "$tmp"
  jar --create --file test.aab payload.txt
)
jarsigner   -keystore "$keystore"   -storepass "$store_password"   -keypass "$key_password"   "$tmp/test.aab" "$alias" >/dev/null

bash scripts/verify-android-signing-cert.sh "$tmp/test.aab" "$expected"

if bash scripts/verify-android-signing-cert.sh     "$tmp/test.aab"     'ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff'; then
  echo "Artifact certificate verifier accepted a wrong fingerprint." >&2
  exit 1
fi

echo "Android signing helper regressions passed."
