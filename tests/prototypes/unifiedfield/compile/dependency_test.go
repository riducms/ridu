package compile_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestActualPackageDependencies(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "list", "-deps", "-test", "-json", "../field", "../operation", "../core", "../consumer")
	cmd.Env = append(os.Environ(), "GOWORK=off")
	output, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			t.Fatalf("go list failed: %s", exit.Stderr)
		}
		t.Fatal(err)
	}
	packages := map[string][]string{}
	decoder := json.NewDecoder(bytes.NewReader(output))
	for {
		var pkg struct {
			ImportPath string
			Imports    []string
		}
		if err := decoder.Decode(&pkg); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		// go list adds a bracketed test-binary identity to augmented test
		// packages. Fold these into their source package for the import graph.
		path := strings.Split(pkg.ImportPath, " [")[0]
		for _, dependency := range pkg.Imports {
			dependency = strings.Split(dependency, " [")[0]
			if !slices.Contains(packages[path], dependency) {
				packages[path] = append(packages[path], dependency)
			}
		}
		if pkg.ImportPath == "github.com/riducms/ridu" ||
			pkg.ImportPath == "github.com/riducms/ridu/core" ||
			pkg.ImportPath == "github.com/riducms/ridu/field" ||
			strings.HasPrefix(pkg.ImportPath, "github.com/riducms/ridu/internal/") {
			t.Fatalf("lower prototype graph imports forbidden runtime/authoring package %s", pkg.ImportPath)
		}
	}
	for _, edge := range [][2]string{
		{"example.com/ridu-gate1/consumer", "example.com/ridu-gate1/core"},
		{"example.com/ridu-gate1/consumer", "example.com/ridu-gate1/field"},
		{"example.com/ridu-gate1/core", "example.com/ridu-gate1/field"},
		{"example.com/ridu-gate1/field", "example.com/ridu-gate1/operation"},
		{"example.com/ridu-gate1/operation", "github.com/riducms/ridu/store"},
	} {
		if !slices.Contains(packages[edge[0]], edge[1]) {
			t.Errorf("missing required actual import %s -> %s", edge[0], edge[1])
		}
	}
	for _, path := range []string{
		"example.com/ridu-gate1/consumer", "example.com/ridu-gate1/core",
		"example.com/ridu-gate1/field", "example.com/ridu-gate1/operation",
		"github.com/riducms/ridu/store", "github.com/riducms/ridu/schema", "github.com/riducms/ridu/query",
	} {
		for _, dependency := range packages[path] {
			if strings.Contains(dependency, "ridu") {
				t.Logf("%s -> %s", path, dependency)
			}
		}
	}
}
