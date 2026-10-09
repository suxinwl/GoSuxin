plugins { alias(libs.plugins.android.library) }
android {
    namespace = "com.xiaoqi.video.core.data"
    compileSdk = 36
    defaultConfig { minSdk = 23; testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner" }
    compileOptions { isCoreLibraryDesugaringEnabled = true; sourceCompatibility = JavaVersion.VERSION_17; targetCompatibility = JavaVersion.VERSION_17 }
}
dependencies {
    coreLibraryDesugaring(libs.desugar.jdk)
    api(project(":core:network"))
    api(libs.coroutines)
    api(libs.lifecycle.viewmodel.compose)
    implementation(libs.datastore)
    implementation(libs.room.runtime)
    implementation(libs.room.ktx)
    annotationProcessor(libs.room.compiler)
    testImplementation(libs.junit)
}

