plugins { alias(libs.plugins.android.library) }
android {
    namespace = "com.xiaoqi.video.core.network"
    compileSdk = 36
    defaultConfig { minSdk = 23; testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner" }
    buildFeatures { buildConfig = true }
    sourceSets.getByName("test").resources.srcDir("src/main/res/raw")
    defaultConfig {
        fun quoted(value: String) = "\"" + value.replace("\\", "\\\\").replace("\"", "\\\"").replace("\r", "\\r").replace("\n", "\\n") + "\""
        buildConfigField("String", "BOOTSTRAP_PASSWORD", quoted(providers.environmentVariable("XIAOQI_BOOTSTRAP_PASSWORD").orNull.orEmpty()))
        buildConfigField("String", "BOOTSTRAP_URL", quoted(providers.environmentVariable("XIAOQI_BOOTSTRAP_URL").orNull ?: "https://59.36.165.33:8976/down/ib3c2v0LlIH4.json"))
    }
    compileOptions { isCoreLibraryDesugaringEnabled = true; sourceCompatibility = JavaVersion.VERSION_17; targetCompatibility = JavaVersion.VERSION_17 }
}
dependencies {
    coreLibraryDesugaring(libs.desugar.jdk)
    api(project(":core:model"))
    api(libs.okhttp)
    api(libs.gson)
    implementation(libs.coroutines)
    testImplementation(libs.junit)
}

