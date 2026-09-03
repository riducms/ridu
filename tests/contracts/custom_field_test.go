package contracts_test

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

const colorPluginKey = "color"

var hexadecimalColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

type colorConfig struct {
	Palette []string `json:"palette"`
}

type colorPlugin struct{}

func (colorPlugin) Key() string { return colorPluginKey }

func (colorPlugin) FieldValidators() map[string]ridu.PluginFieldValidator {
	return map[string]ridu.PluginFieldValidator{colorPluginKey: validateColor}
}

func colorField(name string, config colorConfig, options ...field.PluginOption) field.Definition {
	encoded, err := json.Marshal(config)
	if err != nil {
		panic(err)
	}
	return field.Plugin(name, colorPluginKey, encoded, options...)
}

func validateColor(ctx ridu.PluginFieldValidationContext) []schema.Issue {
	value, valid := ctx.Value.StringValue()
	if !valid || !hexadecimalColor.MatchString(value) {
		return []schema.Issue{{
			Code:    "invalid_color",
			Path:    ctx.Field.Path.String(),
			Message: "color must use the #RRGGBB format",
		}}
	}
	return nil
}

func TestCustomFieldCarriesConfigAndUsesItsRuntimeValidator(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name:    "Custom field contract",
		Plugins: []ridu.Plugin{colorPlugin{}},
		Collections: []ridu.Collection{{
			Slug: "brands",
			Fields: []field.Definition{
				colorField(
					"accent",
					colorConfig{Palette: []string{"#663399", "#FFFFFF"}},
					field.Required(),
				),
			},
		}},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}

	manifestField := application.Manifest().Snapshot().Collections[0].Fields[0]
	if manifestField.ID != "brands-accent" {
		t.Fatalf("derived custom field ID = %q, want brands-accent", manifestField.ID)
	}
	if manifestField.Plugin == nil || manifestField.Plugin.Key != colorPluginKey {
		t.Fatalf("plugin field = %#v", manifestField.Plugin)
	}
	var config colorConfig
	if err := json.Unmarshal(manifestField.Plugin.Config, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Palette) != 2 || config.Palette[0] != "#663399" {
		t.Fatalf("plugin config = %#v", config)
	}

	document, err := application.Local().Create(
		context.Background(),
		"brands",
		store.Values{"accent": store.String("#663399")},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := document.Values["accent"].StringValue(); value != "#663399" {
		t.Fatalf("stored accent = %q", value)
	}

	_, err = application.Local().Create(
		context.Background(),
		"brands",
		store.Values{"accent": store.String("purple")},
		nil,
	)
	var operationError *ridu.OperationError
	if !errors.As(err, &operationError) || len(operationError.Issues) != 1 {
		t.Fatalf("invalid color error = %#v", err)
	}
	if issue := operationError.Issues[0]; issue.Code != "invalid_color" || issue.Path != "accent" {
		t.Fatalf("invalid color issue = %#v", issue)
	}
}
