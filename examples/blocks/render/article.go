package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/riducms/ridu/examples/blocks/generated"
	"github.com/riducms/ridu/plugins/richtext"
)

// ArticleHTML renders typed article payloads and nested rich text without any
// reference fetches. The caller supplies access-approved populated values.
func ArticleHTML(w io.Writer, document richtext.Document[generated.ArticlesBodyBlocksBlockPayload]) error {
	markup, err := richtext.RenderDocument(document, func(payload generated.ArticlesBodyBlocksBlockPayload) (string, error) {
		switch block := payload.Value.(type) {
		case *generated.Callout:
			var output strings.Builder
			output.WriteString("<aside><h2>" + text(block.Title) + "</h2><p>" + optionalText(block.Message) + "</p>")
			if detail, ok := block.Detail.Get(); ok {
				nested, err := richtext.RenderDocument(detail, func(payload generated.CalloutDetailBlocksBlockPayload) (string, error) {
					switch block := payload.Value.(type) {
					case *generated.CTA:
						return "<aside>" + text(block.Label) + "</aside>", nil
					default:
						return "", fmt.Errorf("unhandled nested block %T", block)
					}
				}, nil)
				if err != nil {
					return "", err
				}
				output.WriteString(nested)
			}
			if aside, ok := block.Aside.Get(); ok {
				nested, err := richtext.RenderDocument(aside, nil, nil)
				if err != nil {
					return "", err
				}
				output.WriteString(nested)
			}
			output.WriteString("</aside>")
			return output.String(), nil
		case *generated.Media:
			return "<figure><figcaption>" + optionalText(block.Caption) + "</figcaption></figure>", nil
		case *generated.CTA:
			return "<aside>" + text(block.Label) + "</aside>", nil
		default:
			return "", fmt.Errorf("unhandled article block %T", block)
		}
	}, nil)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, markup)
	return err
}
