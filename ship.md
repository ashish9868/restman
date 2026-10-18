# Shipping Restman

`scripts/package.sh` builds the UI, compiles the three desktop binaries, and writes installers into `release/`. The version string comes from `VERSION`.

From a Linux machine the script produces:

| File | What it is |
| --- | --- |
| `restman_<version>_linux_<arch>.deb` | Debian package. Native GTK window. |
| `restman_<version>_windows_amd64.zip` | Portable Windows folder. Opens the system browser. |
| `restman_<version>_windows_amd64_setup.exe` | NSIS installer. Written when `makensis` is installed. |
| `restman_<version>_darwin_amd64.tar.gz` | macOS app for Intel. Opens the system browser. |
| `restman_<version>_darwin_arm64.tar.gz` | macOS app for Apple silicon. Opens the system browser. |
| `restman_<version>_darwin_<arch>.dmg` | Disk image. Written when the script runs on a Mac (`hdiutil`). |
| `SHA256SUMS` | Checksums of the files above. |

Linux is a GTK 3 and WebKitGTK 4.1 window. Windows and macOS builds launch the same local UI in the system browser. Save and open dialogs are part of the GTK window, so those builds use the browser download for collection export.

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

Unzip `restman_<version>_windows_amd64.zip` and run `Restman\Restman.exe`. The zip keeps `Restman.exe` beside `frontend\dist`, which is the layout the program expects.

With NSIS installed, the same script writes `restman_<version>_windows_amd64_setup.exe`. That installer copies the program to `C:\Program Files\Restman` and adds Start menu and desktop shortcuts. Compile the script by hand from a staged folder with:

```bash
makensis packaging/windows/restman.nsi
```

`@OUTFILE@` in that `.nsi` is replaced when `scripts/package.sh` runs. Sign the setup executable on a Windows machine before a public download:

```powershell
signtool sign /fd SHA256 /tr http://timestamp.digicert.com /td SHA256 Restman-setup.exe
```

## macOS

Each archive contains `Restman.app`. On a Mac:

```bash
tar -xzf restman_0.1.0_darwin_arm64.tar.gz
open Restman.app
```

Intel Macs use the `darwin_amd64` archive. Apple silicon uses `darwin_arm64`.

To ship one universal app and a disk image, do this on a Mac after producing both binaries (or after extracting both apps and replacing the executable):

```bash
lipo -create restman-amd64 restman-arm64 -output Restman.app/Contents/MacOS/restman
hdiutil create -volname Restman -srcfolder Restman.app -ov -format UDZO restman_0.1.0_darwin_universal.dmg
```

`./scripts/package.sh darwin` runs `hdiutil` itself when that command exists, and writes a `.dmg` next to the tar archive.

Sign and notarize on a Mac before distribution outside your own machines:

```bash
codesign --force --deep --sign "Developer ID Application: Your Name" Restman.app
ditto -c -k --keepParent Restman.app Restman.zip
xcrun notarytool submit Restman.zip --keychain-profile "notary" --wait
xcrun stapler staple Restman.app
```

## Where data lives

The installer does not include a workspace. On first launch Restman creates `~/Restman` (`%USERPROFILE%\Restman` on Windows). History, cookies, and settings go to the OS config directory: `~/.config/restman` on Linux, `%AppData%\restman` on Windows, and `~/Library/Application Support/restman` on macOS.

## Check the packages

```bash
sha256sum -c release/SHA256SUMS
dpkg-deb -I release/restman_*_linux_*.deb
unzip -l release/restman_*_windows_amd64.zip
tar -tzf release/restman_*_darwin_arm64.tar.gz | head
```
