package richtext

import (
	"encoding/json"
	"fmt"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
)

// ValidateFields checks every declared rich-text host after all resource and
// graph transforms. Embedded children retain ordinary field
// ownership; only the plugin envelope and finite feature configuration are checked.
func (plugin) ValidateFields(context core.FieldGraphContext, graph field.Fields) error {
	work := 0
	var walk func(field.Fields, string, int) error
	walk = func(fields field.Fields, path string, depth int) error {
		if depth > 64 {
			return fmt.Errorf("%s: rich-text schema exceeds the finite configuration depth", path)
		}
		for _, node := range fields {
			definition := field.Snapshot(node)
			work++
			at := path + "." + definition.Name()
			if work > 10000 {
				return fmt.Errorf("%s: schema work budget exceeded", at)
			}
			if definition.PluginKey() == Key {
				var settings Config
				if err := json.Unmarshal(definition.PluginConfig(), &settings); err != nil {
					return fmt.Errorf("%s: invalid rich-text settings: %w", at, err)
				}
				enabled := false
				for _, feature := range settings.Features {
					switch feature {
					case FeatureLinks, FeatureLists, FeatureCode, FeatureHorizontalRule, FeatureUploads, FeatureRelationships:
					case FeatureBlocks:
						enabled = true
					default:
						return fmt.Errorf("%s.features: unknown rich-text feature %q", at, feature)
					}
				}
				trees := definition.EmbeddedTrees()
				if len(trees) != 1 || trees[0].Key != "blocks" || len(trees[0].Root) != 1 || trees[0].Root[0] != "root" || trees[0].Children != "children" || trees[0].Tag != "type" || len(trees[0].Cases) != 1 {
					return fmt.Errorf("%s: use richtext.Field to declare the supported embedded schema", at)
				}
				c := trees[0].Cases[0]
				if c.TagValue != "block" || c.Payload != "fields" || c.Discriminator != "blockType" || c.Identity != "_key" {
					return fmt.Errorf("%s: rich-text block envelope must use fields, blockType and _key", at)
				}
				if enabled && len(c.Types) == 0 {
					return fmt.Errorf("%s: FeatureBlocks requires explicit Config.Blocks fields", at)
				}
				if !enabled && len(c.Types) > 0 {
					return fmt.Errorf("%s: Config.Blocks fields were supplied while Features explicitly disables blocks", at)
				}
			}
			for _, branch := range definition.Branches() {
				branchPath := at
				if branch.Selector.Boundary == field.EmbeddedCase {
					branchPath += "." + branch.Selector.Tree + "." + branch.Selector.Case
				}
				if branch.Name != "" {
					branchPath += "." + branch.Name
				}
				if err := walk(branch.Fields, branchPath, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(graph, string(context.Slug), 0)
}

var _ core.FieldGraphValidator = plugin{}
