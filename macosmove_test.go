package main

import "testing"

func TestAppBundlePath(t *testing.T) {
	app := appBundlePath("/Volumes/Restman/Restman.app/Contents/MacOS/restman")
	if app != "/Volumes/Restman/Restman.app" {
		t.Fatalf("app path: %s", app)
	}
	if appBundlePath("/usr/local/bin/restman") != "" {
		t.Fatal("a loose binary is not an app bundle")
	}
}

func TestShouldOfferMove(t *testing.T) {
	if !shouldOfferMove("/Volumes/Restman/Restman.app") {
		t.Fatal("disk image should offer the move")
	}
	if !shouldOfferMove("/Users/ashish/Downloads/Restman.app") {
		t.Fatal("Downloads should offer the move")
	}
	if shouldOfferMove("/Applications/Restman.app") {
		t.Fatal("Applications should not offer the move")
	}
	if shouldOfferMove("/Users/ashish/Applications/Restman.app") {
		t.Fatal("the user Applications folder should not offer the move")
	}
	if shouldOfferMove("") {
		t.Fatal("empty path should not offer the move")
	}
}
