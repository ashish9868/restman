//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func offerMoveToApplications() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	app := appBundlePath(exe)
	if !shouldOfferMove(app) {
		return false
	}
	if !confirmMove() {
		return false
	}
	destRoot := writableApplicationsDir()
	if destRoot == "" {
		notify("Restman could not find a writable Applications folder.")
		return false
	}
	dest := filepath.Join(destRoot, filepath.Base(app))
	if err := os.RemoveAll(dest); err != nil {
		notify("Restman could not replace the copy in Applications.")
		return false
	}
	if err := exec.Command("ditto", app, dest).Run(); err != nil {
		notify("Restman could not be moved to the Applications folder.")
		return false
	}
	_ = exec.Command("xattr", "-dr", "com.apple.quarantine", dest).Run()
	if err := exec.Command("open", "-n", dest).Start(); err != nil {
		notify("Restman was copied to Applications, but it could not be opened.")
		return false
	}
	return true
}

func confirmMove() bool {
	const script = `display dialog "Move Restman to the Applications folder?" buttons {"Not Now", "Move to Applications"} default button "Move to Applications" with title "Move to Applications"`
	out, err := exec.Command("osascript", "-e", script).CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "Move to Applications")
}

func notify(message string) {
	script := fmt.Sprintf(`display dialog %q buttons {"OK"} default button "OK" with title "Restman"`, message)
	_ = exec.Command("osascript", "-e", script).Run()
}

func writableApplicationsDir() string {
	if canWrite("/Applications") {
		return "/Applications"
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, "Applications")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	if !canWrite(dir) {
		return ""
	}
	return dir
}

func canWrite(dir string) bool {
	f, err := os.CreateTemp(dir, ".restman-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}
