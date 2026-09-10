package core_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/store"
)

// TestLocaleViewMemoryProbe is an opt-in, one-operation diagnostic intended for
// a fresh test process per case. It does not run in correctness gates or impose
// memory/timing thresholds. Sampling changes execution; use the benchmarks for
// uninstrumented time and allocation comparisons.
//
// RIDU_LOCALE_MEMORY=true enables JSON output. SHAPE defaults to native, MODE to
// ScalarUpdate and SIZE to 1000 (all prefixed RIDU_LOCALE_MEMORY_). MODE also accepts
// the six BenchmarkLocaleViewControls modes. SERIALIZE=true includes encoding.
// Wrap a fresh process with /usr/bin/time -l on macOS to obtain process-lifetime
// maximum RSS, which includes fixture setup. The reported sampled operation heap
// peak is an observed lower bound, not an exact maximum or process RSS peak.
func TestLocaleViewMemoryProbe(t *testing.T) {
	if os.Getenv("RIDU_LOCALE_MEMORY") != "true" {
		t.Skip("set RIDU_LOCALE_MEMORY=true for the separate memory diagnostic")
	}
	shape := localeMemoryEnv("SHAPE", "native")
	mode := localeMemoryEnv("MODE", "ScalarUpdate")
	size, err := strconv.Atoi(localeMemoryEnv("SIZE", "1000"))
	if err != nil || size < 4 || size > 5000 {
		t.Fatal("RIDU_LOCALE_MEMORY_SIZE must be between 4 and 5000")
	}
	if shape != "native" && shape != "embedded" {
		t.Fatal("unknown memory probe shape")
	}
	known := mode == "ScalarUpdate"
	for _, candidate := range localeViewControlModes {
		known = known || mode == candidate
	}
	if !known {
		t.Fatal("unknown memory probe mode")
	}
	serialize := os.Getenv("RIDU_LOCALE_MEMORY_SERIALIZE") == "true"
	var fixture localeViewFixture
	if mode == "ScalarUpdate" {
		fixture = localeViewFixture{ordinaryValueFixture: newOrdinaryValueFixture(t, t.Context(), shape, "ScalarUpdate", size), mode: mode, hooks: &localeViewHooks{}}
	} else {
		fixture = newLocaleViewFixture(t, t.Context(), shape, mode, size)
	}

	report := localeMemoryReport{Shape: shape, Mode: mode, Rows: size, Serialize: serialize, SampleIntervalNS: int64(time.Millisecond)}
	runtime.GC()
	report.Before = localeMemoryCheckpoint("fixture_after_gc")
	var start runtime.MemStats
	runtime.ReadMemStats(&start)
	stop := make(chan struct{})
	done := make(chan uint64)
	go func() {
		peak := start.HeapAlloc
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				var sampled runtime.MemStats
				runtime.ReadMemStats(&sampled)
				if sampled.HeapAlloc > peak {
					peak = sampled.HeapAlloc
				}
			case <-stop:
				done <- peak
				return
			}
		}
	}()
	var result store.Document
	if mode == "ScalarUpdate" {
		result, err = fixture.ordinaryValueFixture.run(t.Context())
	} else {
		result, err = fixture.run(t.Context())
	}
	var encoded []byte
	if err == nil && serialize {
		encoded, err = json.Marshal(result.Values)
	}
	var end runtime.MemStats
	runtime.ReadMemStats(&end)
	close(stop)
	report.SampledPeakHeapAlloc = <-done
	if end.HeapAlloc > report.SampledPeakHeapAlloc {
		report.SampledPeakHeapAlloc = end.HeapAlloc
	}
	if err != nil {
		t.Fatal(err)
	}
	report.InstrumentedTotalAlloc = end.TotalAlloc - start.TotalAlloc
	report.InstrumentedMallocs = end.Mallocs - start.Mallocs
	report.SerializedBytes = len(encoded)
	report.RetainedViews = len(fixture.hooks.retained)
	report.AfterOperation = localeMemoryPointFromStats("operation_complete_before_gc", end)
	if mode == "ScalarUpdate" {
		fixture.ordinaryValueFixture.checkResult(t, result)
	} else {
		fixture.check(t, result)
	}

	runtime.GC()
	report.WithSnapshots = localeMemoryCheckpoint("result_and_snapshots_after_gc")
	runtime.KeepAlive(fixture)
	runtime.KeepAlive(result)
	runtime.KeepAlive(encoded)
	fixture.hooks.retained = nil
	runtime.GC()
	report.WithoutSnapshots = localeMemoryCheckpoint("result_without_snapshots_after_gc")
	runtime.KeepAlive(fixture)
	runtime.KeepAlive(result)
	runtime.KeepAlive(encoded)
	fixture = localeViewFixture{}
	result = store.Document{}
	encoded = nil
	runtime.GC()
	report.AfterRelease = localeMemoryCheckpoint("fixture_and_result_released_after_gc")
	output, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("RIDU_LOCALE_MEMORY %s\n", output)
}

func localeMemoryEnv(name, fallback string) string {
	if value := os.Getenv("RIDU_LOCALE_MEMORY_" + name); value != "" {
		return value
	}
	return fallback
}

type localeMemoryReport struct {
	Shape                  string            `json:"shape"`
	Mode                   string            `json:"mode"`
	Rows                   int               `json:"rows"`
	Serialize              bool              `json:"serialize"`
	SerializedBytes        int               `json:"serialized_bytes"`
	RetainedViews          int               `json:"retained_views"`
	SampleIntervalNS       int64             `json:"sample_interval_ns"`
	SampledPeakHeapAlloc   uint64            `json:"sampled_peak_heap_alloc_bytes"`
	InstrumentedTotalAlloc uint64            `json:"instrumented_operation_total_alloc_bytes"`
	InstrumentedMallocs    uint64            `json:"instrumented_operation_mallocs"`
	Before                 localeMemoryPoint `json:"before"`
	AfterOperation         localeMemoryPoint `json:"after_operation"`
	WithSnapshots          localeMemoryPoint `json:"with_snapshots"`
	WithoutSnapshots       localeMemoryPoint `json:"without_snapshots"`
	AfterRelease           localeMemoryPoint `json:"after_release"`
}

type localeMemoryPoint struct {
	Phase       string `json:"phase"`
	HeapAlloc   uint64 `json:"heap_alloc_bytes"`
	HeapInuse   uint64 `json:"heap_inuse_bytes"`
	HeapObjects uint64 `json:"heap_objects"`
	NumGC       uint32 `json:"num_gc"`
	RSS         uint64 `json:"rss_bytes,omitempty"`
	RSSError    string `json:"rss_error,omitempty"`
}

func localeMemoryCheckpoint(phase string) localeMemoryPoint {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return localeMemoryPointFromStats(phase, stats)
}

// Heap counters are captured before the ps subprocess, whose temporary Go
// allocations are excluded from the next operation-counter baseline. RSS is a
// nearby process snapshot, not a synchronized heap measurement or exact peak.
func localeMemoryPointFromStats(phase string, stats runtime.MemStats) localeMemoryPoint {
	point := localeMemoryPoint{Phase: phase, HeapAlloc: stats.HeapAlloc, HeapInuse: stats.HeapInuse, HeapObjects: stats.HeapObjects, NumGC: stats.NumGC}
	output, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		point.RSSError = err.Error()
		return point
	}
	kilobytes, err := strconv.ParseUint(strings.TrimSpace(string(output)), 10, 64)
	if err != nil {
		point.RSSError = err.Error()
		return point
	}
	point.RSS = kilobytes * 1024
	return point
}
