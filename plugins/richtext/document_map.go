package richtext

import "fmt"

// MapDocumentBlocks copies the editor tree while explicitly converting each
// typed block payload. Pass the generated output payload's Retain method to make
// an update document: only block identities are retained, so omitted, redacted,
// localized and populated fields are never copied into a write. Edit returned
// update payloads and save with the parent document's revision.
func MapDocumentBlocks[From, To any](document Document[From], convert func(From) (To, error)) (Document[To], error) {
	if document.Version != DocumentVersion || document.Root.Type != "root" {
		return Document[To]{}, fmt.Errorf("rich-text document requires version 1 and a root node")
	}
	count := 0
	var copyNode func(Node[From], int) (Node[To], error)
	copyNode = func(node Node[From], depth int) (Node[To], error) {
		count++
		if depth > 64 || count > 10_000 {
			return Node[To]{}, fmt.Errorf("rich-text document exceeds its complexity limit")
		}
		if node.Version != 0 && node.Version != DocumentVersion || node.Type == "root" && depth != 0 {
			return Node[To]{}, fmt.Errorf("unsupported rich-text node envelope")
		}
		switch node.Type {
		case "block":
			if node.Version != DocumentVersion || node.Children != nil {
				return Node[To]{}, fmt.Errorf("rich-text block requires version 1 without editor children")
			}
		case "root", "paragraph", "heading", "quote", "text", "linebreak", "link", "list", "listitem", "code", "horizontalrule", "upload", "relationship":
			if node.Fields != nil || !isContainerNode(node.Type) && node.Children != nil {
				return Node[To]{}, fmt.Errorf("rich-text node %q contains unsupported fields or children", node.Type)
			}
		default:
			return Node[To]{}, fmt.Errorf("unsupported rich-text node %q; retain raw JSON for export or migration", node.Type)
		}
		copy := Node[To]{
			Type: node.Type, Version: node.Version, Text: node.Text,
			Format: append([]byte(nil), node.Format...), Detail: node.Detail, Mode: node.Mode, Style: node.Style,
			TextFormat: node.TextFormat, TextStyle: node.TextStyle, Indent: node.Indent, Tag: node.Tag,
			URL: node.URL, Target: node.Target, Rel: node.Rel, Title: node.Title,
			ListType: node.ListType, Start: node.Start, Value: node.Value, Language: node.Language, Theme: node.Theme,
			RelationTo: node.RelationTo, ID: node.ID, Caption: node.Caption,
		}
		if node.Direction != nil {
			value := *node.Direction
			copy.Direction = &value
		}
		if node.Checked != nil {
			value := *node.Checked
			copy.Checked = &value
		}
		if node.Type == "block" {
			if node.Fields == nil || convert == nil {
				return Node[To]{}, fmt.Errorf("rich-text block requires a typed payload and conversion callback")
			}
			payload, err := convert(*node.Fields)
			if err != nil {
				return Node[To]{}, err
			}
			copy.Fields = &payload
		}
		if node.Children != nil {
			copy.Children = make([]Node[To], len(node.Children))
			for index, child := range node.Children {
				var err error
				copy.Children[index], err = copyNode(child, depth+1)
				if err != nil {
					return Node[To]{}, err
				}
			}
		}
		return copy, nil
	}
	root, err := copyNode(document.Root, 0)
	if err != nil {
		return Document[To]{}, err
	}
	return Document[To]{Version: DocumentVersion, Root: root}, nil
}
