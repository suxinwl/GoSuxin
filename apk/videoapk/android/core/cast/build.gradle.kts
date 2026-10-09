plugins { alias(libs.plugins.android.library) }
android {
    namespace = "com.xiaoqi.video.core.cast"
    compileSdk = 36
    defaultConfig { minSdk = 23; testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner" }
    compileOptions { isCoreLibraryDesugaringEnabled = true; sourceCompatibility = JavaVersion.VERSION_17; targetCompatibility = JavaVersion.VERSION_17 }
}
dependencies {
    coreLibraryDesugaring(libs.desugar.jdk)
    api(project(":core:data"))
    implementation(libs.jupnp) { exclude(group = "org.eclipse.jetty"); exclude(group = "org.jupnp", module = "org.jupnp.support") }
    testImplementation(libs.junit)
}

