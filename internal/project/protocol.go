// Package project owns the private, versioned machine protocol between the
// portable Ridu CLI and an application's compiled project entry.
package project

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

const (
	// ProtocolVersion changes when the CLI/project framing or request contract is incompatible.
	ProtocolVersion = 1
	// Command is the private application argument that enters machine mode.
	Command = "__ridu"
	// MigrationDatabaseURLEnvironment carries a selected network database into
	// the short-lived compiled project process without exposing credentials in
	// command-line arguments. Project.Run removes it before resolving config or
	// invoking application callbacks.
	MigrationDatabaseURLEnvironment = "RIDU_INTERNAL_PROJECT_MIGRATION_DATABASE_URL"
	// ResponsePrefix makes the frame recoverable even if application initialization writes stdout.
	ResponsePrefix = "RIDU_PROJECT_RESPONSE "
	// MaxGeneratedArtifacts and MaxGeneratedArtifactBytes bound decoded plugin
	// output at both sides of the project protocol.
	MaxGeneratedArtifacts     = 128
	MaxGeneratedArtifactBytes = 64 << 20
	// MaxResponseBytes bounds the complete framed JSON line, including base64
	// expansion and the canonical manifest.
	MaxResponseBytes = 128 << 20
)

// Response is the successful manifest handshake returned by a project entry.
type Response struct {
	ProtocolVersion  int                                 `json:"protocolVersion"`
	FrameworkVersion string                              `json:"frameworkVersion"`
	ManifestVersion  uint32                              `json:"manifestVersion"`
	Manifest         json.RawMessage                     `json:"manifest"`
	Artifacts        []Artifact                          `json:"artifacts,omitempty"`
	DataTransforms   []migration.DataTransformDescriptor `json:"dataTransforms,omitempty"`
}

// Artifact is one deterministic generated file returned by a compiled plugin.
// The CLI maps the namespaced identity to an application-owned destination.
type Artifact struct {
	Plugin  string `json:"plugin"`
	Name    string `json:"name"`
	Content []byte `json:"content"`
}

// ArtifactRequest identifies one configured plugin artifact the CLI needs.
type ArtifactRequest struct {
	Plugin string
	Name   string
}

// EncodeResponse returns one newline-terminated framed response.
func EncodeResponse(response Response) ([]byte, error) {
	if err := validateArtifacts(response.Artifacts); err != nil {
		return nil, err
	}
	if err := validateDataTransformDescriptors(response.DataTransforms); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("encode project response: %w", err)
	}
	framed := make([]byte, 0, len(ResponsePrefix)+len(encoded)+1)
	framed = append(framed, ResponsePrefix...)
	framed = append(framed, encoded...)
	framed = append(framed, '\n')
	if len(framed) > MaxResponseBytes {
		return nil, fmt.Errorf("project response exceeds the %d-byte protocol limit", MaxResponseBytes)
	}
	return framed, nil
}

// DecodeResponse finds and validates exactly one response frame in project stdout.
func DecodeResponse(output []byte) (Response, error) {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, MaxResponseBytes)

	var response Response
	found := false
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, ResponsePrefix) {
			continue
		}
		if found {
			return Response{}, fmt.Errorf("project emitted more than one machine response frame")
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, ResponsePrefix)), &response); err != nil {
			return Response{}, fmt.Errorf("decode project response: %w", err)
		}
		found = true
	}
	if err := scanner.Err(); err != nil {
		return Response{}, fmt.Errorf("scan project response: %w", err)
	}
	if !found {
		return Response{}, fmt.Errorf("project did not emit a %sframe", ResponsePrefix)
	}
	if response.ProtocolVersion != ProtocolVersion {
		return Response{}, fmt.Errorf("project protocol version %d is incompatible with CLI protocol version %d", response.ProtocolVersion, ProtocolVersion)
	}
	if len(response.Manifest) == 0 {
		return Response{}, fmt.Errorf("project response did not include a schema manifest")
	}
	if err := validateArtifacts(response.Artifacts); err != nil {
		return Response{}, err
	}
	if err := validateDataTransformDescriptors(response.DataTransforms); err != nil {
		return Response{}, err
	}
	return response, nil
}

func validateDataTransformDescriptors(descriptors []migration.DataTransformDescriptor) error {
	seen := make(map[string]struct{}, len(descriptors))
	for _, descriptor := range descriptors {
		if err := descriptor.Validate(); err != nil {
			return fmt.Errorf("project response: %w", err)
		}
		if _, duplicate := seen[descriptor.Name]; duplicate {
			return fmt.Errorf("project response includes duplicate data transform %q", descriptor.Name)
		}
		seen[descriptor.Name] = struct{}{}
	}
	return nil
}

func validateArtifacts(artifacts []Artifact) error {
	if len(artifacts) > MaxGeneratedArtifacts {
		return fmt.Errorf("project response includes %d generated artifacts; limit is %d", len(artifacts), MaxGeneratedArtifacts)
	}
	seenArtifacts := make(map[string]struct{}, len(artifacts))
	totalBytes := 0
	for index, artifact := range artifacts {
		if !schema.IsValidCollectionSlug(artifact.Plugin) || !schema.IsValidCollectionSlug(artifact.Name) {
			return fmt.Errorf("project response artifact %d has an invalid plugin or name", index)
		}
		if len(artifact.Content) == 0 {
			return fmt.Errorf("project response artifact %s/%s has empty content", artifact.Plugin, artifact.Name)
		}
		identity := artifact.Plugin + "/" + artifact.Name
		if _, exists := seenArtifacts[identity]; exists {
			return fmt.Errorf("project response includes duplicate artifact %q", identity)
		}
		seenArtifacts[identity] = struct{}{}
		totalBytes += len(artifact.Content)
		if totalBytes > MaxGeneratedArtifactBytes {
			return fmt.Errorf("project response generated artifact content exceeds the %d-byte limit", MaxGeneratedArtifactBytes)
		}
	}
	return nil
}
