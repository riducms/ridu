package content

import (
	"strings"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

func trimText(
	_ operation.Context,
	value operation.Value[store.Value],
) (operation.Change[store.Value], error) {
	raw, present := value.Get()
	if !present {
		// An update may omit this field; leave it unchanged.
		return operation.Keep[store.Value](), nil
	}
	text, valid := raw.StringValue()
	if !valid {
		// Let Ridu validate values that are not strings.
		return operation.Keep[store.Value](), nil
	}
	// Present wraps the new value; Replace applies it to this field.
	return operation.Replace(
		operation.Present(store.String(strings.TrimSpace(text))),
	), nil
}

var Posts = ridu.Collection{
	Slug: "posts",
	Fields: field.Fields{
		field.Text("title").Required().Hooks(field.Hooks[string]{
			BeforeValidate: []field.RawTransform{trimText},
		}),
	},
}
