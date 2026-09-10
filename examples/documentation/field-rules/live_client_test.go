package content

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/internal/typescript"
)

var updateLiveClient = flag.Bool("update-live-client", false, "regenerate the live validation documentation client")

func TestLiveValidationClientMatchesCatalog(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{Name: "Live validation example", Collections: Catalog()})
	if err != nil {
		t.Fatal(err)
	}
	want, err := typescript.Client(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join("..", "live-validation", "generated", "ridu.generated.ts")
	if *updateLiveClient {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("live validation client is stale; run go test ./examples/documentation/field-rules -run TestLiveValidationClientMatchesCatalog -update-live-client")
	}
}
