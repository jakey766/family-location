plugins { kotlin("jvm") }
kotlin { compilerOptions { jvmTarget.set(org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17) } }
tasks.withType<JavaCompile>().configureEach { options.release.set(17) }
dependencies {
    implementation("com.google.code.gson:gson:2.11.0")
    implementation("com.squareup.okhttp3:okhttp:4.12.0")
    testImplementation("junit:junit:4.13.2")
}
tasks.test {
    // All test-created temporary files remain inside the project.
    systemProperty("java.io.tmpdir", rootDir.resolve("../.cache/tmp").canonicalPath)
    testLogging { events("failed", "skipped", "passed") }
}
