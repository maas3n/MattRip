#!/usr/bin/env bash
set -euo pipefail

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

keystore="$tmp/gradle-signing.jks"
store_password='gradle-store-password-123'
key_password='gradle-key-password-456'
alias='mattrip-gradle-test'

keytool -genkeypair   -keystore "$keystore"   -storetype JKS   -storepass "$store_password"   -alias "$alias"   -keypass "$key_password"   -keyalg RSA -keysize 2048 -sigalg SHA256withRSA   -validity 3650   -dname "CN=MattRip Gradle Signing Test, O=MattRip" >/dev/null

gradle -p android   -PMATTRIP_SIGNING_STORE_FILE="$keystore"   -PMATTRIP_SIGNING_STORE_PASSWORD="$store_password"   -PMATTRIP_SIGNING_KEY_ALIAS="$alias"   -PMATTRIP_SIGNING_KEY_PASSWORD="$key_password"   -PMATTRIP_REQUIRE_SIGNING=true   :app:validateSigningRelease

if gradle -p android -PMATTRIP_REQUIRE_SIGNING=true :app:tasks >/dev/null 2>&1; then
  echo "Gradle accepted MATTRIP_REQUIRE_SIGNING=true without signing credentials." >&2
  exit 1
fi

if gradle -p android     -PMATTRIP_SIGNING_STORE_FILE="$keystore"     -PMATTRIP_REQUIRE_SIGNING=true     :app:tasks >/dev/null 2>&1; then
  echo "Gradle accepted an incomplete signing configuration." >&2
  exit 1
fi

echo "Android Gradle release-signing regression passed."
