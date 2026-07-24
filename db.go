package main

import (
	"context"
	"log"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	client  *mongo.Client
	db      *mongo.Database
	scanCol *mongo.Collection
)

// InitDB connects to MongoDB and prepares the scans collection.
func InitDB() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	var err error
	client, err = mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %v", err)
	}
	if err = client.Ping(ctx, nil); err != nil {
		log.Fatalf("Failed to ping MongoDB: %v", err)
	}
	log.Println("Connected to MongoDB successfully!")

	db = client.Database("netscout")
	scanCol = db.Collection("scans")

	// Index on finished_at (descending) to make "latest" lookups fast.
	idx := mongo.IndexModel{Keys: bson.D{{Key: "finished_at", Value: -1}}}
	if _, err := scanCol.Indexes().CreateOne(context.Background(), idx); err != nil {
		log.Printf("Could not create index on finished_at: %v", err)
	}
}
