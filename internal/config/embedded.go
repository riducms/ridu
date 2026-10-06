package config

import (
	"strings"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/schema"
)

func (r *fieldResolver) resolveEmbeddedTrees(definition field.View, configPath string, path []string) []schema.EmbeddedTree {
	trees := definition.EmbeddedTrees()
	result := make([]schema.EmbeddedTree, len(trees))
	for i, t := range trees {
		resolved := schema.EmbeddedTree{Version: schema.EmbeddedTreeVersion, Key: t.Key, Root: append([]string{}, t.Root...), Children: t.Children, Tag: t.Tag, Cases: make([]schema.EmbeddedTreeCase, len(t.Cases))}
		for j, c := range t.Cases {
			// Cases select definitions resolved like any other, retaining the same
			// field restrictions, stable IDs, naming, defaults, and reference contracts.
			parent := append(append([]string{}, path...), t.Key)
			parent = append(parent, c.TagValue)
			// An empty selection reserves the envelope while admitting no payloads.
			for _, block := range c.Types {
				r.resolver.resolveRegisteredBlock(block)
			}
			resolvedCase := schema.EmbeddedTreeCase{TagValue: c.TagValue, Payload: c.Payload, Discriminator: c.Discriminator, Identity: c.Identity, BlockReferences: c.BlockReferences}
			resolved.Cases[j] = schema.ReferenceCase(resolvedCase, r.resolver.blockDefinitions, r.collectionID, parent)
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
