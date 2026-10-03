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

val playStoreFile = providers.gradleProperty("MATTRIP_UPLOAD_STORE_FILE").orNull
val playStorePassword = providers.gradleProperty("MATTRIP_UPLOAD_STORE_PASSWORD").orNull
val playKeyAlias = providers.gradleProperty("MATTRIP_UPLOAD_KEY_ALIAS").orNull
val playKeyPassword = providers.gradleProperty("MATTRIP_UPLOAD_KEY_PASSWORD").orNull
val hasPlaySigning = listOf(
    playStoreFile,
    playStorePassword,
    playKeyAlias,
    playKeyPassword,
).all { !it.isNullOrBlank() }

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
        if (hasPlaySigning) {
            create("playUpload") {
                storeFile = file(requireNotNull(playStoreFile))
                storePassword = requireNotNull(playStorePassword)
                keyAlias = requireNotNull(playKeyAlias)
                keyPassword = requireNotNull(playKeyPassword)
            }
        }
    }

    buildTypes {
        getByName("release") {
            signingConfigs.findByName("playUpload")?.let {
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
