pluginManagement {
    repositories { google(); mavenCentral(); gradlePluginPortal() }
}
dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories { google(); mavenCentral() }
}
rootProject.name = "XiaoqiVideo"
include(":app-mobile", ":app-tv", ":core:model", ":core:network", ":core:data", ":core:player", ":core:download", ":core:cast", ":core:design", ":feature:catalog", ":feature:account", ":feature:library", ":feature:live")
