package httpapi

import (
	"encoding/json"
	"slices"
	"strconv"
	"time"

	"github.com/riducms/ridu/store"
)

// documentMetadataNames lists every metadata member a wire document can carry.
var documentMetadataNames = [...]string{"id", "createdAt", "updatedAt", "deletedAt", "_status", "_revision", "_publishedRevision", "_hasDraftChanges", "_localization"}

// documentJSON encodes a document in its wire shape: metadata and fields share
// one object with sorted names, a field replaces metadata of the same name, and
// populated references use the same shape. The bytes are those encoding/json
// produces for the equivalent map, including its HTML-safe string escaping.
//
// A value encoding/json cannot represent, such as a non-finite number, yields an
// empty message. That fails the enclosing response encode, as the map did.
func documentJSON(document store.Document) json.RawMessage {
	encoded, err := appendDocumentJSON(make([]byte, 0, 512), document)
	if err != nil {
		return json.RawMessage{}
	}
	return encoded
}

func appendDocumentJSON(dst []byte, document store.Document) ([]byte, error) {
	names := make([]string, 0, len(document.Values)+len(documentMetadataNames))
	for _, name := range documentMetadataNames {
		if _, replaced := document.Values[name]; !replaced && hasDocumentMetadata(document, name) {
			names = append(names, name)
		}
	}
	for name := range document.Values {
		names = append(names, name)
	}
	slices.Sort(names)
	dst = append(dst, '{')
	for index, name := range names {
		if index > 0 {
			dst = append(dst, ',')
		}
		dst = appendStringJSON(dst, name)
		dst = append(dst, ':')
		value, field := document.Values[name]
		if !field {
			dst = appendDocumentMetadataJSON(dst, document, name)
			continue
		}
		var err error
		if dst, err = appendValueJSON(dst, value); err != nil {
			return nil, err
		}
	}
	return append(dst, '}'), nil
}

func hasDocumentMetadata(document store.Document, name string) bool {
	switch name {
	case "deletedAt":
		return document.DeletedAt != nil
	case "_status":
		return document.Status != ""
	case "_revision":
		return document.Revision > 0
	case "_publishedRevision", "_hasDraftChanges":
		return document.PublishedRevision > 0
	case "_localization":
		return len(document.LocalizationSources) > 0
	default:
		return true
	}
}

func appendDocumentMetadataJSON(dst []byte, document store.Document, name string) []byte {
	switch name {
	case "id":
		return appendStringJSON(dst, document.ID)
	case "createdAt":
		return appendTimeJSON(dst, document.CreatedAt)
	case "updatedAt":
		return appendTimeJSON(dst, document.UpdatedAt)
	case "deletedAt":
		return appendTimeJSON(dst, *document.DeletedAt)
	case "_status":
		return appendStringJSON(dst, string(document.Status))
	case "_revision":
		return strconv.AppendInt(dst, int64(document.Revision), 10)
	case "_publishedRevision":
		return strconv.AppendInt(dst, int64(document.PublishedRevision), 10)
	case "_hasDraftChanges":
		return strconv.AppendBool(dst, document.HasDraftChanges)
	default:
		paths := make([]string, 0, len(document.LocalizationSources))
		for path := range document.LocalizationSources {
			paths = append(paths, path)
		}
		slices.Sort(paths)
		dst = append(dst, `{"sources":{`...)
		for index, path := range paths {
			if index > 0 {
				dst = append(dst, ',')
			}
			dst = appendStringJSON(dst, path)
			dst = append(dst, ':')
			dst = appendStringJSON(dst, string(document.LocalizationSources[path]))
		}
		return append(dst, "}}"...)
	}
}

// appendValueJSON walks containers itself so populated references keep the wire
// document shape; store encodes scalars with encoding/json's rules.
func appendValueJSON(dst []byte, value store.Value) ([]byte, error) {
	switch value.Kind() {
	case store.ValueObject:
		names := make([]string, 0, value.Len())
		for name := range value.Entries() {
			names = append(names, name)
		}
		slices.Sort(names)
		dst = append(dst, '{')
		for index, name := range names {
			if index > 0 {
				dst = append(dst, ',')
			}
			dst = appendStringJSON(dst, name)
			dst = append(dst, ':')
			var err error
			if dst, err = appendValueJSON(dst, value.Get(name)); err != nil {
				return nil, err
			}
		}
		return append(dst, '}'), nil
	case store.ValueList:
		dst = append(dst, '[')
		first := true
		for item := range value.Elements() {
			if !first {
				dst = append(dst, ',')
			}
			first = false
			var err error
			if dst, err = appendValueJSON(dst, item); err != nil {
				return nil, err
			}
		}
		return append(dst, ']'), nil
	case store.ValueDocument:
		document, _ := value.CopyDocument()
		return appendDocumentJSON(dst, document)
	case store.ValueString, store.ValueNumber, store.ValueBoolean:
		return value.AppendJSON(dst)
	default:
		return append(dst, "null"...), nil
	}
}

func appendStringJSON(dst []byte, text string) []byte {
	encoded, _ := store.String(text).AppendJSON(dst)
	return encoded
}

// appendTimeJSON writes the RFC 3339 UTC form, which never needs escaping.
func appendTimeJSON(dst []byte, value time.Time) []byte {
	dst = append(dst, '"')
	dst = value.UTC().AppendFormat(dst, time.RFC3339Nano)
	return append(dst, '"')
}
