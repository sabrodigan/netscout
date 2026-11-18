package main

import (
	"bytes"
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/mux"
)

// build is the git commit this binary was built from.
// go build -ldflags "-X main.build=$(git rev-parse --short HEAD)"
var build = "dev"

//go:embed web
var webFS embed.FS

func main() {
	InitDB()
	InitPlaces()

	r := mux.NewRouter()

	api := r.PathPrefix("/api").Subrouter()
	api.HandleFunc("/places", listPlacesHandler).Methods("GET")
	api.HandleFunc("/scans", createScanHandler).Methods("POST")
	api.HandleFunc("/scans", listScansHandler).Methods("GET")
	api.HandleFunc("/scans/progress", scanProgressHandler).Methods("GET")
	api.HandleFunc("/scans/latest", latestScanHandler).Methods("GET")
	api.HandleFunc("/scans/latest-diff", latestDiffHandler).Methods("GET")
	api.HandleFunc("/scans/archive", listArchivedScansHandler).Methods("GET")
	api.HandleFunc("/scans/compare", compareScansHandler).Methods("GET")
	api.HandleFunc("/scans/{id}", getScanHandler).Methods("GET")

	// Serve the embedded web UI from the web/ subdirectory.
	static, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("failed to open embedded web assets: %v", err)
	}
	r.PathPrefix("/").Handler(uiHandler(static))

	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8092"
	}
	// No WriteTimeout: POST /api/scans holds the response open for the whole
	// scan, which can take several minutes at the maxProbes budget.
	srv := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	log.Printf("netscout listening on http://%s", addr)
	log.Fatal(srv.ListenAndServe())
}

func uiHandler(static fs.FS) http.Handler {
	files := http.FileServer(http.FS(static))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			files.ServeHTTP(w, r)
			return
		}
		page, err := fs.ReadFile(static, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(bytes.ReplaceAll(page, []byte("__BUILD__"), []byte(build)))
	})
}
