#!/usr/bin/env bash
set -euo pipefail

out="${1:-}"
if [[ -z "$out" ]]; then
  echo "Usage: $0 /path/outside/the/repository" >&2
  exit 2
fi

command -v keytool >/dev/null || {
  echo "keytool is required (JDK 17 or newer)." >&2
  exit 1
}

repo_root="$(git rev-parse --show-toplevel 2>/dev/null || true)"
mkdir -p "$out"
out="$(cd "$out" && pwd -P)"
if [[ -n "$repo_root" ]]; then
  repo_root="$(cd "$repo_root" && pwd -P)"
  case "$out/" in
    "$repo_root/"*)
      echo "Refusing to create private signing keys inside the Git repository: $out" >&2
      exit 1
      ;;
  esac
fi

read -rsp "App-signing keystore password: " app_store_password
echo
read -rsp "App-signing private-key password: " app_key_password
echo
read -rsp "Play upload keystore password: " upload_store_password
echo
read -rsp "Play upload private-key password: " upload_key_password
echo

for value in "$app_store_password" "$app_key_password" "$upload_store_password" "$upload_key_password"; do
  [[ ${#value} -ge 12 ]] || {
    echo "Use passwords of at least 12 characters." >&2
    exit 1
  }
done

app_keystore="$out/mattrip-app-signing.jks"
upload_keystore="$out/mattrip-upload.jks"
app_alias="mattrip-app-signing"
upload_alias="mattrip-upload"

for file in "$app_keystore" "$upload_keystore"; do
  [[ ! -e "$file" ]] || {
    echo "Refusing to overwrite existing key material: $file" >&2
    exit 1
  }
done

keytool -genkeypair   -keystore "$app_keystore"   -storetype JKS   -storepass "$app_store_password"   -alias "$app_alias"   -keypass "$app_key_password"   -keyalg RSA -keysize 4096 -sigalg SHA256withRSA   -validity 10000   -dname "CN=MattRip App Signing, OU=MattRip, O=maas3n"

keytool -genkeypair   -keystore "$upload_keystore"   -storetype JKS   -storepass "$upload_store_password"   -alias "$upload_alias"   -keypass "$upload_key_password"   -keyalg RSA -keysize 4096 -sigalg SHA256withRSA   -validity 10000   -dname "CN=MattRip Play Upload, OU=MattRip, O=maas3n"

keytool -exportcert -rfc   -keystore "$app_keystore"   -storepass "$app_store_password"   -alias "$app_alias"   -file "$out/mattrip-app-signing-cert.pem"

keytool -exportcert -rfc   -keystore "$upload_keystore"   -storepass "$upload_store_password"   -alias "$upload_alias"   -file "$out/mattrip-upload-cert.pem"

fingerprint() {
  local keystore="$1" password="$2" alias="$3"
  local tmp
  tmp="$(mktemp)"
  keytool -exportcert -keystore "$keystore" -storepass "$password" -alias "$alias" -file "$tmp" >/dev/null
  sha256sum "$tmp" | awk '{print $1}'
  rm -f "$tmp"
}

app_sha="$(fingerprint "$app_keystore" "$app_store_password" "$app_alias")"
upload_sha="$(fingerprint "$upload_keystore" "$upload_store_password" "$upload_alias")"

base64 -w0 "$app_keystore" > "$out/mattrip-app-signing.jks.base64"
base64 -w0 "$upload_keystore" > "$out/mattrip-upload.jks.base64"
chmod 600 "$app_keystore" "$upload_keystore" "$out/"*.base64

cat <<EOF

Created two distinct MattRip Android identities outside the repository.

App-signing alias: $app_alias
App-signing SHA-256: $app_sha
App-signing base64 file: $out/mattrip-app-signing.jks.base64
Public certificate: $out/mattrip-app-signing-cert.pem

Play upload alias: $upload_alias
Play upload SHA-256: $upload_sha
Play upload base64 file: $out/mattrip-upload.jks.base64
Public certificate: $out/mattrip-upload-cert.pem

Keep both .jks files, both passwords, and the base64 files private.
Back up the app-signing keystore offline before publishing any Android APK.
EOF
