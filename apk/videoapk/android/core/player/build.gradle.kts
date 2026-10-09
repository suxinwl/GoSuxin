plugins { alias(libs.plugins.android.library) }
android {
    namespace = "com.xiaoqi.video.core.player"
    compileSdk = 36
    defaultConfig { minSdk = 23; testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner" }
    compileOptions { isCoreLibraryDesugaringEnabled = true; sourceCompatibility = JavaVersion.VERSION_17; targetCompatibility = JavaVersion.VERSION_17 }
}
dependencies {
    coreLibraryDesugaring(libs.desugar.jdk)
    api(project(":core:data"))
    api(libs.media3.exoplayer)
    implementation(libs.media3.hls)
    api(libs.media3.ui)
    implementation(libs.media3.session)
    implementation(libs.media3.datasource.okhttp)
    implementation(libs.lifecycle.service)
    testImplementation(libs.junit)
}

