package generate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/typescript"
	"github.com/riducms/ridu/tests/contracts/blockreferences"
)

func TestBlockRegistryExactGeneratedContracts(t *testing.T) {
	var expected [][]byte
	for _, references := range []bool{false, true} {
		manifest, err := core.Resolve(blockreferences.Config(references))
		if err != nil {
			t.Fatal(err)
		}
		ts, err := typescript.Client(manifest)
		if err != nil {
			t.Fatal(err)
		}
		goSource, err := goClient(manifest)
		if err != nil {
			t.Fatal(err)
		}
		api, err := openAPI(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Count(string(goSource), "type Card struct {") != 1 {
			t.Fatal("shared Go contract repeated")
		}
		ts = regexp.MustCompile(`manifest-[0-9a-f]{64}`).ReplaceAll(ts, []byte("manifest-DIGEST"))
		// Registry and embedded descriptors are metadata; compare exact standard OpenAPI contracts.
		var document map[string]any
		if err := json.Unmarshal(api, &document); err != nil {
			t.Fatal(err)
		}
		var strip func(any)
		strip = func(value any) {
			switch v := value.(type) {
			case map[string]any:
				delete(v, "x-ridu-blocks")
				delete(v, "x-ridu-embeddedTrees")
				for _, child := range v {
					strip(child)
				}
			case []any:
				for _, child := range v {
					strip(child)
				}
			}
		}
		strip(document)
		api, _ = json.Marshal(document)
		outputs := [][]byte{ts, goSource, api}
		if !references {
			expected = outputs
		} else {
			for i, output := range outputs {
				if !bytes.Equal(expected[i], output) {
					t.Errorf("inline/reference contract %d differs", i)
				}
			}
			compileRichTextGoConsumer(t, goSource, "package generated_test\n")
			compileRichTextTSConsumerSource(t, ts, "")
		}
	}
}
func TestBlockRegistryManifestScale(t *testing.T) {
	var sizes []int
	for _, references := range []bool{false, true} {
		definition := field.Block{Slug: "card", TypeName: "Card"}
		for i := range 40 {
			definition.Fields = append(definition.Fields, field.Text(fmt.Sprintf("text%d", i)))
		}
		config := core.Config{Name: "Scale"}
		fields := field.Fields{}
		for i := range 100 {
			b := field.Blocks(fmt.Sprintf("layout%d", i), definition)
			if references {
				b = field.Blocks(fmt.Sprintf("layout%d", i)).References("card")
			}
			fields = append(fields, b)
		}
		config.Collections = []core.Collection{{Slug: "pages", Fields: fields}}
		if references {
			config.Blocks = []field.Block{definition}
		}
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		start := time.Now()
		manifest, err := core.Resolve(config)
		if err != nil {
			t.Fatal(err)
		}
		elapsed := time.Since(start)
		runtime.ReadMemStats(&after)
		data, _ := manifest.Bytes()
		sizes = append(sizes, len(data))
		t.Logf("references=%v placements=100 fields/definition=40 manifest=%dB resolution=%s allocated=%dB", references, len(data), elapsed, after.TotalAlloc-before.TotalAlloc)
	}
	if sizes[1] >= sizes[0]/10 {
		t.Fatalf("registry size benefit missing: %v", sizes)
	}
}
