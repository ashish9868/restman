#!/usr/bin/env bash
# Build a Restman disk image that shows the app and an Applications folder.
# Usage: scripts/make-dmg.sh Restman.app output.dmg
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: scripts/make-dmg.sh Restman.app output.dmg" >&2
  exit 2
fi

APP="$1"
OUT="$2"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BACKGROUND="$ROOT/packaging/macos/dmg-background.png"

if [[ ! -d "$APP/Contents/MacOS" ]]; then
  echo "not an app bundle: $APP" >&2
  exit 1
fi
if ! command -v hdiutil >/dev/null 2>&1; then
  echo "hdiutil is required, so this script runs on macOS" >&2
  exit 1
fi
if [[ ! -f "$BACKGROUND" ]]; then
  echo "missing $BACKGROUND" >&2
  exit 1
fi

work="$(mktemp -d)"
mount=""
KEYCHAIN=""
NOTARIZE=0
OLD_KEYCHAINS=()

cleanup() {
  if [[ -n "$mount" ]]; then
    hdiutil detach "$mount" >/dev/null 2>&1 || true
  fi
  if [[ -n "$KEYCHAIN" ]]; then
    security delete-keychain "$KEYCHAIN" >/dev/null 2>&1 || true
  fi
  if [[ ${#OLD_KEYCHAINS[@]} -gt 0 ]]; then
    security list-keychains -d user -s "${OLD_KEYCHAINS[@]}" >/dev/null 2>&1 || true
  fi
  rm -rf "$work"
}
trap cleanup EXIT

sign_app() {
  local app="$1"
  if [[ -z "${APPLE_CERTIFICATE_BASE64:-}" || -z "${APPLE_CERTIFICATE_PASSWORD:-}" ]]; then
    echo "No Developer ID certificate is configured. Gatekeeper will flag this download."
    return
  fi

  local p12="$work/certificate.p12"
  local pass identity
  KEYCHAIN="$work/signing.keychain-db"
  pass="$(openssl rand -hex 16)"
  P12="$p12" python3 - <<'PY'
import base64, os, pathlib
pathlib.Path(os.environ["P12"]).write_bytes(base64.b64decode(os.environ["APPLE_CERTIFICATE_BASE64"]))
PY

  while IFS= read -r line; do
    line="${line//\"/}"
    line="${line#"${line%%[![:space:]]*}"}"
    line="${line%"${line##*[![:space:]]}"}"
    if [[ -n "$line" ]]; then
      OLD_KEYCHAINS+=("$line")
    fi
  done < <(security list-keychains -d user)

  security create-keychain -p "$pass" "$KEYCHAIN"
  security set-keychain-settings -lut 21600 "$KEYCHAIN"
  security unlock-keychain -p "$pass" "$KEYCHAIN"
  security import "$p12" -k "$KEYCHAIN" -P "$APPLE_CERTIFICATE_PASSWORD" -T /usr/bin/codesign -T /usr/bin/security
  security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "$pass" "$KEYCHAIN" >/dev/null
  security list-keychains -d user -s "$KEYCHAIN"
  identity="$(security find-identity -v -p codesigning "$KEYCHAIN" | sed -n 's/.*"\(Developer ID Application:[^"]*\)".*/\1/p' | head -1)"
  if [[ -z "$identity" ]]; then
    echo "The certificate has no Developer ID Application identity." >&2
    exit 1
  fi
  codesign --force --options runtime --timestamp --sign "$identity" --keychain "$KEYCHAIN" "$app/Contents/MacOS/restman"
  codesign --force --options runtime --timestamp --sign "$identity" --keychain "$KEYCHAIN" "$app"
  codesign --verify --verbose=2 "$app"
  if [[ -n "${APPLE_ID:-}" && -n "${APPLE_TEAM_ID:-}" && -n "${APPLE_APP_PASSWORD:-}" ]]; then
    NOTARIZE=1
  else
    echo "Signed with Developer ID. Notarization secrets are missing, so Gatekeeper can still flag the download."
  fi
}

stage="$work/Restman.app"
cp -R "$APP" "$stage"
sign_app "$stage"

tmpdmg="$work/restman.rw.dmg"
mount="$work/mount"
mkdir -p "$mount"
kb="$(du -sk "$stage" | awk '{print $1}')"
mb="$(((kb + 1023) / 1024))"
hdiutil create -size "$((mb + 64))m" -fs APFS -volname Restman -ov "$tmpdmg"
hdiutil attach "$tmpdmg" -readwrite -noverify -noautoopen -mountpoint "$mount"

cp -R "$stage" "$mount/Restman.app"
ln -s /Applications "$mount/Applications"
mkdir -p "$mount/.background"
cp "$BACKGROUND" "$mount/.background/background.png"
chflags hidden "$mount/.background" || true
chmod -R a+rX "$mount/Restman.app"

if ! osascript <<'EOF'
tell application "Finder"
  tell disk "Restman"
    open
    delay 1
    set current view of container window to icon view
    set toolbar visible of container window to false
    set statusbar visible of container window to false
    set the bounds of container window to {200, 120, 860, 520}
    set viewOptions to the icon view options of container window
    set arrangement of viewOptions to not arranged
    set icon size of viewOptions to 128
    set background picture of viewOptions to file ".background:background.png"
    set position of item "Restman.app" of container window to {180, 170}
    set position of item "Applications" of container window to {480, 170}
    close
    open
    update without registering applications
    delay 2
  end tell
end tell
EOF
then
  echo "Finder layout was not applied. The disk image still contains the Applications folder."
fi

sync
hdiutil detach "$mount"
mount=""
mkdir -p "$(dirname "$OUT")"
rm -f "$OUT"
hdiutil convert "$tmpdmg" -format UDZO -imagekey zlib-level=9 -o "$OUT"

if [[ "$NOTARIZE" == "1" ]]; then
  xcrun notarytool submit "$OUT" \
    --apple-id "$APPLE_ID" \
    --team-id "$APPLE_TEAM_ID" \
    --password "$APPLE_APP_PASSWORD" \
    --wait
  xcrun stapler staple "$OUT"
fi

echo "Wrote $OUT"
