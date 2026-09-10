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

func TestRichTextBlocksEnginePerformance(t *testing.T) {
	richtextblocks.RunPerformance(t, "memory", memoryRichTextBlocksFactory)
}

func memoryRichTextBlocksFactory(t *testing.T, config ridu.Config) (store.Store, *ridu.App) {
	t.Helper()
	backend := teststore.New()
	app, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	return backend, app
}
