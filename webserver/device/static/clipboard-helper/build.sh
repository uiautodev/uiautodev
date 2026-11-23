#!/usr/bin/env bash
# Rebuild ../clipboard-helper.jar from Main.java.
#
# Requirements: JDK 8+ and Android SDK with platforms/android-34 and
# build-tools/34.0.0. Set ANDROID_HOME (or ANDROID_SDK_ROOT) first, e.g.
#
#   ANDROID_HOME=$HOME/Library/Android/sdk ./build.sh
set -euo pipefail

SDK="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-$HOME/Library/Android/sdk}}"
ANDROID_JAR="$SDK/platforms/android-34/android.jar"
D8="$SDK/build-tools/34.0.0/d8"

here="$(cd "$(dirname "$0")" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

mkdir -p "$work/classes"
javac --release 8 -cp "$ANDROID_JAR" -d "$work/classes" "$here/Main.java"
"$D8" --min-api 26 --output "$here/../clipboard-helper.jar" "$work/classes/com/uiautodev/clipboard/Main.class"
echo "wrote $here/../clipboard-helper.jar"
