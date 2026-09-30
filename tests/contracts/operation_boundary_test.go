package contracts_test

import (
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/core"
	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/operation"
)

// Resource callbacks, field callbacks and execution must share one named type,
// so applications can reuse operation predicates without conversions.
func TestOperationVocabularyCrossesEveryCallbackBoundary(t *testing.T) {
	allowUpdate := func(kind operation.Kind) bool { return kind == operation.Update }
	resource := ridu.AccessContext{Operation: operation.Update}
	hook := core.HookContext{Operation: resource.Operation}
	field := operation.Context{Operation: hook.Operation}
	effect := core.AfterCommitEffect{Operation: field.Operation}
	request := operationengine.Request{Operation: effect.Operation}
	for _, kind := range []operation.Kind{resource.Operation, hook.Operation, field.Operation, effect.Operation, request.Operation} {
		if !allowUpdate(kind) {
			t.Fatalf("callback changed operation: %s", kind)
		}
	}
}
