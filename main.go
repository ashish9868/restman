package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"restman/internal/app"
	"restman/internal/session"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8787", "loopback address for the local UI")
	workspace := flag.String("workspace", "", "workspace directory (default ~/Restman)")
	flag.Parse()

	dataDir, err := session.ConfigDir()
	if err != nil {
		log.Fatal(err)
	}
	api, err := app.New(dataDir, *workspace)
	if err != nil {
		log.Fatal(err)
	}
	defer api.Close()

	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/rpc", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var call struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&call); err != nil {
			writeRPC(w, http.StatusBadRequest, nil, err)
			return
		}
		if call.Method == "SetTitle" {
			var in struct {
				Title string `json:"title"`
			}
			_ = json.Unmarshal(call.Params, &in)
			setWindowTitle(in.Title)
			writeRPC(w, http.StatusOK, nil, nil)
			return
		}
		if call.Method == "SetZoom" {
			var in struct {
				Level float64 `json:"level"`
			}
			_ = json.Unmarshal(call.Params, &in)
			setZoom(in.Level)
			writeRPC(w, http.StatusOK, nil, nil)
			return
		}
		if call.Method == "SaveFile" {
			var in struct {
				Name    string `json:"name"`
				Content string `json:"content"`
			}
			if err := json.Unmarshal(call.Params, &in); err != nil {
				writeRPC(w, http.StatusBadRequest, nil, err)
				return
			}
			path, err := saveFile(in.Name)
			if err != nil {
				writeRPC(w, http.StatusBadRequest, nil, err)
				return
			}
			if path == "" {
				writeRPC(w, http.StatusOK, "", nil)
				return
			}
			if err := os.WriteFile(path, []byte(in.Content), 0o644); err != nil {
				writeRPC(w, http.StatusBadRequest, nil, err)
				return
			}
			writeRPC(w, http.StatusOK, path, nil)
			return
		}
		if call.Method == "PickFile" {
			path, err := pickFile()
			if err != nil {
				writeRPC(w, http.StatusBadRequest, nil, err)
				return
			}
			writeRPC(w, http.StatusOK, path, nil)
			return
		}
		result, err := api.Dispatch(call.Method, call.Params)
		if err != nil {
			writeRPC(w, http.StatusBadRequest, nil, err)
			return
		}
		writeRPC(w, http.StatusOK, result, nil)
	})
	mux.Handle("/", staticHandler())

	url := "http://" + listener.Addr().String()
	fmt.Println("Restman " + url)
	if err := serve(listener, mux, url); err != nil {
		log.Fatal(err)
	}
}

func writeRPC(w http.ResponseWriter, status int, result any, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	payload := map[string]any{"result": result}
	if err != nil {
		payload["error"] = err.Error()
	}
	_ = json.NewEncoder(w).Encode(payload)
}

func staticHandler() http.Handler {
	exeDir := ""
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		exeDir = filepath.Dir(exe)
	}
	cwd, _ := os.Getwd()
	for _, root := range distCandidates(exeDir, cwd) {
		info, err := os.Stat(root)
		if err == nil && info.IsDir() {
			return spa(root)
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "frontend is not built; run npm install && npm run build in frontend/", http.StatusServiceUnavailable)
	})
}

func distCandidates(exeDir, cwd string) []string {
	var roots []string
	if env := os.Getenv("RESTMAN_DIST"); env != "" {
		roots = append(roots, env)
	}
	if exeDir != "" {
		roots = append(roots,
			filepath.Join(exeDir, "frontend", "dist"),
			filepath.Clean(filepath.Join(exeDir, "..", "share", "restman", "frontend", "dist")),
			filepath.Clean(filepath.Join(exeDir, "..", "Resources", "frontend", "dist")),
		)
	}
	if cwd != "" {
		roots = append(roots, filepath.Join(cwd, "frontend", "dist"))
	}
	return roots
}

func spa(root string) http.Handler {
	files := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		clean := filepath.Clean(path)
		if strings.HasPrefix(clean, "..") {
			http.NotFound(w, r)
			return
		}
		if _, err := os.Stat(filepath.Join(root, clean)); err != nil {
			r.URL.Path = "/"
		}
		files.ServeHTTP(w, r)
	})
}
