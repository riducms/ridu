package field

// EmbeddedTree describes a finite tree inside a plugin value. Root is a literal
// property path to a node (or a list of nodes). Children is a structural child
// list property. Payload objects are handed back to ordinary field processing;
// they are never searched for more nodes by this descriptor.
type EmbeddedTree struct {
	Key      string
	Root     []string
	Children string
	Tag      string
	Cases    []EmbeddedTreeCase
}

// EmbeddedTreeCase declares schema-owned payloads for one structural node tag.
// Discriminator and Identity name properties inside Payload. Types reuse the
// ordinary field vocabulary; plugin settings must not serialize these definitions.
type EmbeddedTreeCase struct {
	TagValue        string
	Payload         string
	Discriminator   string
	Identity        string
	Types           []Block
	BlockReferences []string
	referencesBound bool
}

// EmbeddedTrees returns detached declarative embedded schema definitions.
func (d View) EmbeddedTrees() []EmbeddedTree { return cloneEmbeddedTrees(d.pluginTrees) }

func cloneEmbeddedTrees(trees []EmbeddedTree) []EmbeddedTree {
	result := append([]EmbeddedTree(nil), trees...)
	for i := range result {
		result[i].Root = append([]string(nil), trees[i].Root...)
		result[i].Cases = append([]EmbeddedTreeCase(nil), trees[i].Cases...)
		for j := range result[i].Cases {
			result[i].Cases[j].BlockReferences = append([]string(nil), trees[i].Cases[j].BlockReferences...)
			result[i].Cases[j].Types = cloneBlocks(trees[i].Cases[j].Types)
		}
	}
	return result
}
