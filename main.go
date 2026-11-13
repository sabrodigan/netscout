package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/mux"
)

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
	r.PathPrefix("/").Handler(http.FileServer(http.FS(static)))

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
