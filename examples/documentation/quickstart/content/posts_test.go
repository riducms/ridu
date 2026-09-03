package content

import (
	"testing"

	"github.com/riducms/ridu"
)

func TestQuickstartConfigResolves(t *testing.T) {
	if _, err := ridu.Resolve(Config()); err != nil {
		t.Fatalf("resolve documentation Quickstart config: %v", err)
	}
}
