plugins {
    id("com.android.application")
}

val mattRipVersionName = providers.gradleProperty("MATTRIP_VERSION_NAME")
    .orElse("0.0.0-dev")
    .get()
val mattRipVersionCode = providers.gradleProperty("MATTRIP_VERSION_CODE")
    .orElse("1")
    .get()
    .toInt()

val signingStoreFile = providers.gradleProperty("MATTRIP_SIGNING_STORE_FILE").orNull
val signingStorePassword = providers.gradleProperty("MATTRIP_SIGNING_STORE_PASSWORD").orNull
val signingKeyAlias = providers.gradleProperty("MATTRIP_SIGNING_KEY_ALIAS").orNull
val signingKeyPassword = providers.gradleProperty("MATTRIP_SIGNING_KEY_PASSWORD").orNull
val signingValues = listOf(
    signingStoreFile,
    signingStorePassword,
    signingKeyAlias,
    signingKeyPassword,
)
val hasAnyReleaseSigning = signingValues.any { !it.isNullOrBlank() }
val hasReleaseSigning = signingValues.all { !it.isNullOrBlank() }
val requireReleaseSigning = providers.gradleProperty("MATTRIP_REQUIRE_SIGNING")
    .orElse("false")
    .get()
    .toBooleanStrictOrNull() ?: error("MATTRIP_REQUIRE_SIGNING must be true or false")

if (hasAnyReleaseSigning && !hasReleaseSigning) {
    error("Incomplete MattRip release signing configuration")
}
if (requireReleaseSigning && !hasReleaseSigning) {
    error("MattRip release signing is required but no complete signing configuration was supplied")
}

android {
    namespace = "io.github.maas3n.mattmux"
    compileSdk = 36

    defaultConfig {
        applicationId = "io.github.maas3n.mattrip"
        minSdk = 26
        targetSdk = 36
        versionCode = mattRipVersionCode
        versionName = mattRipVersionName

        ndk {
            abiFilters += listOf("arm64-v8a", "x86_64")
        }

        // Purchases stay disabled until production device validation, signing,
        // and purchase-verification readiness are complete.
        buildConfigField("boolean", "ENABLE_BILLING_PURCHASES", "false")
    }

    signingConfigs {
        if (hasReleaseSigning) {
            create("releaseSigning") {
                storeFile = file(requireNotNull(signingStoreFile))
                storePassword = requireNotNull(signingStorePassword)
                keyAlias = requireNotNull(signingKeyAlias)
                keyPassword = requireNotNull(signingKeyPassword)
            }
        }
    }

    buildTypes {
        getByName("release") {
            signingConfigs.findByName("releaseSigning")?.let {
                signingConfig = it
            }
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    buildFeatures {
        buildConfig = true
    }

    packaging {
        jniLibs {
            // AGP 9 + NDK r30 keeps native libraries Play/16 KB-page compatible.
            useLegacyPackaging = false
        }
        resources {
            excludes += "/META-INF/{AL2.0,LGPL2.1}"
        }
    }
}

dependencies {
    implementation("com.android.billingclient:billing:9.1.0")
    testImplementation("junit:junit:4.13.2")
}
