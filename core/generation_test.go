package core

import (
	"errors"
	"strings"
	"testing"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/project"
)

type generationTestPlugin struct {
	artifacts []PluginGeneratedArtifact
	err       error
	seenName  string
}

func (*generationTestPlugin) Key() string { return "generator" }

func (*generationTestPlugin) Descriptor() PluginDescriptor {
	return PluginDescriptor{
		Version: "0.1.0", GoPackage: "example.com/generator", APIVersion: PluginAPIVersion,
		Ridu: RiduCompatibility{Minimum: FrameworkVersion, MaximumExclusive: "0.2.0"},
	}
}

func (plugin *generationTestPlugin) GeneratedArtifacts(ctx PluginGenerationContext) ([]PluginGeneratedArtifact, error) {
	plugin.seenName = ctx.Manifest.Snapshot().Application.Name
	return plugin.artifacts, plugin.err
}

type undescribedGenerationPlugin struct{}

func (undescribedGenerationPlugin) Key() string { return "undescribed" }
func (undescribedGenerationPlugin) GeneratedArtifacts(PluginGenerationContext) ([]PluginGeneratedArtifact, error) {
	return []PluginGeneratedArtifact{{Name: "schema", Content: []byte("content")}}, nil
}

func TestProjectGenerationUsesResolvedManifestAndDefensiveArtifactBytes(t *testing.T) {
	content := []byte("generated content\n")
	plugin := &generationTestPlugin{artifacts: []PluginGeneratedArtifact{{Name: "contract", Content: content}}}
	manifest, artifacts, err := resolveProjectGeneration(Config{
		Name: "Generation fixture", Plugins: []Plugin{plugin},
		Collections: []Collection{{Slug: "posts", Fields: []field.Definition{field.Text("title")}}},
	}, []project.ArtifactRequest{{Plugin: "generator", Name: "contract"}})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Snapshot().Application.Name != "Generation fixture" || plugin.seenName != "Generation fixture" {
		t.Fatalf("resolved generation names = %q / %q", manifest.Snapshot().Application.Name, plugin.seenName)
	}
	if len(artifacts) != 1 || artifacts[0].Plugin != "generator" || artifacts[0].Name != "contract" || string(artifacts[0].Content) != "generated content\n" {
		t.Fatalf("resolved generation artifacts = %#v", artifacts)
	}
	content[0] = 'X'
	plugin.artifacts[0].Content[1] = 'Y'
	if string(artifacts[0].Content) != "generated content\n" {
		t.Fatalf("project artifact aliases plugin bytes: %q", artifacts[0].Content)
	}
}

func TestProjectGenerationRejectsInvalidProviderOutput(t *testing.T) {
	tests := []struct {
		name      string
		artifacts []PluginGeneratedArtifact
		provider  error
		want      string
	}{
		{name: "provider failure", provider: errors.New("generation failed"), want: "generation failed"},
		{name: "invalid name", artifacts: []PluginGeneratedArtifact{{Name: "GraphQL", Content: []byte("content")}}, want: "lowercase kebab-case"},
		{name: "empty content", artifacts: []PluginGeneratedArtifact{{Name: "schema"}}, want: "must not be empty"},
		{name: "duplicate", artifacts: []PluginGeneratedArtifact{{Name: "schema", Content: []byte("one")}, {Name: "schema", Content: []byte("two")}}, want: "already declared"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plugin := &generationTestPlugin{artifacts: test.artifacts, err: test.provider}
			_, _, err := resolveProjectGeneration(Config{
				Name: "Invalid generation", Plugins: []Plugin{plugin},
				Collections: []Collection{{Slug: "posts", Fields: []field.Definition{field.Text("title")}}},
			}, []project.ArtifactRequest{{Plugin: "generator", Name: "schema"}})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("generation error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestProjectGenerationSkipsUnrequestedProviders(t *testing.T) {
	plugin := &generationTestPlugin{err: errors.New("must not run")}
	manifest, artifacts, err := resolveProjectGeneration(Config{
		Name: "Unrequested generation", Plugins: []Plugin{plugin},
		Collections: []Collection{{Slug: "posts", Fields: []field.Definition{field.Text("title")}}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 0 || manifest.Snapshot().Application.Name != "Unrequested generation" {
		t.Fatalf("unrequested result = %#v / %q", artifacts, manifest.Snapshot().Application.Name)
	}
}

func TestGenerationProviderRequiresDescriptor(t *testing.T) {
	_, err := Resolve(Config{
		Name: "Undescribed generation", Plugins: []Plugin{undescribedGenerationPlugin{}},
		Collections: []Collection{{Slug: "posts", Fields: []field.Definition{field.Text("title")}}},
	})
	if err == nil || !strings.Contains(err.Error(), "generation capabilities") {
		t.Fatalf("missing descriptor error = %v", err)
	}
}
