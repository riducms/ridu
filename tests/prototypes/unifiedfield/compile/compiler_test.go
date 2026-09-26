// Package compile_test executes the compiler against intentionally invalid
// external consumers. Fixture text is copied to temporary .go source files so
// intentionally invalid syntax cannot break repository-wide gofmt checks. Each
// failure must match its specific prototype API constraint.
package compile_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompilerRejectsUnsupportedContracts(t *testing.T) {
	probes := []struct {
		name        string
		diagnostics []string
	}{
		{"embedded_context", []string{"operation.Context", "as context.Context", "missing method Deadline"}},
		{"concrete_slice", []string{"cannot use []field.TextField", "as field.Fields"}},
		{"node_scalar_method", []string{"node.MaxLength undefined", "field.Node has no field or method MaxLength"}},
		{"external_node", []string{"does not implement field.Node (unexported method snapshot)"}},
		{"wrong_validator", []string{"operation.Value[int]", "as field.Validator[string]"}},
		{"wrong_write_hook", []string{"operation.Value[float64]", "as field.Transform[string]"}},
		{"wrong_read_hook", []string{"operation.Value[operation.ID]", "as field.OutputTransform[operation.ReferenceOutput]"}},
		{"wrong_reference_hook", []string{"operation.Value[string]", "as field.Transform[operation.ID]"}},
		{"wrong_raw_hook", []string{"operation.Value[any]", "as field.RawTransform"}},
	}
	for _, probe := range probes {
		t.Run(probe.name, func(t *testing.T) {
			source, err := os.ReadFile(filepath.Join("testdata", probe.name, "probe.go.txt"))
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "probe.go")
			if err := os.WriteFile(path, source, 0o600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "go", "test", "-run=^$", path)
			cmd.Env = append(os.Environ(), "GOWORK=off")
			output, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("compiler probe timed out: %s", output)
			}
			if err == nil {
				t.Fatalf("expected the compiler to reject this contract; output: %s", output)
			}
			for _, diagnostic := range probe.diagnostics {
				if !strings.Contains(string(output), diagnostic) {
					t.Fatalf("wanted compiler diagnostic %q, got:\n%s", diagnostic, output)
				}
			}
		})
	}
}
