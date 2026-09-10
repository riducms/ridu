package color

import "github.com/riducms/ridu"

// Match pairingVersion in the TypeScript admin plugin.
const AdminPluginPairingVersion = 1

func (plugin) Descriptor() ridu.PluginDescriptor {
	// Tell Ridu which admin package and export to import.
	admin := ridu.AdminPluginMetadata{
		Package:        "@acme/ridu-color-admin",
		Export:         "colorAdminPlugin",
		APIVersion:     ridu.AdminPluginAPIVersion,
		PairingVersion: AdminPluginPairingVersion,
	}

	return ridu.PluginDescriptor{
		Version:    "1.0.0",
		GoPackage:  "github.com/riducms/ridu/examples/documentation/custom-components/color",
		APIVersion: ridu.PluginAPIVersion,
		Ridu: ridu.RiduCompatibility{
			Minimum: ridu.FrameworkVersion,
		},
		Admin: &admin,
		// Import these value types into generated application code.
		FieldTypes: []ridu.PluginFieldType{{
			Key:               Key,
			TypeScriptPackage: "@acme/ridu-color-admin",
			TypeScriptOutput:  "Color",
			TypeScriptInput:   "ColorInput",
			GoPackage:         "github.com/riducms/ridu/examples/documentation/custom-components/color",
			GoType:            "Value",
			JSONSchema: []byte(
				`{"type":"string","pattern":"^#[0-9A-Fa-f]{6}$"}`,
			),
		}},
	}
}
