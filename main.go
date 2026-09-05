package main

import (
	"embed"
	"io/fs"
	"log"
	"net/http"

	"github.com/gorilla/mux"
)

//go:embed web
var webFS embed.FS

func main() {
	InitDB()

	r := mux.NewRouter()

	api := r.PathPrefix("/api").Subrouter()
	api.HandleFunc("/scans", createScanHandler).Methods("POST")
	api.HandleFunc("/scans", listScansHandler).Methods("GET")
	api.HandleFunc("/scans/progress", scanProgressHandler).Methods("GET")
	api.HandleFunc("/scans/latest", latestScanHandler).Methods("GET")
	api.HandleFunc("/scans/latest-diff", latestDiffHandler).Methods("GET")
	api.HandleFunc("/scans/compare", compareScansHandler).Methods("GET")
	api.HandleFunc("/scans/{id}", getScanHandler).Methods("GET")

	// Serve the embedded web UI from the web/ subdirectory.
	static, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("failed to open embedded web assets: %v", err)
	}
	r.PathPrefix("/").Handler(http.FileServer(http.FS(static)))

	const addr = "0.0.0.0:8092"
	log.Printf("netscout listening on http://%s", addr)
	log.Fatal(http.ListenAndServe(addr, r))
}
