package content

import (
	"strings"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
)

func formatCode(
	_ operation.Context,
	value operation.Value[string],
) (operation.Change[string], error) {
	code, present := value.Get()
	if !present {
		return operation.Keep[string](), nil
	}
	// In an AfterRead hook, Set changes the response, not storage.
	return operation.Set(strings.ToUpper(code)), nil
}

var DisplayCode = field.Text("displayCode").AfterRead(formatCode)
