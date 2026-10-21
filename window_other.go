//go:build !linux && !windows && !darwin && !browser

package main

import (
	"fmt"
	"net"
	"net/http"
	"os/exec"
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
	go func() {
		_ = http.Serve(listener, handler)
	}()
	fmt.Println("Opening " + page)
	cmd := exec.Command("xdg-open", page)
	if err := cmd.Start(); err != nil {
		fmt.Println("Open " + page + " in a browser")
	}
	select {}
}
