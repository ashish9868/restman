#!/usr/bin/env bash
# Build Restman installers for Linux, Windows, and macOS.
# Usage: scripts/package.sh [all|linux|windows|darwin]
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
VERSION="$(tr -d '[:space:]' < "$ROOT/VERSION")"
OUT="$ROOT/release"
STAGE="$OUT/.stage"
TARGET="${1:-all}"

if [[ -x "$ROOT/.tools/go/bin/go" ]]; then
  export PATH="$ROOT/.tools/go/bin:$PATH"
fi
if ! command -v go >/dev/null 2>&1; then
  echo "go is not installed" >&2
  exit 1
fi

mkdir -p "$OUT"
rm -rf "$STAGE"
mkdir -p "$STAGE"

build_ui() {
  if [[ ! -d "$ROOT/frontend/node_modules" ]]; then
    if [[ -f "$ROOT/frontend/package-lock.json" ]]; then
      (cd "$ROOT/frontend" && npm ci)
    else
      (cd "$ROOT/frontend" && npm install)
    fi
  fi
  (cd "$ROOT/frontend" && npm run build)
}

linux_pkgconfig() {
  if pkg-config --exists gtk+-3.0 webkit2gtk-4.1; then
    return
  fi
  local sysroot="$ROOT/.tools/sysroot"
  if [[ -d "$sysroot/usr/lib/x86_64-linux-gnu/pkgconfig" ]]; then
    export PKG_CONFIG_SYSROOT_DIR="$sysroot"
    export PKG_CONFIG_PATH="$sysroot/usr/lib/x86_64-linux-gnu/pkgconfig:$sysroot/usr/share/pkgconfig"
    return
  fi
  echo "Install libgtk-3-dev, libwebkit2gtk-4.1-dev, and pkg-config." >&2
  exit 1
}

host_deb_arch() {
  case "$(uname -m)" in
    x86_64) echo amd64 ;;
    aarch64 | arm64) echo arm64 ;;
    *) uname -m ;;
  esac
}

copy_ui() {
  local dest="$1"
  mkdir -p "$dest"
  cp -a "$ROOT/frontend/dist/." "$dest/"
}

package_linux() {
  local arch bin stage deb
  arch="$(host_deb_arch)"
  bin="$STAGE/restman-linux"
  linux_pkgconfig
  echo "Building Linux ($arch)"
  CGO_ENABLED=1 go build -trimpath -ldflags "-s -w" -o "$bin" .
  stage="$STAGE/deb"
  rm -rf "$stage"
  install -D -m 755 "$bin" "$stage/usr/bin/restman"
  mkdir -p "$stage/usr/share/restman/frontend/dist"
  copy_ui "$stage/usr/share/restman/frontend/dist"
  install -D -m 644 "$ROOT/packaging/linux/restman.desktop" "$stage/usr/share/applications/restman.desktop"
  install -D -m 644 "$ROOT/packaging/linux/restman.svg" "$stage/usr/share/icons/hicolor/scalable/apps/restman.svg"
  mkdir -p "$stage/DEBIAN"
  cat >"$stage/DEBIAN/control" <<EOF
Package: restman
Version: ${VERSION}
Section: net
Priority: optional
Architecture: ${arch}
Depends: libgtk-3-0, libwebkit2gtk-4.1-0
Maintainer: Restman <restman@localhost>
Description: Local API client
 Collections, environments, and history stay on this computer.
EOF
  deb="$OUT/restman_${VERSION}_linux_${arch}.deb"
  if dpkg-deb --help 2>/dev/null | grep -q root-owner-group; then
    dpkg-deb --root-owner-group --build "$stage" "$deb"
  else
    dpkg-deb --build "$stage" "$deb"
  fi
  echo "Wrote $deb"
}

windows_version4() {
  local ver="$1" n
  n="$(awk -F. '{print NF}' <<<"$ver")"
  case "$n" in
    4) printf '%s\n' "$ver" ;;
    3) printf '%s.0\n' "$ver" ;;
    2) printf '%s.0.0\n' "$ver" ;;
    1) printf '%s.0.0.0\n' "$ver" ;;
    *) printf '0.0.0.0\n' ;;
  esac
}

attach_windows_resources() {
  local syso="$ROOT/rsrc_windows_amd64.syso"
  local ver4 major minor patch build json manifest
  rm -f "$syso"
  ver4="$(windows_version4 "$VERSION")"
  IFS=. read -r major minor patch build <<<"$ver4"
  json="$STAGE/versioninfo.json"
  manifest="$STAGE/restman.manifest"
  mkdir -p "$STAGE"
  sed "s/@VERSION4@/${ver4}/g" "$ROOT/packaging/windows/restman.manifest" >"$manifest"
  cat >"$json" <<EOF
{
  "FixedFileInfo": {
    "FileVersion": {"Major": ${major}, "Minor": ${minor}, "Patch": ${patch}, "Build": ${build}},
    "ProductVersion": {"Major": ${major}, "Minor": ${minor}, "Patch": ${patch}, "Build": ${build}},
    "FileFlagsMask": "3f",
    "FileFlags ": "00",
    "FileOS": "040004",
    "FileType": "01",
    "FileSubType": "00"
  },
  "StringFileInfo": {
    "CompanyName": "D2i Technology",
    "FileDescription": "Restman",
    "FileVersion": "${VERSION}",
    "InternalName": "Restman",
    "LegalCopyright": "D2i Technology",
    "OriginalFilename": "Restman.exe",
    "ProductName": "Restman",
    "ProductVersion": "${VERSION}"
  },
  "VarFileInfo": {
    "Translation": {"LangID": "0409", "CharsetID": "04B0"}
  },
  "IconPath": "${ROOT}/packaging/windows/restman.ico",
  "ManifestPath": "${manifest}"
}
EOF
  if ! go run github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.4.1 -64 -o "$syso" "$json"; then
    echo "Restman.exe will have no Windows icon." >&2
    rm -f "$syso"
  fi
}

sign_windows() {
  local file="$1"
  local desc="$2"
  local pfx signed
  if [[ -z "${WINDOWS_CERTIFICATE_BASE64:-}" || -z "${WINDOWS_CERTIFICATE_PASSWORD:-}" ]]; then
    echo "No Authenticode certificate is configured. SmartScreen will flag $(basename "$file")."
    return 0
  fi
  if ! command -v osslsigncode >/dev/null 2>&1; then
    echo "osslsigncode is not installed, so $(basename "$file") was not signed." >&2
    return 1
  fi
  pfx="$(mktemp)"
  signed="$(mktemp --suffix=.exe)"
  P12="$pfx" python3 - <<'PY'
import base64, os, pathlib
pathlib.Path(os.environ["P12"]).write_bytes(base64.b64decode(os.environ["WINDOWS_CERTIFICATE_BASE64"]))
PY
  osslsigncode sign -pkcs12 "$pfx" -pass "$WINDOWS_CERTIFICATE_PASSWORD" \
    -n "$desc" -i "https://d2itechnology.com/" -h sha256 \
    -ts "http://timestamp.digicert.com" \
    -in "$file" -out "$signed"
  rm -f "$pfx"
  mv "$signed" "$file"
}

package_windows() {
  local parent="$STAGE/winroot"
  local app="$parent/Restman"
  local zip setup nsi ver4
  rm -rf "$parent"
  mkdir -p "$app"
  echo "Building Windows amd64"
  attach_windows_resources
  if ! CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o "$app/Restman.exe" .; then
    rm -f "$ROOT/rsrc_windows_amd64.syso"
    return 1
  fi
  rm -f "$ROOT/rsrc_windows_amd64.syso"
  sign_windows "$app/Restman.exe" "Restman"
  copy_ui "$app/frontend/dist"
  cp "$ROOT/packaging/windows/restman.ico" "$app/restman.ico"
  zip="$OUT/restman_${VERSION}_windows_amd64.zip"
  rm -f "$zip"
  (cd "$parent" && zip -r "$zip" Restman)
  echo "Wrote $zip"
  setup="$OUT/restman_${VERSION}_windows_amd64_setup.exe"
  nsi="$app/restman.nsi"
  ver4="$(windows_version4 "$VERSION")"
  sed -e "s|@OUTFILE@|${setup}|g" -e "s|@VERSION@|${VERSION}|g" -e "s|@VERSION4@|${ver4}|g" \
    "$ROOT/packaging/windows/restman.nsi" >"$nsi"
  if command -v makensis >/dev/null 2>&1; then
    (cd "$app" && makensis restman.nsi)
    sign_windows "$setup" "Restman Setup"
    echo "Wrote $setup"
  else
    echo "makensis is not installed. The zip is the Windows package. Install NSIS and re-run to build the setup exe."
  fi
}

package_darwin() {
  local arch="$1"
  local stage="$STAGE/darwin-$arch"
  local app="$stage/Restman.app"
  local archive dmg
  rm -rf "$stage"
  mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources/frontend/dist"
  echo "Building macOS $arch"
  CGO_ENABLED=0 GOOS=darwin GOARCH="$arch" go build -trimpath -ldflags "-s -w" -o "$app/Contents/MacOS/restman" .
  sed "s/@VERSION@/${VERSION}/g" "$ROOT/packaging/macos/Info.plist" >"$app/Contents/Info.plist"
  cp "$ROOT/packaging/macos/restman.icns" "$app/Contents/Resources/restman.icns"
  copy_ui "$app/Contents/Resources/frontend/dist"
  chmod 755 "$app/Contents/MacOS/restman"
  archive="$OUT/restman_${VERSION}_darwin_${arch}.tar.gz"
  tar -C "$stage" -czf "$archive" Restman.app
  echo "Wrote $archive"
  if command -v hdiutil >/dev/null 2>&1; then
    dmg="$OUT/restman_${VERSION}_darwin_${arch}.dmg"
    "$ROOT/scripts/make-dmg.sh" "$app" "$dmg"
  fi
}

build_ui
case "$TARGET" in
all)
  package_linux
  package_windows
  package_darwin amd64
  package_darwin arm64
  ;;
linux) package_linux ;;
windows) package_windows ;;
darwin)
  package_darwin amd64
  package_darwin arm64
  ;;
*)
  echo "usage: scripts/package.sh [all|linux|windows|darwin]" >&2
  exit 2
  ;;
esac

if command -v sha256sum >/dev/null 2>&1; then
  (
    cd "$OUT"
    sha256sum restman_"${VERSION}"_* >SHA256SUMS
  )
  echo "Wrote $OUT/SHA256SUMS"
fi
rm -rf "$STAGE"
echo "Packages are in $OUT"
