package core

import (
	"fmt"

	"github.com/riducms/ridu/internal/project"
	"github.com/riducms/ridu/schema"
)

func resolveProjectGeneration(applicationConfig Config, requests []project.ArtifactRequest) (schema.Manifest, []project.Artifact, error) {
	resolved, manifest, err := resolveConfig(applicationConfig)
	if err != nil {
		return schema.Manifest{}, nil, err
	}

	requested := make(map[string]map[string]struct{}, len(requests))
	for _, request := range requests {
		if requested[request.Plugin] == nil {
			requested[request.Plugin] = make(map[string]struct{})
		}
		requested[request.Plugin][request.Name] = struct{}{}
	}

	var artifacts []project.Artifact
	totalBytes := 0
	seen := make(map[string]struct{})
	for pluginIndex, plugin := range resolved.Plugins {
		requestedNames, isRequested := requested[plugin.Key()]
		if !isRequested {
			continue
		}
		provider, ok := plugin.(GenerationProvider)
		if !ok {
			return schema.Manifest{}, nil, fmt.Errorf("plugin %q does not provide configured generated artifacts", plugin.Key())
		}
		generated, err := provider.GeneratedArtifacts(PluginGenerationContext{Manifest: manifest})
		if err != nil {
			return schema.Manifest{}, nil, fmt.Errorf("generate plugin %q artifacts: %w", plugin.Key(), err)
		}
		for artifactIndex, artifact := range generated {
			path := fmt.Sprintf("plugins[%d].generatedArtifacts[%d]", pluginIndex, artifactIndex)
			if !schema.IsValidCollectionSlug(artifact.Name) {
				return schema.Manifest{}, nil, schema.NewValidationError([]schema.Issue{{
					Code: "invalid_plugin_generated_artifact", Path: path + ".name",
					Message: "generated artifact names must be lowercase kebab-case",
				}})
			}
			if len(artifact.Content) == 0 {
				return schema.Manifest{}, nil, schema.NewValidationError([]schema.Issue{{
					Code: "invalid_plugin_generated_artifact", Path: path + ".content",
					Message: "generated artifact content must not be empty",
				}})
			}
			identity := plugin.Key() + "/" + artifact.Name
			if _, exists := seen[identity]; exists {
				return schema.Manifest{}, nil, schema.NewValidationError([]schema.Issue{{
					Code: "duplicate_plugin_generated_artifact", Path: path + ".name",
					Message: fmt.Sprintf("generated artifact %q is already declared", identity),
				}})
			}
			seen[identity] = struct{}{}
			totalBytes += len(artifact.Content)
			if len(seen) > project.MaxGeneratedArtifacts || totalBytes > project.MaxGeneratedArtifactBytes {
				return schema.Manifest{}, nil, fmt.Errorf("plugin generated artifacts exceed the project protocol limit")
			}
			if _, requestedArtifact := requestedNames[artifact.Name]; !requestedArtifact {
				continue
			}
			artifacts = append(artifacts, project.Artifact{
				Plugin: plugin.Key(), Name: artifact.Name, Content: append([]byte(nil), artifact.Content...),
			})
		}
		delete(requested, plugin.Key())
	}
	for _, request := range requests {
		found := false
		for _, artifact := range artifacts {
			if artifact.Plugin == request.Plugin && artifact.Name == request.Name {
				found = true
				break
			}
		}
		if !found {
			return schema.Manifest{}, nil, fmt.Errorf("plugin %q did not provide configured generated artifact %q", request.Plugin, request.Name)
		}
	}
	return manifest, artifacts, nil
}
