package main

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Host is a single discovered address within a scan.
type Host struct {
	IP        string `bson:"ip" json:"ip"`
	Status    string `bson:"status" json:"status"` // "up" or "down"
	Hostname  string `bson:"hostname,omitempty" json:"hostname,omitempty"`
	OpenPorts []int  `bson:"open_ports" json:"openPorts"`
}

// Scan is one run of the network scanner, with its hosts embedded.
// Every run is inserted as a new document so history is retained in full.
type Scan struct {
	ID         primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	CIDR       string             `bson:"cidr" json:"cidr"`
	Ports      []int              `bson:"ports" json:"ports"`
	StartedAt  time.Time          `bson:"started_at" json:"startedAt"`
	FinishedAt time.Time          `bson:"finished_at" json:"finishedAt"`
	HostCount  int                `bson:"host_count" json:"hostCount"` // number of "up" hosts
	Hosts      []Host             `bson:"hosts" json:"hosts"`
}

// ScanSummary is the lightweight projection used for the history list.
type ScanSummary struct {
	ID         primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	CIDR       string             `bson:"cidr" json:"cidr"`
	StartedAt  time.Time          `bson:"started_at" json:"startedAt"`
	FinishedAt time.Time          `bson:"finished_at" json:"finishedAt"`
	HostCount  int                `bson:"host_count" json:"hostCount"`
}

// HostChange records a host present in both scans whose details differ.
type HostChange struct {
	IP           string `json:"ip"`
	Hostname     string `json:"hostname,omitempty"`
	FromStatus   string `json:"fromStatus"`
	ToStatus     string `json:"toStatus"`
	AddedPorts   []int  `json:"addedPorts"`
	RemovedPorts []int  `json:"removedPorts"`
}

// ScanDiff is the comparison between two scans (from -> to).
type ScanDiff struct {
	FromID  primitive.ObjectID `json:"fromId"`
	ToID    primitive.ObjectID `json:"toId"`
	Added   []Host             `json:"added"`   // hosts in "to" but not "from"
	Removed []Host             `json:"removed"` // hosts in "from" but not "to"
	Changed []HostChange       `json:"changed"` // hosts in both, with differences
}
