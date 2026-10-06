package graphql

import (
	"fmt"
	"slices"
	"strings"

	enginegraphql "github.com/graphql-go/graphql"
	"github.com/riducms/ridu/internal/membership"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

// Filters mirror the schema's nesting. A group, array or Blocks field has its
// own input object, and a Blocks field's input selects a block by slug. A
// block has one filter input wherever it is placed, so the schema and the
// retained parse tree grow with block definitions, not placements. A node
// keeps only the members a filter needs to find its canonical path.
type whereNode struct {
	input   *enginegraphql.InputObject
	members []whereMember
}

// whereMember is a filterable field, or a block of a Blocks field's input, by
// GraphQL name. child is nil for a field filtered with operators.
type whereMember struct {
	name, segment string
	child         *whereNode
}

func (node *whereNode) member(name string) (whereMember, bool) {
	for _, member := range node.members {
		if member.name == name {
			return member, true
		}
	}
	return whereMember{}, false
}

// whereInput returns a resource's filter input, building its tree once.
func (builder *schemaBuilder) whereInput(current resource) *enginegraphql.InputObject {
	if existing := builder.where[current.ID]; existing != nil {
		return existing.input
	}
	root := &whereNode{}
	builder.where[current.ID] = root
	fields := builder.whereMembers(root, current.name, current.Fields)
	var input *enginegraphql.InputObject
	input = enginegraphql.NewInputObject(enginegraphql.InputObjectConfig{Name: current.name + "Where", Fields: enginegraphql.InputObjectConfigFieldMapThunk(func() enginegraphql.InputObjectConfigFieldMap {
		for _, name := range []string{"and", "or", "AND", "OR"} {
			fields[name] = &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.NewList(enginegraphql.NewNonNull(input))}
		}
		fields["not"] = &enginegraphql.InputObjectFieldConfig{Type: input}
		fields["NOT"] = &enginegraphql.InputObjectFieldConfig{Type: input}
		fields["id"] = &enginegraphql.InputObjectFieldConfig{Type: builder.stringOperators()}
		return fields
	})})
	root.input = input
	return input
}

// whereMembers adds fields' filters to node and returns their input fields.
// owner names the output object that declares fields, so select filters reuse
// that object's enums.
func (builder *schemaBuilder) whereMembers(node *whereNode, owner string, fields []schema.Field) enginegraphql.InputObjectConfigFieldMap {
	config := enginegraphql.InputObjectConfigFieldMap{}
	add := func(segment string, input enginegraphql.Input, child *whereNode) {
		name := fieldName(segment)
		node.members = append(node.members, whereMember{name: name, segment: segment, child: child})
		config[name] = &enginegraphql.InputObjectFieldConfig{Type: input}
	}
	for _, field := range fields {
		if field.QueryRestricted {
			continue
		}
		switch field.Type {
		case schema.FieldTypeGroup, schema.FieldTypeArray:
			if field.Nested != nil {
				name := owner + typeName(field.Name)
				if child := builder.whereObject(name+"Where", name, field.Nested.ResolvedFields()); child != nil {
					add(field.Name, child.input, child)
				}
			}
			continue
		case schema.FieldTypeBlocks:
			if child := builder.whereBlocks(owner, field); child != nil {
				add(field.Name, child.input, child)
			}
			continue
		case schema.FieldTypeRelationship:
			if field.Relationship == nil {
				continue
			}
		case schema.FieldTypeUpload:
			if field.Upload == nil {
				continue
			}
		case schema.FieldTypeText, schema.FieldTypeTextarea, schema.FieldTypeEmail, schema.FieldTypeCode, schema.FieldTypeDate,
			schema.FieldTypeNumber, schema.FieldTypeTextList, schema.FieldTypeNumberList, schema.FieldTypeCheckbox, schema.FieldTypeSelect, schema.FieldTypeRadio:
		default:
			continue
		}
		add(field.Name, builder.whereOperators(owner, field), nil)
	}
	node.members = slices.Clip(node.members)
	return config
}

// whereObject is the filter input for fields, or nil when none is filterable.
func (builder *schemaBuilder) whereObject(name, owner string, fields []schema.Field) *whereNode {
	node := &whereNode{}
	config := builder.whereMembers(node, owner, fields)
	if len(node.members) == 0 {
		return nil
	}
	node.input = enginegraphql.NewInputObject(enginegraphql.InputObjectConfig{Name: name, Fields: config})
	return node
}

// whereBlocks selects a Blocks field's filterable blocks by slug.
func (builder *schemaBuilder) whereBlocks(owner string, field schema.Field) *whereNode {
	if field.Blocks == nil {
		return nil
	}
	container := owner + typeName(field.Name)
	node := &whereNode{}
	config := enginegraphql.InputObjectConfigFieldMap{}
	for _, block := range field.Blocks.Definitions() {
		if child := builder.blockWhere(block); child != nil {
			name := fieldName(block.Slug)
			node.members = append(node.members, whereMember{name: name, segment: block.Slug, child: child})
			config[name] = &enginegraphql.InputObjectFieldConfig{Type: child.input}
		}
	}
	if len(node.members) == 0 {
		return nil
	}
	node.members = slices.Clip(node.members)
	node.input = enginegraphql.NewInputObject(enginegraphql.InputObjectConfig{Name: container + "Where", Fields: config})
	return node
}

// blockWhere shares one filter input for each block definition.
func (builder *schemaBuilder) blockWhere(block schema.BlockType) *whereNode {
	if node, built := builder.blockWheres[block.Slug]; built {
		return node
	}
	name := blockTypeName(block)
	node := builder.whereObject(name+"Where", name, block.ResolvedFields())
	builder.blockWheres[block.Slug] = node
	return node
}

// whereOperators is a filterable field's shared operator input. owner names
// the output object that declares field, whose select enum the filter reuses.
// Set-valued fields take membership operators only: in, not_in and exists.
func (builder *schemaBuilder) whereOperators(owner string, field schema.Field) enginegraphql.Input {
	switch membership.KindOf(field) {
	case membership.Numbers:
		return builder.membershipOperators("RiduNumberListWhere", enginegraphql.Float)
	case membership.References:
		return builder.referenceOperators(owner, field)
	case membership.Strings:
		if field.Type == schema.FieldTypeSelect {
			option := builder.selectEnum(owner, field)
			return builder.membershipOperators(option.Name()+"ManyWhere", option)
		}
		// Text lists and the IDs of has-many relationships and uploads.
		return builder.membershipOperators("RiduStringListWhere", enginegraphql.String)
	}
	switch field.Type {
	case schema.FieldTypeNumber:
		return builder.numberOperators()
	case schema.FieldTypeCheckbox:
		return builder.booleanOperators()
	case schema.FieldTypeSelect, schema.FieldTypeRadio:
		return builder.enumOperators(builder.selectEnum(owner, field))
	default:
		return builder.stringOperators()
	}
}

// referenceOperators filters a polymorphic relationship by membership. A
// candidate takes the GraphQL relationship shape, {relationTo, value}.
func (builder *schemaBuilder) referenceOperators(owner string, field schema.Field) *enginegraphql.InputObject {
	name := owner + typeName(field.Name) + "Reference"
	reference := builder.operatorInput(name+"Input", func() enginegraphql.InputObjectConfigFieldMap {
		return enginegraphql.InputObjectConfigFieldMap{
			"relationTo": {Type: enginegraphql.NewNonNull(builder.relationshipTargetEnum(name+"RelationTo", field.Relationship.Targets))},
			"value":      {Type: enginegraphql.NewNonNull(enginegraphql.ID)},
		}
	})
	return builder.membershipOperators(name+"Where", reference)
}

// whereExpression reads filters through the tree built with the resource's
// where input rather than inspecting its schema on every request.
func (builder *schemaBuilder) whereExpression(current resource, raw interface{}) (query.Expression, error) {
	values, _ := raw.(map[string]interface{})
	if len(values) == 0 {
		return nil, nil
	}
	builder.whereInput(current)
	return parseWhere(values, builder.where[current.ID], nil)
}

// parseWhere compiles one input object. Logical operators and id are the
// resource root's; nested inputs extend the canonical path prefix.
func parseWhere(values map[string]interface{}, node *whereNode, prefix []string) (query.Expression, error) {
	var expressions []query.Expression
	root := prefix == nil
	for _, key := range sortedKeys(values) {
		value := values[key]
		switch {
		case root && (key == "and" || key == "or" || key == "AND" || key == "OR"):
			rawChildren, _ := value.([]interface{})
			children := make([]query.Expression, 0, len(rawChildren))
			for _, rawChild := range rawChildren {
				childMap, _ := rawChild.(map[string]interface{})
				child, err := parseWhere(childMap, node, nil)
				if err != nil {
					return nil, err
				}
				if child != nil {
					children = append(children, child)
				}
			}
			combined, err := combineLogical(strings.ToLower(key), children)
			if err != nil {
				return nil, err
			}
			if combined != nil {
				expressions = append(expressions, combined)
			}
		case root && (key == "not" || key == "NOT"):
			childMap, _ := value.(map[string]interface{})
			child, err := parseWhere(childMap, node, nil)
			if err != nil {
				return nil, err
			}
			if child != nil {
				expressions = append(expressions, query.Not(child))
			}
		default:
			var segments []string
			if root && key == "id" {
				segments = []string{"id"}
			} else if member, found := node.member(key); found && member.child == nil {
				segments = append(append([]string(nil), prefix...), member.segment)
			} else if found {
				nested, _ := value.(map[string]interface{})
				expression, err := parseWhere(nested, member.child, append(append([]string{}, prefix...), member.segment))
				if err != nil {
					return nil, err
				}
				if expression != nil {
					expressions = append(expressions, expression)
				}
				continue
			} else {
				return nil, fmt.Errorf("unknown where field %q", key)
			}
			path, err := query.NewPath(segments...)
			if err != nil {
				return nil, err
			}
			operators, _ := value.(map[string]interface{})
			for _, operator := range sortedKeys(operators) {
				expression, err := comparison(path, operator, operators[operator])
				if err != nil {
					return nil, err
				}
				expressions = append(expressions, expression)
			}
		}
	}
	return combineLogical("and", expressions)
}

func comparison(path query.Path, operator string, raw interface{}) (query.Expression, error) {
	value := queryValue(raw)
	var kind query.Operator
	switch operator {
	case "equals":
		kind = query.OperatorEqual
	case "not_equals":
		kind = query.OperatorNotEqual
	case "in", "not_in":
		kind = query.OperatorIn
	case "contains":
		kind = query.OperatorContains
	case "like":
		kind = query.OperatorLike
	case "greater_than":
		kind = query.OperatorGreaterThan
	case "greater_than_equal":
		kind = query.OperatorGreaterThanEqual
	case "less_than":
		kind = query.OperatorLessThan
	case "less_than_equal":
		kind = query.OperatorLessThanEqual
	case "exists":
		kind = query.OperatorExists
	default:
		return nil, fmt.Errorf("unknown where operator %q", operator)
	}
	expression, err := query.Compare(path, kind, value)
	if err != nil {
		return nil, err
	}
	if operator == "not_in" {
		return query.Not(expression), nil
	}
	return expression, nil
}

func queryValue(raw interface{}) query.Value {
	switch value := raw.(type) {
	case nil:
		return query.Null()
	case string:
		return query.String(value)
	case float64:
		return query.Number(value)
	case int:
		return query.Number(float64(value))
	case bool:
		return query.Boolean(value)
	case []interface{}:
		items := make([]query.Value, len(value))
		for index, item := range value {
			items[index] = queryValue(item)
		}
		return query.List(items...)
	case map[string]interface{}:
		// A polymorphic candidate in the GraphQL relationship shape.
		relationTo, _ := value["relationTo"].(string)
		id, _ := value["value"].(string)
		return query.Reference(relationTo, id)
	default:
		return query.String(fmt.Sprint(value))
	}
}

func combineLogical(kind string, expressions []query.Expression) (query.Expression, error) {
	switch len(expressions) {
	case 0:
		return nil, nil
	case 1:
		return expressions[0], nil
	}
	if kind == "or" {
		return query.Or(expressions...), nil
	}
	return query.And(expressions...), nil
}
