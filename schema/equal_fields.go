package schema

import (
	"reflect"
	"slices"
)

// EqualFields reports whether two field lists, each bound to its own schema's
// block definitions, describe the same fields at every placement: equal field
// metadata and equal selected definitions. Lazy bindings and materialized
// placement views are not compared, so the result does not depend on how
// either schema was traversed. Each definition is compared once, so the cost
// follows definitions rather than placements.
func EqualFields(left, right []Field) bool {
	comparison := fieldsComparison{compared: map[string]bool{}}
	return comparison.lists(left, right)
}

type fieldsComparison struct {
	// left and right are the block registries of the two schemas, found at
	// their first bound containers.
	left, right *BlockDefinitions
	compared    map[string]bool
}

func (c *fieldsComparison) lists(left, right []Field) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !c.field(left[index], right[index]) {
			return false
		}
	}
	return true
}

func (c *fieldsComparison) field(left, right Field) bool {
	leftNested, rightNested := left.Nested, right.Nested
	leftBlocks, rightBlocks := left.Blocks, right.Blocks
	leftPlugin, rightPlugin := left.Plugin, right.Plugin
	left.Nested, left.Blocks, left.Plugin = nil, nil, nil
	right.Nested, right.Blocks, right.Plugin = nil, nil, nil
	if !reflect.DeepEqual(left, right) || (leftNested == nil) != (rightNested == nil) || (leftBlocks == nil) != (rightBlocks == nil) || (leftPlugin == nil) != (rightPlugin == nil) {
		return false
	}
	if leftNested != nil {
		leftMetadata, rightMetadata := *leftNested, *rightNested
		leftMetadata.Fields, leftMetadata.bound, rightMetadata.Fields, rightMetadata.bound = nil, nil, nil, nil
		if !reflect.DeepEqual(leftMetadata, rightMetadata) || !c.lists(leftNested.ResolvedFields(), rightNested.ResolvedFields()) {
			return false
		}
	}
	if leftBlocks != nil {
		if leftBlocks.MinRows != rightBlocks.MinRows || leftBlocks.MaxRows != rightBlocks.MaxRows || !c.selection(leftBlocks.BlockReferences, rightBlocks.BlockReferences, leftBlocks.bound, rightBlocks.bound) {
			return false
		}
	}
	if leftPlugin != nil {
		leftMetadata, rightMetadata := *leftPlugin, *rightPlugin
		leftMetadata.EmbeddedTrees, rightMetadata.EmbeddedTrees = nil, nil
		if !reflect.DeepEqual(leftMetadata, rightMetadata) || len(leftPlugin.EmbeddedTrees) != len(rightPlugin.EmbeddedTrees) {
			return false
		}
		for treeIndex, leftTree := range leftPlugin.EmbeddedTrees {
			rightTree := rightPlugin.EmbeddedTrees[treeIndex]
			if leftTree.Version != rightTree.Version || leftTree.Key != rightTree.Key || leftTree.Children != rightTree.Children || leftTree.Tag != rightTree.Tag || !slices.Equal(leftTree.Root, rightTree.Root) || len(leftTree.Cases) != len(rightTree.Cases) {
				return false
			}
			for caseIndex, leftCase := range leftTree.Cases {
				rightCase := rightTree.Cases[caseIndex]
				if leftCase.TagValue != rightCase.TagValue || leftCase.Payload != rightCase.Payload || leftCase.Discriminator != rightCase.Discriminator || leftCase.Identity != rightCase.Identity || !c.selection(leftCase.BlockReferences, rightCase.BlockReferences, leftCase.bound, rightCase.bound) {
					return false
				}
			}
		}
	}
	return true
}

// selection compares two containers' selected definitions, each once.
func (c *fieldsComparison) selection(left, right []string, leftBound, rightBound *boundBlocks) bool {
	if !slices.Equal(left, right) {
		return false
	}
	if c.left == nil && leftBound != nil {
		c.left = leftBound.scope.registry
	}
	if c.right == nil && rightBound != nil {
		c.right = rightBound.scope.registry
	}
	for _, slug := range left {
		if c.compared[slug] {
			continue
		}
		c.compared[slug] = true
		if c.left == nil || c.right == nil {
			return false
		}
		leftBlock, leftFound := c.left.templates[slug]
		rightBlock, rightFound := c.right.templates[slug]
		if !leftFound || !rightFound {
			if leftFound != rightFound {
				return false
			}
			continue
		}
		leftFields, rightFields := leftBlock.Fields, rightBlock.Fields
		leftBlock.Fields, leftBlock.bound, leftBlock.shared = nil, nil, nil
		rightBlock.Fields, rightBlock.bound, rightBlock.shared = nil, nil, nil
		if !reflect.DeepEqual(leftBlock, rightBlock) || !c.lists(leftFields, rightFields) {
			return false
		}
	}
	return true
}
