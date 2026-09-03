package typescript_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/riducms/ridu/internal/typescript"
)

func TestProtocolTypeScriptIsCurrent(t *testing.T) {
	root := moduleRoot(t)
	path := filepath.Join(root, "packages", "protocol", "src", "generated.ts")
	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read generated protocol %s: %v\nactual:\n%s", path, err, typescript.Protocol())
	}
	actual := typescript.Protocol()
	if string(actual) != string(expected) {
		t.Fatalf("generated protocol drift; regenerate %s\nexpected:\n%s\nactual:\n%s", path, expected, actual)
	}
	if strings.Contains(string(actual), " any") || strings.Contains(string(actual), "any[]") {
		t.Fatal("generated public protocol contains any")
	}
	for _, contract := range []string{
		"nameTranslations?: Record<string, string>",
		"singularTranslations?: Record<string, string>",
		"labelTranslations?: Record<string, string>",
		"descriptionTranslations?: Record<string, string>",
		"placeholderTranslations?: Record<string, string>",
		"tabTranslations?: Record<string, string>",
		"rowLabels?: SchemaArrayRowLabels",
		"rowLabelComponent?: SchemaFieldAdminComponent",
		"slug?: SchemaSlugField",
		"sourcePath: string",
	} {
		if !strings.Contains(string(actual), contract) {
			t.Fatalf("generated public protocol is missing localized display contract %q", contract)
		}
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}
