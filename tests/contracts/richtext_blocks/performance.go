package richtextblocks

import (
	"encoding/json"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/store"
)

type sample struct {
	Milliseconds float64 `json:"ms"`
	Bytes        uint64  `json:"allocatedBytes"`
	Allocations  uint64  `json:"allocations"`
}

func measured(t testing.TB, operation func() error) sample {
	t.Helper()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	if err := operation(); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	runtime.ReadMemStats(&after)
	return sample{Milliseconds: float64(elapsed) / float64(time.Millisecond), Bytes: after.TotalAlloc - before.TotalAlloc, Allocations: after.Mallocs - before.Mallocs}
}

// RunPerformanceBenchmark is an opt-in diagnostic benchmark for intentionally
// large versioned aggregate documents. It reports logical JSON bytes submitted
// and retained version bytes, not WAL, BSON compression, filesystem writes or
// fsync latency. The first operation and five subsequent warm samples are
// reported separately. Use -benchtime=1x so every matrix case runs once.
func RunPerformanceBenchmark(b *testing.B, adapter string, factory BenchmarkFactory) {
	b.Helper()
	for _, count := range []int{10, 100, 500} {
		for _, targetBytes := range []int{50 * 1024, 500 * 1024, 2 * 1024 * 1024} {
			b.Run(fmt.Sprintf("%d-blocks/%d-bytes", count, targetBytes), func(b *testing.B) {
				if b.N != 1 {
					b.Fatalf("rich-text block diagnostics require -benchtime=1x; got %d iterations", b.N)
				}
				_, app := factory(b, configuration(b, &observations{}))
				nodes := make([]store.Value, count)
				for i := range nodes {
					nodes[i] = Block("callout", fmt.Sprintf("block-%d", i), store.Values{"title": store.String(fmt.Sprintf("Callout %d", i))})
				}
				encoded, _ := json.Marshal(Document(nodes...))
				padding := targetBytes - len(encoded)
				if padding > 0 {
					nodes = append(nodes, paragraph(strings.Repeat("x", padding)))
				}
				values := store.Values{"title": store.String("Measured aggregate"), "body": Document(nodes...)}
				input, _ := json.Marshal(values)
				var document store.Document
				create := measured(b, func() error {
					var err error
					document, err = app.Local().Create(b.Context(), "articles", values, ridu.MutationOptions{})
					return err
				})
				draft := true
				var reads, writes []sample
				for i := range 6 {
					reads = append(reads, measured(b, func() error {
						_, err := app.Local().Find(b.Context(), "articles", document.ID, ridu.FindOptions{Draft: &draft})
						return err
					}))
					// A small edit still persists the ordinary aggregate, plus a full
					// revision snapshot. This is deliberate write-amplification proof.
					values["title"] = store.String(fmt.Sprintf("Measured aggregate %d", i))
					writes = append(writes, measured(b, func() error {
						var err error
						document, err = app.Local().Update(b.Context(), "articles", document.ID, values, ridu.MutationOptions{ExpectedRevision: document.Revision})
						return err
					}))
				}
				versions, err := app.Local().Versions(b.Context(), "articles", document.ID, ridu.FindOptions{})
				if err != nil {
					b.Fatal(err)
				}
				versionBytes := 0
				for _, version := range versions {
					encoded, _ := json.Marshal(version.Snapshot.Values)
					versionBytes += len(encoded)
				}
				output, _ := json.Marshal(document.Values)
				p95 := func(values []sample) float64 {
					numbers := make([]float64, len(values))
					for i, item := range values {
						numbers[i] = item.Milliseconds
					}
					sort.Float64s(numbers)
					return numbers[len(numbers)-1]
				}
				result := struct {
					Adapter              string   `json:"adapter"`
					Host                 string   `json:"host"`
					Blocks               int      `json:"blocks"`
					TargetBytes          int      `json:"targetBytes"`
					InputBytes           int      `json:"inputBytes"`
					StoredAggregateBytes int      `json:"storedAggregateBytes"`
					VersionCount         int      `json:"versionCount"`
					VersionBytes         int      `json:"versionBytes"`
					Create               sample   `json:"create"`
					FirstRead            sample   `json:"firstRead"`
					FirstUpdate          sample   `json:"firstUpdate"`
					WarmRead             []sample `json:"warmRead"`
					WarmUpdate           []sample `json:"warmUpdate"`
					ReadP95              float64  `json:"warmReadP95Ms"`
					UpdateP95            float64  `json:"warmUpdateP95Ms"`
				}{adapter, runtime.GOOS + "/" + runtime.GOARCH, count, targetBytes, len(input), len(output), len(versions), versionBytes, create, reads[0], writes[0], reads[1:], writes[1:], p95(reads[1:]), p95(writes[1:])}
				data, _ := json.Marshal(result)
				b.Logf("PERF %s", data)
			})
		}
	}
}
