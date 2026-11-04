package core

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestExecuteReadsStandardVariablesForUnsetOptions(t *testing.T) {
	t.Setenv("PORT", "9000")
	t.Setenv("RIDU_ALLOWED_ORIGINS", "https://app.example, tauri://localhost")
	t.Setenv("RIDU_ALLOWED_HOSTS", "cms.example")
	t.Setenv("RIDU_TRUSTED_PROXY_CIDRS", "10.0.0.0/8")
	t.Setenv("RIDU_TRUSTED_PROXY_HOPS", "2")
	t.Setenv("RIDU_SETUP_TOKEN", strings.Repeat("a", 32))
	t.Setenv("RIDU_STRICT_TRANSPORT_SECURITY", "max-age=63072000")
	t.Setenv("RIDU_READINESS_TIMEOUT", "3s")
	t.Setenv("RIDU_SHUTDOWN_TIMEOUT", "20s")
	t.Setenv("RIDU_WORKER_DRAIN_TIMEOUT", "25s")
	t.Setenv("RIDU_READINESS_DRAIN_DELAY", "-1s")
	// The readiness overrides turn off production checks, so a variable must
	// never set them; only code does.
	t.Setenv("RIDU_ALLOW_UNVERIFIABLE_READINESS", "true")
	t.Setenv("RIDU_SKIP_READINESS_PREFLIGHT", "true")
	options := executeOptions{address: "127.0.0.1:8080"}
	if err := applyEnvironment(&options); err != nil {
		t.Fatal(err)
	}
	handler, server := options.handler, options.server
	if options.address != ":9000" ||
		!reflect.DeepEqual(handler.AllowedOrigins, []string{"https://app.example", "tauri://localhost"}) ||
		!reflect.DeepEqual(handler.AllowedHosts, []string{"cms.example"}) ||
		!reflect.DeepEqual(handler.TrustedProxyCIDRs, []string{"10.0.0.0/8"}) ||
		handler.TrustedProxyHops != 2 || len(handler.SetupToken) != 32 ||
		handler.StrictTransportSecurity != "max-age=63072000" || handler.ReadinessTimeout != 3*time.Second ||
		!handler.SecureCookies {
		t.Fatalf("handler options = %#v, address %q", handler, options.address)
	}
	if server.ShutdownTimeout != 20*time.Second || server.WorkerDrainTimeout != 25*time.Second ||
		server.ReadinessDrainDelay != -time.Second || server.AllowUnverifiableReadiness || server.SkipReadinessPreflight {
		t.Fatalf("server options = %#v", server)
	}

	t.Setenv("RIDU_ADDRESS", "0.0.0.0:7000")
	t.Setenv("RIDU_SECURE_COOKIES", "false")
	options = executeOptions{address: "127.0.0.1:8080"}
	if err := applyEnvironment(&options); err != nil {
		t.Fatal(err)
	}
	if options.address != "0.0.0.0:7000" || options.handler.SecureCookies {
		t.Fatalf("RIDU_ADDRESS over PORT = %q, secure cookies %v", options.address, options.handler.SecureCookies)
	}
}

func TestExecuteOptionsSetInCodeWinOverVariables(t *testing.T) {
	t.Setenv("RIDU_ADDRESS", "0.0.0.0:7000")
	t.Setenv("RIDU_ALLOWED_HOSTS", "cms.example")
	t.Setenv("RIDU_TRUSTED_PROXY_HOPS", "2")
	t.Setenv("RIDU_SHUTDOWN_TIMEOUT", "20s")
	options := executeOptions{address: "127.0.0.1:8080"}
	for _, option := range []ExecuteOption{
		WithAddress("127.0.0.1:9100"),
		WithHandlerOptions(HandlerOptions{AllowedHosts: []string{"app.internal"}, TrustedProxyHops: 1}),
		WithServerOptions(ServerOptions{ShutdownTimeout: time.Second}),
	} {
		option(&options)
	}
	if err := applyEnvironment(&options); err != nil {
		t.Fatal(err)
	}
	if options.address != "127.0.0.1:9100" || !reflect.DeepEqual(options.handler.AllowedHosts, []string{"app.internal"}) ||
		options.handler.TrustedProxyHops != 1 || options.server.ShutdownTimeout != time.Second {
		t.Fatalf("explicit options were replaced: address %q, handler %#v, server %#v", options.address, options.handler, options.server)
	}
}

func TestExecuteRefusesMalformedVariables(t *testing.T) {
	for name, raw := range map[string]string{
		"RIDU_TRUSTED_PROXY_HOPS": "-1",
		"RIDU_SHUTDOWN_TIMEOUT":   "soon",
		"RIDU_SECURE_COOKIES":     "off",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, raw)
			options := executeOptions{}
			if err := applyEnvironment(&options); err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("%s=%s error = %v", name, raw, err)
			}
		})
	}
}
