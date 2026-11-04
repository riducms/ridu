package core

import (
	"time"

	"github.com/riducms/ridu/internal/environment"
)

// applyEnvironment fills each server option the application left at its zero
// value from Ridu's standard variables, so a variable a release adds takes
// effect without changing the application's main.go. An option the
// application set, including with WithAddress, wins over its variable.
func applyEnvironment(options *executeOptions) error {
	if !options.addressConfigured {
		if address := environment.Text("RIDU_ADDRESS"); address != "" {
			options.address = address
		} else if port := environment.Text("PORT"); port != "" {
			// A hosting platform that assigns PORT expects every interface.
			options.address = ":" + port
		}
	}
	handler, server := &options.handler, &options.server
	fillList(&handler.AllowedOrigins, "RIDU_ALLOWED_ORIGINS")
	fillList(&handler.AllowedHosts, "RIDU_ALLOWED_HOSTS")
	fillList(&handler.TrustedProxyCIDRs, "RIDU_TRUSTED_PROXY_CIDRS")
	fillText(&handler.SetupToken, "RIDU_SETUP_TOKEN")
	fillText(&handler.StrictTransportSecurity, "RIDU_STRICT_TRANSPORT_SECURITY")
	if handler.TrustedProxyHops == 0 {
		hops, err := environment.Count("RIDU_TRUSTED_PROXY_HOPS")
		if err != nil {
			return err
		}
		handler.TrustedProxyHops = hops
	}
	for name, value := range map[string]*time.Duration{
		"RIDU_READINESS_TIMEOUT":     &handler.ReadinessTimeout,
		"RIDU_SHUTDOWN_TIMEOUT":      &server.ShutdownTimeout,
		"RIDU_WORKER_DRAIN_TIMEOUT":  &server.WorkerDrainTimeout,
		"RIDU_READINESS_DRAIN_DELAY": &server.ReadinessDrainDelay,
	} {
		if *value != 0 {
			continue
		}
		duration, err := environment.Duration(name)
		if err != nil {
			return err
		}
		*value = duration
	}
	// AllowUnverifiableReadiness and SkipReadinessPreflight each turn off a
	// check a production server fails closed on, so only code sets them: a
	// variable could otherwise talk a server out of its migration history.
	// The generated PostgreSQL and SQLite servers read the two variables ridu
	// dev sets; a MongoDB server is told through its internal argument.
	// Cookies are Secure unless RIDU_SECURE_COOKIES=false, which ridu dev
	// sets for plain-HTTP development.
	if environment.Text("RIDU_SECURE_COOKIES") == "" {
		handler.SecureCookies = true
	} else if secure, err := environment.Bool("RIDU_SECURE_COOKIES"); err != nil {
		return err
	} else if secure {
		handler.SecureCookies = true
	}
	return nil
}

func fillList(value *[]string, name string) {
	if len(*value) == 0 {
		*value = environment.List(name)
	}
}

func fillText(value *string, name string) {
	if *value == "" {
		*value = environment.Text(name)
	}
}
