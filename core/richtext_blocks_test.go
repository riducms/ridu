package core_test

import (
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/store"
	richtextblocks "github.com/riducms/ridu/tests/contracts/richtext_blocks"
)

func TestRichTextBlocksEngineAcceptance(t *testing.T) {
	richtextblocks.Run(t, memoryRichTextBlocksFactory)
}

func TestRichTextBlockReferencesEngineAcceptance(t *testing.T) {
	richtextblocks.RunReferences(t, memoryRichTextBlocksFactory)
}

func BenchmarkRichTextBlocksEnginePerformance(b *testing.B) {
	richtextblocks.RunPerformanceBenchmark(b, "memory", memoryRichTextBlocksBackend)
}

func memoryRichTextBlocksFactory(t *testing.T, config ridu.Config) (store.Store, *ridu.App) {
	return memoryRichTextBlocksBackend(t, config)
}

func memoryRichTextBlocksBackend(t testing.TB, config ridu.Config) (store.Store, *ridu.App) {
	t.Helper()
	backend := teststore.New()
	app, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	return backend, app
}
