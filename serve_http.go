//go:build browser

package main

import (
	"fmt"
	"net"
	"net/http"
)

func saveFile(string) (string, error) {
	return "", fmt.Errorf("save from the desktop window")
}

func pickFile() (string, error) {
	return "", fmt.Errorf("choose the file from the desktop window")
}

func setWindowTitle(string) {}

func setZoom(float64) {}

func serve(listener net.Listener, handler http.Handler, page string) error {
	fmt.Println("Open " + page + " in a browser. Build with -tags desktop for the native window.")
	return http.Serve(listener, handler)
}
