package postgres

import (
	"errors"
	"strings"
	"testing"

	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

func TestCapabilityDecreasePreflightRejectsCurrentCapabilityDisables(t *testing.T) {
	before := capabilityPreflightManifest(capabilityPreflightStatefulCollection("stateful"))
	tests := map[string]struct {
		code   string
		mutate func(*schema.Collection)
	}{
		"authentication": {"RIDU_AUTH_DISABLE_STATE_UNSAFE", func(collection *schema.Collection) {
			collection.Capabilities.Auth, collection.Auth = false, nil
		}},
		"API keys": {"RIDU_API_KEYS_DISABLE_STATE_UNSAFE", func(collection *schema.Collection) {
			collection.Auth.APIKeys = false
		}},
		"versions": {"RIDU_VERSIONS_DISABLE_STATE_UNSAFE", func(collection *schema.Collection) {
			collection.Capabilities.Versions, collection.Versions = false, nil
		}},
		"drafts": {"RIDU_DRAFTS_DISABLE_STATE_UNSAFE", func(collection *schema.Collection) {
			collection.Versions.Drafts = false
		}},
		"document locking": {"RIDU_DOCUMENT_LOCK_DISABLE_STATE_UNSAFE", func(collection *schema.Collection) {
			collection.Capabilities.Locking, collection.DocumentLock = false, nil
		}},
		"trash": {"RIDU_TRASH_DISABLE_STATE_UNSAFE", func(collection *schema.Collection) {
			collection.Capabilities.Trash = false
		}},
		"upload": {"RIDU_UPLOAD_COLLECTION_REMOVAL_UNSAFE", func(collection *schema.Collection) {
			collection.Capabilities.Upload, collection.Upload = false, nil
		}},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			afterSnapshot := before.Snapshot()
			test.mutate(&afterSnapshot.Collections[0])
			artifact := capabilityAssertOnlyArtifact(t, before, schema.NewManifest(afterSnapshot))
			assertCapabilityPreflightRisk(t, artifact, test.code)
		})
	}
}

func TestCapabilityDecreasePreflightRejectsUnprovenCollectionRename(t *testing.T) {
	beforeCollection := capabilityPreflightStatefulCollection("users")
	before := capabilityPreflightManifest(beforeCollection)
	afterCollection := capabilityPreflightStatefulCollection("members")
	after := capabilityPreflightManifest(afterCollection)
	artifact, err := ridumigration.NewArtifact("unproven-collection-rename", atlasPlanner(), &before, after)
	if err != nil {
		t.Fatal(err)
	}
	artifact.Phases, err = phasesFromOperations(artifact.FromDigest, []ridumigration.Operation{
		{
			Kind: ridumigration.StepRenameContent, Name: "claim collection identity",
			Rename: &ridumigration.Rename{CollectionBefore: beforeCollection.Slug, CollectionAfter: afterCollection.Slug},
		},
		{Kind: ridumigration.StepAssertSchema, Name: "verify resulting PostgreSQL schema"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := artifact.Validate(); err != nil {
		t.Fatalf("unproven rename fixture: %v", err)
	}
	if err := validatePostgresCapabilityDecreasePreflight(artifact); err == nil || !strings.Contains(err.Error(), "requires exactly one matching physical table rename") {
		t.Fatalf("unproven rename continuity = %v", err)
	}
}

func capabilityAssertOnlyArtifact(t *testing.T, before, after schema.Manifest) ridumigration.Artifact {
	t.Helper()
	artifact, err := ridumigration.NewArtifact("capability-decrease", atlasPlanner(), &before, after)
	if err != nil {
		t.Fatal(err)
	}
	artifact.Phases, err = phasesFromOperations(artifact.FromDigest, []ridumigration.Operation{{
		Kind: ridumigration.StepAssertSchema, Name: "verify resulting PostgreSQL schema",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := artifact.Validate(); err != nil {
		t.Fatal(err)
	}
	return artifact
}

func capabilityPreflightManifest(collections ...schema.Collection) schema.Manifest {
	return schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Capability preflight"},
		Collections: collections, Plugins: []schema.Plugin{},
	})
}

func capabilityPreflightStatefulCollection(slug schema.CollectionSlug) schema.Collection {
	return schema.Collection{
		ID: schema.StableID(slug), Slug: slug,
		Capabilities: schema.Capabilities{Auth: true, Upload: true, Versions: true, Trash: true, Locking: true},
		Auth: &schema.AuthSettings{
			IdentityField: "email", APIKeys: true, PasswordReset: true, VerifyEmail: true,
		},
		Upload:       &schema.UploadSettings{},
		Versions:     &schema.VersionSettings{Drafts: true, MaxPerDocument: 10},
		DocumentLock: &schema.DocumentLockSettings{DurationSeconds: 60},
		Fields:       []schema.Field{},
	}
}

func assertCapabilityPreflightRisk(t *testing.T, artifact ridumigration.Artifact, code string) {
	t.Helper()
	var safety *SafetyError
	err := validatePostgresCapabilityDecreasePreflight(artifact)
	if !errors.As(err, &safety) || !hasRiskCode(safety.Risks, code) {
		t.Fatalf("capability preflight risk %s = %#v, %v", code, safety, err)
	}
}
