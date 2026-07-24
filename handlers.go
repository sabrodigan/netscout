package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil {
		json.NewEncoder(w).Encode(data)
	}
}

func httpError(w http.ResponseWriter, status int, msg string) {
	jsonResponse(w, status, map[string]string{"error": msg})
}

// createScanHandler runs a new scan and stores it. Optional JSON body:
// {"cidr": "192.168.1.0/24", "ports": [22,80,443]}. Missing fields fall back
// to the local /24 and the default port set.
func createScanHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CIDR  string `json:"cidr"`
		Ports []int  `json:"ports"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req) // body is optional
	}

	if req.CIDR == "" {
		req.CIDR = localCIDR()
		if req.CIDR == "" {
			httpError(w, http.StatusBadRequest, "could not determine local network; supply a cidr")
			return
		}
	}

	log.Printf("Starting scan of %s", req.CIDR)
	scan, err := runScan(req.CIDR, req.Ports)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	scan.ID = primitive.NewObjectID()

	if _, err := scanCol.InsertOne(context.Background(), scan); err != nil {
		httpError(w, http.StatusInternalServerError, "failed to store scan")
		return
	}
	log.Printf("Scan of %s complete: %d hosts up", scan.CIDR, scan.HostCount)
	jsonResponse(w, http.StatusCreated, scan)
}

// listScansHandler returns scan summaries, newest first.
func listScansHandler(w http.ResponseWriter, r *http.Request) {
	opts := options.Find().
		SetSort(bson.D{{Key: "finished_at", Value: -1}}).
		SetProjection(bson.D{{Key: "hosts", Value: 0}, {Key: "ports", Value: 0}})

	cursor, err := scanCol.Find(context.Background(), bson.M{}, opts)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "failed to list scans")
		return
	}
	var scans []ScanSummary
	if err := cursor.All(context.Background(), &scans); err != nil {
		httpError(w, http.StatusInternalServerError, "failed to parse scans")
		return
	}
	if scans == nil {
		scans = []ScanSummary{}
	}
	jsonResponse(w, http.StatusOK, scans)
}

// getScanHandler returns a single full scan by id.
func getScanHandler(w http.ResponseWriter, r *http.Request) {
	id, err := primitive.ObjectIDFromHex(mux.Vars(r)["id"])
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid scan id")
		return
	}
	scan, err := findScanByID(id)
	if err != nil {
		httpError(w, http.StatusNotFound, "scan not found")
		return
	}
	jsonResponse(w, http.StatusOK, scan)
}

// latestScanHandler returns the most recent scan.
func latestScanHandler(w http.ResponseWriter, r *http.Request) {
	scan, err := findLatestScans(1)
	if err != nil || len(scan) == 0 {
		httpError(w, http.StatusNotFound, "no scans yet")
		return
	}
	jsonResponse(w, http.StatusOK, scan[0])
}

// compareScansHandler diffs two explicit scans: ?from={id}&to={id}.
func compareScansHandler(w http.ResponseWriter, r *http.Request) {
	fromID, err := primitive.ObjectIDFromHex(r.URL.Query().Get("from"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid 'from' id")
		return
	}
	toID, err := primitive.ObjectIDFromHex(r.URL.Query().Get("to"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid 'to' id")
		return
	}
	from, err := findScanByID(fromID)
	if err != nil {
		httpError(w, http.StatusNotFound, "'from' scan not found")
		return
	}
	to, err := findScanByID(toID)
	if err != nil {
		httpError(w, http.StatusNotFound, "'to' scan not found")
		return
	}
	jsonResponse(w, http.StatusOK, diffScans(from, to))
}

// latestDiffHandler diffs the two most recent scans (previous -> latest).
func latestDiffHandler(w http.ResponseWriter, r *http.Request) {
	scans, err := findLatestScans(2)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "failed to load scans")
		return
	}
	if len(scans) < 2 {
		httpError(w, http.StatusConflict, "need at least two scans to compare")
		return
	}
	// findLatestScans returns newest first, so scans[1] is the previous scan.
	jsonResponse(w, http.StatusOK, diffScans(scans[1], scans[0]))
}

func findScanByID(id primitive.ObjectID) (*Scan, error) {
	var scan Scan
	err := scanCol.FindOne(context.Background(), bson.M{"_id": id}).Decode(&scan)
	if err != nil {
		return nil, err
	}
	return &scan, nil
}

func findLatestScans(n int64) ([]*Scan, error) {
	opts := options.Find().SetSort(bson.D{{Key: "finished_at", Value: -1}}).SetLimit(n)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cursor, err := scanCol.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	var scans []*Scan
	if err := cursor.All(ctx, &scans); err != nil {
		return nil, err
	}
	return scans, nil
}

// scanProgressHandler returns the progress of the currently active scan, if any.
func scanProgressHandler(w http.ResponseWriter, r *http.Request) {
	if !globalScanActive.Load() {
		jsonResponse(w, http.StatusOK, map[string]interface{}{"active": false})
		return
	}
	jsonResponse(w, http.StatusOK, map[string]interface{}{
		"active":  true,
		"scanned": globalScanScanned.Load(),
		"total":   globalScanTotal.Load(),
		"up":      globalScanUp.Load(),
	})
}
