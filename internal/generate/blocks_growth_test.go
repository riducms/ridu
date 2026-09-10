package generate

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/typescript"
	"github.com/riducms/ridu/schema"
)

func TestReusableBlocksGeneratedGrowth(t *testing.T) {
	hero := field.Block{TypeName: "Hero", Slug: "hero", Fields: field.Fields{field.Text("heading").Required(), field.Text("description"), field.Group("settings", field.Fields{field.Checkbox("wide"), field.Number("spacing")}), field.Array("links", field.Fields{field.Text("label").Required(), field.Text("url").Required()}), field.Blocks("children", field.Block{Slug: "note", Fields: field.Fields{field.Text("body")}})}}
	checkRepeatedDefinitionGrowth(t, "Hero", func(count int) schema.Manifest {
		fields := make(field.Fields, count)
		for index := range fields {
			fields[index] = field.Blocks(fmt.Sprintf("layout%d", index), hero)
		}
		manifest, err := core.Resolve(core.Config{Name: "Block growth", Collections: []core.Collection{{Slug: "pages", Fields: fields}}})
		if err != nil {
			t.Fatal(err)
		}
		return manifest
	})
}

// The size matrix is a cheap structural contract. Compile the repeated (10-use)
// case once; explicit performance qualification compiles all three workloads.
// Timing is diagnostic only: shared-runner latency is not a correctness oracle.
func checkRepeatedDefinitionGrowth(t *testing.T, namedType string, resolve func(int) schema.Manifest) {
	t.Helper()
	sizes := make(map[string][]int)
	performance := os.Getenv("RIDU_TEST_GENERATION_PERF") == "true"
	for _, count := range []int{1, 10, 50} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			start := time.Now()
			manifest := resolve(count)
			manifestBytes, err := manifest.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			ts, err := typescript.Client(manifest)
			if err != nil {
				t.Fatal(err)
			}
			source, err := goClient(manifest)
			if err != nil {
				t.Fatal(err)
			}
			api, err := openAPI(manifest)
			if err != nil {
				t.Fatal(err)
			}
			for name, content := range map[string][]byte{"manifest": manifestBytes, "TypeScript": ts, "Go": source, "OpenAPI": api} {
				sizes[name] = append(sizes[name], len(content))
			}
			// Shared authored block definitions must not multiply with their placements.
			if got := strings.Count(string(source), "type "+namedType+" struct {"); got != 1 {
				t.Fatalf("shared Go definition count = %d, want 1", got)
			}
			t.Logf("placements=%d manifest=%dB TypeScript=%dB Go=%dB OpenAPI=%dB generation=%s", count, len(manifestBytes), len(ts), len(source), len(api), time.Since(start).Round(time.Millisecond))
			if count != 10 && !performance {
				return
			}
			start = time.Now()
			compileRichTextGoConsumer(t, source, "package generated_test\n")
			t.Logf("placements=%d external Go compile=%s", count, time.Since(start).Round(time.Millisecond))
			start = time.Now()
			compileRichTextTSConsumerSource(t, ts, "")
			t.Logf("placements=%d external TypeScript check=%s", count, time.Since(start).Round(time.Millisecond))
		})
	}
	for name, measured := range sizes {
		if len(measured) != 3 {
			t.Errorf("%s growth cases incomplete", name)
			continue
		}
		// Allow doubled marginal bytes per placement for longer numeric names and
		// future fixed-width contracts, but reject quadratic/explosive duplication.
		first, later := measured[1]-measured[0], measured[2]-measured[1]
		if first <= 0 || later <= 0 || int64(later)*9 > int64(first)*40*2 {
			t.Errorf("%s generated growth is not bounded per placement: 1=%d, 10=%d, 50=%d bytes", name, measured[0], measured[1], measured[2])
		}
	}
}
