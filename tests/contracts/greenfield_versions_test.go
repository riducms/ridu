package contracts_test

import (
	"testing"

	"github.com/riducms/ridu/internal/pluginregistry"
	"github.com/riducms/ridu/internal/project"
	"github.com/riducms/ridu/internal/projectfile"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/plugins/formbuilder"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/plugins/seo"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/schema"
)

func TestUnpublishedContractVersionsRemainOne(t *testing.T) {
	versions := map[string]uint32{
		"admin plugin API":          schema.CurrentAdminPluginAPIVersion,
		"backend plugin API":        schema.CurrentPluginAPIVersion,
		"migration artifact":        migration.ArtifactVersion,
		"migration physical runner": migration.PhysicalContractVersion,
		"migration runner":          migration.RunnerContractVersion,
		"plugin registry":           uint32(pluginregistry.CurrentVersion),
		"project command":           uint32(project.ProtocolVersion),
		"project file":              uint32(projectfile.CurrentVersion),
		"REST wire protocol":        protocol.CurrentVersion,
		"rich-text document":        richtext.DocumentVersion,
		"rich-text pairing":         richtext.AdminPluginPairingVersion,
		"schema manifest":           uint32(schema.CurrentVersion),
		"form-builder pairing":      formbuilder.AdminPluginPairingVersion,
		"SEO pairing":               seo.AdminPluginPairingVersion,
	}
	for contract, version := range versions {
		if version != 1 {
			t.Errorf("%s version = %d, want greenfield version 1 before the first public release", contract, version)
		}
	}
}
