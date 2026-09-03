package project_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/internal/project"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

func TestRunAndDecodeManifestResponse(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	stdout.WriteString("application startup noise\n")
	t.Setenv(project.MigrationDatabaseURLEnvironment, "mongodb://stale-user:stale-password@example.invalid/ridu")

	err := project.Run(
		[]string{project.Command, "manifest", "--protocol-version", fmt.Sprint(project.ProtocolVersion), "--framework-version", "test"},
		&stdout,
		&stderr,
		"test",
		func() (schema.Manifest, error) {
			if os.Getenv(project.MigrationDatabaseURLEnvironment) != "" {
				return schema.Manifest{}, fmt.Errorf("private migration database selection remained visible to manifest resolution")
			}
			return schema.NewManifest(schema.Snapshot{
				Version:     schema.CurrentVersion,
				Application: schema.Application{Name: "Fixture"},
				Collections: []schema.Collection{},
				Plugins:     []schema.Plugin{},
			}), nil
		},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("Run: %v; stderr: %s", err, stderr.String())
	}
	response, err := project.DecodeResponse(stdout.Bytes())
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	if response.FrameworkVersion != "test" {
		t.Fatalf("framework version = %q, want test", response.FrameworkVersion)
	}
	manifest, err := schema.Parse(response.Manifest)
	if err != nil {
		t.Fatalf("Parse response manifest: %v", err)
	}
	if got := manifest.Snapshot().Application.Name; got != "Fixture" {
		t.Fatalf("application name = %q, want Fixture", got)
	}
}

func TestRunRejectsVersionMismatchBeforeResolution(t *testing.T) {
	resolved := false
	err := project.Run(
		[]string{project.Command, "manifest", "--protocol-version", fmt.Sprint(project.ProtocolVersion), "--framework-version", "old"},
		ioDiscard{},
		ioDiscard{},
		"new",
		func() (schema.Manifest, error) {
			resolved = true
			return schema.Manifest{}, nil
		},
		nil,
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "install matching Ridu versions") {
		t.Fatalf("Run error = %v, want actionable framework mismatch", err)
	}
	if resolved {
		t.Fatal("resolver ran before version compatibility was established")
	}
}

func TestRunAndDecodeGenerationArtifacts(t *testing.T) {
	var stdout bytes.Buffer
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Generated"},
		Collections: []schema.Collection{}, Plugins: []schema.Plugin{{Key: "graphql"}},
	})
	err := project.Run(
		[]string{project.Command, "generate", "--protocol-version", fmt.Sprint(project.ProtocolVersion), "--framework-version", "test", "--artifact", "graphql/schema"},
		&stdout, ioDiscard{}, "test",
		func() (schema.Manifest, error) {
			return schema.Manifest{}, fmt.Errorf("manifest-only resolver must not run")
		},
		func(requests []project.ArtifactRequest) (schema.Manifest, []project.Artifact, error) {
			if len(requests) != 1 || requests[0].Plugin != "graphql" || requests[0].Name != "schema" {
				t.Fatalf("artifact requests = %#v", requests)
			}
			return manifest, []project.Artifact{{Plugin: "graphql", Name: "schema", Content: []byte("type Query { ok: Boolean! }\n")}}, nil
		},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	response, err := project.DecodeResponse(stdout.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Artifacts) != 1 || string(response.Artifacts[0].Content) != "type Query { ok: Boolean! }\n" {
		t.Fatalf("generation artifacts = %#v", response.Artifacts)
	}
}

func TestDecodeResponseRejectsProtocolMismatch(t *testing.T) {
	encoded, err := project.EncodeResponse(project.Response{
		ProtocolVersion:  project.ProtocolVersion + 1,
		FrameworkVersion: "test",
		ManifestVersion:  uint32(schema.CurrentVersion),
		Manifest:         []byte(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := project.DecodeResponse(encoded); err == nil || !strings.Contains(err.Error(), "incompatible") {
		t.Fatalf("DecodeResponse error = %v, want protocol incompatibility", err)
	}
}

func TestDecodeResponseRejectsInvalidOrDuplicateArtifacts(t *testing.T) {
	for _, test := range []struct {
		name      string
		artifacts []project.Artifact
		want      string
	}{
		{name: "invalid identity", artifacts: []project.Artifact{{Plugin: "GraphQL", Name: "schema", Content: []byte("content")}}, want: "invalid plugin or name"},
		{name: "empty", artifacts: []project.Artifact{{Plugin: "graphql", Name: "schema"}}, want: "empty content"},
		{name: "duplicate", artifacts: []project.Artifact{{Plugin: "graphql", Name: "schema", Content: []byte("one")}, {Plugin: "graphql", Name: "schema", Content: []byte("two")}}, want: "duplicate artifact"},
	} {
		t.Run(test.name, func(t *testing.T) {
			encoded, err := project.EncodeResponse(project.Response{
				ProtocolVersion: project.ProtocolVersion, FrameworkVersion: "test", ManifestVersion: uint32(schema.CurrentVersion), Manifest: []byte(`{"version":1}`), Artifacts: test.artifacts,
			})
			if err == nil {
				_, err = project.DecodeResponse(encoded)
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("DecodeResponse error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestEncodeResponseRejectsTooManyGeneratedArtifacts(t *testing.T) {
	artifacts := make([]project.Artifact, project.MaxGeneratedArtifacts+1)
	for index := range artifacts {
		artifacts[index] = project.Artifact{Plugin: "generator", Name: fmt.Sprintf("artifact-%d", index), Content: []byte("content")}
	}
	_, err := project.EncodeResponse(project.Response{
		ProtocolVersion: project.ProtocolVersion, FrameworkVersion: "test",
		ManifestVersion: uint32(schema.CurrentVersion), Manifest: []byte(`{"version":1}`), Artifacts: artifacts,
	})
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("artifact limit error = %v", err)
	}
}

func TestProjectMigrationDriverRunsAndPublishesValidatedTransforms(t *testing.T) {
	descriptor := migration.DataTransformDescriptor{Name: "seed-data", Checksum: migration.DataTransformChecksum([]byte("seed-v1"))}
	driver := &recordingProjectMigrationDriver{descriptors: []migration.DataTransformDescriptor{descriptor}}
	databaseURL := "mongodb://project-user:project-password@example.invalid/ridu"
	t.Setenv(project.MigrationDatabaseURLEnvironment, databaseURL)
	manifest := schema.NewManifest(schema.Snapshot{
		Version: schema.CurrentVersion, Application: schema.Application{Name: "Fixture"}, Collections: []schema.Collection{}, Plugins: []schema.Plugin{},
	})
	var stdout bytes.Buffer
	err := project.Run([]string{
		project.Command, "migrate", "--protocol-version", fmt.Sprint(project.ProtocolVersion), "--framework-version", "test",
		"--action", "verify", "--directory", "/tmp/migrations", "--allow-insecure-database", "--allow-maintenance", "--allow-unbounded",
		"--lock-wait", "7s", "--operation-timeout", "2m",
		"--lock-timeout", "3s", "--statement-timeout", "4m", "--batch-timeout", "5s", "--idle-transaction-timeout", "6s",
		"--stop-after-phase", "phase-002",
	}, &stdout, ioDiscard{}, "test", func() (schema.Manifest, error) {
		if os.Getenv(project.MigrationDatabaseURLEnvironment) != "" {
			return schema.Manifest{}, fmt.Errorf("private migration database selection remained visible during config resolution")
		}
		return manifest, nil
	}, nil, driver)
	if err != nil {
		t.Fatal(err)
	}
	if driver.request.Action != migration.ProjectVerify || driver.request.Directory != "/tmp/migrations" || driver.request.DatabaseURL != databaseURL ||
		!driver.request.AllowInsecureDatabase || !driver.request.AllowMaintenance || !driver.request.AllowUnbounded ||
		driver.request.LockWait != 7*time.Second || driver.request.OperationTimeout != 2*time.Minute ||
		driver.request.LockTimeout != 3*time.Second || driver.request.StatementTimeout != 4*time.Minute ||
		driver.request.BatchTimeout != 5*time.Second || driver.request.IdleTransactionTimeout != 6*time.Second ||
		driver.request.StopAfterPhase != "phase-002" {
		t.Fatalf("migration request = %#v", driver.request)
	}
	if driver.privateEnvironment != "" {
		t.Fatal("project migration driver could read the credential-bearing private environment after request construction")
	}
	gotDigest, err := migration.DigestManifest(driver.manifest)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest, err := migration.DigestManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if gotDigest != wantDigest {
		t.Fatalf("migration manifest digest = %s, want %s", gotDigest, wantDigest)
	}
	response, err := project.DecodeResponse(stdout.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(response.DataTransforms) != 1 || response.DataTransforms[0] != descriptor {
		t.Fatalf("data transforms = %#v", response.DataTransforms)
	}

	invalid := &recordingProjectMigrationDriver{descriptors: []migration.DataTransformDescriptor{{Name: "Bad"}}}
	if err := project.Run([]string{
		project.Command, "manifest", "--protocol-version", fmt.Sprint(project.ProtocolVersion), "--framework-version", "test",
	}, ioDiscard{}, ioDiscard{}, "test", func() (schema.Manifest, error) { return manifest, nil }, nil, invalid); err == nil || !strings.Contains(err.Error(), "data transform") {
		t.Fatalf("invalid transform handshake error = %v", err)
	}
}

type recordingProjectMigrationDriver struct {
	descriptors        []migration.DataTransformDescriptor
	request            migration.ProjectRequest
	manifest           schema.Manifest
	privateEnvironment string
}

func (driver *recordingProjectMigrationDriver) Validate() error { return nil }

func (driver *recordingProjectMigrationDriver) DataTransforms() []migration.DataTransformDescriptor {
	return driver.descriptors
}

func (driver *recordingProjectMigrationDriver) RunProjectMigration(_ context.Context, request migration.ProjectRequest, manifest schema.Manifest) error {
	driver.request = request
	driver.manifest = manifest
	driver.privateEnvironment = os.Getenv(project.MigrationDatabaseURLEnvironment)
	return nil
}

type ioDiscard struct{}

func (ioDiscard) Write(value []byte) (int, error) { return len(value), nil }
