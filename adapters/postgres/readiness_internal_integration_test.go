package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestPostgresPingRequiresUploadLockPool(t *testing.T) {
	databaseURL := os.Getenv("RIDU_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("set RIDU_POSTGRES_URL to run PostgreSQL integration tests")
	}
	backend, err := OpenWithConfig(context.Background(), PoolConfig{
		DatabaseURL: databaseURL, AllowInsecureTransport: true,
		MaxConnections: 1, MaxUploadLockConnections: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(backend.Close)
	backend.uploadLockPool.Close()
	if err := backend.Ping(context.Background()); err == nil || !strings.Contains(err.Error(), "upload-lock pool") {
		t.Fatalf("ping after upload-lock pool closure = %v, want upload-lock failure", err)
	}
}
