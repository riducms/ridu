package render_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/riducms/ridu/examples/blocks/generated"
	"github.com/riducms/ridu/examples/blocks/render"
	"github.com/riducms/ridu/plugins/richtext"
)

func TestTypedNestedArticleRendering(t *testing.T) {
	const raw = `{"version":1,"root":{"type":"root","children":[{"type":"block","version":1,"fields":{"blockType":"callout","_key":"callout","title":"Read <first>","detail":{"version":1,"root":{"type":"root","children":[{"type":"block","version":1,"fields":{"blockType":"cta","_key":"cta","label":"Continue"}}]}}}},{"type":"block","version":1,"fields":{"blockType":"media","_key":"media","caption":"Photo","asset":{"id":"image","createdAt":"2026-09-05T00:00:00Z","updatedAt":"2026-09-05T00:00:00Z","title":"Image","url":"/image.png"}}}]}}`
	var document richtext.Document[generated.ArticlesBodyBlocksBlockPayload]
	if err := json.Unmarshal([]byte(raw), &document); err != nil {
		t.Fatal(err)
	}
	media, ok := document.Root.Children[1].Fields.Value.(*generated.Media)
	if !ok || media.Asset == nil || media.Asset.Document == nil || media.Asset.Document.Title == nil || *media.Asset.Document.Title != "Image" {
		t.Fatal("populated typed output lost")
	}
	var output strings.Builder
	if err := render.ArticleHTML(&output, document); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Read &lt;first&gt;") || !strings.Contains(output.String(), "Continue</aside>") || !strings.Contains(output.String(), "Photo</figcaption>") {
		t.Fatal(output.String())
	}
}
