#!/bin/bash
# Builds macOSStat. Requires Go 1.23+ and the Command Line Tools.
set -euo pipefail
cd "$(dirname "$0")"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

usage() {
  cat <<'U'
./build.sh           ./macOSStat binary for this Mac
./build.sh release   dist/macOSStat universal binary and dist/macOSStat.tar.gz
                     (arm64 + x86_64, macOS 11+)
./build.sh app       optional dist/macOSStat.app and dist/macOSStat.zip
./build.sh check     gofmt, go vet and tests
./build.sh clean     remove ./macOSStat and dist/
U
}

check() {
  local bad
  bad=$(gofmt -l .)
  if [ -n "$bad" ]; then
    echo "gofmt needed: $bad" >&2
    return 1
  fi
  go vet ./...
  go test ./...
}

build_bin() {
  go build -o macOSStat .
  echo "Done: ./macOSStat"
}

# build_universal OUT
build_universal() {
  local arch
  for arch in arm64 amd64; do
    # -a: cached cgo objects may target a different minimum macOS version
    MACOSX_DEPLOYMENT_TARGET=11.0 CGO_ENABLED=1 GOOS=darwin GOARCH=$arch \
      CC="clang -arch ${arch/amd64/x86_64}" \
      go build -a -trimpath -ldflags="-s -w" -o "$TMP/$arch" .
  done
  lipo -create -output "$1" "$TMP/arm64" "$TMP/amd64"
}

build_release() {
  mkdir -p dist
  rm -f dist/macOSStat dist/macOSStat.tar.gz
  build_universal dist/macOSStat
  # Ad-hoc signature: required to run on arm64, does not satisfy Gatekeeper.
  codesign --force --sign - --identifier local.macosstat dist/macOSStat
  COPYFILE_DISABLE=1 tar --uid 0 --gid 0 --uname root --gname wheel -czf dist/macOSStat.tar.gz -C dist macOSStat
  echo "Done: dist/macOSStat, dist/macOSStat.tar.gz"
}

build_app() {
APP=dist/macOSStat.app
go run ./tools/icon "$TMP/icon.png"
mkdir "$TMP/icon.iconset"
for s in 16 32 128 256 512; do
  sips -z $s $s "$TMP/icon.png" --out "$TMP/icon.iconset/icon_${s}x${s}.png" >/dev/null
  sips -z $((s * 2)) $((s * 2)) "$TMP/icon.png" --out "$TMP/icon.iconset/icon_${s}x${s}@2x.png" >/dev/null
done

rm -rf "$APP" dist/macOSStat.zip
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
build_universal "$APP/Contents/MacOS/macOSStat"
iconutil -c icns "$TMP/icon.iconset" -o "$APP/Contents/Resources/icon.icns"
cat > "$APP/Contents/Info.plist" <<'P'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>macOSStat</string>
  <key>CFBundleDisplayName</key><string>macOSStat</string>
  <key>CFBundleIdentifier</key><string>local.macosstat</string>
  <key>CFBundleExecutable</key><string>macOSStat</string>
  <key>CFBundleIconFile</key><string>icon</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>1.0</string>
  <key>CFBundleVersion</key><string>1</string>
  <key>LSMinimumSystemVersion</key><string>11.0</string>
  <key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
P
codesign --force --sign - "$APP"
ditto -c -k --keepParent "$APP" dist/macOSStat.zip
echo "Done: $APP, dist/macOSStat.zip"
}

case "${1:-bin}" in
  bin) check; build_bin ;;
  release) check; build_release ;;
  app) check; build_app ;;
  check) check ;;
  clean) rm -rf macOSStat dist ;;
  -h|--help|help) usage ;;
  *) usage >&2; exit 2 ;;
esac
