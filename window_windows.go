//go:build windows && !browser

package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"unsafe"

	"github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"

	"restman/internal/session"
)

var (
	uiMu sync.Mutex
	ui   webview2.WebView
)

func serve(listener net.Listener, handler http.Handler, page string) error {
	dataDir, err := session.ConfigDir()
	if err != nil {
		dataDir = os.TempDir()
	}
	webDir := filepath.Join(dataDir, "WebView2")
	_ = os.MkdirAll(webDir, 0o755)

	window := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     true,
		DataPath:  webDir,
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title:  "Restman",
			Width:  1440,
			Height: 900,
			IconId: 1,
			Center: true,
		},
	})
	if window == nil {
		messageBox("Restman needs the Microsoft Edge WebView2 runtime. Install it, then open Restman again.")
		return fmt.Errorf("webview2 runtime is not installed")
	}
	uiMu.Lock()
	ui = window
	uiMu.Unlock()

	go func() {
		if err := http.Serve(listener, handler); err != nil {
			log.Fatal(err)
		}
	}()
	fmt.Println("Opening Restman")
	window.Navigate(page)
	window.Run()
	return nil
}

func setWindowTitle(title string) {
	if title == "" {
		title = "Restman"
	}
	window := currentWindow()
	if window == nil {
		return
	}
	window.Dispatch(func() { window.SetTitle(title) })
}

func setZoom(level float64) {
	if level < 0.5 {
		level = 0.5
	}
	if level > 2 {
		level = 2
	}
	window := currentWindow()
	if window == nil {
		return
	}
	script := fmt.Sprintf("document.documentElement.style.zoom = '%g'", level)
	window.Dispatch(func() { window.Eval(script) })
}

func saveFile(name string) (string, error) {
	return chooseFile(name, true)
}

func pickFile() (string, error) {
	return chooseFile("", false)
}

func chooseFile(name string, save bool) (string, error) {
	window := currentWindow()
	if window == nil {
		if save {
			return "", fmt.Errorf("save from the desktop window")
		}
		return "", fmt.Errorf("choose the file from the desktop window")
	}
	var path string
	done := make(chan struct{})
	window.Dispatch(func() {
		path = winFileDialog(uintptr(window.Window()), name, save)
		close(done)
	})
	<-done
	return path, nil
}

func currentWindow() webview2.WebView {
	uiMu.Lock()
	defer uiMu.Unlock()
	return ui
}

func messageBox(text string) {
	title, _ := windows.UTF16PtrFromString("Restman")
	body, _ := windows.UTF16PtrFromString(text)
	_, _ = windows.MessageBox(0, body, title, windows.MB_OK|windows.MB_ICONWARNING)
}

type openFileName struct {
	StructSize      uint32
	Owner           uintptr
	Instance        uintptr
	Filter          *uint16
	CustomFilter    *uint16
	MaxCustomFilter uint32
	FilterIndex     uint32
	File            *uint16
	MaxFile         uint32
	FileTitle       *uint16
	MaxFileTitle    uint32
	InitialDir      *uint16
	Title           *uint16
	Flags           uint32
	FileOffset      uint16
	FileExtension   uint16
	DefExt          *uint16
	CustData        uintptr
	FnHook          uintptr
	TemplateName    *uint16
	Reserved        unsafe.Pointer
	ReservedFlags   uint32
	FlagsEx         uint32
}

const (
	ofnExplorer        = 0x00080000
	ofnFileMustExist   = 0x00001000
	ofnPathMustExist   = 0x00000800
	ofnOverwritePrompt = 0x00000002
	ofnNoChangeDir     = 0x00000008
	ofnDontAddToRecent = 0x02000000
)

func winFileDialog(owner uintptr, name string, save bool) string {
	comdlg32 := windows.NewLazySystemDLL("comdlg32.dll")
	procName := "GetOpenFileNameW"
	title := "Select file"
	flags := uint32(ofnExplorer | ofnFileMustExist | ofnPathMustExist | ofnNoChangeDir | ofnDontAddToRecent)
	filter := utf16z("All files", "*.*")
	var defExt *uint16
	if save {
		procName = "GetSaveFileNameW"
		title = "Save file"
		flags = ofnExplorer | ofnOverwritePrompt | ofnPathMustExist | ofnNoChangeDir
		filter = utf16z("JSON", "*.json", "All files", "*.*")
		defExt, _ = windows.UTF16PtrFromString("json")
	}
	buf := make([]uint16, 32768)
	if name != "" {
		copied, _ := windows.UTF16FromString(name)
		copy(buf, copied)
	}
	info := openFileName{
		Owner:   owner,
		Filter:  &filter[0],
		File:    &buf[0],
		MaxFile: uint32(len(buf)),
		Title:   mustUTF16(title),
		Flags:   flags,
		DefExt:  defExt,
	}
	info.StructSize = uint32(unsafe.Sizeof(info))
	proc := comdlg32.NewProc(procName)
	ret, _, _ := proc.Call(uintptr(unsafe.Pointer(&info)))
	if ret == 0 {
		return ""
	}
	return windows.UTF16ToString(buf)
}

func mustUTF16(value string) *uint16 {
	ptr, _ := windows.UTF16PtrFromString(value)
	return ptr
}

func utf16z(parts ...string) []uint16 {
	var buf []uint16
	for _, part := range parts {
		encoded, _ := windows.UTF16FromString(part)
		buf = append(buf, encoded...)
	}
	return append(buf, 0)
}
