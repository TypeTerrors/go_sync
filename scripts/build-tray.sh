#!/bin/sh
# Native only: macOS uses Cocoa/CGO. Never pretend a static cross-build is a .app.
set -eu
cd "$(dirname "$0")/.."
desktop_os=$(go env GOHOSTOS)
desktop_arch=$(go env GOHOSTARCH)
case "$desktop_os/$desktop_arch" in
  linux/amd64|linux/arm64|darwin/amd64|darwin/arm64) ;;
  *) echo "Unsupported desktop target: $desktop_os/$desktop_arch" >&2; exit 1 ;;
esac
desktop_stage="dist/desktop/$desktop_os-$desktop_arch"
mkdir -p "$desktop_stage/bin"
CGO_ENABLED=0 GOOS="$desktop_os" GOARCH="$desktop_arch" go build -trimpath -o "$desktop_stage/bin/gosync" ./cmd/server
MACOSX_DEPLOYMENT_TARGET=12.0 CGO_ENABLED=1 GOOS="$desktop_os" GOARCH="$desktop_arch" go build -trimpath -tags desktop -o "$desktop_stage/bin/gosync-desktop" ./cmd/tray
python3 scripts/package-desktop.py "$desktop_os" "$desktop_arch" "$desktop_stage"
