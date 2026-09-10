// Package render demonstrates ordinary Go rendering of generated block variants.
package render

import (
	"fmt"
	"github.com/riducms/ridu/examples/blocks/generated"
	"html"
	"io"
)

// HTML renders single-locale page and campaign layouts using their shared block types.
// The element type is inferred from each generated list; no list conversion is needed.
// Go does not enforce exhaustiveness; the default deliberately reports new types.
func HTML[B interface{ BlockType() string }](w io.Writer, blocks []B) error {
	for _, value := range blocks {
		var markup string
		switch block := any(value).(type) {
		case *generated.Hero:
			markup = "<header><h1>" + text(block.Heading) + "</h1></header>"
		case *generated.Content:
			markup = "<section><h2>" + optionalText(block.Title) + "</h2></section>"
		case *generated.Media:
			markup = "<figure><figcaption>" + optionalText(block.Caption) + "</figcaption></figure>"
		case *generated.CTA:
			markup = "<aside>" + text(block.Label) + "</aside>"
		default:
			return fmt.Errorf("unhandled block variant %T", value)
		}
		if _, err := io.WriteString(w, markup); err != nil {
			return err
		}
	}
	return nil
}
func text(value *string) string {
	if value == nil {
		return ""
	}
	return html.EscapeString(*value)
}

func optionalText(value *generated.BlockOptional[string]) string {
	text, ok := value.Get()
	if !ok {
		return ""
	}
	return html.EscapeString(text)
}
