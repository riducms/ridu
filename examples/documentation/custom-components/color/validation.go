package color

import (
	"regexp"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/schema"
)

var hexadecimal = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

func (plugin) FieldValidators() map[string]ridu.PluginFieldValidator {
	return map[string]ridu.PluginFieldValidator{Key: validateColor}
}

func validateColor(
	ctx ridu.PluginFieldValidationContext,
) []schema.Issue {
	value, ok := ctx.Value.StringValue()
	if ok && hexadecimal.MatchString(value) {
		// No issues means the value passed validation.
		return nil
	}
	// RuntimePath identifies the exact field, including array rows.
	return []schema.Issue{{
		Code:    "invalid_color",
		Path:    ctx.RuntimePath,
		Message: "color must use the #RRGGBB format",
	}}
}
