package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riducms/ridu/internal/cli"
)

func TestUpgradeRewritesEveryRiduPinWithoutInstalling(t *testing.T) {
	root := t.TempDir()
	write := func(path, contents string) {
		t.Helper()
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("ridu.toml", "version = 1\ndatabase = \"postgres\"\npackage_manager = \"bun\"\nentry = \"./cmd/server\"\nadmin = \"./admin\"\nschema = \"./generated/ridu.schema.json\"\nclient = \"./generated/ridu.generated.ts\"\nmigrations = \"./migrations\"\nplugins = \"./ridu.plugins.json\"\nplugin_go = \"./content/ridu_plugins.generated.go\"\n")
	write("go.mod", "module example.com/app\n\ngo 1.25\n\nrequire github.com/riducms/ridu v0.4.0\n")
	write("package.json", "{\n\t\"name\": \"app\",\n\t\"dependencies\": {\n\t\t\"@riducms/sdk\": \"0.4.0\",\n\t\t\"svelte\": \"5.0.0\"\n\t},\n\t\"devDependencies\": { \"@riducms/cli\": \"0.4.0\" }\n}\n")
	write("apps/web/package.json", "{\n  \"dependencies\": {\n    \"@riducms/sveltekit\": \"0.4.0\",\n    \"@riducms/ui\": \"workspace:*\"\n  }\n}\n")
	write("node_modules/@riducms/sdk/package.json", "{\"name\": \"@riducms/sdk\", \"dependencies\": {\"@riducms/protocol\": \"0.4.0\"}}\n")
	write("apps/web/.vercel/output/functions/page.func/package.json", "{\"dependencies\": {\"@riducms/sdk\": \"0.4.0\"}}\n")
	write("ridu.plugins.json", `{
  "version": 1,
  "plugins": [
    {
      "key": "richtext",
      "goPackage": "github.com/riducms/ridu/plugins/richtext",
      "goVersion": "v0.4.0",
      "constructor": "New",
      "adminPackage": "@riducms/plugin-richtext",
      "adminVersion": "0.4.0"
    }
  ]
}
`)

	var stdout, stderr bytes.Buffer
	if code := cli.Run(t.Context(), []string{"upgrade", "v0.5.0", "--no-install"}, &stdout, &stderr, cli.Options{WorkingDirectory: root}); code != 0 {
		t.Fatalf("upgrade exit %d\nstdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}
	read := func(path string) string {
		t.Helper()
		contents, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		return string(contents)
	}
	if got := read("package.json"); got != "{\n\t\"name\": \"app\",\n\t\"dependencies\": {\n\t\t\"@riducms/sdk\": \"0.5.0\",\n\t\t\"svelte\": \"5.0.0\"\n\t},\n\t\"devDependencies\": { \"@riducms/cli\": \"0.5.0\" }\n}\n" {
		t.Fatalf("root package.json was not rewritten in place:\n%s", got)
	}
	web := read("apps/web/package.json")
	if !strings.Contains(web, `"@riducms/sveltekit": "0.5.0"`) || !strings.Contains(web, `"@riducms/ui": "workspace:*"`) {
		t.Fatalf("workspace package.json = %s", web)
	}
	if !strings.Contains(read("node_modules/@riducms/sdk/package.json"), `"0.4.0"`) {
		t.Fatal("upgrade rewrote an installed dependency")
	}
	if !strings.Contains(read("apps/web/.vercel/output/functions/page.func/package.json"), `"0.4.0"`) {
		t.Fatal("upgrade rewrote build output in a hidden directory")
	}
	plugins := read("ridu.plugins.json")
	if !strings.Contains(plugins, `"goVersion": "v0.5.0"`) || !strings.Contains(plugins, `"adminVersion": "0.5.0"`) {
		t.Fatalf("plugin registry = %s", plugins)
	}
	if !fileContains(t, filepath.Join(root, "content", "ridu_plugins.generated.go"), "github.com/riducms/ridu/plugins/richtext") {
		t.Fatal("plugin registration source was not written")
	}
	if !strings.Contains(read("go.mod"), "github.com/riducms/ridu v0.5.0") {
		t.Fatalf("go.mod = %s", read("go.mod"))
	}
	for _, want := range []string{"workspace:*", "ridu migrate create --name ridu-0-5-0"} {
		if !strings.Contains(stdout.String()+stderr.String(), want) {
			t.Fatalf("output is missing %q:\n%s\n%s", want, stdout.String(), stderr.String())
		}
	}
}

func fileContains(t *testing.T, path, text string) bool {
	t.Helper()
	contents, err := os.ReadFile(path)
	return err == nil && strings.Contains(string(contents), text)
}
