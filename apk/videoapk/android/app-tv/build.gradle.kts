plugins { alias(libs.plugins.android.application); alias(libs.plugins.kotlin.compose) }
android {
    namespace = "com.xiaoqi.video.tv"
    compileSdk = 36
    defaultConfig { minSdk = 23; testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner" }
    compileOptions { isCoreLibraryDesugaringEnabled = true; sourceCompatibility = JavaVersion.VERSION_17; targetCompatibility = JavaVersion.VERSION_17 }

    defaultConfig { applicationId = "com.xiaoqi.video.tv"; targetSdk = 36; versionCode = 116; versionName = "1.0.16" }
    buildFeatures { compose = true; buildConfig = true }
    signingConfigs {
        create("release") {
            val path = providers.environmentVariable("XIAOQI_SIGN_STORE").orNull
            if (path != null) {
                storeFile = file(path)
                storePassword = providers.environmentVariable("XIAOQI_SIGN_STORE_PASSWORD").orNull
                keyAlias = providers.environmentVariable("XIAOQI_SIGN_ALIAS").orNull
                keyPassword = providers.environmentVariable("XIAOQI_SIGN_KEY_PASSWORD").orNull
            }
        }
    }
    buildTypes {
        release { isMinifyEnabled = false; signingConfig = signingConfigs.getByName("release") }
    }
    packaging { resources.excludes += setOf("META-INF/INDEX.LIST", "META-INF/*.SF", "META-INF/*.RSA", "META-INF/*.DSA", "META-INF/LICENSE*", "META-INF/NOTICE*") }
}
dependencies {
    coreLibraryDesugaring(libs.desugar.jdk)
    implementation(project(":feature:catalog"))
    implementation(project(":core:cast"))
    implementation(libs.activity.compose)
    implementation(platform(libs.compose.bom))
    implementation(libs.tv.material)
    androidTestImplementation(libs.androidx.test.runner)
    androidTestImplementation(platform(libs.compose.bom))
    androidTestImplementation(libs.androidx.test.junit)
    androidTestImplementation(libs.compose.test)
    debugImplementation(libs.compose.test.manifest)
}

