package main

import (
	"context"
	"log"
	"os"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// placeCol reads the locations saved by pilocate. Those live in the host
// MongoDB database "pilocate", separate from the scan history in this app.
var placeCol *mongo.Collection

type knownPlace struct {
	IP       string `bson:"ip" json:"ip"`
	MAC      string `bson:"mac,omitempty" json:"mac,omitempty"`
	Hostname string `bson:"hostname,omitempty" json:"hostname,omitempty"`
	Place    string `bson:"place" json:"place"`
}

func InitPlaces() {
	uri := os.Getenv("PILOCATE_MONGO_URI")
	if uri == "" {
		uri = "mongodb://127.0.0.1:27017"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		log.Printf("Pi locations are unavailable: %v", err)
		return
	}
	if err = client.Ping(ctx, nil); err != nil {
		log.Printf("Pi locations are unavailable: %v", err)
		return
	}
	placeCol = client.Database("pilocate").Collection("pis")
	log.Println("Reading Pi locations from pilocate")
}

func loadPlaces(ctx context.Context) ([]knownPlace, error) {
	if placeCol == nil {
		return nil, mongo.ErrClientDisconnected
	}
	cursor, err := placeCol.Find(ctx, bson.M{"place": bson.M{"$gt": ""}})
	if err != nil {
		return nil, err
	}
	var places []knownPlace
	if err := cursor.All(ctx, &places); err != nil {
		return nil, err
	}
	if places == nil {
		places = []knownPlace{}
	}
	sort.Slice(places, func(i, j int) bool { return ipLess(places[i].IP, places[j].IP) })
	return places, nil
}
