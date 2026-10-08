// Package enableversions plans and checks a collection or global that starts
// keeping versions while it may already store documents. Each store adapter
// executes the plan; this package owns the choices and what they mean.
package enableversions

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Choices maps each resource that starts keeping versions, by its after stable
// ID, to what becomes of the documents it already stores.
type Choices map[schema.StableID]migration.ExistingDocuments

// Report is how many documents, trashed ones included, a resource that starts
// keeping versions already stores.
type Report struct {
	Resource  schema.Collection
	Documents int64
}

// Step is one resource that starts keeping versions, with what becomes of
// the documents it already stores.
type Step struct {
	Resource schema.Collection
	Existing migration.ExistingDocuments
}

// Plan returns a step for each resource that starts keeping versions between
// before and after, in migration.VersionsEnabled order. Every such resource
// needs a choice, and only those resources may have one.
func Plan(before, after schema.Snapshot, renames map[schema.StableID]schema.StableID, choices Choices) ([]Step, error) {
	enabled := migration.VersionsEnabled(before, after, renames)
	starting := make(map[schema.StableID]bool, len(enabled))
	var missing []string
	steps := make([]Step, 0, len(enabled))
	for _, resource := range enabled {
		starting[resource.ID] = true
		existing, chosen := choices[resource.ID]
		if !chosen {
			missing = append(missing, Describe(resource))
			continue
		}
		if err := Validate(resource, existing); err != nil {
			return nil, err
		}
		steps = append(steps, Step{Resource: resource, Existing: existing})
	}
	if len(missing) != 0 {
		return nil, fmt.Errorf("RIDU_VERSIONS_EXISTING_REQUIRED: %s %s keeping versions; choose what the documents already stored become with ridu migrate create --versions-existing=published, draft or require-empty", strings.Join(missing, " and "), startVerb(len(missing)))
	}
	var extra []string
	for id := range choices {
		if !starting[id] {
			extra = append(extra, string(id))
		}
	}
	if len(extra) != 0 {
		sort.Strings(extra)
		return nil, fmt.Errorf("existing documents were chosen for %s, which does not start keeping versions", strings.Join(extra, ", "))
	}
	return steps, nil
}

// Payload is the step's immutable artifact record.
func (step Step) Payload() migration.EnableVersionsPayload {
	return migration.EnableVersionsPayload{ResourceID: step.Resource.ID, Existing: step.Existing}
}

// Validate checks that existing is a choice resource can take.
func Validate(resource schema.Collection, existing migration.ExistingDocuments) error {
	if _, err := migration.ParseExistingDocuments(string(existing)); err != nil {
		return fmt.Errorf("%s: %w", Describe(resource), err)
	}
	if existing == migration.ExistingDraft && (resource.Versions == nil || !resource.Versions.Drafts) {
		return fmt.Errorf("%s does not enable drafts, so its existing documents cannot become drafts; choose published or require-empty", Describe(resource))
	}
	return nil
}

// Recorded returns the choices an artifact's enable-versions steps record.
func Recorded(artifact migration.Artifact) (Choices, error) {
	choices := make(Choices)
	for _, phase := range artifact.Phases {
		for _, step := range phase.Steps {
			if step.Kind != migration.StepEnableVersions {
				continue
			}
			var payload migration.EnableVersionsPayload
			if err := json.Unmarshal(step.Payload, &payload); err != nil {
				return nil, fmt.Errorf("decode migration %s step %s: %w", artifact.Name, step.ID, err)
			}
			choices[payload.ResourceID] = payload.Existing
		}
	}
	return choices, nil
}

// Name labels the step in the artifact.
func (step Step) Name() string {
	switch step.Existing {
	case migration.ExistingPublished:
		return fmt.Sprintf("enable versions on %s: publish existing documents", Describe(step.Resource))
	case migration.ExistingDraft:
		return fmt.Sprintf("enable versions on %s: keep existing documents as drafts", Describe(step.Resource))
	}
	return fmt.Sprintf("enable versions on %s: require no stored documents", Describe(step.Resource))
}

// Risk explains to a reviewer what the step does to stored documents.
func (step Step) Risk() migration.Risk {
	resource := step.Resource
	message := fmt.Sprintf("stop the migration if %s stores any document, trashed documents included", Describe(resource))
	switch step.Existing {
	case migration.ExistingPublished:
		message = fmt.Sprintf("publish every document %s stores, with a first version and a matching draft, so readers keep seeing them; stop application writers while the migration runs", Describe(resource))
	case migration.ExistingDraft:
		message = fmt.Sprintf("keep every document %s stores as an unpublished draft with a first version; public readers stop seeing them until they are published; stop application writers while the migration runs", Describe(resource))
	}
	return migration.Risk{Code: "RIDU_VERSIONS_ENABLE", Level: migration.RiskWarning, Message: message}
}

// NotEmptyError rejects a require-empty choice for a resource that stores
// documents.
func NotEmptyError(resource schema.Collection, documents int64) error {
	return fmt.Errorf("RIDU_VERSIONS_ENABLE_NOT_EMPTY: %s stores %s, and this migration enables versions only while it stores none; if no database has applied the migration, replace it with one created by ridu migrate create --versions-existing=published or draft, otherwise remove the documents first", Describe(resource), documentCount(documents))
}

// StoredDocumentsError rejects development schema sync that would enable
// versions on a resource that stores documents.
func StoredDocumentsError(resource schema.Collection, documents int64) error {
	return fmt.Errorf("RIDU_VERSIONS_EXISTING_DOCUMENTS: %s stores %s, so enabling versions needs a decision about them; run ridu dev in a terminal to choose, or create a migration with ridu migrate create --versions-existing=published, draft or require-empty", Describe(resource), documentCount(documents))
}

// Convert returns document as a resource that keeps versions stores it after
// existing applies: published with a live head, or an unpublished draft. Its
// revision starts at 1 unless the document already has one.
func Convert(resource schema.Collection, document store.Document, existing migration.ExistingDocuments) store.Document {
	converted := store.CloneDocument(document)
	if converted.Revision < 1 {
		converted.Revision = 1
	}
	converted.HasDraftChanges = false
	converted.PublishedRevision = 0
	converted.Status = store.StatusDraft
	if existing == migration.ExistingPublished {
		converted.Status = store.StatusPublished
		if resource.Versions != nil && resource.Versions.Drafts {
			converted.PublishedRevision = converted.Revision
		}
	}
	return converted
}

// Describe names a resource for messages, such as collection "posts".
func Describe(resource schema.Collection) string {
	if resource.Capabilities.Global {
		return fmt.Sprintf("global %q", resource.Slug)
	}
	return fmt.Sprintf("collection %q", resource.Slug)
}

func startVerb(count int) string {
	if count == 1 {
		return "starts"
	}
	return "start"
}

func documentCount(documents int64) string {
	if documents == 1 {
		return "1 document"
	}
	return fmt.Sprintf("%d documents", documents)
}
