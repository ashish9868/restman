# Shipping Restman

`scripts/package.sh` builds the UI, compiles the three desktop binaries, and writes installers into `release/`. The version string comes from `VERSION`.

From a Linux machine the script produces:

| File | What it is |
| --- | --- |
| `restman_<version>_linux_<arch>.deb` | Debian package. Native GTK window. |
| `restman_<version>_windows_amd64.zip` | Portable Windows folder. Native WebView2 window. |
| `restman_<version>_windows_amd64_setup.exe` | NSIS installer. Written when `makensis` is installed. |
| `restman_<version>_darwin_amd64.tar.gz` | macOS app for Intel. Native WebKit window. |
| `restman_<version>_darwin_arm64.tar.gz` | macOS app for Apple silicon. Native WebKit window. |
| `restman_<version>_darwin_<arch>.dmg` | Disk image. Written when the script runs on a Mac (`hdiutil`). |
| `SHA256SUMS` | Checksums of the files above. |

Linux is a GTK 3 and WebKitGTK 4.1 window. Windows is a WebView2 window, and macOS is a WKWebView window. The macOS app is compiled on a Mac because that window links Cocoa and WebKit. Windows 11 includes the WebView2 runtime. Windows 10 needs the Evergreen runtime from Microsoft.

## Prepare the machine

Linux packages need the GTK and WebKit headers on the build machine, and the matching runtime libraries on the machine that installs the `.deb`.

```bash
sudo apt install libgtk-3-dev libwebkit2gtk-4.1-dev pkg-config dpkg-dev zip
cd frontend && npm install && cd ..
```

Go 1.26 or newer must be on `PATH`. If `go` is missing and `.tools/go/bin/go` exists, the script uses that.

The Windows setup executable also needs NSIS:

```bash
sudo apt install nsis
```

A `.dmg` is created only on macOS, where `hdiutil` exists. From Linux, ship the `.tar.gz` app archives.

## GitHub release

`.github/workflows/release.yml` runs on every `v*` tag. It builds the Linux package, the Windows zip and setup executable, both macOS app archives, and a universal disk image, then attaches them to a GitHub Release. Set `VERSION`, commit it, and push a matching tag:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The workflow writes that tag into `VERSION` before packaging, so `v0.1.0` produces `restman_0.1.0_*`. Running the workflow by hand from the Actions tab builds the same files and keeps them as artifacts. It publishes a release only for a tag.

## Build

```bash
./scripts/package.sh          # linux, windows, and both mac archs
./scripts/package.sh linux
./scripts/package.sh windows
./scripts/package.sh darwin
```

Bump `VERSION` before a release. The script rebuilds `frontend/dist` and copies it into each package. The binary looks for that UI next to itself, under `/usr/share/restman` for the `.deb`, and inside `Restman.app/Contents/Resources` on macOS. `RESTMAN_DIST` overrides the UI directory.

## Linux

Install on Debian, Ubuntu, and derivatives:

```bash
sudo apt install ./release/restman_0.1.0_linux_amd64.deb
restman
```

The package depends on `libgtk-3-0` and `libwebkit2gtk-4.1-0`. It installs:

- `/usr/bin/restman`
- `/usr/share/restman/frontend/dist`
- a launcher at `/usr/share/applications/restman.desktop`

Build the `.deb` on Linux. The native window links WebKitGTK, so it is not cross-compiled from Windows or macOS.

## Windows

Run `restman_<version>_windows_amd64_setup.exe`. The wizard asks for a folder, defaults to `C:\Program Files\Restman`, and adds Start menu and desktop shortcuts. Windows lists it under Settings, Apps, and the uninstaller removes the shortcuts and the registry entry.

The zip is the portable layout: `Restman.exe` beside `frontend\dist`. It does not install into Program Files.

`@OUTFILE@`, `@VERSION@`, and `@VERSION4@` in `packaging/windows/restman.nsi` are replaced when `scripts/package.sh` runs.

SmartScreen flags an unsigned download. Add these Actions secrets and publish a new tag so the workflow can Authenticode-sign `Restman.exe` and the setup executable:

- `WINDOWS_CERTIFICATE_BASE64`, the code-signing certificate exported as a `.pfx` and base64-encoded
- `WINDOWS_CERTIFICATE_PASSWORD`

The build machine needs `osslsigncode`. Until those secrets are set, the installer still installs into Program Files, and the first open needs confirmation in SmartScreen.

## macOS

Each archive contains `Restman.app`. Intel Macs use the `darwin_amd64` archive. Apple silicon uses `darwin_arm64`.

The disk image is the installer. It shows Restman, an Applications folder, and the words “Drag Restman to the Applications folder.” Opening the app from a disk image or the Downloads folder asks to move it into Applications and then relaunches that copy.

`./scripts/package.sh darwin` writes the archives. On a Mac it also writes a disk image per architecture. The GitHub release builds one universal app with `lipo` and then runs `scripts/make-dmg.sh`.

macOS flags a download that is not notarized. Add these Actions secrets and publish a new tag so the release workflow can sign with a Developer ID, notarize the disk image, and staple the ticket:

- `APPLE_CERTIFICATE_BASE64`, the Developer ID Application certificate exported as a `.p12` and base64-encoded
- `APPLE_CERTIFICATE_PASSWORD`
- `APPLE_ID`
- `APPLE_TEAM_ID`
- `APPLE_APP_PASSWORD`, an app-specific password for `notarytool`

Until those are set, the disk image still installs by dragging Restman to Applications. The first open needs confirmation in Privacy & Security.

## Where data lives

The installer does not include a workspace. On first launch Restman creates `~/Restman` (`%USERPROFILE%\Restman` on Windows). History, cookies, and settings go to the OS config directory: `~/.config/restman` on Linux, `%AppData%\restman` on Windows, and `~/Library/Application Support/restman` on macOS.

## Check the packages

```bash
sha256sum -c release/SHA256SUMS
dpkg-deb -I release/restman_*_linux_*.deb
unzip -l release/restman_*_windows_amd64.zip
tar -tzf release/restman_*_darwin_arm64.tar.gz | head
```
