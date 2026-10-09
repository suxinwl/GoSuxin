plugins { alias(libs.plugins.android.library); alias(libs.plugins.kotlin.compose) }
android {
    namespace = "com.xiaoqi.video.feature.catalog"
    compileSdk = 36
    defaultConfig { minSdk = 23; testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner" }
    compileOptions { isCoreLibraryDesugaringEnabled = true; sourceCompatibility = JavaVersion.VERSION_17; targetCompatibility = JavaVersion.VERSION_17 }
    buildFeatures { compose = true }
}
dependencies {
    coreLibraryDesugaring(libs.desugar.jdk)
    api(project(":core:design"))
    api(project(":core:player"))
    implementation(project(":core:cast"))
    api(project(":feature:account"))
    api(project(":feature:library"))
    api(project(":feature:live"))
    implementation(libs.activity.compose)
    implementation(libs.tv.material)
    testImplementation(libs.junit)
}

