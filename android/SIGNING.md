# MattRip Android release signing

MattRip uses **two distinct Android private keys**:

1. **App-signing key** — signs the public GitHub APK. This is the long-lived installed-app identity.
2. **Play upload key** — signs AAB files uploaded to Google Play. Google Play verifies this key, then signs delivered APKs with the app-signing key configured in Play App Signing.

The keys must remain distinct. Do not use a debug key for either role.

## Why the split matters

Android updates require a compatible signing identity. MattRip publishes a direct GitHub APK as well as a Play AAB, so Play App Signing must use the same MattRip app-signing identity as the GitHub APK. The separate upload key is only for authenticating future Play uploads.

For a new Play listing, do **not** let the GitHub APK and Play-distributed app end up with unrelated app-signing certificates. In Play Console, configure MattRip's own app-signing key when enrolling in Play App Signing, then register MattRip's separate upload certificate for AAB uploads.

Application ID:

```text
io.github.maas3n.mattrip
```

## Generate the two keys

Generate the keys on a trusted local machine, outside the Git repository:

```bash
bash scripts/create-android-signing-keys.sh ../mattrip-android-signing
```

The script refuses to create private signing material inside the repository. It creates:

```text
mattrip-app-signing.jks
mattrip-app-signing.jks.base64
mattrip-app-signing-cert.pem
mattrip-upload.jks
mattrip-upload.jks.base64
mattrip-upload-cert.pem
```

The two `.jks` files and their Base64 forms are private. The two PEM certificates and their SHA-256 fingerprints are public.

Back up both private keystores and their passwords securely. The **app-signing key is especially critical** because it signs direct-distribution APK updates. Keep at least one offline backup.

## GitHub configuration

Use protected GitHub Environments so the two private keys are scoped separately.

### Environment: `android-release`

Add these environment secrets:

- `ANDROID_APP_SIGNING_KEYSTORE_BASE64` — contents of `mattrip-app-signing.jks.base64`
- `ANDROID_APP_SIGNING_STORE_PASSWORD`
- `ANDROID_APP_SIGNING_KEY_ALIAS` — default generator value: `mattrip-app-signing`
- `ANDROID_APP_SIGNING_KEY_PASSWORD`

Recommended: require deployment approval for the `android-release` environment before public-release jobs can use the app-signing private key.

### Environment: `android-play`

Add these environment secrets:

- `ANDROID_UPLOAD_KEYSTORE_BASE64` — contents of `mattrip-upload.jks.base64`
- `ANDROID_UPLOAD_STORE_PASSWORD`
- `ANDROID_UPLOAD_KEY_ALIAS` — default generator value: `mattrip-upload`
- `ANDROID_UPLOAD_KEY_PASSWORD`

### Repository Actions variables

Add these non-secret repository variables:

- `ANDROID_APP_SIGNING_CERT_SHA256`
- `ANDROID_UPLOAD_CERT_SHA256`

Use the 64-character hexadecimal SHA-256 values printed by `create-android-signing-keys.sh`. Colons and case are normalized by CI, but plain lowercase hex is preferred.

## Verify GitHub configuration

Run **Android signing self-test** from GitHub Actions.

It verifies:

- both Base64 keystores decode;
- the configured aliases exist;
- keystore passwords work;
- private-key passwords work by signing a temporary JAR;
- each public SHA-256 fingerprint matches the actual certificate;
- the app-signing and upload certificates are different.

This workflow is intentionally lightweight and does not rebuild FFmpeg.

## Play Console setup

For the first MattRip Play setup:

1. Create/select the app with application ID `io.github.maas3n.mattrip`.
2. Enroll in Play App Signing.
3. When Play asks how the app-signing key should be established, choose the flow that lets you provide MattRip's existing **app-signing key**. Follow the current Play Console instructions for securely transferring that key.
4. Verify the Play Console **app signing certificate SHA-256** matches `ANDROID_APP_SIGNING_CERT_SHA256`.
5. Register `mattrip-upload-cert.pem` as the **upload certificate**.
6. Verify the Play Console **upload certificate SHA-256** matches `ANDROID_UPLOAD_CERT_SHA256`.
7. Build Play bundles only through **Android / ChromeOS Play bundle**, which signs the AAB with the upload key.

Google Play may change the secure key-import flow over time; follow the current Play Console/Android Developers procedure instead of committing export tooling or private-key material to this repository.

## Release behavior

The unified GitHub release workflow:

- uses only the `android-release` app-signing key for `MattRip-<version>-Android.apk`;
- refuses to build the release APK without complete signing credentials;
- verifies the final APK certificate against `ANDROID_APP_SIGNING_CERT_SHA256`;
- rejects Android debug certificates.

The Play workflow:

- uses only the `android-play` upload key;
- refuses to build a production AAB without complete signing credentials;
- verifies the final AAB certificate against `ANDROID_UPLOAD_CERT_SHA256`.

Gradle release builds use generic `MATTRIP_SIGNING_*` properties. Production workflows always pass `-PMATTRIP_REQUIRE_SIGNING=true`, making unsigned production builds a hard failure.

## Key loss or compromise

Do not replace keys silently.

- If the **Play upload key** is lost or compromised, use Play Console's upload-key reset process and intentionally update the `android-play` secrets plus `ANDROID_UPLOAD_CERT_SHA256`.
- If the **app-signing key** is affected, treat it as a release-security incident. Direct GitHub APK update continuity depends on that identity, and Play signing-key changes must follow Google's supported key-upgrade process.
