package richtext

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/riducms/ridu/store"
)

// Document is the portable rich-text envelope. T is the generated, field-specific
// block payload codec. It is not a map: configured variants retain their exact
// input/output types and discriminator through ordinary generated block codecs.
type Document[T any] struct {
	Version int     `json:"version"`
	Root    Node[T] `json:"root"`
}

// Node contains portable editor properties. Fields is present only for atomic
// block nodes and holds the field-specific generated payload union.
type Node[T any] struct {
	Type       string          `json:"type"`
	Version    int             `json:"version,omitempty"`
	Children   []Node[T]       `json:"children,omitempty"`
	Fields     *T              `json:"fields,omitempty"`
	Text       string          `json:"text,omitempty"`
	Format     json.RawMessage `json:"format,omitempty"`
	Detail     int             `json:"detail,omitempty"`
	Mode       string          `json:"mode,omitempty"`
	Style      string          `json:"style,omitempty"`
	TextFormat int             `json:"textFormat,omitempty"`
	TextStyle  string          `json:"textStyle,omitempty"`
	Direction  *string         `json:"direction,omitempty"`
	Indent     int             `json:"indent,omitempty"`
	Tag        string          `json:"tag,omitempty"`
	URL        string          `json:"url,omitempty"`
	Target     string          `json:"target,omitempty"`
	Rel        string          `json:"rel,omitempty"`
	Title      string          `json:"title,omitempty"`
	ListType   string          `json:"listType,omitempty"`
	Start      int             `json:"start,omitempty"`
	Value      int             `json:"value,omitempty"`
	Checked    *bool           `json:"checked,omitempty"`
	Language   string          `json:"language,omitempty"`
	Theme      string          `json:"theme,omitempty"`
	RelationTo string          `json:"relationTo,omitempty"`
	ID         string          `json:"id,omitempty"`
	Caption    string          `json:"caption,omitempty"`
}

// RenderDocument renders a typed document without loading an editor or fetching
// referenced records. The block callback receives the generated payload codec;
// applications dispatch its Value union by concrete generated type. Missing
// callbacks fail explicitly. Use RenderHTML for deliberate raw recovery tooling.
func RenderDocument[T any](document Document[T], block func(T) (string, error), nodes map[string]func(store.Values) (string, error)) (string, error) {
	data, err := json.Marshal(document)
	if err != nil {
		return "", err
	}
	var value store.Value
	if err := json.Unmarshal(data, &value); err != nil {
		return "", err
	}
	renderers := make(map[string]func(store.Values) (string, error), len(nodes)+1)
	for key, renderer := range nodes {
		renderers[key] = renderer
	}
	renderers["block"] = func(node store.Values) (string, error) {
		if block == nil {
			return "", fmt.Errorf("no renderer registered for rich-text block")
		}
		data, err := json.Marshal(node["fields"])
		if err != nil {
			return "", err
		}
		var payload T
		if err := json.Unmarshal(data, &payload); err != nil {
			return "", fmt.Errorf("decode rich-text block: %w", err)
		}
		return block(payload)
	}
	return RenderHTML(value, renderers)
}

// UnmarshalJSON rejects unsupported documents rather than dropping historical
// content while decoding. Export/migration tools should retain raw JSON when a
// typed decoder returns an error.
func (document *Document[T]) UnmarshalJSON(data []byte) error {
	type wire Document[T]
	var decoded wire
	if err := strictDecode(data, &decoded); err != nil {
		return fmt.Errorf("rich-text document: %w", err)
	}
	if decoded.Version != DocumentVersion {
		return fmt.Errorf("unsupported rich-text document version %d", decoded.Version)
	}
	if decoded.Root.Type != "root" {
		return fmt.Errorf("rich-text document root must have type root")
	}
	*document = Document[T](decoded)
	return nil
}

func (document Document[T]) MarshalJSON() ([]byte, error) {
	if document.Version != DocumentVersion || document.Root.Type != "root" {
		return nil, fmt.Errorf("rich-text document requires version 1 and a root node")
	}
	type wire Document[T]
	return json.Marshal(wire(document))
}

func (node *Node[T]) UnmarshalJSON(data []byte) error {
	type wire Node[T]
	var decoded struct {
		wire
		RawVersion json.RawMessage `json:"version"`
	}
	if err := strictDecode(data, &decoded); err != nil {
		return fmt.Errorf("rich-text node: %w", err)
	}
	if len(decoded.RawVersion) > 0 {
		if err := json.Unmarshal(decoded.RawVersion, &decoded.Version); err != nil || decoded.Version != DocumentVersion {
			return fmt.Errorf("rich-text node version must be 1 when provided")
		}
	}
	switch decoded.Type {
	case "block":
		var properties map[string]json.RawMessage
		if err := json.Unmarshal(data, &properties); err != nil {
			return err
		}
		if len(properties) != 3 || properties["type"] == nil || properties["version"] == nil || properties["fields"] == nil || decoded.Version != DocumentVersion || decoded.Fields == nil {
			return fmt.Errorf("rich-text block must contain only type block, version 1 and a typed fields payload")
		}
	case "root", "paragraph", "heading", "quote", "text", "linebreak", "link", "list", "listitem", "code", "horizontalrule", "upload", "relationship":
		if decoded.Fields != nil {
			return fmt.Errorf("rich-text node %q cannot contain a block payload", decoded.Type)
		}
		if !isContainerNode(decoded.Type) && decoded.Children != nil {
			return fmt.Errorf("rich-text node %q cannot contain editor children", decoded.Type)
		}
	default:
		return fmt.Errorf("unsupported rich-text node %q; retain raw JSON for export or migration", decoded.Type)
	}
	for _, child := range decoded.Children {
		if child.Type == "root" {
			return fmt.Errorf("root nodes cannot be nested inside a rich-text document")
		}
	}
	*node = Node[T](decoded.wire)
	return nil
}

func (node Node[T]) MarshalJSON() ([]byte, error) {
	type wire Node[T]
	if node.Version != 0 && node.Version != DocumentVersion {
		return nil, fmt.Errorf("rich-text node version must be 1 when provided")
	}
	for _, child := range node.Children {
		if child.Type == "root" {
			return nil, fmt.Errorf("root nodes cannot be nested inside a rich-text document")
		}
	}
	switch node.Type {
	case "block":
		if node.Version != DocumentVersion || node.Fields == nil || node.Children != nil {
			return nil, fmt.Errorf("rich-text block requires version 1 and typed fields, without editor children")
		}
		data, err := json.Marshal(wire(node))
		if err != nil {
			return nil, err
		}
		var properties map[string]json.RawMessage
		if err := json.Unmarshal(data, &properties); err != nil {
			return nil, err
		}
		if len(properties) != 3 {
			return nil, fmt.Errorf("rich-text block may contain only type, version and fields")
		}
		return data, nil
	case "root", "paragraph", "heading", "quote", "text", "linebreak", "link", "list", "listitem", "code", "horizontalrule", "upload", "relationship":
		if node.Fields != nil {
			return nil, fmt.Errorf("rich-text node %q cannot contain a block payload", node.Type)
		}
		if !isContainerNode(node.Type) && node.Children != nil {
			return nil, fmt.Errorf("rich-text node %q cannot contain editor children", node.Type)
		}
	default:
		return nil, fmt.Errorf("unsupported rich-text node %q; retain raw JSON for export or migration", node.Type)
	}
	// Shadow only fields that require an empty JSON value. Do not decode the
	// whole subtree again at each level: each child owns its single codec pass.
	var children *[]Node[T]
	var text *string
	switch node.Type {
	case "root", "paragraph", "heading", "quote", "link", "list", "listitem", "code":
		list := node.Children
		if list == nil {
			list = []Node[T]{}
		}
		children = &list
	default:
		if node.Children != nil {
			children = &node.Children
		}
	}
	if node.Type == "text" {
		text = &node.Text
	} else if node.Text != "" {
		text = &node.Text
	}
	return json.Marshal(struct {
		wire
		Children *[]Node[T] `json:"children,omitempty"`
		Text     *string    `json:"text,omitempty"`
	}{wire: wire(node), Children: children, Text: text})
}

func strictDecode(data []byte, into any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("expected one JSON value")
	}
	return nil
}
