// Package migration defines Ridu's database-agnostic, reviewable migration
// artifact vocabulary. Store adapters translate schema manifests into physical
// steps while Ridu retains ownership of content-aware semantic steps.
package migration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// ArtifactVersion is the migration artifact format emitted by this build.
const ArtifactVersion uint32 = 1

// ArtifactIdentity is one committed migration file's immutable runtime
// identity. Name is the ordered filename recorded in an adapter's ledger;
// Digest authenticates the canonical artifact contents.
type ArtifactIdentity struct {
	Name   string `json:"name"`
	Digest string `json:"digest"`
}

// DigestArtifactHistory returns the SHA-256 identity of one complete ordered
// migration history. Names must be unique and strictly increasing because
// committed artifact filenames define execution order.
func DigestArtifactHistory(identities []ArtifactIdentity) (string, error) {
	if len(identities) == 0 {
		return "", fmt.Errorf("migration artifact history is empty")
	}
	previous := ""
	for index, identity := range identities {
		if identity.Name == "" || len(identity.Name) > 255 || strings.TrimSpace(identity.Name) != identity.Name || !utf8.ValidString(identity.Name) {
			return "", fmt.Errorf("migration artifact identity %d has an invalid name", index)
		}
		for _, character := range identity.Name {
			if unicode.IsControl(character) {
				return "", fmt.Errorf("migration artifact identity %d has an invalid name", index)
			}
		}
		if index != 0 && identity.Name <= previous {
			return "", fmt.Errorf("migration artifact identities must be unique and strictly ordered by name")
		}
		if !validDigest(identity.Digest) {
			return "", fmt.Errorf("migration artifact identity %s has an invalid digest", identity.Name)
		}
		previous = identity.Name
	}
	encoded, err := json.Marshal(identities)
	if err != nil {
		return "", fmt.Errorf("encode migration artifact history: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// StepKind identifies how a migration runner executes one ordered step.
type StepKind string

const (
	// StepSQL executes one physical schema statement.
	StepSQL StepKind = "sql"
	// StepRenameContent rewrites schema-addressed content after physical renames.
	StepRenameContent StepKind = "rename_content"
	// StepAssertSchema verifies the resulting physical schema fingerprint.
	StepAssertSchema StepKind = "assert_schema"
	// StepBackfillReferences derives the current-document reference index after
	// its physical table has been created. Version snapshots are not indexed.
	StepBackfillReferences StepKind = "backfill_references"
	// StepRetireResources removes framework-owned shared state for resources
	// that a reviewed destructive migration removes. It is a typed semantic
	// executor and must run before the corresponding physical tables are dropped.
	StepRetireResources StepKind = "retire_resources"
	// StepDataTransform invokes one application-compiled, checksum-bound
	// callback inside the adapter's migration transaction.
	StepDataTransform StepKind = "data_transform"
	// StepCanonicalizeAuthIdentities rewrites authored authentication identity
	// values to store.CanonicalAuthIdentity before an adapter replaces its
	// legacy uniqueness contract.
	StepCanonicalizeAuthIdentities StepKind = "canonicalize_auth_identities"
)

// DataTransformDescriptor is the immutable artifact identity of one compiled
// migration callback. Checksum is author-supplied SHA-256 source identity;
// changing callback behavior requires a new checksum before artifact creation.
type DataTransformDescriptor struct {
	Name     string `json:"name"`
	Checksum string `json:"checksum"`
}

// DataTransaction is the transaction-bound semantic surface available to a
// migration callback. It intentionally omits Commit, Rollback, and generic SQL
// so the adapter retains ownership of the enclosing schema/data transaction.
type DataTransaction interface {
	Create(context.Context, store.CreateRequest) (store.Document, error)
	Find(context.Context, store.Request) (store.Document, error)
	List(context.Context, store.Request) (store.Page, error)
	Update(context.Context, store.UpdateRequest) (store.Document, error)
	Trash(context.Context, store.Request) (store.Document, error)
	Restore(context.Context, store.Request) (store.Document, error)
	Delete(context.Context, store.Request) (store.Document, error)
}

// DataTransformCallback performs one direction of a reviewed migration.
type DataTransformCallback func(context.Context, DataTransaction) error

// DataTransform binds executable up/down behavior to the immutable descriptor
// stored in an artifact. Both directions are required for lifecycle parity.
type DataTransform struct {
	DataTransformDescriptor
	Up   DataTransformCallback
	Down DataTransformCallback
}

// DataTransformChecksum returns the lowercase SHA-256 identity of reviewed
// callback source bytes. Applications normally compute and commit this value
// when authoring the migration callback.
func DataTransformChecksum(source []byte) string {
	digest := sha256.Sum256(source)
	return hex.EncodeToString(digest[:])
}

// Validate checks the immutable and executable portions of a registration.
func (transform DataTransform) Validate() error {
	if err := transform.DataTransformDescriptor.Validate(); err != nil {
		return err
	}
	if transform.Up == nil || transform.Down == nil {
		return fmt.Errorf("data transform %q requires both up and down callbacks", transform.Name)
	}
	return nil
}

// Validate checks one artifact-safe callback descriptor.
func (descriptor DataTransformDescriptor) Validate() error {
	if !schema.IsValidCollectionSlug(descriptor.Name) {
		return fmt.Errorf("data transform name %q must use lowercase kebab-case", descriptor.Name)
	}
	if !validDigest(descriptor.Checksum) {
		return fmt.Errorf("data transform %q checksum must be a lowercase SHA-256 digest", descriptor.Name)
	}
	return nil
}

// RiskLevel classifies the operational impact of a migration step.
type RiskLevel string

const (
	// RiskNotice records operational context that does not require intervention.
	RiskNotice RiskLevel = "notice"
	// RiskWarning records a change that deserves explicit deployment review.
	RiskWarning RiskLevel = "warning"
	// RiskDestructive records a change that can permanently remove stored data.
	RiskDestructive RiskLevel = "destructive"
)

// Risk is one machine-readable finding produced while planning a migration.
type Risk struct {
	// Code is a stable identifier suitable for CI policy.
	Code string `json:"code"`
	// Level classifies whether the finding is informational, risky, or destructive.
	Level RiskLevel `json:"level"`
	// Message explains the concrete impact and required review.
	Message string `json:"message"`
}

// Rename records explicit, committed content identity intent. Inference is
// allowed only while proposing this value; runners execute only persisted intent.
type Rename struct {
	// CollectionBefore and CollectionAfter are current public slugs.
	CollectionBefore schema.CollectionSlug `json:"collectionBefore"`
	CollectionAfter  schema.CollectionSlug `json:"collectionAfter"`
	// FieldBefore and FieldAfter are canonical field paths. Empty paths mean the
	// rename applies to the collection itself.
	FieldBefore string `json:"fieldBefore,omitempty"`
	FieldAfter  string `json:"fieldAfter,omitempty"`
	// Fields records every nested or top-level field path whose identity changed
	// as part of a collection rename.
	Fields []FieldRename `json:"fields,omitempty"`
}

// FieldRename preserves one field address across an authored rename.
type FieldRename struct {
	// Before is the canonical field path in the before manifest.
	Before string `json:"before"`
	// After is the canonical field path in the after manifest.
	After string `json:"after"`
}

// Operation is one ordered physical or semantic planner operation. It is
// converted into a checkpointed artifact step before an artifact is written.
type Operation struct {
	// Kind selects the runner behavior.
	Kind StepKind `json:"kind"`
	// Name is a stable, review-friendly operation label.
	Name string `json:"name"`
	// SQL is present only for StepSQL.
	SQL string `json:"sql,omitempty"`
	// Rename is present only for StepRenameContent.
	Rename *Rename `json:"rename,omitempty"`
	// ResourceIDs is present only while planning a StepRetireResources step.
	ResourceIDs []schema.StableID `json:"resourceIds,omitempty"`
	// PurgeVersionOwnerIDs identifies surviving resources whose complete
	// historical version rows could otherwise restore references to a retired
	// resource. It is present only while planning StepRetireResources.
	PurgeVersionOwnerIDs []schema.StableID `json:"purgeVersionOwnerIds,omitempty"`
	// AuthIdentities is present only while planning a
	// StepCanonicalizeAuthIdentities step.
	AuthIdentities []AuthIdentityResource `json:"authIdentities,omitempty"`
}

// AuthIdentityResource freezes one authored identity field addressed by a
// canonicalization migration. Names are needed by JSON-document adapters;
// stable IDs bind physical-column adapters to the same manifest field.
type AuthIdentityResource struct {
	CollectionID schema.StableID `json:"collectionId"`
	FieldID      schema.StableID `json:"fieldId"`
	FieldName    string          `json:"fieldName"`
}

// Artifact is the immutable source of truth for one migration. It embeds both
// manifest states so history does not depend on a mutable latest-snapshot file.
type Artifact struct {
	// Version identifies this Ridu artifact JSON format.
	Version uint32 `json:"version"`
	// Name is the lowercase author-supplied migration name.
	Name string `json:"name"`
	// Planner records the exact planner and version used at creation.
	Planner Planner `json:"planner"`
	// MinimumRunnerContract is the lowest execution contract that may run
	// every phase and executor in this artifact.
	MinimumRunnerContract uint32 `json:"minimumRunnerContract,omitempty"`
	// PreviousArtifactDigest binds this artifact to the exact preceding artifact,
	// including data-only transitions whose manifest digest does not change.
	// It is empty only for the initial artifact.
	PreviousArtifactDigest string `json:"previousArtifactDigest"`
	// FromDigest and ToDigest establish immutable manifest lineage.
	FromDigest string `json:"fromDigest"`
	ToDigest   string `json:"toDigest"`
	// Before is absent only for an initial migration from an empty schema.
	Before *schema.Snapshot `json:"before,omitempty"`
	// After is the complete desired manifest after every step succeeds.
	After schema.Snapshot `json:"after"`
	// Phases are the resumable execution plan.
	Phases []Phase `json:"phases"`
	// Risks retains machine-readable planner and linter findings for review.
	Risks []Risk `json:"risks"`

	codec *artifactCodecState
}

// DigestManifest returns the SHA-256 digest of a canonical manifest.
func DigestManifest(manifest schema.Manifest) (string, error) {
	encoded, err := manifest.Bytes()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// Digest returns the SHA-256 digest of the canonical artifact JSON.
func (artifact Artifact) Digest() (string, error) {
	encoded, err := artifact.canonicalBytes()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// Validate checks the planner-owned structure and manifest lineage of an
// artifact plan. Publication binds a non-initial plan to its exact predecessor;
// serialization and digesting reject the plan until that binding exists.
func (artifact Artifact) Validate() error {
	if artifact.Version != ArtifactVersion {
		return fmt.Errorf("unsupported migration artifact version %d", artifact.Version)
	}
	if strings.TrimSpace(artifact.Name) == "" {
		return fmt.Errorf("migration artifact name is required")
	}
	if strings.TrimSpace(artifact.Planner.Name) == "" || strings.TrimSpace(artifact.Planner.Version) == "" {
		return fmt.Errorf("migration %s planner provenance is malformed", artifact.Name)
	}
	after, err := artifact.validatedAfterManifest()
	if err != nil {
		return fmt.Errorf("migration %s after manifest: %w", artifact.Name, err)
	}
	afterDigest, err := DigestManifest(after)
	if err != nil {
		return err
	}
	if afterDigest != artifact.ToDigest {
		return fmt.Errorf("migration %s after-manifest digest mismatch", artifact.Name)
	}
	if artifact.Before == nil {
		if artifact.PreviousArtifactDigest != "" {
			return fmt.Errorf("migration %s has a previous artifact digest without a before manifest", artifact.Name)
		}
		if artifact.FromDigest != "" {
			return fmt.Errorf("migration %s has a from digest without a before manifest", artifact.Name)
		}
	} else {
		if artifact.PreviousArtifactDigest != "" && !validDigest(artifact.PreviousArtifactDigest) {
			return fmt.Errorf("migration %s previous artifact digest is malformed", artifact.Name)
		}
		before, err := artifact.validatedBeforeManifest()
		if err != nil {
			return fmt.Errorf("migration %s before manifest: %w", artifact.Name, err)
		}
		beforeDigest, err := DigestManifest(before)
		if err != nil {
			return err
		}
		if beforeDigest != artifact.FromDigest {
			return fmt.Errorf("migration %s before-manifest digest mismatch", artifact.Name)
		}
	}
	if err := artifact.validate(); err != nil {
		return err
	}
	return validateRisks(artifact)
}

func validateRisks(artifact Artifact) error {
	for index, risk := range artifact.Risks {
		if strings.TrimSpace(risk.Code) == "" || strings.TrimSpace(risk.Message) == "" {
			return fmt.Errorf("migration %s risk %d is incomplete", artifact.Name, index)
		}
		switch risk.Level {
		case RiskNotice, RiskWarning, RiskDestructive:
		default:
			return fmt.Errorf("migration %s risk %d has unknown level %q", artifact.Name, index, risk.Level)
		}
	}
	return nil
}

func validatedManifest(snapshot schema.Snapshot) (schema.Manifest, error) {
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return schema.Manifest{}, err
	}
	return schema.Parse(encoded)
}
