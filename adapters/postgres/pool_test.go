package postgres

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizedPoolConfigProvidesBoundedProductionDefaults(t *testing.T) {
	configured, err := normalizedPoolConfig(PoolConfig{DatabaseURL: "postgres://user:password@localhost/database?sslmode=require"})
	if err != nil {
		t.Fatal(err)
	}
	if configured.MaxConns != 10 || configured.MinConns != 0 || configured.MaxConnLifetime != time.Hour || configured.MaxConnIdleTime != 30*time.Minute || configured.ConnConfig.ConnectTimeout != 10*time.Second {
		t.Fatalf("pool defaults = %#v", configured)
	}
	for name, expected := range map[string]string{
		"application_name":                    "ridu",
		"statement_timeout":                   "60000",
		"lock_timeout":                        "10000",
		"idle_in_transaction_session_timeout": "60000",
	} {
		if configured.ConnConfig.RuntimeParams[name] != expected {
			t.Fatalf("runtime parameter %s = %q, want %q", name, configured.ConnConfig.RuntimeParams[name], expected)
		}
	}
}

func TestNormalizedPoolConfigValidatesBoundsAndExplicitDisables(t *testing.T) {
	configured, err := normalizedPoolConfig(PoolConfig{
		DatabaseURL:            "postgres://user:password@localhost/database?sslmode=disable&statement_timeout=999999",
		AllowInsecureTransport: true,
		MaxConnections:         5, MinConnections: 2,
		ConnectTimeout: -1, StatementTimeout: -1, LockTimeout: -1, IdleInTransactionSessionTimeout: -1,
		MaxConnectionLifetime: -1, MaxConnectionLifetimeJitter: -1, MaxConnectionIdleTime: -1, HealthCheckPeriod: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if configured.MaxConns != 5 || configured.MinConns != 2 || configured.ConnConfig.ConnectTimeout != 0 || configured.MaxConnLifetime != 0 || configured.ConnConfig.RuntimeParams["statement_timeout"] != "0" {
		t.Fatalf("explicit pool configuration = %#v", configured)
	}
	for _, invalid := range []PoolConfig{
		{DatabaseURL: "postgres://localhost/database?sslmode=require", MaxConnections: 1, MinConnections: 2},
		{DatabaseURL: "postgres://localhost/database?sslmode=require", MaxConnections: -1},
		{DatabaseURL: "postgres://localhost/database?sslmode=require", MaxUploadLockConnections: -1},
		{DatabaseURL: "postgres://localhost/database?sslmode=require", StatementTimeout: time.Microsecond},
	} {
		if _, err := normalizedPoolConfig(invalid); err == nil {
			t.Fatalf("invalid pool configuration succeeded: %#v", invalid)
		}
	}
}

func TestNormalizedPoolConfigRequiresTLSUnlessExplicitlyAdmitted(t *testing.T) {
	for _, databaseURL := range []string{
		"postgres://user:secret@localhost/database",
		"postgres://user:secret@localhost/database?sslmode=prefer",
		"postgres://user:secret@localhost/database?sslmode=allow",
		"postgres://user:secret@localhost/database?sslmode=disable",
	} {
		if _, err := normalizedPoolConfig(PoolConfig{DatabaseURL: databaseURL}); err == nil {
			t.Fatalf("insecure PostgreSQL URL %q succeeded without explicit admission", databaseURL)
		} else if strings.Contains(err.Error(), "secret") {
			t.Fatalf("PostgreSQL configuration error exposed credentials: %v", err)
		}
		if _, err := normalizedPoolConfig(PoolConfig{DatabaseURL: databaseURL, AllowInsecureTransport: true}); err != nil {
			t.Fatalf("explicitly admitted PostgreSQL URL %q failed: %v", databaseURL, err)
		}
	}

	for _, mode := range []string{"require", "verify-ca", "verify-full"} {
		databaseURL := "postgres://user:secret@localhost/database?sslmode=" + mode
		if _, err := normalizedPoolConfig(PoolConfig{DatabaseURL: databaseURL}); err != nil {
			t.Fatalf("TLS-only PostgreSQL mode %q failed: %v", mode, err)
		}
	}
	if _, err := normalizedPoolConfig(PoolConfig{DatabaseURL: "postgres://user:secret@%"}); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("invalid PostgreSQL URL error = %v", err)
	}
}
