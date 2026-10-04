# Google Play Console setup

## App identity

- App name: `MattRip`
- Application ID: `io.github.maas3n.mattrip`
- Primary device target: Chromebook / ChromeOS Android runtime
- Current target SDK: Android 16 / API 36

The application ID becomes permanent after the first Play publication. Confirm it before creating the production listing.

## Monetization

Create a one-time product:

- Product ID: `mattrip_pro`
- Type: one-time, non-consumable
- Suggested initial price: EUR 5.99, with Play-managed local pricing as desired

The app uses Google Play Billing and restores the entitlement with `queryPurchasesAsync()`.

Before production, add server-side purchase-token verification using the Google Play Developer API. The current client-side entitlement flow is suitable for development/internal testing but is not strong anti-tamper protection for a paid open-source client.

## Testing order

1. Create the app in Play Console using `io.github.maas3n.mattrip`.
2. Upload an Android App Bundle to Internal testing.
3. Create and activate `mattrip_pro`.
4. Add license tester accounts.
5. Verify product query, purchase, pending purchase, cancellation, restore on reinstall, and acknowledgement.
6. Enable `BuildConfig.ENABLE_BILLING_PURCHASES` only after the native remux engine passes Chromebook tests.
7. Move through closed testing and production when Play account requirements are satisfied.

## Chromebook checks

Use Play Console Device catalog to confirm Chromebook availability. Test at minimum one x86_64 Chromebook and one arm64 Chromebook if the native remux engine ships both ABIs.


## Signing identity

Before the first Play publication, configure the two-key model in [SIGNING.md](SIGNING.md).

- **MattRip app-signing key**: the long-lived certificate used for direct GitHub APKs and the app-signing identity that Play App Signing must use.
- **MattRip upload key**: a separate key used only to sign AAB uploads to Play.

For a new Play app, use the current Play Console flow that lets you provide MattRip's existing app-signing key instead of accepting an unrelated Play-generated identity. Then register the separate MattRip upload certificate.

Verify in Play Console that:

- the app signing certificate SHA-256 equals the repository Actions variable `ANDROID_APP_SIGNING_CERT_SHA256`;
- the upload certificate SHA-256 equals `ANDROID_UPLOAD_CERT_SHA256`.

If those fingerprints do not match, do not publish. A Play build signed under a different app-signing identity would not be update-compatible with direct MattRip APKs.
