package mongodb

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/riducms/ridu/internal/membership"
	"github.com/riducms/ridu/internal/primitivefield"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// mongoNestedPath is a filter path through arrays, blocks or a localized
// container. Each level is one value scope: the document's authored values for
// the first, and a row of the repeated field that ends the previous level
// otherwise. A level's segments lead from its scope to that repeated field, or
// to the leaf on the last level. A localized ancestor clears its descendants'
// localization, so at most one segment of a path stores a locale map.
type mongoNestedPath struct {
	path query.Path
	// base addresses the authored values beneath the scope's storage prefix.
	base        string
	root        schema.Field
	levels      []mongoNestedLevel
	leaf        schema.Field
	leafKind    mongoNestedLeafKind
	kind        mongoScalarKind
	localeChain []schema.LocaleCode
}

type mongoNestedLevel struct {
	segments []string
	// localized counts the segments up to and including a localized field,
	// whose stored value is a locale map. Zero means none.
	localized int
	// blockType binds the level's rows to one block definition.
	blockType string
}

type mongoNestedLeafKind uint8

const (
	mongoNestedScalarLeaf mongoNestedLeafKind = iota + 1
	// mongoNestedListLeaf is a list of strings or numbers compared by
	// membership: a text or number list, or a has-many select, relationship
	// or upload, whose items are option values or document IDs.
	mongoNestedListLeaf
	// mongoNestedReferenceLeaf is a polymorphic relationship compared by
	// membership: one {relationTo, id} object, or a list of them.
	mongoNestedReferenceLeaf
	// mongoNestedJSONLeaf compares an opaque JSON or plugin value by the
	// operand's type: a stored scalar of that type can match it.
	mongoNestedJSONLeaf
	// mongoNestedPresenceLeaf is a value no scalar operand equals: a group,
	// array or blocks container, or a point. Only presence and null
	// comparisons can match it.
	mongoNestedPresenceLeaf
)

// mongoNestedLeafKindOf classifies a field as a nested-path leaf; ok is
// false for a field without stored query semantics.
func mongoNestedLeafKindOf(field schema.Field) (mongoNestedLeafKind, mongoScalarKind, bool) {
	switch membership.KindOf(field) {
	case membership.Strings, membership.Numbers:
		return mongoNestedListLeaf, 0, true
	case membership.References:
		return mongoNestedReferenceLeaf, 0, true
	}
	if field.Type == schema.FieldTypeJSON || field.Type == schema.FieldTypePlugin {
		return mongoNestedJSONLeaf, 0, true
	}
	if kind, supported := mongoScalarFieldKind(field); supported {
		return mongoNestedScalarLeaf, kind, true
	}
	switch field.Type {
	case schema.FieldTypeGroup, schema.FieldTypeArray, schema.FieldTypeBlocks, schema.FieldTypePoint,
		schema.FieldTypeRelationship, schema.FieldTypeUpload:
		return mongoNestedPresenceLeaf, 0, true
	}
	return 0, 0, false
}

// comparedKind is the stored scalar kind an operand can equal or order
// against at the leaf; ok is false when no stored value can match it.
func (path mongoNestedPath) comparedKind(operand query.Value) (mongoScalarKind, bool) {
	switch path.leafKind {
	case mongoNestedScalarLeaf:
		return path.kind, true
	case mongoNestedJSONLeaf:
		switch operand.Kind() {
		case query.ValueString:
			return mongoStringScalar, true
		case query.ValueNumber:
			return mongoNumberScalar, true
		case query.ValueBoolean:
			return mongoBooleanScalar, true
		}
	}
	return 0, false
}

// mongoLeafTest is a condition on the leaf at value, a dotted path relative to
// the innermost scope. never reports that no leaf value meets it; a nil
// condition with never false is met by every value the path reaches.
type mongoLeafTest func(value string) (condition bson.D, never bool)

// resolveMongoNestedPath resolves a filter or access path that crosses an
// array, blocks field or localized container, that ends at an opaque or
// presence-only value, or that ends at a nested or localized has-many select.
// ok is false for any other path, which resolveMongoPredicatePath handles.
// Resolution reads only the schema.
func resolveMongoNestedPath(collection schema.Collection, path query.Path, role string, scope mongoPredicateScope) (mongoNestedPath, bool, error) {
	segments := path.Segments()
	if len(segments) == 0 || collection.Upload != nil && segments[0] == "sizes" {
		return mongoNestedPath{}, false, nil
	}
	resolved := mongoNestedPath{
		path: path, base: scope.path(mongoAuthoredValuesPath),
		levels: []mongoNestedLevel{{}}, localeChain: scope.localeChain,
	}
	fields := collection.Fields
	nested := false
	for index := 0; index < len(segments); index++ {
		field, found := mongoFieldNamed(fields, segments[index])
		if !found {
			if nested {
				return mongoNestedPath{}, true, fmt.Errorf("MongoDB %s field %q is not in collection %q", role, path.String(), collection.Slug)
			}
			return mongoNestedPath{}, false, nil
		}
		level := &resolved.levels[len(resolved.levels)-1]
		level.segments = append(level.segments, field.Name)
		if field.Localized {
			level.localized = len(level.segments)
		}
		if index == 0 {
			resolved.root = field
		}
		if index == len(segments)-1 {
			resolved.leaf = field
			kind, _, _ := mongoNestedLeafKindOf(field)
			switch kind {
			case mongoNestedJSONLeaf, mongoNestedPresenceLeaf, mongoNestedReferenceLeaf:
				nested = true
			case mongoNestedListLeaf:
				// Text and number lists outside repeated rows compile with
				// their locale fallback as primitive-list paths.
				nested = nested || !primitivefield.IsList(field)
			}
			break
		}
		nested = nested || field.Localized
		switch {
		case field.Type == schema.FieldTypeGroup && field.Nested != nil:
			fields = field.Nested.ResolvedFields()
		case field.Type == schema.FieldTypeArray && field.Nested != nil:
			nested = true
			resolved.levels = append(resolved.levels, mongoNestedLevel{})
			fields = field.Nested.ResolvedFields()
		case field.Type == schema.FieldTypeBlocks && field.Blocks != nil:
			nested = true
			index++
			block, found := field.Blocks.Definition(segments[index])
			if !found || index == len(segments)-1 {
				return mongoNestedPath{}, true, fmt.Errorf("MongoDB %s path %q must select a block type and a field inside blocks %q", role, path.String(), field.Name)
			}
			resolved.levels = append(resolved.levels, mongoNestedLevel{blockType: block.Slug})
			fields = block.ResolvedFields()
		default:
			if nested {
				return mongoNestedPath{}, true, fmt.Errorf("MongoDB %s field %q inside path %q cannot be traversed", role, field.Name, path.String())
			}
			return mongoNestedPath{}, false, nil
		}
	}
	if !nested {
		return mongoNestedPath{}, false, nil
	}
	if !mongoRepeatedPredicateRole(role) {
		if len(resolved.levels) > 1 {
			return mongoNestedPath{}, true, fmt.Errorf("MongoDB %s does not support repeated path %q", role, path.String())
		}
		return mongoNestedPath{}, true, fmt.Errorf("MongoDB %s does not support path %q", role, path.String())
	}
	for _, level := range resolved.levels {
		if level.localized == 0 {
			continue
		}
		if len(scope.localeChain) == 0 {
			return mongoNestedPath{}, true, fmt.Errorf("MongoDB %s path %q requires a locale chain", role, path.String())
		}
		if _, err := mongoConfiguredLocales(scope.localeChain); err != nil {
			return mongoNestedPath{}, true, fmt.Errorf("MongoDB %s path %q locale chain: %w", role, path.String(), err)
		}
	}
	leafKind, kind, supported := mongoNestedLeafKindOf(resolved.leaf)
	if !supported {
		return mongoNestedPath{}, true, fmt.Errorf("MongoDB %s path %q ends at unsupported field type %q", role, path.String(), resolved.leaf.Type)
	}
	resolved.leafKind, resolved.kind = leafKind, kind
	return resolved, true, nil
}

// leafLocalized reports whether the leaf itself stores a locale map, so a
// reached value is the first visible locale of its chain.
func (path mongoNestedPath) leafLocalized() bool {
	last := path.levels[len(path.levels)-1]
	return last.localized != 0 && last.localized == len(last.segments)
}

// some is a predicate that holds when some value the path reaches meets test.
// A value is reached through a row at each repeated level, bound to its block
// type when the level names one, and through the first visible locale of a
// localized segment. ok is false when no value can meet test.
func (path mongoNestedPath) some(test mongoLeafTest) (bson.D, bool) {
	return path.someFrom(0, path.base, test)
}

func (path mongoNestedPath) someFrom(depth int, base string, test mongoLeafTest) (bson.D, bool) {
	level := path.levels[depth]
	reach := func(target string, guards []bson.D) (bson.D, bool) {
		if depth == len(path.levels)-1 {
			condition, never := test(target)
			if never {
				return nil, false
			}
			if condition != nil {
				guards = append(guards, condition)
			}
			return mongoAnd(guards), true
		}
		rows, ok := path.someFrom(depth+1, "", test)
		if !ok {
			return nil, false
		}
		match := make([]bson.D, 0, 3)
		if blockType := path.levels[depth+1].blockType; blockType != "" {
			match = append(match, mongoTypeGuard("blockType", "string"), bson.D{{Key: "blockType", Value: bson.D{{Key: "$eq", Value: blockType}}}})
		}
		if len(rows) != 0 {
			match = append(match, rows)
		}
		return mongoAnd(append(guards, bson.D{{Key: target, Value: bson.D{{Key: "$elemMatch", Value: mongoAnd(match)}}}})), true
	}
	if level.localized == 0 {
		return reach(base+strings.Join(level.segments, "."), mongoObjectGuards(base, level.segments[:len(level.segments)-1]))
	}
	prefix, rest := level.segments[:level.localized], level.segments[level.localized:]
	locales := base + strings.Join(prefix, ".")
	shared := append(mongoObjectGuards(base, prefix[:len(prefix)-1]), mongoTypeGuard(locales, "object"))
	branches := make([]bson.D, 0, len(path.localeChain))
	skipped := make([]bson.D, 0, len(path.localeChain))
	for index, locale := range path.localeChain {
		candidate := locales + "." + string(locale)
		guards := append(append(append([]bson.D(nil), shared...), skipped...), mongoLocaleSelected(candidate, index == len(path.localeChain)-1))
		target := candidate
		if len(rest) != 0 {
			guards = append(guards, mongoTypeGuard(candidate, "object"))
			guards = append(guards, mongoObjectGuards(candidate+".", rest[:len(rest)-1])...)
			target += "." + strings.Join(rest, ".")
		}
		if branch, ok := reach(target, guards); ok {
			branches = append(branches, branch)
		}
		skipped = append(skipped, mongoLocaleSkipped(candidate))
	}
	if len(branches) == 0 {
		return nil, false
	}
	return mongoOr(branches), true
}

// mongoObjectGuards requires each ancestor, a dotted path below base, to be a
// stored object, so a dotted query never descends through an array instead.
func mongoObjectGuards(base string, ancestors []string) []bson.D {
	guards := make([]bson.D, 0, len(ancestors))
	for index := range ancestors {
		guards = append(guards, mongoTypeGuard(base+strings.Join(ancestors[:index+1], "."), "object"))
	}
	return guards
}

// mongoLocaleSkipped matches a locale value that fallback passes over:
// absent, null or an empty string.
func mongoLocaleSkipped(candidate string) bson.D {
	return bson.D{{Key: "$or", Value: bson.A{
		bson.D{{Key: candidate, Value: bson.D{{Key: "$exists", Value: false}}}},
		mongoExactNullPredicate(candidate),
		bson.D{{Key: candidate, Value: bson.D{{Key: "$eq", Value: ""}, {Key: "$not", Value: bson.D{{Key: "$type", Value: "array"}}}}}},
	}}}
}

// mongoLocaleSelected matches the locale value fallback selects at its chain
// position: any present value for the last locale, otherwise one fallback
// does not pass over.
func mongoLocaleSelected(candidate string, last bool) bson.D {
	if last {
		return bson.D{{Key: "$nor", Value: bson.A{mongoNullPredicate(candidate)}}}
	}
	return bson.D{{Key: "$nor", Value: bson.A{mongoLocaleSkipped(candidate)}}}
}

// compileMongoNestedComparison compiles a comparison over every value a
// nested path reaches with the store-independent multi-value semantics:
// positive operators hold when some value meets them, NotEqual and Exists
// false negate that, and an Equal null also holds when no value is present.
func compileMongoNestedComparison(path mongoNestedPath, comparison query.Comparison) (bson.D, error) {
	if err := membership.ValidateComparison(path.leaf, comparison); err != nil {
		return nil, err
	}
	some := func(test mongoLeafTest) bson.D {
		predicate, ok := path.some(test)
		if !ok {
			return mongoConstant(false)
		}
		return predicate
	}
	localized := path.leafLocalized()
	present := func(value string) (bson.D, bool) {
		if localized {
			return nil, false
		}
		return bson.D{{Key: value, Value: bson.D{{Key: "$exists", Value: true}}}}, false
	}
	nonNull := func(value string) (bson.D, bool) {
		if localized {
			return nil, false
		}
		return mongoNonNullPredicate(value), false
	}
	// Fallback never selects a null locale value, so a localized leaf reaches
	// no explicit null.
	explicitNull := func(value string) (bson.D, bool) {
		return mongoExactNullPredicate(value), localized
	}
	equal := func(expected query.Value) mongoLeafTest {
		return func(value string) (bson.D, bool) {
			kind, comparable := path.comparedKind(expected)
			if !comparable {
				return nil, true
			}
			condition, compatible, err := compileMongoEquality(mongoPredicatePath{storagePath: value, kind: kind}, expected)
			if err != nil || !compatible {
				return nil, true
			}
			return condition, false
		}
	}
	var predicate bson.D
	switch comparison.Operator {
	case query.OperatorExists:
		want, _ := comparison.Value.BooleanValue()
		predicate = some(nonNull)
		if !want {
			predicate = mongoNot(predicate)
		}
	case query.OperatorEqual, query.OperatorNotEqual:
		if comparison.Value.Kind() == query.ValueNull {
			predicate = mongoOr([]bson.D{mongoNot(some(present)), some(explicitNull)})
		} else {
			predicate = some(equal(comparison.Value))
		}
		if comparison.Operator == query.OperatorNotEqual {
			predicate = mongoNot(predicate)
		}
	case query.OperatorIn:
		if path.leafKind == mongoNestedListLeaf {
			candidates := make(bson.A, 0, len(comparison.Value.Values()))
			for _, item := range comparison.Value.Values() {
				if text, ok := item.StringValue(); ok {
					candidates = append(candidates, text)
				} else if number, ok := item.NumberValue(); ok {
					candidates = append(candidates, number)
				}
			}
			predicate = some(func(value string) (bson.D, bool) {
				return bson.D{{Key: value, Value: bson.D{{Key: "$elemMatch", Value: bson.D{{Key: "$in", Value: candidates}}}}}}, false
			})
			break
		}
		if path.leafKind == mongoNestedReferenceLeaf {
			predicate = some(func(value string) (bson.D, bool) {
				return mongoReferenceMembership(value, path.leaf.Relationship.HasMany, comparison.Value.Values()), false
			})
			break
		}
		matches := make([]bson.D, 0, len(comparison.Value.Values())+1)
		for _, item := range comparison.Value.Values() {
			if item.Kind() == query.ValueNull {
				matches = append(matches, mongoNot(some(present)), some(explicitNull))
				continue
			}
			matches = append(matches, some(equal(item)))
		}
		predicate = mongoOr(matches)
	case query.OperatorContains, query.OperatorLike:
		text, _ := comparison.Value.StringValue()
		predicate = some(func(value string) (bson.D, bool) {
			// The type guard excludes arrays: a JSON list never contains text.
			if kind, comparable := path.comparedKind(comparison.Value); !comparable || kind != mongoStringScalar {
				return nil, true
			}
			words := []string{text}
			if comparison.Operator == query.OperatorLike {
				words = strings.Fields(text)
			}
			conditions := []bson.D{mongoTypeGuard(value, "string")}
			for _, word := range words {
				conditions = append(conditions, bson.D{{Key: value, Value: bson.D{{Key: "$regex", Value: bson.Regex{Pattern: regexp.QuoteMeta(word), Options: "i"}}}}})
			}
			return mongoAnd(conditions), false
		})
	case query.OperatorGreaterThan, query.OperatorGreaterThanEqual, query.OperatorLessThan, query.OperatorLessThanEqual:
		operator := map[query.Operator]string{
			query.OperatorGreaterThan: "$gt", query.OperatorGreaterThanEqual: "$gte",
			query.OperatorLessThan: "$lt", query.OperatorLessThanEqual: "$lte",
		}[comparison.Operator]
		predicate = some(func(value string) (bson.D, bool) {
			kind, comparable := path.comparedKind(comparison.Value)
			if !comparable {
				return nil, true
			}
			expected, compatible, err := mongoOperand(mongoPredicatePath{storagePath: value, kind: kind}, comparison.Value)
			if err != nil || !compatible {
				return nil, true
			}
			return mongoAnd([]bson.D{
				mongoTypeGuard(value, mongoBSONType(kind)),
				{{Key: value, Value: bson.D{{Key: operator, Value: expected}}}},
			}), false
		})
	default:
		return nil, fmt.Errorf("MongoDB predicate has unsupported comparison operator %q", comparison.Operator)
	}
	return mongoAnd([]bson.D{mongoNestedShapePredicate(path), predicate}), nil
}

// mongoReferenceMembership holds when the polymorphic relationship at value,
// one {relationTo, id} object or a list of them, holds a reference equal to
// one of candidates. Both members are compared exactly; a list matches when
// one of its objects matches a candidate entirely.
func mongoReferenceMembership(value string, hasMany bool, candidates []query.Value) bson.D {
	matches := make([]bson.D, 0, len(candidates))
	for _, candidate := range candidates {
		relationTo, id, _ := candidate.ReferenceValue()
		if hasMany {
			matches = append(matches, bson.D{{Key: value, Value: bson.D{{Key: "$elemMatch", Value: bson.D{
				{Key: "relationTo", Value: bson.D{{Key: "$eq", Value: relationTo}}},
				{Key: "id", Value: bson.D{{Key: "$eq", Value: id}}},
			}}}}})
			continue
		}
		matches = append(matches, mongoAnd([]bson.D{
			mongoTypeGuard(value, "object"),
			{{Key: value + ".relationTo", Value: bson.D{{Key: "$eq", Value: relationTo}}}},
			{{Key: value + ".id", Value: bson.D{{Key: "$eq", Value: id}}}},
		}))
	}
	return mongoOr(matches)
}

func mongoNot(predicate bson.D) bson.D {
	return bson.D{{Key: "$nor", Value: bson.A{predicate}}}
}

// mongoNestedShapePredicate guards the stored shape of everything a nested
// path compares, so a negated or decoder-free predicate cannot admit a value
// the strict decoder rejects: the root field with every row the path reaches
// described by its declared fields, and unique row keys in each list it
// traverses. Rows off the path are described by identity alone, keeping the
// description proportional to the path rather than to the block graph.
func mongoNestedShapePredicate(path mongoNestedPath) bson.D {
	guards := []bson.D{{{Key: "$jsonSchema", Value: mongoJSONSchemaAtPath(
		strings.Split(path.base+path.root.Name, "."),
		mongoGuidedFieldJSONSchema(path.root, path.path.Segments()[1:]),
		false,
	)}}}
	if len(path.levels) > 1 {
		guards = append(guards, bson.D{{Key: "$expr", Value: path.rowKeysFrom(0, "$"+strings.TrimSuffix(path.base, "."))}})
	}
	return mongoAnd(guards)
}

// rowKeysFrom checks the lists that level depth reaches from scope, an
// aggregation expression for the level's object, and every list the path
// traverses beneath their rows. Each list's row keys must be unique.
func (path mongoNestedPath) rowKeysFrom(depth int, scope string) any {
	level := path.levels[depth]
	rowVariable := fmt.Sprintf("riduRow%d", depth)
	check := func(list string) any {
		conditions := bson.A{mongoUniqueRowKeysExpression(list)}
		if depth+1 < len(path.levels)-1 {
			rows := any(list)
			if blockType := path.levels[depth+1].blockType; blockType != "" {
				rows = bson.D{{Key: "$filter", Value: bson.D{
					{Key: "input", Value: list}, {Key: "as", Value: rowVariable},
					{Key: "cond", Value: bson.D{{Key: "$eq", Value: bson.A{"$$" + rowVariable + ".blockType", blockType}}}},
				}}}
			}
			conditions = append(conditions, bson.D{{Key: "$allElementsTrue", Value: bson.A{bson.D{{Key: "$map", Value: bson.D{
				{Key: "input", Value: rows}, {Key: "as", Value: rowVariable},
				{Key: "in", Value: path.rowKeysFrom(depth+1, "$$"+rowVariable)},
			}}}}}})
		}
		return bson.D{{Key: "$cond", Value: bson.A{
			bson.D{{Key: "$eq", Value: bson.A{bson.D{{Key: "$type", Value: list}}, "array"}}},
			bson.D{{Key: "$and", Value: conditions}},
			true,
		}}}
	}
	if level.localized == 0 {
		return check(scope + "." + strings.Join(level.segments, "."))
	}
	// Shape does not depend on the request locale: check every stored locale.
	locales := scope + "." + strings.Join(level.segments[:level.localized], ".")
	localeVariable := fmt.Sprintf("riduLocale%d", depth)
	target := "$$" + localeVariable + ".v"
	if rest := level.segments[level.localized:]; len(rest) != 0 {
		target += "." + strings.Join(rest, ".")
	}
	return bson.D{{Key: "$allElementsTrue", Value: bson.A{bson.D{{Key: "$map", Value: bson.D{
		{Key: "input", Value: bson.D{{Key: "$cond", Value: bson.A{
			bson.D{{Key: "$eq", Value: bson.A{bson.D{{Key: "$type", Value: locales}}, "object"}}},
			bson.D{{Key: "$objectToArray", Value: locales}},
			bson.A{},
		}}}},
		{Key: "as", Value: localeVariable},
		{Key: "in", Value: check(target)},
	}}}}}}
}

// mongoGuidedFieldJSONSchema describes a stored field as a repeated-path
// shape guard needs: rows by their declared fields and repeated fields among
// them by row identity, except that the field rest continues through is
// described the same way, recursively. rest is the remaining query path.
func mongoGuidedFieldJSONSchema(field schema.Field, rest []string) bson.D {
	if field.Localized {
		unlocalized := field
		unlocalized.Localized = false
		return bson.D{
			{Key: "bsonType", Value: "object"},
			{Key: "additionalProperties", Value: false},
			{Key: "patternProperties", Value: bson.D{{Key: mongoLocaleCodePattern, Value: mongoNullableJSONSchema(mongoGuidedFieldJSONSchema(unlocalized, rest))}}},
		}
	}
	if len(rest) == 0 {
		return mongoCollectionFieldJSONSchema(field, nil, mongoRowFields)
	}
	switch {
	case field.Type == schema.FieldTypeGroup && field.Nested != nil:
		result := mongoGuidedObjectJSONSchema(field.Nested.ResolvedFields(), false, "", rest)
		result[0].Value = bson.A{"object", "null"}
		return result
	case field.Type == schema.FieldTypeArray && field.Nested != nil:
		return mongoRepeatedArrayJSONSchema(field, mongoGuidedObjectJSONSchema(field.Nested.ResolvedFields(), true, "", rest))
	case field.Type == schema.FieldTypeBlocks && field.Blocks != nil:
		types := field.Blocks.ResolvedTypes()
		variants := make(bson.A, len(types))
		for index, block := range types {
			if block.Slug == rest[0] && len(rest) > 1 {
				variants[index] = mongoGuidedObjectJSONSchema(block.ResolvedFields(), true, block.Slug, rest[1:])
			} else {
				variants[index] = mongoCollectionObjectJSONSchema(block.ResolvedFields(), true, block.Slug, nil, mongoRowIdentity)
			}
		}
		return mongoRepeatedArrayJSONSchema(field, bson.D{{Key: "anyOf", Value: variants}})
	default:
		return mongoCollectionFieldJSONSchema(field, nil, mongoRowFields)
	}
}

// mongoGuidedObjectJSONSchema describes an object's declared fields with the
// field named by rest described along rest.
func mongoGuidedObjectJSONSchema(fields []schema.Field, row bool, blockType string, rest []string) bson.D {
	result := mongoCollectionObjectJSONSchema(fields, row, blockType, nil, mongoRowIdentity)
	field, found := mongoFieldNamed(fields, rest[0])
	if !found {
		return result
	}
	for index := range result {
		if result[index].Key != "properties" {
			continue
		}
		properties := append(bson.D(nil), result[index].Value.(bson.D)...)
		for property := range properties {
			if properties[property].Key == field.Name {
				properties[property].Value = mongoGuidedFieldJSONSchema(field, rest[1:])
			}
		}
		result[index].Value = properties
	}
	return result
}
