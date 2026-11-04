package mongodb

import (
	"strings"
	"testing"
)

func TestConfigFromEnvironmentReadsTheStandardVariables(t *testing.T) {
	t.Setenv("DATABASE_URL", "mongodb://db.example/ridu?replicaSet=rs0&tls=true")
	t.Setenv("RIDU_ALLOW_INSECURE_DATABASE", "")
	config, err := ConfigFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if config.DatabaseURL != "mongodb://db.example/ridu?replicaSet=rs0&tls=true" || config.AllowInsecureTransport {
		t.Fatalf("config = %#v", config)
	}
	t.Setenv("RIDU_ALLOW_INSECURE_DATABASE", "sometimes")
	if _, err := ConfigFromEnvironment(); err == nil || !strings.Contains(err.Error(), "RIDU_ALLOW_INSECURE_DATABASE") {
		t.Fatalf("malformed insecure flag error = %v", err)
	}
}
