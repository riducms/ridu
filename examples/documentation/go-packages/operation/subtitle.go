package content

import (
	"strings"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
)

var Subtitle = field.Text("subtitle").Hooks(field.Hooks[string]{
	BeforeChange: []field.Transform[string]{cleanSubtitle},
})

func cleanSubtitle(
	_ operation.Context,
	value operation.Value[string],
) (operation.Change[string], error) {
	subtitle, present := value.Get()
	if !present {
		// There is no value to clean up; keep its current empty state.
		return operation.Keep[string](), nil
	}
	subtitle = strings.TrimSpace(subtitle)
	if subtitle == "" {
		// Clear this optional field instead of saving spaces.
		return operation.Replace(operation.Empty[string]()), nil
	}
	// Replace supplies this field's new value for the same save.
	return operation.Replace(operation.Present(subtitle)), nil
}
