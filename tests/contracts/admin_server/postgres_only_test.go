package main

import (
	"os/exec"
	"strings"
	"testing"
)

// The performance comparison measures the postgresonly build as a generated
// PostgreSQL application; it must not link the other official adapters.
func TestPostgresOnlyBuildLinksOnlyThePostgreSQLAdapter(t *testing.T) {
	output, err := exec.Command("go", "list", "-tags", "postgresonly", "-deps", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, output)
	}
	dependencies := strings.Fields(string(output))
	linked := func(prefix string) bool {
		for _, dependency := range dependencies {
			if strings.HasPrefix(dependency, prefix) {
				return true
			}
		}
		return false
	}
	for _, excluded := range []string{"github.com/riducms/ridu/adapters/sqlite", "github.com/riducms/ridu/adapters/mongodb", "modernc.org/sqlite", "go.mongodb.org/mongo-driver"} {
		if linked(excluded) {
			t.Errorf("postgresonly build links %s", excluded)
		}
	}
	if !linked("github.com/riducms/ridu/adapters/postgres") {
		t.Error("postgresonly build does not link the PostgreSQL adapter")
	}
}
