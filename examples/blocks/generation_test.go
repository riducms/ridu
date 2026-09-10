package blocks_test

import (
	"context"
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/internal/generate"
	"github.com/riducms/ridu/internal/projectfile"
	"testing"
)

func TestReferenceGeneratedContractsHaveNoDrift(t *testing.T) {
	definition, err := projectfile.Discover(".")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := generate.Run(context.Background(), definition, ridu.FrameworkVersion, true); err != nil {
		t.Fatal(err)
	}
}
