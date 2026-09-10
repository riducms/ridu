package config

import (
	"fmt"
	"strings"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
)

func (r *fieldResolver) resolveEmbeddedTrees(definition field.View, configPath string, path []string) []schema.EmbeddedTree {
	trees := definition.EmbeddedTrees()
	result := make([]schema.EmbeddedTree, len(trees))
	for i, t := range trees {
		p := fmt.Sprintf("%s.plugin.embeddedTrees[%d]", configPath, i)
		resolved := schema.EmbeddedTree{Version: schema.EmbeddedTreeVersion, Key: t.Key, Root: append([]string{}, t.Root...), Children: t.Children, Tag: t.Tag, Cases: make([]schema.EmbeddedTreeCase, len(t.Cases))}
		for j, c := range t.Cases {
			// Resolve a schema-only ordinary Blocks definition to retain the exact same
			// field restrictions, stable IDs, naming, defaults, and reference contracts.
			parent := append(append([]string{}, path...), t.Key)
			parent = append(parent, c.TagValue)
			container := &schema.BlocksField{}
			if len(c.Types)+len(c.BlockReferences) > 0 {
				container = r.resolveBlocks(c.Types, c.BlockReferences, fmt.Sprintf("%s.cases[%d]", p, j), parent)
			}
			resolvedCase := schema.EmbeddedTreeCase{TagValue: c.TagValue, Payload: c.Payload, Discriminator: c.Discriminator, Identity: c.Identity, Types: container.Types, BlockReferences: container.BlockReferences}
			if len(c.BlockReferences) > 0 {
				resolvedCase = schema.ReferenceCase(resolvedCase, r.resolver.blockTemplates, r.collectionID, parent)
			}
			resolved.Cases[j] = resolvedCase
		}
		result[i] = resolved
	}
	if err := schema.ValidateEmbeddedTrees(result, configPath+".plugin.embeddedTrees"); err != nil {
		message := err.Error()
		p := configPath + ".plugin.embeddedTrees"
		if at := strings.Index(message, ":"); at >= 0 {
			p = message[:at]
			message = strings.TrimSpace(message[at+1:])
		}
		r.resolver.issue("invalid_embedded_tree", p, message)
	}
	return result
}
