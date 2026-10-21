package main

import (
	"path/filepath"
	"strings"
)

func appBundlePath(exe string) string {
	macOS := filepath.Dir(exe)
	contents := filepath.Dir(macOS)
	app := filepath.Dir(contents)
	if filepath.Base(macOS) != "MacOS" || filepath.Base(contents) != "Contents" {
		return ""
	}
	if !strings.HasSuffix(app, ".app") {
		return ""
	}
	return app
}

func shouldOfferMove(app string) bool {
	if app == "" {
		return false
	}
	return filepath.Base(filepath.Dir(app)) != "Applications"
}
