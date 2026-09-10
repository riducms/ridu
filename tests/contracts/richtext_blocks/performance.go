package richtextblocks

import (
	"encoding/json"
	"fmt"
	"os"
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

func measured(t *testing.T, operation func() error) sample {
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

// RunPerformance is opt-in because these versioned aggregate documents are
// intentionally large. It reports logical JSON bytes submitted and retained
// version bytes, not WAL, BSON compression, filesystem writes or fsync latency.
// The first operation and five subsequent warm samples are reported separately.
func RunPerformance(t *testing.T, adapter string, factory Factory) {
	t.Helper()
	if os.Getenv("RIDU_BLOCKS_PERFORMANCE") != "1" {
		t.Skip("set RIDU_BLOCKS_PERFORMANCE=1 for measured aggregate JSON workloads")
	}
	for _, count := range []int{10, 100, 500} {
		for _, targetBytes := range []int{50 * 1024, 500 * 1024, 2 * 1024 * 1024} {
			t.Run(fmt.Sprintf("%d-blocks/%d-bytes", count, targetBytes), func(t *testing.T) {
				_, app := factory(t, configuration(t, &observations{}))
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
				create := measured(t, func() error {
					var err error
					document, err = app.Local().Create(t.Context(), "articles", values, nil)
					return err
				})
				draft := true
				var reads, writes []sample
				for i := range 6 {
					reads = append(reads, measured(t, func() error {
						_, err := app.Local().FindWithOptions(t.Context(), "articles", document.ID, ridu.FindOptions{Draft: &draft})
						return err
					}))
					// A small edit still persists the ordinary aggregate, plus a full
					// revision snapshot. This is deliberate write-amplification proof.
					values["title"] = store.String(fmt.Sprintf("Measured aggregate %d", i))
					writes = append(writes, measured(t, func() error {
						var err error
						document, err = app.Local().UpdateRevision(t.Context(), "articles", document.ID, values, document.Revision, nil)
						return err
					}))
				}
				versions, err := app.Local().Versions(t.Context(), "articles", document.ID, nil)
				if err != nil {
					t.Fatal(err)
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
				t.Logf("PERF %s", data)
			})
		}
	}
}
