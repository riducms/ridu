package blocks_test

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/sqlite"
	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/examples/blocks/content"
)

func TestReferenceSDKCreatesThroughHTTP(t *testing.T) { referenceHTTPConsumer(t, "scripts/seed.ts") }

func TestReferenceBrowserRoundTrip(t *testing.T) {
	referenceHTTPConsumer(t, "scripts/verify-browser.ts")
}

func referenceHTTPConsumer(t *testing.T, script string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	backend, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "sdk.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	config := content.Config()
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Migrate(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	app, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(app.Handler(ridu.HandlerOptions{}))
	defer server.Close()
	// Apply the example source mappings to nested SDK imports as well.
	command := exec.CommandContext(ctx, "bun", "run", "--tsconfig-override", "./tsconfig.json", script)
	command.Env = append(os.Environ(), "RIDU_URL="+server.URL)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("reference SDK HTTP creation: %v\n%s", err, output)
	}
	// Bun can print usage and exit successfully for misplaced CLI options.
	// Require the script's HTTP side effect so that cannot become a false pass.
	pages, err := app.Local().List(ctx, "pages", core.ListOptions{})
	if err != nil {
		t.Fatalf("read pages created by reference script: %v", err)
	}
	if len(pages.Documents) == 0 {
		t.Fatal("reference script exited without creating a page through HTTP")
	}
}
