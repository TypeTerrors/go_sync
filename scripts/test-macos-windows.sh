#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
test "$(uname -s)" = Darwin
test_binary=$(mktemp -t gosync-appkit-tests)
trap 'rm -f "$test_binary"' EXIT HUP INT TERM
clang -fobjc-arc -fmodules -mmacosx-version-min=12.0 \
  -framework Cocoa -framework UniformTypeIdentifiers \
  scripts/test-macos-windows.m internal/desktop/ui/dashboard_darwin.m \
  -o "$test_binary"
"$test_binary"
