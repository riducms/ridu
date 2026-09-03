package graphql

import (
	enginegraphql "github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

// populationsFor translates selected relationship and upload fields into the
// operation engine's bounded population contract. A selected relationship
// chain increases Depth only as far as the query asks for it; the engine caps
// and authorizes every target collection in that chain.
func (builder *schemaBuilder) populationsFor(fields []schema.Field, info enginegraphql.ResolveInfo) []query.Population {
	seen := make(map[string]int)
	var result []query.Population
	var walk func([]schema.Field, *ast.SelectionSet, int, map[string]bool)
	walk = func(currentFields []schema.Field, selectionSet *ast.SelectionSet, wrapperDepth int, fragments map[string]bool) {
		if selectionSet == nil || wrapperDepth > 2 {
			return
		}
		byName := graphQLFieldsByName(currentFields)
		for _, selection := range selectionSet.Selections {
			switch selected := selection.(type) {
			case *ast.Field:
				field, exists := byName[selected.Name.Value]
				if !exists {
					walk(currentFields, selected.SelectionSet, wrapperDepth+1, fragments)
					continue
				}
				if selected.SelectionSet == nil {
					continue
				}
				if field.Nested != nil {
					walk(field.Nested.Fields, selected.SelectionSet, wrapperDepth, fragments)
					continue
				}
				if field.Blocks != nil {
					for _, block := range field.Blocks.Types {
						walk(block.Fields, selected.SelectionSet, wrapperDepth, fragments)
					}
					continue
				}
				if !isPopulatable(field) {
					continue
				}
				depth := builder.relationshipSelectionDepth(field, selected.SelectionSet, info, fragments)
				path := field.Path.String()
				if index, duplicate := seen[path]; duplicate {
					if depth > result[index].Depth {
						result[index].Depth = depth
					}
					continue
				}
				seen[path] = len(result)
				result = append(result, query.Population{Path: field.Path, Depth: depth})
			case *ast.InlineFragment:
				walk(currentFields, selected.SelectionSet, wrapperDepth, fragments)
			case *ast.FragmentSpread:
				name := selected.Name.Value
				if fragments[name] {
					continue
				}
				definition, exists := info.Fragments[name]
				fragment, ok := definition.(*ast.FragmentDefinition)
				if !exists || !ok {
					continue
				}
				cloned := make(map[string]bool, len(fragments)+1)
				for key, value := range fragments {
					cloned[key] = value
				}
				cloned[name] = true
				walk(currentFields, fragment.SelectionSet, wrapperDepth, cloned)
			}
		}
	}
	for _, field := range info.FieldASTs {
		walk(fields, field.SelectionSet, 0, map[string]bool{})
	}
	return result
}

// outputFieldsFor returns exactly the computed fields selected beneath the
// current root or join field. Inverse joins have dedicated GraphQL resolvers,
// so they are intentionally excluded from the operation engine's eager output
// resolution. The returned slice is always non-nil: selecting no computed
// fields must mean "resolve none", not the local API default of "resolve all".
func (builder *schemaBuilder) outputFieldsFor(fields []schema.Field, info enginegraphql.ResolveInfo) []query.Path {
	byName := graphQLFieldsByName(fields)
	result := make([]query.Path, 0)
	seen := make(map[string]bool)
	var walk func(*ast.SelectionSet, int, map[string]bool)
	walk = func(selectionSet *ast.SelectionSet, wrapperDepth int, fragments map[string]bool) {
		if selectionSet == nil || wrapperDepth > 2 {
			return
		}
		for _, selection := range selectionSet.Selections {
			switch selected := selection.(type) {
			case *ast.Field:
				field, exists := byName[selected.Name.Value]
				if !exists {
					walk(selected.SelectionSet, wrapperDepth+1, fragments)
					continue
				}
				if field.Type != schema.FieldTypeVirtual {
					continue
				}
				path := field.Path.String()
				if !seen[path] {
					seen[path] = true
					result = append(result, field.Path)
				}
			case *ast.InlineFragment:
				walk(selected.SelectionSet, wrapperDepth, fragments)
			case *ast.FragmentSpread:
				name := selected.Name.Value
				if fragments[name] {
					continue
				}
				definition, exists := info.Fragments[name]
				fragment, ok := definition.(*ast.FragmentDefinition)
				if !exists || !ok {
					continue
				}
				walk(fragment.SelectionSet, wrapperDepth, cloneFragmentSet(fragments, name))
			}
		}
	}
	for _, field := range info.FieldASTs {
		walk(field.SelectionSet, 0, map[string]bool{})
	}
	return result
}

func (builder *schemaBuilder) relationshipSelectionDepth(field schema.Field, selectionSet *ast.SelectionSet, info enginegraphql.ResolveInfo, fragments map[string]bool) int {
	depth := 1
	for _, target := range relationshipTargetsForField(field) {
		resource, exists := builder.resources[target.CollectionID]
		if !exists {
			continue
		}
		candidate := 1 + builder.selectedRelationshipDepth(resource.Fields, selectionSet, info, fragments, 1)
		if candidate > depth {
			depth = candidate
		}
	}
	if depth > 5 {
		return 5
	}
	return depth
}

func (builder *schemaBuilder) selectedRelationshipDepth(fields []schema.Field, selectionSet *ast.SelectionSet, info enginegraphql.ResolveInfo, fragments map[string]bool, level int) int {
	if selectionSet == nil || level >= 5 {
		return 0
	}
	byName := graphQLFieldsByName(fields)
	maximum := 0
	var walk func(*ast.SelectionSet, map[string]bool)
	walk = func(current *ast.SelectionSet, active map[string]bool) {
		if current == nil {
			return
		}
		for _, selection := range current.Selections {
			switch selected := selection.(type) {
			case *ast.Field:
				field, exists := byName[selected.Name.Value]
				if !exists {
					// Polymorphic relationships add a GraphQL-only value wrapper.
					if selected.Name.Value == "value" {
						walk(selected.SelectionSet, active)
					}
					continue
				}
				if selected.SelectionSet == nil {
					continue
				}
				if field.Nested != nil {
					candidate := builder.selectedRelationshipDepth(field.Nested.Fields, selected.SelectionSet, info, active, level)
					if candidate > maximum {
						maximum = candidate
					}
					continue
				}
				if field.Blocks != nil {
					for _, block := range field.Blocks.Types {
						candidate := builder.selectedRelationshipDepth(block.Fields, selected.SelectionSet, info, active, level)
						if candidate > maximum {
							maximum = candidate
						}
					}
					continue
				}
				if !isPopulatable(field) {
					continue
				}
				depth := 1
				for _, target := range relationshipTargetsForField(field) {
					resource, found := builder.resources[target.CollectionID]
					if !found {
						continue
					}
					candidate := 1 + builder.selectedRelationshipDepth(resource.Fields, selected.SelectionSet, info, active, level+1)
					if candidate > depth {
						depth = candidate
					}
				}
				if depth > maximum {
					maximum = depth
				}
			case *ast.InlineFragment:
				walk(selected.SelectionSet, active)
			case *ast.FragmentSpread:
				name := selected.Name.Value
				if active[name] {
					continue
				}
				definition, exists := info.Fragments[name]
				fragment, ok := definition.(*ast.FragmentDefinition)
				if !exists || !ok {
					continue
				}
				cloned := cloneFragmentSet(active, name)
				walk(fragment.SelectionSet, cloned)
			}
		}
	}
	walk(selectionSet, fragments)
	return maximum
}

func graphQLFieldsByName(fields []schema.Field) map[string]schema.Field {
	result := make(map[string]schema.Field, len(fields))
	for _, field := range fields {
		result[fieldName(field.Name)] = field
	}
	return result
}

func isPopulatable(field schema.Field) bool {
	return field.Type == schema.FieldTypeRelationship || field.Type == schema.FieldTypeUpload
}

func relationshipTargetsForField(field schema.Field) []schema.RelationshipTarget {
	if field.Relationship != nil {
		if field.Relationship.Polymorphic {
			return field.Relationship.Targets
		}
		return []schema.RelationshipTarget{{CollectionID: field.Relationship.CollectionID, CollectionSlug: field.Relationship.CollectionSlug}}
	}
	if field.Upload != nil {
		return []schema.RelationshipTarget{{CollectionID: field.Upload.CollectionID, CollectionSlug: field.Upload.CollectionSlug}}
	}
	return nil
}

func cloneFragmentSet(source map[string]bool, name string) map[string]bool {
	cloned := make(map[string]bool, len(source)+1)
	for key, value := range source {
		cloned[key] = value
	}
	cloned[name] = true
	return cloned
}
