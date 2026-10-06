// Command ridu is the Ridu side of the block-heavy Ridu/Payload benchmark. It builds its
// configuration from the framework-neutral JSON spec written by ../spec.ts, so both frameworks
// serve equivalent schemas. The default build links only PostgreSQL; the blocksmongodb tag links
// only MongoDB, and the blocksgraphql tag adds the optional GraphQL plugin.
//
//	ridu migrations <directory>  write the initial migration artifact and print its history digest
//	ridu migrate <directory>     apply the artifact history to the configured database
//	ridu probe <directory>       measure resolution, manifest, generated contracts and live heap
//	ridu                         serve; RIDU_BLOCKS_HISTORY_DIGEST selects production startup
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"runtime/pprof"
	"syscall"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/tests/performance/blocks/blockspec"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	spec, err := blockspec.Read(os.Getenv("RIDU_BLOCKS_SPEC"))
	if err != nil {
		return err
	}
	if len(args) == 0 {
		return serve(ctx, spec)
	}
	if len(args) != 2 {
		return errors.New("usage: ridu [migrations|migrate|probe <directory>]")
	}
	switch args[0] {
	case "migrations":
		config, err := buildConfig(spec)
		if err != nil {
			return err
		}
		digest, err := writeMigrations(ctx, config, args[1])
		if err != nil {
			return err
		}
		fmt.Println(digest)
		return nil
	case "migrate":
		config, err := buildConfig(spec)
		if err != nil {
			return err
		}
		return applyMigrations(ctx, config, args[1])
	case "probe":
		return probe(spec, args[1])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

// buildConfig maps the spec onto Ridu authoring config with this build's plugins.
func buildConfig(spec blockspec.Spec) (ridu.Config, error) {
	return blockspec.Config(spec, extraPlugins())
}

func serve(parent context.Context, spec blockspec.Spec) error {
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()
	address := os.Getenv("RIDU_BLOCKS_ADDRESS")
	if address == "" {
		address = "127.0.0.1:18191"
	}
	config, err := buildConfig(spec)
	if err != nil {
		return err
	}
	backend, closeBackend, err := openStore(ctx)
	if err != nil {
		return err
	}
	defer closeBackend()
	application, err := ridu.New(config, backend)
	if err != nil {
		return err
	}
	// Like ridu.Execute's production preflight: verify the separately migrated database
	// against the executable migration history before serving.
	if digest := os.Getenv("RIDU_BLOCKS_HISTORY_DIGEST"); digest != "" {
		if err := verifyMigrations(ctx, backend, application.Manifest(), digest); err != nil {
			return fmt.Errorf("production readiness preflight: %w", err)
		}
	}
	mux := http.NewServeMux()
	// Benchmark-only diagnostics. Neither route is part of a Ridu application.
	mux.HandleFunc("GET /__bench/memory", func(response http.ResponseWriter, _ *http.Request) {
		runtime.GC()
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		response.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(response).Encode(map[string]uint64{
			"heapAlloc": stats.HeapAlloc, "heapObjects": stats.HeapObjects, "heapSys": stats.HeapSys,
			"heapInuse": stats.HeapInuse, "sys": stats.Sys, "numGC": uint64(stats.NumGC),
		})
	})
	mux.HandleFunc("GET /__bench/heap", func(response http.ResponseWriter, _ *http.Request) {
		runtime.GC()
		response.Header().Set("Content-Type", "application/octet-stream")
		_ = pprof.Lookup("heap").WriteTo(response, 0)
	})
	// Returns unused heap pages to the OS, to separate retained garbage from live memory in RSS.
	mux.HandleFunc("POST /__bench/free-os-memory", func(response http.ResponseWriter, _ *http.Request) {
		debug.FreeOSMemory()
		response.WriteHeader(http.StatusNoContent)
	})
	// CPU profiles bracket one load when the runner sets BLOCKS_PROFILE=true.
	var cpuProfile bytes.Buffer
	mux.HandleFunc("POST /__bench/cpu/start", func(response http.ResponseWriter, _ *http.Request) {
		cpuProfile.Reset()
		if err := pprof.StartCPUProfile(&cpuProfile); err != nil {
			http.Error(response, err.Error(), http.StatusConflict)
		}
	})
	mux.HandleFunc("POST /__bench/cpu/stop", func(response http.ResponseWriter, _ *http.Request) {
		pprof.StopCPUProfile()
		response.Header().Set("Content-Type", "application/octet-stream")
		_, _ = response.Write(cpuProfile.Bytes())
	})
	mux.Handle("/", application.Handler(ridu.HandlerOptions{AuthRateLimit: 1000}))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	serveError := make(chan error, 1)
	go func() { serveError <- server.Serve(listener) }()
	select {
	case <-ctx.Done():
	case err := <-serveError:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(shutdown)
}

// probe measures schema-proportional framework work without a database: resolution, manifest
// size, generated contracts, App construction and the live heap it retains.
func probe(spec blockspec.Spec, directory string) error {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	var before runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	started := time.Now()
	config, err := buildConfig(spec)
	if err != nil {
		return err
	}
	manifest, err := ridu.Resolve(config)
	resolveDuration := time.Since(started)
	var resolved runtime.MemStats
	runtime.ReadMemStats(&resolved)
	result := map[string]any{"scenario": spec.Scenario, "references": spec.References, "graphql": graphQLEnabled}
	result["resolveAllocBytes"] = resolved.TotalAlloc - before.TotalAlloc
	if err != nil {
		result["accepted"] = false
		result["error"] = err.Error()
		result["resolveMs"] = milliseconds(resolveDuration)
		return json.NewEncoder(os.Stdout).Encode(result)
	}
	result["accepted"] = true
	result["resolveMs"] = milliseconds(resolveDuration)
	manifestBytes, err := manifest.Bytes()
	if err != nil {
		return err
	}
	result["manifestBytes"] = len(manifestBytes)
	generated, err := generatedContracts(manifest, directory)
	if err != nil {
		return err
	}
	result["generated"] = generated
	runtime.GC()
	var beforeNew runtime.MemStats
	runtime.ReadMemStats(&beforeNew)
	started = time.Now()
	application, err := newProbeApplication(config)
	if err != nil {
		return err
	}
	result["newMs"] = milliseconds(time.Since(started))
	var afterNew runtime.MemStats
	runtime.ReadMemStats(&afterNew)
	result["newAllocBytes"] = afterNew.TotalAlloc - beforeNew.TotalAlloc
	handler := application.Handler(ridu.HandlerOptions{})
	// The admin downloads /api/schema before it renders, so its size is part of the schema cost.
	schemaResponse := httptest.NewRecorder()
	handler.ServeHTTP(schemaResponse, httptest.NewRequest(http.MethodGet, "/api/schema", nil))
	if schemaResponse.Code != http.StatusOK {
		return fmt.Errorf("GET /api/schema: status %d", schemaResponse.Code)
	}
	result["schemaResponseBytes"] = schemaResponse.Body.Len()
	schemaResponse = nil
	runtime.GC()
	debug.FreeOSMemory()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	result["appHeapAllocBytes"] = stats.HeapAlloc
	result["appHeapObjects"] = stats.HeapObjects
	result["totalAllocBytesDuringProbe"] = stats.TotalAlloc - before.TotalAlloc
	profile, err := os.Create(directory + "/heap.pprof")
	if err != nil {
		return err
	}
	if err := pprof.Lookup("heap").WriteTo(profile, 0); err != nil {
		return err
	}
	if err := profile.Close(); err != nil {
		return err
	}
	runtime.KeepAlive(handler)
	runtime.KeepAlive(application)
	return json.NewEncoder(os.Stdout).Encode(result)
}

func milliseconds(duration time.Duration) float64 {
	return float64(duration.Microseconds()) / 1000
}
