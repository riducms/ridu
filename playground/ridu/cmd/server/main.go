package main

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"example.com/ridu-dogfood/content"
	"example.com/ridu-dogfood/internal/adminassets"
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/postgres"
	"github.com/riducms/ridu/store"
)

func main() {
	applicationConfig := content.Config()
	var options []ridu.ExecuteOption
	if len(os.Args) == 1 {
		options = runtimeOptions(applicationConfig)
	}
	if err := ridu.Execute(applicationConfig, options...); err != nil {
		log.Fatal(err)
	}
}

func runtimeOptions(applicationConfig ridu.Config) []ridu.ExecuteOption {
	return []ridu.ExecuteOption{
		ridu.WithStore(func(ctx context.Context) (store.Store, error) {
			return postgres.OpenWithConfig(ctx, postgres.PoolConfig{
				DatabaseURL:              os.Getenv("DATABASE_URL"),
				AllowInsecureTransport:   envBool("RIDU_ALLOW_INSECURE_DATABASE"),
				MaxUploadLockConnections: envInt32("RIDU_POSTGRES_UPLOAD_LOCK_CONNECTIONS"),
			})
		}),
		ridu.WithAddress(env("RIDU_ADDRESS", ":8080")),
		ridu.WithHandlerOptions(ridu.HandlerOptions{
			AdminAssets:             adminassets.FS(),
			AllowedOrigins:          envList("RIDU_ALLOWED_ORIGINS"),
			AllowedHosts:            envList("RIDU_ALLOWED_HOSTS"),
			TrustedProxyCIDRs:       envList("RIDU_TRUSTED_PROXY_CIDRS"),
			ReadinessTimeout:        envDuration("RIDU_READINESS_TIMEOUT"),
			StrictTransportSecurity: os.Getenv("RIDU_STRICT_TRANSPORT_SECURITY"),
		}),
		ridu.WithServerOptions(ridu.ServerOptions{
			ShutdownTimeout:            envDuration("RIDU_SHUTDOWN_TIMEOUT"),
			WorkerDrainTimeout:         envDuration("RIDU_WORKER_DRAIN_TIMEOUT"),
			ReadinessDrainDelay:        envDuration("RIDU_READINESS_DRAIN_DELAY"),
			AllowUnverifiableReadiness: envBool("RIDU_ALLOW_UNVERIFIABLE_READINESS"),
			SkipReadinessPreflight:     envBool("RIDU_SKIP_READINESS_PREFLIGHT"),
		}),
	}
}

func envBool(name string) bool {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return false
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		log.Fatalf("%s must be a boolean: %v", name, err)
	}
	return value
}

func envDuration(name string) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		log.Fatalf("%s must be a Go duration: %v", name, err)
	}
	return value
}

func envInt32(name string) int32 {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0
	}
	value, err := strconv.ParseInt(raw, 10, 32)
	if err != nil || value < 0 {
		log.Fatalf("%s must be a non-negative 32-bit integer", name)
	}
	return int32(value)
}

func envList(name string) []string {
	var values []string
	for _, value := range strings.Split(os.Getenv(name), ",") {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
