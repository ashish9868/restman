package main

import (
	"path/filepath"
	"testing"
)

func TestDistCandidates(t *testing.T) {
	t.Setenv("RESTMAN_DIST", "")
	linux := distCandidates(filepath.Join(string(filepath.Separator), "usr", "bin"), filepath.Join("src", "restman"))
	share := filepath.Join(string(filepath.Separator), "usr", "share", "restman", "frontend", "dist")
	if !contains(linux, share) {
		t.Fatalf("linux share path missing: %v", linux)
	}
	if !contains(linux, filepath.Join("src", "restman", "frontend", "dist")) {
		t.Fatalf("dev path missing: %v", linux)
	}

	t.Setenv("RESTMAN_DIST", filepath.Join("opt", "ui"))
	macExe := filepath.Join("Applications", "Restman.app", "Contents", "MacOS")
	mac := distCandidates(macExe, "")
	if mac[0] != filepath.Join("opt", "ui") {
		t.Fatalf("env path: %v", mac)
	}
	resources := filepath.Join("Applications", "Restman.app", "Contents", "Resources", "frontend", "dist")
	if !contains(mac, resources) {
		t.Fatalf("mac resources path missing: %v", mac)
	}
	beside := filepath.Join(macExe, "frontend", "dist")
	if !contains(mac, beside) {
		t.Fatalf("beside-binary path missing: %v", mac)
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
