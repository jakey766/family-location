import java.util.Properties

plugins {
    id("com.android.application")
    kotlin("android")
}

val local = Properties().apply {
    rootDir.resolve("app.local.properties").takeIf { it.isFile }?.inputStream()?.use { load(it) }
}
val signing = Properties().apply {
    rootDir.resolve("../secrets/android-signing.properties").takeIf { it.isFile }?.inputStream()?.use { load(it) }
}
val releaseReady = listOf("storeFile", "storePassword", "keyAlias", "keyPassword").all {
    !signing.getProperty(it).isNullOrBlank()
}

android {
    namespace = "com.jk.family"
    compileSdk = 35
    defaultConfig {
        applicationId = "com.jk.family"
        minSdk = 25
        targetSdk = 35
        versionCode = 7
        versionName = "0.5.6"
        manifestPlaceholders["AMAP_API_KEY"] = local.getProperty("AMAP_API_KEY", "")
    }
    signingConfigs {
        getByName("debug") { storeFile = rootDir.resolve("../.cache/android-user/debug.keystore") }
        if (releaseReady) create("release") {
            storeFile = rootDir.resolve(signing.getProperty("storeFile"))
            storePassword = signing.getProperty("storePassword")
            keyAlias = signing.getProperty("keyAlias")
            keyPassword = signing.getProperty("keyPassword")
        }
    }
    buildTypes {
        getByName("release") {
            isMinifyEnabled = false
            if (releaseReady) signingConfig = signingConfigs.getByName("release")
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
        isCoreLibraryDesugaringEnabled = true
    }
    kotlinOptions { jvmTarget = "17" }
    testOptions { unitTests.isIncludeAndroidResources = true }
    lint { abortOnError = true }
}

tasks.matching { it.name == "preReleaseBuild" }.configureEach {
    doFirst {
        check(releaseReady) { "Release requires secrets/android-signing.properties; see docs/ANDROID.md" }
        check(!local.getProperty("AMAP_API_KEY").isNullOrBlank()) { "Release requires AMAP_API_KEY in android/app.local.properties" }
    }
}
dependencies {
    implementation(project(":core"))
    implementation("com.amap.api:location:6.4.9")
    coreLibraryDesugaring("com.android.tools:desugar_jdk_libs:2.1.5")
    testImplementation("junit:junit:4.13.2")
    testImplementation("org.robolectric:robolectric:4.14.1")
}

tasks.withType<Test>().configureEach {
    systemProperty("robolectric.dependency.repo.url", "https://repo.maven.apache.org/maven2")
    systemProperty("java.io.tmpdir", rootDir.resolve("../.cache/tmp").canonicalPath)
}
