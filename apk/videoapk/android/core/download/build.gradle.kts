plugins { alias(libs.plugins.android.library) }
android {
    namespace = "com.xiaoqi.video.core.download"
    compileSdk = 36
    defaultConfig { minSdk = 23; testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner" }
    compileOptions { isCoreLibraryDesugaringEnabled = true; sourceCompatibility = JavaVersion.VERSION_17; targetCompatibility = JavaVersion.VERSION_17 }
}
dependencies {
    coreLibraryDesugaring(libs.desugar.jdk)
    implementation(libs.media3.datasource.okhttp)
    api(project(":core:player"))
    implementation(libs.media3.workmanager)
    implementation(libs.work.runtime)
    implementation(libs.room.runtime)
    testImplementation(libs.junit)
}

