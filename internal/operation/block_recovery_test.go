package operation

import (
	"fmt"
	"testing"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestAdapterSchemaRecoveryKeepsActionableOperationFailure(t *testing.T) {
	failure := translateStoreError(fmt.Errorf("decode stored document: %w", &store.SchemaRecoveryError{Issues: []schema.Issue{{Code: "unknown_block_schema", Path: "layout.2.blockType", Message: "Restore the missing block schema or migrate the stored document before using it."}}}))
	result, ok := failure.(*Error)
	if !ok || result.Status != 409 || result.Code != "block_recovery_required" || len(result.Issues) != 1 || result.Issues[0].Path != "layout.2.blockType" {
		t.Fatalf("recovery failure = %#v", failure)
	}
}
