// Package payload imports a normalized Payload CMS export without coupling
// Ridu to Payload's database schema. Source-specific extractors emit Export;
// this package owns assessment, chosen-version selection, and Ridu writes.
package payload

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"sort"
	"strings"
	"time"

	ridu "github.com/riducms/ridu"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

type Export struct {
	Collections []Collection
}

type Collection struct {
	Slug      string
	Documents []Record
}

// ID is a normalized Payload document ID. JSON strings are preserved exactly;
// integer JSON numbers are converted to their exact base-10 spelling so
// Payload's numeric, UUID, and custom IDs share Ridu's canonical string wire
// representation without passing through float64.
type ID string

func (id *ID) UnmarshalJSON(encoded []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("decode Payload ID: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("decode Payload ID: trailing JSON value")
	}
	var canonical string
	switch candidate := value.(type) {
	case string:
		canonical = candidate
	case json.Number:
		raw := candidate.String()
		integer, ok := new(big.Int).SetString(raw, 10)
		if !ok || strings.ContainsAny(raw, ".eE") {
			return fmt.Errorf("Payload numeric ID %q must be an integer", raw)
		}
		canonical = integer.String()
	default:
		return fmt.Errorf("Payload ID must be a string or integer")
	}
	if err := store.ValidateDocumentID(canonical); err != nil {
		return fmt.Errorf("invalid Payload ID: %w", err)
	}
	*id = ID(canonical)
	return nil
}

func (id ID) MarshalJSON() ([]byte, error) { return json.Marshal(string(id)) }
func (id ID) String() string               { return string(id) }

type Record struct {
	ID               ID
	Data             json.RawMessage
	Status           store.Status
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Versions         []Version
	SelectedRevision int
}

type Version struct {
	Revision  int
	Data      json.RawMessage
	Status    store.Status
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Assessment struct {
	Collections int
	Documents   int
	Versions    int
	Issues      []string
}

func Assess(manifest schema.Manifest, source Export) Assessment {
	known := make(map[string]schema.Collection)
	for _, collection := range manifest.Snapshot().Collections {
		known[string(collection.Slug)] = collection
	}
	result := Assessment{Collections: len(source.Collections)}
	seenCollections := make(map[string]struct{}, len(source.Collections))
	seenIDsByCollection := make(map[string]map[ID]struct{}, len(source.Collections))
	for _, collection := range source.Collections {
		if _, duplicate := seenCollections[collection.Slug]; duplicate {
			result.Issues = append(result.Issues, fmt.Sprintf("collection %q appears more than once in the Payload export", collection.Slug))
		} else {
			seenCollections[collection.Slug] = struct{}{}
		}
		target, exists := known[collection.Slug]
		if !exists {
			result.Issues = append(result.Issues, fmt.Sprintf("collection %q is not present in the Ridu manifest", collection.Slug))
			continue
		}
		result.Documents += len(collection.Documents)
		seenIDs := seenIDsByCollection[collection.Slug]
		if seenIDs == nil {
			seenIDs = make(map[ID]struct{}, len(collection.Documents))
			seenIDsByCollection[collection.Slug] = seenIDs
		}
		for _, document := range collection.Documents {
			result.Versions += len(document.Versions)
			exactID := document.ID
			if err := store.ValidateDocumentID(exactID.String()); err != nil {
				result.Issues = append(result.Issues, fmt.Sprintf("collection %q contains a document without an ID", collection.Slug))
			} else if _, duplicate := seenIDs[exactID]; duplicate {
				result.Issues = append(result.Issues, fmt.Sprintf("collection %q contains duplicate canonical ID %q", collection.Slug, exactID))
			} else {
				seenIDs[exactID] = struct{}{}
			}
			if document.SelectedRevision != 0 && target.Versions == nil {
				result.Issues = append(result.Issues, fmt.Sprintf("collection %q selects a version but is not version-enabled", collection.Slug))
			}
			if _, err := selected(document); err != nil {
				result.Issues = append(result.Issues, fmt.Sprintf("%s/%s: %v", collection.Slug, document.ID, err))
			}
		}
	}
	sort.Strings(result.Issues)
	return result
}

type Target interface {
	Import(context.Context, string, store.Values, ridu.ImportOptions, *store.Document) (store.Document, error)
}

type Result struct {
	Imported int
	IDs      map[string]map[string]string
}

func Import(ctx context.Context, target Target, source Export, actor *store.Document) (Result, error) {
	result := Result{IDs: make(map[string]map[string]string)}
	seenCollections := make(map[string]struct{}, len(source.Collections))
	for _, collection := range source.Collections {
		if _, duplicate := seenCollections[collection.Slug]; duplicate {
			return result, fmt.Errorf("validate Payload export: collection %q appears more than once", collection.Slug)
		}
		seenCollections[collection.Slug] = struct{}{}
		seenIDs := make(map[ID]struct{}, len(collection.Documents))
		for _, record := range collection.Documents {
			exactID := record.ID
			if err := store.ValidateDocumentID(exactID.String()); err != nil {
				return result, fmt.Errorf("validate %s ID: %w", collection.Slug, err)
			}
			if _, duplicate := seenIDs[exactID]; duplicate {
				return result, fmt.Errorf("validate %s: duplicate canonical ID %q", collection.Slug, exactID)
			}
			seenIDs[exactID] = struct{}{}
		}
	}
	for _, collection := range source.Collections {
		result.IDs[collection.Slug] = make(map[string]string, len(collection.Documents))
		for _, record := range collection.Documents {
			chosen, err := selected(record)
			if err != nil {
				return result, fmt.Errorf("select %s/%s: %w", collection.Slug, record.ID, err)
			}
			var values store.Values
			if err := json.Unmarshal(chosen.Data, &values); err != nil {
				return result, fmt.Errorf("decode %s/%s: %w", collection.Slug, record.ID, err)
			}
			canonicalID := record.ID.String()
			document, err := target.Import(ctx, collection.Slug, values, ridu.ImportOptions{ID: canonicalID, Status: chosen.Status, CreatedAt: chosen.CreatedAt, UpdatedAt: chosen.UpdatedAt}, actor)
			if err != nil {
				return result, fmt.Errorf("import %s/%s: %w", collection.Slug, record.ID, err)
			}
			result.IDs[collection.Slug][canonicalID] = document.ID
			result.Imported++
		}
	}
	return result, nil
}

func selected(record Record) (Version, error) {
	base := Version{Data: record.Data, Status: record.Status, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt}
	if record.SelectedRevision == 0 {
		if base.Status == "" {
			base.Status = store.StatusPublished
		}
		return base, nil
	}
	for _, version := range record.Versions {
		if version.Revision == record.SelectedRevision {
			return version, nil
		}
	}
	return Version{}, fmt.Errorf("selected revision %d is missing", record.SelectedRevision)
}
