#!/bin/sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
export GRADLE_USER_HOME="$ROOT/.cache/gradle"
export ANDROID_USER_HOME="$ROOT/.cache/android-user"
export ANDROID_EMULATOR_HOME="$ROOT/.cache/android-user/emulator"
export ANDROID_AVD_HOME="$ROOT/.cache/android-user/avd"
export ANDROID_HOME="${ANDROID_HOME:-$ROOT/.cache/android-sdk}"
export TMPDIR="$ROOT/.cache/tmp"
mkdir -p "$GRADLE_USER_HOME" "$ANDROID_USER_HOME" "$TMPDIR"
if [ -z "${JAVA_HOME:-}" ] && [ -x '/Applications/IntelliJ IDEA.app/Contents/jbr/Contents/Home/bin/java' ]; then
    export JAVA_HOME='/Applications/IntelliJ IDEA.app/Contents/jbr/Contents/Home'
fi
# Keep JVM incidental files, preferences and tool state inside the workspace.
export JAVA_TOOL_OPTIONS="${JAVA_TOOL_OPTIONS:-} -Djava.io.tmpdir=$TMPDIR -Duser.home=$ROOT/.cache/android-user"
cd "$ROOT/android"
exec ./gradlew --no-daemon --console=plain "$@"
