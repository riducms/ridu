package postgres

import (
	"strings"
	"testing"
)

func TestConfigFromEnvironmentReadsTheStandardVariables(t *testing.T) {
	t.Setenv("DATABASE_URL", " postgres://ridu@db.example/ridu?sslmode=require ")
	t.Setenv("RIDU_ALLOW_INSECURE_DATABASE", "true")
	t.Setenv("RIDU_POSTGRES_UPLOAD_LOCK_CONNECTIONS", "6")
	config, err := ConfigFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if config.DatabaseURL != "postgres://ridu@db.example/ridu?sslmode=require" || !config.AllowInsecureTransport || config.MaxUploadLockConnections != 6 {
		t.Fatalf("config = %#v", config)
	}
	t.Setenv("RIDU_POSTGRES_UPLOAD_LOCK_CONNECTIONS", "many")
	if _, err := ConfigFromEnvironment(); err == nil || !strings.Contains(err.Error(), "RIDU_POSTGRES_UPLOAD_LOCK_CONNECTIONS") {
		t.Fatalf("malformed upload locks error = %v", err)
	}
}
