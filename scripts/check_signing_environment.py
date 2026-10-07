"""Report missing signing setting names together; never print their values."""
import os
import sys

SETTINGS = {
    'KEYSTORE_B64': 'ANDROID_APP_SIGNING_KEYSTORE_BASE64',
    'STORE_PASSWORD': 'ANDROID_APP_SIGNING_STORE_PASSWORD',
    'KEY_ALIAS': 'ANDROID_APP_SIGNING_KEY_ALIAS',
    'KEY_PASSWORD': 'ANDROID_APP_SIGNING_KEY_PASSWORD',
    'CERT_SHA256': 'ANDROID_APP_SIGNING_CERT_SHA256 (Actions variable)',
}


def missing_settings(env):
    return [name for key, name in SETTINGS.items() if not env.get(key, '').strip()]


if __name__ == '__main__':
    missing = missing_settings(os.environ)
    if missing:
        print('Android release signing is not configured. Missing: ' + ', '.join(missing), file=sys.stderr)
        print('Configure the existing app-signing identity in android-release; see android/SIGNING.md. No unsigned fallback is allowed.', file=sys.stderr)
        sys.exit(1)
