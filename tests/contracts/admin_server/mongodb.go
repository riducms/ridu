package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var mongoFixtureDatabasePattern = regexp.MustCompile(`^ridu_admin_fixture_[a-f0-9]{16}$`)

// Every browser worker gets a new database, never the URL's original database.
// The fixed reserved namespace bounds reset and cleanup to fixture data.
func mongoFixtureURL(databaseURL string) (string, error) {
	parsed, err := url.Parse(databaseURL)
	if err != nil || (parsed.Scheme != "mongodb" && parsed.Scheme != "mongodb+srv") {
		return "", fmt.Errorf("RIDU_MONGODB_URL must be a valid MongoDB URL")
	}
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("generate MongoDB fixture database: %w", err)
	}
	parsed.Path = fmt.Sprintf("/ridu_admin_fixture_%x", suffix)
	return parsed.String(), nil
}

func resetMongoFixture(ctx context.Context, databaseURL string) error {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return fmt.Errorf("parse MongoDB fixture URL")
	}
	databaseName := strings.TrimPrefix(parsed.Path, "/")
	if !mongoFixtureDatabasePattern.MatchString(databaseName) {
		return fmt.Errorf("refusing to drop non-fixture MongoDB database %q", databaseName)
	}
	client, err := mongo.Connect(options.Client().ApplyURI(databaseURL))
	if err != nil {
		return fmt.Errorf("connect to MongoDB fixture: %w", err)
	}
	defer client.Disconnect(context.Background())
	if err := client.Database(databaseName).Drop(ctx); err != nil {
		return fmt.Errorf("drop MongoDB fixture: %w", err)
	}
	return nil
}
