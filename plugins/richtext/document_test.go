package richtext_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/riducms/ridu/plugins/richtext"
)

type typedCallout struct {
	Key       string `json:"_key"`
	BlockType string `json:"blockType"`
	Title     string `json:"title"`
}

func TestTypedDocumentCodecAndRenderer(t *testing.T) {
	payload := typedCallout{Key: "one", BlockType: "callout", Title: "Hello"}
	document := richtext.Document[typedCallout]{Version: 1, Root: richtext.Node[typedCallout]{Type: "root", Children: []richtext.Node[typedCallout]{{Type: "paragraph", Children: []richtext.Node[typedCallout]{{Type: "text", Text: "Safe <text>"}}}, {Type: "block", Version: 1, Fields: &payload}}}}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	var decoded richtext.Document[typedCallout]
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	rendered, err := richtext.RenderDocument(decoded, func(block typedCallout) (string, error) {
		if block.BlockType != "callout" {
			return "", fmt.Errorf("unknown block %s", block.BlockType)
		}
		return "<aside>" + block.Title + "</aside>", nil
	}, nil)
	if err != nil || rendered != "<p>Safe &lt;text&gt;</p><aside>Hello</aside>" {
		t.Fatalf("%q %v", rendered, err)
	}
	if _, err := richtext.RenderDocument(decoded, nil, nil); err == nil {
		t.Fatal("missing renderer silently omitted a block")
	}
	for _, raw := range []string{
		`{"version":1,"root":{"type":"root","children":[{"type":"retired","secret":"keep raw"}]}}`,
		`{"version":1,"root":{"type":"root","children":[{"type":"block","version":1,"blockType":"legacy","fields":{}}]}}`,
		`{"version":1,"root":{"type":"root","children":[{"type":"block","version":1,"children":[],"fields":{}}]}}`,
	} {
		var value richtext.Document[typedCallout]
		if err := json.Unmarshal([]byte(raw), &value); err == nil {
			t.Fatalf("unknown content decoded and lost: %s", raw)
		}
	}
	emptyText, err := json.Marshal(richtext.Node[typedCallout]{Type: "text"})
	if err != nil || !strings.Contains(string(emptyText), `"text":""`) {
		t.Fatalf("empty text = %s %v", emptyText, err)
	}
	empty, err := json.Marshal(richtext.Document[typedCallout]{Version: 1, Root: richtext.Node[typedCallout]{Type: "root"}})
	if err != nil || !strings.Contains(string(empty), `"children":[]`) {
		t.Fatalf("empty document = %s %v", empty, err)
	}
}

func TestMapDocumentBlocksRetainsProseWithoutAliasingOrLosingInvalidPayloads(t *testing.T) {
	payload := typedCallout{Key: "one", BlockType: "callout", Title: "Hello"}
	direction := "ltr"
	document := richtext.Document[typedCallout]{Version: 1, Root: richtext.Node[typedCallout]{Type: "root", Direction: &direction, Children: []richtext.Node[typedCallout]{{Type: "paragraph", TextFormat: 1, Children: []richtext.Node[typedCallout]{{Type: "text", Text: "Original", Format: json.RawMessage("1")}}}, {Type: "block", Version: 1, Fields: &payload}}}}
	convert := func(value typedCallout) (string, error) { return value.Key, nil }
	converted, err := richtext.MapDocumentBlocks(document, convert)
	if err != nil || *converted.Root.Children[1].Fields != "one" || converted.Root.Children[0].TextFormat != 1 {
		t.Fatalf("converted = %#v, %v", converted, err)
	}
	converted.Root.Children[0].Children[0].Text = "Edited"
	converted.Root.Children[0].Children[0].Format[0] = '2'
	*converted.Root.Direction = "rtl"
	if document.Root.Children[0].Children[0].Text != "Original" || string(document.Root.Children[0].Children[0].Format) != "1" || *document.Root.Direction != "ltr" {
		t.Fatal("conversion mutated the original editor tree")
	}
	for _, node := range []richtext.Node[typedCallout]{
		{Type: "paragraph", Fields: &payload},
		{Type: "block", Version: 1, Fields: &payload, Children: []richtext.Node[typedCallout]{}},
		{Type: "block", Version: 2, Fields: &payload},
		{Type: "retired"},
	} {
		document.Root.Children = []richtext.Node[typedCallout]{node}
		if _, err := richtext.MapDocumentBlocks(document, convert); err == nil {
			t.Fatalf("conversion discarded invalid %s node data", node.Type)
		}
	}
}
