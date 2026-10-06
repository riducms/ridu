package mongodb

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/riducms/ridu/internal/membership"
	"github.com/riducms/ridu/internal/primitivefield"
	"github.com/riducms/ridu/internal/querypath"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
)

const (
	mongoIDPath              = "_id"
	mongoCreatedAtPath       = "meta.createdAt"
	mongoUpdatedAtPath       = "meta.updatedAt"
	mongoDeletedAtPath       = "meta.deletedAt"
	mongoStatusPath          = "meta.status"
	mongoRevisionPath        = "meta.revision"
	mongoCodecPath           = "meta.codec"
	mongoIncarnationPath     = "meta.incarnation"
	mongoFencePath           = "meta.fence"
	mongoAuthoredValuesPath  = "values."
	mongoAscendingDirection  = int32(1)
	mongoDescendingDirection = int32(-1)
	maxMongoSortTerms        = 32
	mongoNoNULStringPattern  = `^[^\x00]*$`
	mongoNonBlankKeyPattern  = `^[^\x00]*[^\x00\x09-\x0d\x20\x{0085}\x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}][^\x00]*$`
	mongoLocaleCodePattern   = `^(?=.{1,35}$)(?!(?i:all|false|none|null)$)[A-Za-z0-9]+(?:[-_][A-Za-z0-9]+)*$`
	mongoRequiredIDPattern   = `^(?!\.{1,2}$)[^\x00]+$`
	mongoOptionalIDPattern   = `^(?:$|(?!\.{1,2}$)[^\x00]+)$`
)

type mongoScalarKind uint8

const (
	mongoStringScalar mongoScalarKind = iota + 1
	mongoNumberScalar
	mongoIntegerScalar
	mongoBooleanScalar
	mongoTimestampScalar
)

type mongoPredicatePath struct {
	storagePath     string
	primitiveList   *schema.Field
	objectAncestors []string
	localePaths     []string
	kind            mongoScalarKind
	alwaysPresent   bool
}

type mongoSortPlan struct {
	order     bson.D
	computed  bson.D
	temporary bson.A
}

// requestPredicate compiles every request restriction into one native MongoDB
// filter. Filter and Access are deliberately siblings in the same $and so an
// adapter cannot authorize a broader read and post-filter it afterward.
func requestPredicate(request store.Request, requireID bool) (bson.D, error) {
	if err := membership.ValidateRequest(request); err != nil {
		return nil, err
	}
	predicates := make([]bson.D, 0, 7)
	if requireID {
		if request.ID == "" {
			return nil, fmt.Errorf("document ID is required")
		}
		predicates = append(predicates, bson.D{{Key: mongoIDPath, Value: request.ID}})
	}

	switch request.Deletion {
	case store.DeletionAll:
		predicates = append(predicates, mongoDeletedAtShapePredicate())
	case store.DeletionTrash:
		predicates = append(predicates, mongoTypeGuard(mongoDeletedAtPath, "long"))
	default:
		predicates = append(predicates, mongoExactNullPredicate(mongoDeletedAtPath))
	}
	if request.PublishedOnly && request.Collection.Versions != nil {
		predicates = append(predicates, bson.D{{Key: mongoStatusPath, Value: string(store.StatusPublished)}})
	}
	if request.ExpectedRevision > 0 {
		predicates = append(predicates, mongoAnd([]bson.D{
			mongoTypeGuard(mongoRevisionPath, "long"),
			{{Key: mongoRevisionPath, Value: bson.D{{Key: "$eq", Value: request.ExpectedRevision}}}},
		}))
	}
	if request.Filter != nil {
		compiled, err := compileMongoNode(request.Collection, *request.Filter, "filter", mongoPredicateScope{localeChain: request.LocaleChain})
		if err != nil {
			return nil, err
		}
		predicates = append(predicates, compiled)
	}
	if request.Access != nil {
		compiled, err := compileMongoAccessNode(
			request.Collection,
			*request.Access,
			"access",
			mongoPredicateScope{localeChain: request.LocaleChain},
			request.AllLocales,
			request.Locales,
		)
		if err != nil {
			return nil, err
		}
		predicates = append(predicates, compiled)
	}
	return mongoAnd(predicates), nil
}

// decoderFreeRequestPredicate adds the declared persisted authored-value
// envelope to requestPredicate. Count, ID-only, and aggregate reads cannot run
// the strict Go decoder over every qualifying row, so this database-side guard
// prevents structurally declared corrupt fields from contributing to their
// totals or selections. Arbitrary JSON/plugin roots receive an exact root-type
// guard; only materialized reads can reject out-of-band corruption deeper inside
// those opaque values. Caller filter and access restrictions remain siblings in
// the same atomic $and.
func decoderFreeRequestPredicate(request store.Request, requireID bool) (bson.D, error) {
	predicate, err := requestPredicate(request, requireID)
	if err != nil {
		return nil, err
	}
	envelope, err := mongoCollectionEnvelopePredicate(request.Collection, request.Locales)
	if err != nil {
		return nil, err
	}
	return mongoAnd([]bson.D{predicate, envelope}), nil
}

// requestSort compiles a stable native MongoDB sort and the narrowly scoped
// computed fields needed by localized fallback terms. Ridu IDs are canonical
// strings, so _id is an ascending deterministic tiebreaker unless the caller
// already supplied an explicit ID term.
func requestSort(request store.Request) (mongoSortPlan, error) {
	result := mongoSortPlan{
		order:     make(bson.D, 0, len(request.Sort)+1),
		computed:  make(bson.D, 0, len(request.Sort)),
		temporary: make(bson.A, 0, len(request.Sort)),
	}
	seen := make(map[string]bool, len(request.Sort)+1)
	hasID := false
	for index, term := range request.Sort {
		resolved, err := resolveMongoPredicatePath(
			request.Collection,
			term.Path,
			"sort",
			mongoPredicateScope{localeChain: request.LocaleChain},
		)
		if err != nil {
			return mongoSortPlan{}, fmt.Errorf("MongoDB sort term %d: %w", index, err)
		}
		direction := mongoAscendingDirection
		switch term.Direction {
		case query.Ascending:
		case query.Descending:
			direction = mongoDescendingDirection
		default:
			return mongoSortPlan{}, fmt.Errorf("MongoDB sort term %d has unknown direction %q", index, term.Direction)
		}
		logicalPath := term.Path.String()
		if seen[logicalPath] {
			return mongoSortPlan{}, fmt.Errorf("MongoDB sort term %d duplicates path %q", index, logicalPath)
		}
		storagePath := resolved.storagePath
		if len(resolved.localePaths) != 0 {
			storagePath = fmt.Sprintf("__riduLocalizedSort%d", index)
			result.computed = append(result.computed, bson.E{Key: storagePath, Value: mongoLocalizedValueExpression(resolved)})
			result.temporary = append(result.temporary, storagePath)
		}
		result.order = append(result.order, bson.E{Key: storagePath, Value: direction})
		seen[logicalPath] = true
		hasID = hasID || resolved.storagePath == mongoIDPath
	}
	if !hasID {
		result.order = append(result.order, bson.E{Key: mongoIDPath, Value: mongoAscendingDirection})
	}
	if len(result.order) > maxMongoSortTerms {
		return mongoSortPlan{}, fmt.Errorf("MongoDB sort supports at most %d terms including the stable ID tiebreaker", maxMongoSortTerms)
	}
	return result, nil
}

// requestProjection returns no MongoDB projection when Select is nil, which
// preserves all authored values and metadata. For an explicit selection it
// uses an inclusion projection and always retains the complete Ridu metadata
// envelope. Population paths are retained because they are needed after the
// primary read even when they were not selected for output.
func requestProjection(request store.Request) (bson.D, error) {
	if request.Select == nil {
		return bson.D{}, nil
	}

	result := bson.D{
		{Key: mongoIDPath, Value: int32(1)},
		{Key: mongoCodecPath, Value: int32(1)},
		{Key: mongoIncarnationPath, Value: int32(1)},
		{Key: mongoCreatedAtPath, Value: int32(1)},
		{Key: mongoUpdatedAtPath, Value: int32(1)},
		{Key: mongoDeletedAtPath, Value: int32(1)},
		{Key: mongoStatusPath, Value: int32(1)},
		{Key: mongoRevisionPath, Value: int32(1)},
		{Key: mongoFencePath, Value: int32(1)},
	}
	seen := map[string]bool{
		mongoIDPath: true, mongoCodecPath: true, mongoIncarnationPath: true, mongoCreatedAtPath: true, mongoUpdatedAtPath: true,
		mongoDeletedAtPath: true, mongoStatusPath: true, mongoRevisionPath: true, mongoFencePath: true,
	}
	appendPath := func(path query.Path, label string, index int) error {
		storagePath, err := resolveMongoProjectionPath(request.Collection, path)
		if err != nil {
			return fmt.Errorf("MongoDB %s path %d: %w", label, index, err)
		}
		for existing := range seen {
			if existing == storagePath || strings.HasPrefix(storagePath, existing+".") {
				return nil
			}
		}
		for existing := range seen {
			if !strings.HasPrefix(existing, storagePath+".") {
				continue
			}
			delete(seen, existing)
			for resultIndex, element := range result {
				if element.Key == existing {
					result = append(result[:resultIndex], result[resultIndex+1:]...)
					break
				}
			}
		}
		result = append(result, bson.E{Key: storagePath, Value: int32(1)})
		seen[storagePath] = true
		return nil
	}
	for index, path := range request.Select {
		if err := appendPath(path, "select", index); err != nil {
			return nil, err
		}
	}
	for index, population := range request.Populate {
		segments := population.Path.Segments()
		if len(segments) == 0 {
			return nil, fmt.Errorf("MongoDB population path %d is empty", index)
		}
		rootPath, err := query.NewPath(segments[0])
		if err != nil {
			return nil, fmt.Errorf("MongoDB population path %d: %w", index, err)
		}
		if err := appendPath(rootPath, "population", index); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func compileMongoNode(collection schema.Collection, node query.Node, role string, scope mongoPredicateScope) (bson.D, error) {
	switch node.Kind {
	case query.ExpressionComparison:
		if node.Comparison == nil {
			return nil, fmt.Errorf("MongoDB %s comparison node is missing its comparison", role)
		}
		if len(node.Children) != 0 {
			return nil, fmt.Errorf("MongoDB %s comparison node must not have children", role)
		}
		if err := validateMongoComparison(*node.Comparison); err != nil {
			return nil, fmt.Errorf("MongoDB %s comparison: %w", role, err)
		}
		nested, isNested, err := resolveMongoNestedPath(collection, node.Comparison.Path, role, scope)
		if err == nil && isNested {
			return compileMongoNestedComparison(nested, *node.Comparison)
		}
		var resolved mongoPredicatePath
		if err == nil {
			resolved, err = resolveMongoPredicatePath(collection, node.Comparison.Path, role, scope)
		}
		if err != nil {
			if role == "filter" {
				return nil, querypath.Unsupported(node.Comparison.Path, err)
			}
			return nil, err
		}
		if resolved.primitiveList != nil {
			return compileMongoPrimitiveList(resolved, *node.Comparison)
		}
		if len(resolved.localePaths) != 0 {
			return compileMongoLocalizedComparison(resolved, *node.Comparison)
		}
		return compileMongoComparison(resolved, *node.Comparison)
	case query.ExpressionAnd, query.ExpressionOr:
		if node.Comparison != nil {
			return nil, fmt.Errorf("MongoDB %s %s node must not contain a comparison", role, node.Kind)
		}
		if len(node.Children) < 2 {
			return nil, fmt.Errorf("MongoDB %s %s node requires at least two children", role, node.Kind)
		}
		children := make([]bson.D, len(node.Children))
		for index, child := range node.Children {
			compiled, err := compileMongoNode(collection, child, role, scope)
			if err != nil {
				return nil, fmt.Errorf("MongoDB %s %s child %d: %w", role, node.Kind, index, err)
			}
			children[index] = compiled
		}
		operator := "$and"
		if node.Kind == query.ExpressionOr {
			operator = "$or"
		}
		compiled := bson.D{{Key: operator, Value: mongoDocumentArray(children)}}
		if node.Kind == query.ExpressionOr {
			shapeGuards, err := mongoNodeRepeatedShapeGuards(collection, node, role, scope)
			if err != nil {
				return nil, err
			}
			compiled = mongoAnd(append(shapeGuards, compiled))
		}
		return compiled, nil
	case query.ExpressionNot:
		if node.Comparison != nil {
			return nil, fmt.Errorf("MongoDB %s not node must not contain a comparison", role)
		}
		if len(node.Children) != 1 {
			return nil, fmt.Errorf("MongoDB %s not node requires exactly one child", role)
		}
		child, err := compileMongoNode(collection, node.Children[0], role, scope)
		if err != nil {
			return nil, fmt.Errorf("MongoDB %s not child: %w", role, err)
		}
		shapeGuards, err := mongoNodeObjectShapeGuards(collection, node.Children[0], role, scope)
		if err != nil {
			return nil, fmt.Errorf("MongoDB %s not child: %w", role, err)
		}
		repeatedShapeGuards, err := mongoNodeRepeatedShapeGuards(collection, node.Children[0], role, scope)
		if err != nil {
			return nil, fmt.Errorf("MongoDB %s not child: %w", role, err)
		}
		shapeGuards = append(shapeGuards, repeatedShapeGuards...)
		return mongoAnd(append(shapeGuards, bson.D{{Key: "$nor", Value: bson.A{child}}})), nil
	default:
		return nil, fmt.Errorf("MongoDB %s predicate has unsupported expression kind %q", role, node.Kind)
	}
}

func validateMongoComparison(comparison query.Comparison) error {
	switch comparison.Operator {
	case query.OperatorEqual, query.OperatorNotEqual:
		if comparison.Value.Kind() == query.ValueList {
			return fmt.Errorf("operator %q does not accept a list operand", comparison.Operator)
		}
	case query.OperatorIn:
		if comparison.Value.Kind() != query.ValueList {
			return fmt.Errorf("operator %q requires a list operand", comparison.Operator)
		}
	case query.OperatorExists:
		if comparison.Value.Kind() != query.ValueBoolean {
			return fmt.Errorf("operator %q requires a boolean operand", comparison.Operator)
		}
	case query.OperatorGreaterThan, query.OperatorGreaterThanEqual, query.OperatorLessThan, query.OperatorLessThanEqual:
		if comparison.Value.Kind() != query.ValueString && comparison.Value.Kind() != query.ValueNumber {
			return fmt.Errorf("operator %q requires a string or number operand", comparison.Operator)
		}
	case query.OperatorContains, query.OperatorLike:
		if comparison.Value.Kind() != query.ValueString {
			return fmt.Errorf("operator %q requires a string operand", comparison.Operator)
		}
	default:
		return fmt.Errorf("unsupported comparison operator %q", comparison.Operator)
	}
	return nil
}

// mongoRowShape selects how much of each row a stored-shape description of a
// repeated field covers. MongoDB's $jsonSchema has no shared definitions, so
// describing the rows of rows would inline every block placement: a block that
// may contain other blocks expands into each path it can reach, which is
// exponential in nesting depth and reached tens of megabytes for ordinary
// layout builders. Descriptions therefore follow the declared definitions,
// never their placements: a filter on a repeated path guards the rows it
// compares to their own fields, and the decoder-free envelope guards rows by
// identity alone.
type mongoRowShape uint8

const (
	// mongoRowFields describes each row's own declared fields. A repeated
	// field among them is described with mongoRowIdentity.
	mongoRowFields mongoRowShape = iota
	// mongoRowIdentity describes only each row's identity: an object with a
	// non-blank row key and, for blocks, a declared discriminator. Its
	// content is validated by the strict decoder, like an opaque JSON value.
	mongoRowIdentity
)

// mongoCollectionEnvelopePredicate mirrors the root of the authored values
// envelope enforced by decodeCollectionDocumentForLocales: the values object
// and its groups exactly, and each repeated field's array, row bound and row
// identity (see mongoRowIdentity). Row content, like opaque JSON and plugin
// values, is checked completely on adapter writes and strict reads only: the
// server evaluates this guard on every document a count or selection scans,
// and describing block rows made that milliseconds per document. The
// predicate is kept separate from requestPredicate because ordinary
// full-document reads must continue to surface strict decoder failures instead
// of hiding corruption as absence. Decoder-free reads compose this guard into
// their database query.
func mongoCollectionEnvelopePredicate(collection schema.Collection, locales []schema.LocaleCode) (bson.D, error) {
	if len(locales) != 0 {
		if _, err := mongoConfiguredLocales(locales); err != nil {
			return nil, err
		}
	}
	guards := []bson.D{{{Key: "$jsonSchema", Value: mongoJSONSchemaAtPath(
		[]string{"values"},
		mongoCollectionObjectJSONSchema(collection.Fields, false, "", locales, mongoRowIdentity),
		true,
	)}}}
	guards = append(guards, bson.D{{Key: "$expr", Value: mongoObjectKeyUniquenessExpression("$values", collection.Fields, mongoRowIdentity)}})
	guards = append(guards, mongoRelationshipIDLengthPredicates(collection.Fields, mongoAuthoredValuesPath)...)
	for _, field := range collection.Fields {
		if field.Category == schema.FieldCategoryPresentation || field.Type != schema.FieldTypeArray && field.Type != schema.FieldTypeBlocks {
			continue
		}
		guards = append(guards, mongoRepeatedRowKeyUniquenessPredicate(mongoAuthoredValuesPath+field.Name))
	}
	return mongoAnd(guards), nil
}

// mongoObjectKeyUniquenessExpression rejects duplicate keys in the object at
// value and in the declared objects below it, to the depth rows selects.
func mongoObjectKeyUniquenessExpression(value string, fields []schema.Field, rows mongoRowShape) bson.D {
	conditions := bson.A{mongoObjectOwnKeyUniquenessExpression(value)}
	for _, field := range fields {
		if field.Category == schema.FieldCategoryPresentation {
			continue
		}
		fieldValue := value + "." + field.Name
		switch {
		case field.Localized:
			conditions = append(conditions, mongoObjectKeyUniquenessExpression(fieldValue, nil, rows))
		case field.Type == schema.FieldTypeGroup:
			conditions = append(conditions, mongoObjectKeyUniquenessExpression(fieldValue, field.Nested.ResolvedFields(), rows))
		case field.Type == schema.FieldTypeArray, field.Type == schema.FieldTypeBlocks:
			var rowFields []schema.Field
			if rows == mongoRowFields {
				rowFields = mongoRepeatedRowFields(field)
			}
			conditions = append(conditions, mongoRepeatedObjectKeyUniquenessExpression(fieldValue, rowFields))
		case field.Type == schema.FieldTypeRelationship && field.Relationship != nil && field.Relationship.Polymorphic:
			if field.Relationship.HasMany {
				conditions = append(conditions, mongoRepeatedObjectKeyUniquenessExpression(fieldValue, nil))
			} else {
				conditions = append(conditions, mongoObjectKeyUniquenessExpression(fieldValue, nil, rows))
			}
		}
	}
	return bson.D{{Key: "$cond", Value: bson.A{
		bson.D{{Key: "$eq", Value: bson.A{bson.D{{Key: "$type", Value: value}}, "object"}}},
		bson.D{{Key: "$and", Value: conditions}},
		true,
	}}}
}

func mongoObjectOwnKeyUniquenessExpression(value string) bson.D {
	keys := bson.D{{Key: "$map", Value: bson.D{
		{Key: "input", Value: bson.D{{Key: "$objectToArray", Value: value}}},
		{Key: "as", Value: "riduObjectField"},
		{Key: "in", Value: "$$riduObjectField.k"},
	}}}
	return bson.D{{Key: "$let", Value: bson.D{
		{Key: "vars", Value: bson.D{{Key: "riduObjectKeys", Value: keys}}},
		{Key: "in", Value: bson.D{{Key: "$eq", Value: bson.A{
			bson.D{{Key: "$size", Value: "$$riduObjectKeys"}},
			bson.D{{Key: "$size", Value: bson.D{{Key: "$setUnion", Value: bson.A{"$$riduObjectKeys", bson.A{}}}}}},
		}}}},
	}}}
}

// mongoRepeatedObjectKeyUniquenessExpression checks each row's own keys and
// the declared row fields; repeated fields among those are checked only to
// their rows' own keys.
func mongoRepeatedObjectKeyUniquenessExpression(value string, rowFields []schema.Field) bson.D {
	return bson.D{{Key: "$cond", Value: bson.A{
		bson.D{{Key: "$eq", Value: bson.A{bson.D{{Key: "$type", Value: value}}, "array"}}},
		bson.D{{Key: "$allElementsTrue", Value: bson.A{bson.D{{Key: "$map", Value: bson.D{
			{Key: "input", Value: value},
			{Key: "as", Value: "riduObject"},
			{Key: "in", Value: mongoObjectKeyUniquenessExpression("$$riduObject", rowFields, mongoRowIdentity)},
		}}}}}},
		true,
	}}}
}

// mongoRepeatedRowFields returns the fields a row of an array or blocks field
// may declare: the array's fields, or every allowed block type's fields.
func mongoRepeatedRowFields(field schema.Field) []schema.Field {
	switch {
	case field.Type == schema.FieldTypeArray && field.Nested != nil:
		return field.Nested.ResolvedFields()
	case field.Type == schema.FieldTypeBlocks && field.Blocks != nil:
		var fields []schema.Field
		for _, block := range field.Blocks.ResolvedTypes() {
			fields = append(fields, block.ResolvedFields()...)
		}
		return fields
	default:
		return nil
	}
}

func mongoRelationshipIDLengthPredicates(fields []schema.Field, prefix string) []bson.D {
	var predicates []bson.D
	for _, field := range fields {
		if field.Category == schema.FieldCategoryPresentation {
			continue
		}
		path := prefix + field.Name
		if field.Type == schema.FieldTypeGroup {
			predicates = append(predicates, mongoRelationshipIDLengthPredicates(field.Nested.ResolvedFields(), path+".")...)
			continue
		}
		reference := mongoReferenceDetails(field)
		if reference == nil {
			continue
		}
		value := any("$" + path)
		if reference.Polymorphic && !reference.HasMany {
			value = "$" + path + ".id"
		}
		var expression any = mongoRelationshipIDLengthExpression(value)
		if reference.HasMany {
			itemValue := any("$$riduRelationship")
			if reference.Polymorphic {
				itemValue = "$$riduRelationship.id"
			}
			expression = bson.D{{Key: "$cond", Value: bson.A{
				bson.D{{Key: "$eq", Value: bson.A{bson.D{{Key: "$type", Value: "$" + path}}, "array"}}},
				bson.D{{Key: "$allElementsTrue", Value: bson.A{bson.D{{Key: "$map", Value: bson.D{
					{Key: "input", Value: "$" + path},
					{Key: "as", Value: "riduRelationship"},
					{Key: "in", Value: mongoRelationshipIDLengthExpression(itemValue)},
				}}}}}},
				true,
			}}}
		}
		predicates = append(predicates, bson.D{{Key: "$expr", Value: expression}})
	}
	return predicates
}

func mongoRelationshipIDLengthExpression(value any) bson.D {
	return bson.D{{Key: "$cond", Value: bson.A{
		bson.D{{Key: "$eq", Value: bson.A{bson.D{{Key: "$type", Value: value}}, "string"}}},
		bson.D{{Key: "$lte", Value: bson.A{bson.D{{Key: "$strLenBytes", Value: value}}, store.MaxDocumentIDBytes}}},
		true,
	}}}
}

func mongoJSONSchemaAtPath(segments []string, leaf bson.D, leafRequired bool) bson.D {
	result := bson.D{
		{Key: "bsonType", Value: "object"},
		{Key: "properties", Value: bson.D{{Key: segments[len(segments)-1], Value: leaf}}},
	}
	if leafRequired {
		result = append(result, bson.E{Key: "required", Value: bson.A{segments[len(segments)-1]}})
	}
	for index := len(segments) - 2; index >= 0; index-- {
		result = bson.D{
			{Key: "bsonType", Value: "object"},
			{Key: "properties", Value: bson.D{{Key: segments[index], Value: result}}},
			{Key: "required", Value: bson.A{segments[index]}},
		}
	}
	return result
}

// mongoRepeatedRootJSONSchema describes a repeated field's stored array and,
// as rows selects, either each row's own declared fields or only its identity.
func mongoRepeatedRootJSONSchema(root schema.Field, locales []schema.LocaleCode, rows mongoRowShape) bson.D {
	var item bson.D
	switch {
	case root.Type == schema.FieldTypeSelect:
		options := make(bson.A, len(root.Select.Options))
		for index, option := range root.Select.Options {
			options[index] = option.Value
		}
		item = bson.D{
			{Key: "bsonType", Value: "string"},
			{Key: "enum", Value: options},
		}
	case root.Type == schema.FieldTypeArray && rows == mongoRowFields:
		item = mongoCollectionObjectJSONSchema(root.Nested.ResolvedFields(), true, "", locales, mongoRowIdentity)
	case root.Type == schema.FieldTypeArray:
		item = mongoRowIdentityJSONSchema(nil)
	case root.Type == schema.FieldTypeBlocks && rows == mongoRowFields:
		blockSchemas := make(bson.A, len(root.Blocks.ResolvedTypes()))
		for index, block := range root.Blocks.ResolvedTypes() {
			blockSchemas[index] = mongoCollectionObjectJSONSchema(block.ResolvedFields(), true, block.Slug, locales, mongoRowIdentity)
		}
		// Each variant requires its own blockType, so a row matches at most
		// one: anyOf states the same shape as oneOf, and MongoDB stops at the
		// matching variant instead of validating every one.
		item = bson.D{{Key: "anyOf", Value: blockSchemas}}
	case root.Type == schema.FieldTypeBlocks:
		slugs := make(bson.A, len(root.Blocks.ResolvedTypes()))
		for index, block := range root.Blocks.ResolvedTypes() {
			slugs[index] = block.Slug
		}
		item = mongoRowIdentityJSONSchema(slugs)
	}

	return mongoRepeatedArrayJSONSchema(root, item)
}

// mongoRepeatedArrayJSONSchema describes a repeated field's stored array whose
// items match item.
func mongoRepeatedArrayJSONSchema(root schema.Field, item bson.D) bson.D {
	// Requiredness and minimum row counts are completeness rules: drafts and
	// documents saved before a field became required may lack them.
	result := bson.D{
		{Key: "bsonType", Value: bson.A{"array", "null"}},
		{Key: "items", Value: item},
	}
	if root.Type == schema.FieldTypeArray && root.Nested.MaxRows > 0 {
		result = append(result, bson.E{Key: "maxItems", Value: root.Nested.MaxRows})
	}
	if root.Type == schema.FieldTypeBlocks && root.Blocks.MaxRows > 0 {
		result = append(result, bson.E{Key: "maxItems", Value: root.Blocks.MaxRows})
	}
	if root.Type == schema.FieldTypeSelect {
		result = append(result, bson.E{Key: "uniqueItems", Value: true})
	}
	return result
}

// mongoRowIdentityJSONSchema describes a row by its identity alone. Block rows
// must name one of blockTypes; a nil blockTypes describes array rows.
func mongoRowIdentityJSONSchema(blockTypes bson.A) bson.D {
	properties := bson.D{{Key: "_key", Value: mongoRowKeyJSONSchema()}}
	result := bson.D{{Key: "bsonType", Value: "object"}}
	if blockTypes == nil {
		return append(result, bson.E{Key: "properties", Value: properties})
	}
	properties = append(properties, bson.E{Key: "blockType", Value: bson.D{
		{Key: "bsonType", Value: "string"},
		{Key: "enum", Value: blockTypes},
	}})
	return append(result,
		bson.E{Key: "properties", Value: properties},
		bson.E{Key: "required", Value: bson.A{"blockType"}},
	)
}

func mongoRowKeyJSONSchema() bson.D {
	return bson.D{
		{Key: "bsonType", Value: "string"},
		{Key: "pattern", Value: mongoNonBlankKeyPattern},
	}
}

// mongoCollectionObjectJSONSchema describes an object's declared fields; rows
// selects how repeated fields among them describe their rows.
func mongoCollectionObjectJSONSchema(fields []schema.Field, row bool, blockType string, locales []schema.LocaleCode, rows mongoRowShape) bson.D {
	properties := make(bson.D, 0, len(fields)+2)
	required := make(bson.A, 0, len(fields)+1)
	if row {
		properties = append(properties, bson.E{Key: "_key", Value: mongoRowKeyJSONSchema()})
	}
	if blockType != "" {
		properties = append(properties, bson.E{Key: "blockType", Value: bson.D{
			{Key: "bsonType", Value: "string"},
			{Key: "enum", Value: bson.A{blockType}},
		}})
		required = append(required, "blockType")
	}
	// A stored object may omit any authored field; only the block
	// discriminator identifies its shape.
	for _, field := range fields {
		if field.Category == schema.FieldCategoryPresentation {
			continue
		}
		properties = append(properties, bson.E{Key: field.Name, Value: mongoCollectionFieldJSONSchema(field, locales, rows)})
	}
	result := bson.D{
		{Key: "bsonType", Value: "object"},
		{Key: "additionalProperties", Value: false},
		{Key: "properties", Value: properties},
	}
	if len(required) != 0 {
		result = append(result, bson.E{Key: "required", Value: required})
	}
	return result
}

// mongoCollectionFieldJSONSchema describes one stored field; rows selects how
// a repeated field, or one nested in a group, describes its rows.
func mongoCollectionFieldJSONSchema(field schema.Field, locales []schema.LocaleCode, rows mongoRowShape) bson.D {
	if field.Localized {
		return mongoLocalizedFieldJSONSchema(field, locales, rows)
	}
	if primitivefield.IsList(field) {
		return mongoPrimitiveListJSONSchema(field)
	}
	if field.Type == schema.FieldTypeGroup {
		result := mongoCollectionObjectJSONSchema(field.Nested.ResolvedFields(), false, "", locales, rows)
		result[0].Value = bson.A{"object", "null"}
		return result
	}
	if field.Type == schema.FieldTypeSelect && field.Select != nil && field.Select.HasMany ||
		field.Type == schema.FieldTypeArray || field.Type == schema.FieldTypeBlocks {
		return mongoRepeatedRootJSONSchema(field, locales, rows)
	}
	if field.Type == schema.FieldTypeRelationship || field.Type == schema.FieldTypeUpload {
		return mongoRelationshipFieldJSONSchema(field)
	}
	if mongoUploadSizesField(field) {
		return bson.D{
			{Key: "bsonType", Value: bson.A{"object", "null"}},
			{Key: "maxProperties", Value: maxMongoUploadImageSizes},
			{Key: "additionalProperties", Value: bson.D{{Key: "bsonType", Value: "object"}}},
		}
	}
	if field.Type == schema.FieldTypeJSON || field.Type == schema.FieldTypePlugin {
		return mongoJSONFieldJSONSchema()
	}
	if field.Type == schema.FieldTypePoint {
		return mongoPointFieldJSONSchema()
	}
	return mongoScalarFieldJSONSchema(field, true)
}

func mongoLocalizedFieldJSONSchema(field schema.Field, locales []schema.LocaleCode, rows mongoRowShape) bson.D {
	unlocalized := field
	unlocalized.Localized = false
	valueSchema := mongoNullableJSONSchema(mongoCollectionFieldJSONSchema(unlocalized, locales, rows))
	result := bson.D{{Key: "bsonType", Value: "object"}, {Key: "additionalProperties", Value: false}}
	if len(locales) == 0 {
		return append(result, bson.E{Key: "patternProperties", Value: bson.D{{Key: mongoLocaleCodePattern, Value: valueSchema}}})
	}
	properties := make(bson.D, 0, len(locales))
	for _, locale := range locales {
		properties = append(properties, bson.E{Key: string(locale), Value: valueSchema})
	}
	return append(result, bson.E{Key: "properties", Value: properties})
}

func mongoNullableJSONSchema(value bson.D) bson.D {
	result := append(bson.D(nil), value...)
	for index := range result {
		if result[index].Key != "bsonType" {
			continue
		}
		switch current := result[index].Value.(type) {
		case string:
			if current != "null" {
				result[index].Value = bson.A{current, "null"}
			}
		case bson.A:
			for _, candidate := range current {
				if candidate == "null" {
					return result
				}
			}
			result[index].Value = append(append(bson.A(nil), current...), "null")
		}
		return result
	}
	return result
}

func mongoRelationshipFieldJSONSchema(field schema.Field) bson.D {
	relationship := mongoReferenceDetails(field)
	if relationship == nil {
		return bson.D{{Key: "bsonType", Value: "object"}, {Key: "additionalProperties", Value: false}}
	}
	var item bson.D
	if relationship.Polymorphic {
		targets := make(bson.A, len(relationship.Targets))
		for index, target := range relationship.Targets {
			targets[index] = string(target.CollectionSlug)
		}
		item = bson.D{
			{Key: "bsonType", Value: "object"},
			{Key: "additionalProperties", Value: false},
			{Key: "properties", Value: bson.D{
				{Key: "relationTo", Value: bson.D{{Key: "bsonType", Value: "string"}, {Key: "enum", Value: targets}}},
				{Key: "id", Value: mongoDocumentIDJSONSchema(false)},
			}},
			{Key: "required", Value: bson.A{"relationTo", "id"}},
		}
	} else {
		item = mongoDocumentIDJSONSchema(!relationship.HasMany)
	}
	if !relationship.HasMany {
		item[0].Value = bson.A{item[0].Value, "null"}
		return item
	}
	return bson.D{
		{Key: "bsonType", Value: bson.A{"array", "null"}},
		{Key: "items", Value: item},
		{Key: "maxItems", Value: maxMongoDocumentReferences},
	}
}

func mongoDocumentIDJSONSchema(allowEmpty bool) bson.D {
	pattern := mongoRequiredIDPattern
	if allowEmpty {
		pattern = mongoOptionalIDPattern
	}
	return bson.D{
		{Key: "bsonType", Value: "string"},
		{Key: "pattern", Value: pattern},
		{Key: "maxLength", Value: 512},
	}
}

func mongoPointFieldJSONSchema() bson.D {
	return bson.D{
		{Key: "bsonType", Value: bson.A{"array", "null"}},
		{Key: "items", Value: bson.A{
			bson.D{
				{Key: "bsonType", Value: "double"},
				{Key: "minimum", Value: -180.0},
				{Key: "maximum", Value: 180.0},
			},
			bson.D{
				{Key: "bsonType", Value: "double"},
				{Key: "minimum", Value: -90.0},
				{Key: "maximum", Value: 90.0},
			},
		}},
		{Key: "minItems", Value: 2},
		{Key: "maxItems", Value: 2},
	}
}

func mongoScalarFieldJSONSchema(field schema.Field, nullable bool) bson.D {

	typeName := "string"
	switch field.Type {
	case schema.FieldTypeNumber:
		typeName = "double"
	case schema.FieldTypeCheckbox:
		typeName = "bool"
	}
	typeValue := any(typeName)
	if nullable {
		typeValue = bson.A{typeName, "null"}
	}
	result := bson.D{{Key: "bsonType", Value: typeValue}}
	if typeName == "string" {
		// Authoring-only length, email, and date-format constraints remain in the
		// operation engine. The persisted string envelope additionally rejects NUL,
		// matching encodeValue/decodeValue without reproducing those authoring rules.
		result = append(result, bson.E{Key: "pattern", Value: mongoNoNULStringPattern})
	} else if typeName == "double" {
		// The codec admits only finite float64 values. Exact bounds also reject
		// out-of-band infinities and NaN from decoder-free negative predicates.
		result = append(result,
			bson.E{Key: "minimum", Value: -math.MaxFloat64},
			bson.E{Key: "maximum", Value: math.MaxFloat64},
		)
	}
	if (field.Type == schema.FieldTypeSelect || field.Type == schema.FieldTypeRadio) && field.Select != nil {
		options := make(bson.A, 0, len(field.Select.Options)+2)
		for _, option := range field.Select.Options {
			options = append(options, option.Value)
		}
		options = append(options, "")
		if nullable {
			options = append(options, nil)
		}
		result = append(result, bson.E{Key: "enum", Value: options})
	}
	return result
}

func mongoJSONFieldJSONSchema() bson.D {
	// Arbitrary JSON cannot be described recursively without duplicating the
	// store.Value codec. Guard the root vocabulary here; adapter writes and reads
	// still validate every nested value through encodeValue and decodeValue.
	return bson.D{{Key: "bsonType", Value: bson.A{"object", "array", "string", "double", "bool", "null"}}}
}

// mongoRepeatedRowKeyUniquenessPredicate requires unique row keys in the
// array or blocks list stored at storagePath.
func mongoRepeatedRowKeyUniquenessPredicate(storagePath string) bson.D {
	return bson.D{{Key: "$expr", Value: mongoUniqueRowKeysExpression("$" + storagePath)}}
}

// mongoUniqueRowKeysExpression is true unless list, an aggregation expression,
// is an array whose rows repeat a string _key.
func mongoUniqueRowKeysExpression(list string) bson.D {
	typeExpression := bson.D{{Key: "$type", Value: "$$riduRepeated"}}
	keys := bson.D{{Key: "$map", Value: bson.D{
		{Key: "input", Value: bson.D{{Key: "$filter", Value: bson.D{
			{Key: "input", Value: "$$riduRepeated"},
			{Key: "as", Value: "riduRow"},
			{Key: "cond", Value: bson.D{{Key: "$eq", Value: bson.A{
				bson.D{{Key: "$type", Value: "$$riduRow._key"}}, "string",
			}}}},
		}}}},
		{Key: "as", Value: "riduRow"},
		{Key: "in", Value: "$$riduRow._key"},
	}}}
	return bson.D{{Key: "$let", Value: bson.D{
		{Key: "vars", Value: bson.D{{Key: "riduRepeated", Value: list}}},
		{Key: "in", Value: bson.D{{Key: "$cond", Value: bson.A{
			bson.D{{Key: "$eq", Value: bson.A{typeExpression, "array"}}},
			bson.D{{Key: "$let", Value: bson.D{
				{Key: "vars", Value: bson.D{{Key: "riduKeys", Value: keys}}},
				{Key: "in", Value: bson.D{{Key: "$eq", Value: bson.A{
					bson.D{{Key: "$size", Value: "$$riduKeys"}},
					bson.D{{Key: "$size", Value: bson.D{{Key: "$setUnion", Value: bson.A{"$$riduKeys", bson.A{}}}}}},
				}}}},
			}}},
			true,
		}}}},
	}}}
}

func mongoOr(predicates []bson.D) bson.D {
	if len(predicates) == 0 {
		return mongoConstant(false)
	}
	if len(predicates) == 1 {
		return predicates[0]
	}
	return bson.D{{Key: "$or", Value: mongoDocumentArray(predicates)}}
}

func compileMongoComparison(path mongoPredicatePath, comparison query.Comparison) (bson.D, error) {
	if comparison.Operator == query.OperatorExists {
		want, _ := comparison.Value.BooleanValue()
		if path.alwaysPresent {
			return mongoConstant(want), nil
		}
		if want {
			return mongoWithObjectAncestors(path, mongoNonNullPredicate(path.storagePath)), nil
		}
		return mongoWithNullableObjectAncestors(path, mongoNullPredicate(path.storagePath)), nil
	}
	if comparison.Operator == query.OperatorIn {
		predicates := make([]bson.D, 0, len(comparison.Value.Values()))
		for _, value := range comparison.Value.Values() {
			compiled, compatible, err := compileMongoEquality(path, value)
			if err != nil {
				return nil, err
			}
			if compatible {
				predicates = append(predicates, compiled)
			}
		}
		if len(predicates) == 0 {
			return mongoConstant(false), nil
		}
		if len(predicates) == 1 {
			return predicates[0], nil
		}
		return bson.D{{Key: "$or", Value: mongoDocumentArray(predicates)}}, nil
	}
	if comparison.Operator == query.OperatorEqual || comparison.Operator == query.OperatorNotEqual {
		equal, compatible, err := compileMongoEquality(path, comparison.Value)
		if err != nil {
			return nil, err
		}
		if !compatible {
			if path.kind == mongoTimestampScalar {
				return mongoConstant(false), nil
			}
			constant := mongoConstant(comparison.Operator == query.OperatorNotEqual)
			if comparison.Operator == query.OperatorNotEqual {
				return mongoWithNullableObjectAncestors(path, constant), nil
			}
			return constant, nil
		}
		if comparison.Operator == query.OperatorNotEqual {
			return mongoWithNullableObjectAncestors(path, bson.D{{Key: "$nor", Value: bson.A{equal}}}), nil
		}
		return equal, nil
	}
	if comparison.Operator == query.OperatorContains || comparison.Operator == query.OperatorLike {
		if path.kind != mongoStringScalar {
			return mongoConstant(false), nil
		}
		text, _ := comparison.Value.StringValue()
		words := []string{text}
		if comparison.Operator == query.OperatorLike {
			words = strings.Fields(text)
		}
		guard := mongoTypeGuard(path.storagePath, "string")
		if len(words) == 0 {
			return mongoWithObjectAncestors(path, guard), nil
		}
		predicates := mongoObjectAncestorGuards(path)
		predicates = append(predicates, guard)
		for _, word := range words {
			predicates = append(predicates, bson.D{{
				Key: path.storagePath,
				Value: bson.D{{
					Key:   "$regex",
					Value: bson.Regex{Pattern: regexp.QuoteMeta(word), Options: "i"},
				}},
			}})
		}
		return mongoAnd(predicates), nil
	}

	expected, compatible, err := mongoOperand(path, comparison.Value)
	if err != nil {
		return nil, err
	}
	if !compatible {
		return mongoConstant(false), nil
	}
	operator := map[query.Operator]string{
		query.OperatorGreaterThan:      "$gt",
		query.OperatorGreaterThanEqual: "$gte",
		query.OperatorLessThan:         "$lt",
		query.OperatorLessThanEqual:    "$lte",
	}[comparison.Operator]
	if operator == "" {
		return nil, fmt.Errorf("MongoDB predicate has unsupported comparison operator %q", comparison.Operator)
	}
	return mongoAnd(append(mongoObjectAncestorGuards(path),
		mongoTypeGuard(path.storagePath, mongoBSONType(path.kind)),
		bson.D{{Key: path.storagePath, Value: bson.D{{Key: operator, Value: expected}}}},
	)), nil
}

func compileMongoEquality(path mongoPredicatePath, value query.Value) (bson.D, bool, error) {
	if value.Kind() == query.ValueNull {
		if path.alwaysPresent {
			return mongoConstant(false), true, nil
		}
		return mongoWithNullableObjectAncestors(path, mongoNullPredicate(path.storagePath)), true, nil
	}
	expected, compatible, err := mongoOperand(path, value)
	if err != nil || !compatible {
		return nil, compatible, err
	}
	return mongoAnd(append(mongoObjectAncestorGuards(path),
		mongoTypeGuard(path.storagePath, mongoBSONType(path.kind)),
		bson.D{{Key: path.storagePath, Value: bson.D{{Key: "$eq", Value: expected}}}},
	)), true, nil
}

func mongoWithObjectAncestors(path mongoPredicatePath, predicate bson.D) bson.D {
	return mongoAnd(append(mongoObjectAncestorGuards(path), predicate))
}

func mongoWithNullableObjectAncestors(path mongoPredicatePath, predicate bson.D) bson.D {
	return mongoAnd(append(mongoNullableObjectAncestorGuards(path), predicate))
}

func mongoObjectAncestorGuards(path mongoPredicatePath) []bson.D {
	guards := make([]bson.D, 0, len(path.objectAncestors))
	for _, ancestor := range path.objectAncestors {
		guards = append(guards, mongoTypeGuard(ancestor, "object"))
	}
	return guards
}

func mongoNullableObjectAncestorGuards(path mongoPredicatePath) []bson.D {
	guards := make([]bson.D, 0, len(path.objectAncestors))
	for _, ancestor := range path.objectAncestors {
		guards = append(guards, mongoNullableObjectPredicate(ancestor))
	}
	return guards
}

func mongoNodeObjectShapeGuards(collection schema.Collection, node query.Node, role string, scope mongoPredicateScope) ([]bson.D, error) {
	ancestors := make([]string, 0)
	seen := make(map[string]bool)
	var collect func(query.Node) error
	collect = func(candidate query.Node) error {
		if candidate.Comparison != nil {
			// A nested path's shape guard already describes its ancestors.
			if _, nested, err := resolveMongoNestedPath(collection, candidate.Comparison.Path, role, scope); err != nil || nested {
				return err
			}
			resolved, err := resolveMongoPredicatePath(collection, candidate.Comparison.Path, role, scope)
			if err != nil {
				return err
			}
			for _, ancestor := range resolved.objectAncestors {
				if !seen[ancestor] {
					seen[ancestor] = true
					ancestors = append(ancestors, ancestor)
				}
			}
		}
		for _, child := range candidate.Children {
			if err := collect(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := collect(node); err != nil {
		return nil, err
	}
	guards := make([]bson.D, 0, len(ancestors))
	for _, ancestor := range ancestors {
		guards = append(guards, mongoNullableObjectPredicate(ancestor))
	}
	return guards, nil
}

func mongoNodeRepeatedShapeGuards(collection schema.Collection, node query.Node, role string, scope mongoPredicateScope) ([]bson.D, error) {
	guards := make([]bson.D, 0)
	seen := make(map[string]struct{})
	var collect func(query.Node) error
	collect = func(candidate query.Node) error {
		if candidate.Comparison != nil {
			nested, isNested, err := resolveMongoNestedPath(collection, candidate.Comparison.Path, role, scope)
			if err != nil {
				return err
			}
			if isNested {
				key := "nested:" + nested.base + nested.path.String()
				if _, duplicate := seen[key]; !duplicate {
					seen[key] = struct{}{}
					guards = append(guards, mongoNestedShapePredicate(nested))
				}
				return nil
			}
			resolved, err := resolveMongoPredicatePath(collection, candidate.Comparison.Path, role, scope)
			if err != nil {
				return err
			}
			if resolved.primitiveList != nil {
				key := "primitive-list:" + resolved.storagePath
				if _, duplicate := seen[key]; !duplicate {
					seen[key] = struct{}{}
					guards = append(guards, mongoPrimitiveListShape(resolved))
				}
			}
		}
		for _, child := range candidate.Children {
			if err := collect(child); err != nil {
				return err
			}
		}
		return nil
	}
	if err := collect(node); err != nil {
		return nil, err
	}
	return guards, nil
}

func mongoOperand(path mongoPredicatePath, value query.Value) (any, bool, error) {
	switch path.kind {
	case mongoStringScalar:
		value, valid := value.StringValue()
		return value, valid, nil
	case mongoNumberScalar:
		value, valid := value.NumberValue()
		return value, valid, nil
	case mongoIntegerScalar:
		value, valid := value.NumberValue()
		return value, valid, nil
	case mongoBooleanScalar:
		value, valid := value.BooleanValue()
		return value, valid, nil
	case mongoTimestampScalar:
		value, valid := value.StringValue()
		if !valid {
			return nil, false, nil
		}
		timestamp, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return nil, false, nil
		}
		nanoseconds := timestamp.UnixNano()
		if !time.Unix(0, nanoseconds).Equal(timestamp) {
			return nil, false, nil
		}
		return nanoseconds, true, nil
	default:
		return nil, false, fmt.Errorf("MongoDB predicate path %q is not a scalar value", path.storagePath)
	}
}

func resolveMongoPredicatePath(collection schema.Collection, path query.Path, role string, scope mongoPredicateScope) (mongoPredicatePath, error) {
	segments := path.Segments()
	if len(segments) == 0 {
		return mongoPredicatePath{}, fmt.Errorf("MongoDB %s path is empty", role)
	}
	if collection.Upload != nil && segments[0] == "sizes" {
		return resolveMongoUploadSizePredicatePath(collection, segments, path, role, scope)
	}
	if len(segments) == 1 {
		switch segments[0] {
		case "id":
			return mongoPredicatePath{storagePath: scope.path(mongoIDPath), kind: mongoStringScalar, alwaysPresent: true}, nil
		case "createdAt":
			return mongoPredicatePath{storagePath: scope.path(mongoCreatedAtPath), kind: mongoTimestampScalar, alwaysPresent: true}, nil
		case "updatedAt":
			return mongoPredicatePath{storagePath: scope.path(mongoUpdatedAtPath), kind: mongoTimestampScalar, alwaysPresent: true}, nil
		case "_status":
			if collection.Versions != nil {
				return mongoPredicatePath{storagePath: scope.path(mongoStatusPath), kind: mongoStringScalar, alwaysPresent: true}, nil
			}
		case "_revision":
			if collection.Versions != nil || collection.Upload != nil {
				return mongoPredicatePath{storagePath: scope.path(mongoRevisionPath), kind: mongoIntegerScalar, alwaysPresent: true}, nil
			}
		}
	}

	field, found := mongoFieldNamed(collection.Fields, segments[0])
	if !found {
		return mongoPredicatePath{}, fmt.Errorf("MongoDB %s field %q is not in collection %q", role, path.String(), collection.Slug)
	}
	if field.Type == schema.FieldTypeArray || field.Type == schema.FieldTypeBlocks || field.Type == schema.FieldTypeSelect && field.Select != nil && field.Select.HasMany {
		return resolveMongoRepeatedPredicatePath(collection, field, path, role)
	}
	storageSegments := []string{segments[0]}
	objectAncestors := make([]string, 0, len(segments)-1)
	localized := false
	for index := range segments {
		if field.Localized {
			if index != len(segments)-1 {
				return mongoPredicatePath{}, fmt.Errorf("MongoDB %s path %q traverses localized field %q; localized containers are not supported", role, path.String(), field.Name)
			}
			if len(scope.localeChain) == 0 {
				if role == "index" || role == "compound index" || role == "list window" {
					return mongoPredicatePath{}, fmt.Errorf("MongoDB %s path %q is localized; localized indexes are not supported", role, path.String())
				}
				return mongoPredicatePath{}, fmt.Errorf("MongoDB %s path %q requires a locale chain", role, path.String())
			}
			if _, err := mongoConfiguredLocales(scope.localeChain); err != nil {
				return mongoPredicatePath{}, fmt.Errorf("MongoDB %s path %q locale chain: %w", role, path.String(), err)
			}
			localized = true
		}
		if field.Type == schema.FieldTypeArray || mongoFieldHasMany(field) {
			return mongoPredicatePath{}, fmt.Errorf("MongoDB %s path %q traverses unsupported nested or reference repeated field %q", role, path.String(), field.Name)
		}
		if field.Type == schema.FieldTypeBlocks {
			return mongoPredicatePath{}, fmt.Errorf("MongoDB %s path %q traverses unsupported nested blocks field %q", role, path.String(), field.Name)
		}
		if index == len(segments)-1 {
			break
		}
		if field.Type != schema.FieldTypeGroup || field.Nested == nil {
			return mongoPredicatePath{}, fmt.Errorf("MongoDB %s field %q is not a non-repeated group and cannot be traversed", role, strings.Join(segments[:index+1], "."))
		}
		objectAncestors = append(objectAncestors, scope.path(mongoAuthoredValuesPath+strings.Join(storageSegments, ".")))
		child, childFound := mongoFieldNamed(field.Nested.ResolvedFields(), segments[index+1])
		if !childFound {
			return mongoPredicatePath{}, fmt.Errorf("MongoDB %s field %q is not in collection %q", role, path.String(), collection.Slug)
		}
		field = child
		storageSegments = append(storageSegments, segments[index+1])
	}

	kind, supported := mongoScalarFieldKind(field)
	if primitivefield.IsList(field) {
		if !mongoRepeatedPredicateRole(role) {
			return mongoPredicatePath{}, fmt.Errorf("MongoDB %s does not support primitive list %q", role, path.String())
		}
		kind, supported = mongoNumberScalar, true
	}
	if !supported {
		return mongoPredicatePath{}, fmt.Errorf("MongoDB %s path %q ends at unsupported non-scalar field type %q", role, path.String(), field.Type)
	}
	resolved := mongoPredicatePath{
		storagePath:     scope.path(mongoAuthoredValuesPath + strings.Join(storageSegments, ".")),
		objectAncestors: objectAncestors,
		kind:            kind,
	}
	if primitivefield.IsList(field) {
		copy := field
		resolved.primitiveList = &copy
	}
	if localized {
		resolved.objectAncestors = append(resolved.objectAncestors, resolved.storagePath)
		resolved.localePaths = make([]string, len(scope.localeChain))
		for index, locale := range scope.localeChain {
			resolved.localePaths[index] = resolved.storagePath + "." + string(locale)
		}
	}
	return resolved, nil
}

func resolveMongoUploadSizePredicatePath(
	collection schema.Collection,
	segments []string,
	path query.Path,
	role string,
	scope mongoPredicateScope,
) (mongoPredicatePath, error) {
	switch role {
	case "filter", "access", "version access":
	default:
		return mongoPredicatePath{}, fmt.Errorf("MongoDB %s does not support configured upload image-size object-key paths", role)
	}
	if len(segments) != 3 || segments[2] != "objectKey" {
		return mongoPredicatePath{}, fmt.Errorf("MongoDB %s path %q is not a configured upload image-size object key", role, path.String())
	}
	sizesField, found := mongoFieldNamed(collection.Fields, "sizes")
	if !found || !mongoUploadSizesField(sizesField) {
		return mongoPredicatePath{}, fmt.Errorf("MongoDB %s path %q is not canonical framework upload metadata", role, path.String())
	}
	configured := false
	for _, size := range collection.Upload.ImageSizes {
		if size.Name == segments[1] {
			configured = true
			break
		}
	}
	if !configured {
		return mongoPredicatePath{}, fmt.Errorf("MongoDB %s path %q is not a configured upload image-size object key", role, path.String())
	}
	sizesPath := scope.path(mongoAuthoredValuesPath + "sizes")
	sizePath := sizesPath + "." + segments[1]
	return mongoPredicatePath{
		storagePath:     sizePath + ".objectKey",
		objectAncestors: []string{sizesPath, sizePath},
		kind:            mongoStringScalar,
	}, nil
}

// resolveMongoRepeatedPredicatePath rejects a path whose root field holds
// many values. Filters through or at arrays, blocks and has-many selects
// resolve as nested paths first; no other role has a single value there.
func resolveMongoRepeatedPredicatePath(
	collection schema.Collection,
	root schema.Field,
	path query.Path,
	role string,
) (mongoPredicatePath, error) {
	if !mongoRepeatedPredicateRole(role) {
		return mongoPredicatePath{}, fmt.Errorf("MongoDB %s does not support repeated path %q", role, path.String())
	}
	return mongoPredicatePath{}, fmt.Errorf("MongoDB %s path %q reaches the repeated field %q of collection %q outside a nested path", role, path.String(), root.Name, collection.Slug)
}

func mongoRepeatedPredicateRole(role string) bool {
	switch role {
	case "filter", "access", "version access":
		return true
	default:
		return false
	}
}

func resolveMongoProjectionPath(collection schema.Collection, path query.Path) (string, error) {
	segments := path.Segments()
	if len(segments) == 0 {
		return "", fmt.Errorf("projection path is empty")
	}
	if len(segments) == 1 {
		switch segments[0] {
		case "id":
			return mongoIDPath, nil
		case "createdAt":
			return mongoCreatedAtPath, nil
		case "updatedAt":
			return mongoUpdatedAtPath, nil
		case "deletedAt":
			return mongoDeletedAtPath, nil
		case "_status":
			if collection.Versions != nil {
				return mongoStatusPath, nil
			}
		case "_revision":
			if collection.Versions != nil || collection.Upload != nil {
				return mongoRevisionPath, nil
			}
		}
	}

	field, found := mongoFieldNamed(collection.Fields, segments[0])
	if !found {
		return "", fmt.Errorf("projection field %q is not in collection %q", path.String(), collection.Slug)
	}
	if field.Type == schema.FieldTypeArray || field.Type == schema.FieldTypeBlocks || field.Type == schema.FieldTypeSelect && field.Select != nil && field.Select.HasMany {
		if len(segments) != 1 {
			return "", fmt.Errorf("projection path %q traverses a repeated field; only whole-root repeated projections are supported", path.String())
		}
		return mongoAuthoredValuesPath + field.Name, nil
	}
	storageSegments := []string{segments[0]}
	for index := range segments {
		if field.Localized {
			if index != len(segments)-1 {
				return "", fmt.Errorf("projection path %q traverses localized field %q; localized containers are not supported", path.String(), field.Name)
			}
		}
		if index == len(segments)-1 {
			break
		}
		if field.Type == schema.FieldTypeArray || mongoFieldHasMany(field) {
			return "", fmt.Errorf("projection path %q traverses repeated field %q; repeated projections are not implemented", path.String(), field.Name)
		}
		if field.Type == schema.FieldTypeBlocks {
			return "", fmt.Errorf("projection path %q traverses blocks field %q; block projections are not implemented", path.String(), field.Name)
		}
		if field.Type != schema.FieldTypeGroup || field.Nested == nil {
			return "", fmt.Errorf("projection field %q is not a non-repeated group and cannot be traversed", strings.Join(segments[:index+1], "."))
		}
		child, childFound := mongoFieldNamed(field.Nested.ResolvedFields(), segments[index+1])
		if !childFound {
			return "", fmt.Errorf("projection field %q is not in collection %q", path.String(), collection.Slug)
		}
		field = child
		storageSegments = append(storageSegments, segments[index+1])
	}
	if mongoUploadSizesField(field) {
		return mongoAuthoredValuesPath + strings.Join(storageSegments, "."), nil
	}
	if field.Type != schema.FieldTypeGroup && field.Type != schema.FieldTypeRelationship && field.Type != schema.FieldTypeUpload &&
		field.Type != schema.FieldTypeJSON && field.Type != schema.FieldTypePlugin && field.Type != schema.FieldTypePoint && !primitivefield.IsList(field) {
		if _, supported := mongoScalarFieldKind(field); !supported {
			return "", fmt.Errorf("projection path %q ends at unsupported field type %q", path.String(), field.Type)
		}
	}
	return mongoAuthoredValuesPath + strings.Join(storageSegments, "."), nil
}

func mongoScalarFieldKind(field schema.Field) (mongoScalarKind, bool) {
	switch field.Type {
	case schema.FieldTypeText, schema.FieldTypeCode, schema.FieldTypeTextarea, schema.FieldTypeEmail,
		schema.FieldTypeDate, schema.FieldTypeRadio:
		return mongoStringScalar, true
	case schema.FieldTypeSelect:
		if field.Select == nil || !field.Select.HasMany {
			return mongoStringScalar, true
		}
	case schema.FieldTypeNumber:
		return mongoNumberScalar, true
	case schema.FieldTypeCheckbox:
		return mongoBooleanScalar, true
	case schema.FieldTypeRelationship:
		if field.Relationship != nil && !field.Relationship.HasMany && !field.Relationship.Polymorphic {
			return mongoStringScalar, true
		}
	case schema.FieldTypeUpload:
		if field.Upload != nil && !field.Upload.HasMany {
			return mongoStringScalar, true
		}
	}
	return 0, false
}

func mongoFieldHasMany(field schema.Field) bool {
	switch field.Type {
	case schema.FieldTypeSelect:
		return field.Select != nil && field.Select.HasMany
	case schema.FieldTypeRelationship:
		return field.Relationship != nil && (field.Relationship.HasMany || field.Relationship.Polymorphic)
	case schema.FieldTypeUpload:
		return field.Upload != nil && field.Upload.HasMany
	default:
		return false
	}
}

func mongoFieldNamed(fields []schema.Field, name string) (schema.Field, bool) {
	for _, field := range fields {
		if field.Name == name && field.Category != schema.FieldCategoryPresentation {
			return field, true
		}
	}
	return schema.Field{}, false
}

func mongoBSONType(kind mongoScalarKind) string {
	switch kind {
	case mongoStringScalar:
		return "string"
	case mongoNumberScalar:
		return "double"
	case mongoIntegerScalar:
		return "long"
	case mongoBooleanScalar:
		return "bool"
	case mongoTimestampScalar:
		return "long"
	default:
		return "missing"
	}
}

func mongoTypeGuard(path, bsonType string) bson.D {
	return bson.D{{
		Key: path,
		Value: bson.D{
			{Key: "$type", Value: bsonType},
			{Key: "$not", Value: bson.D{{Key: "$type", Value: "array"}}},
		},
	}}
}

func mongoNullPredicate(path string) bson.D {
	return bson.D{{
		Key: "$or",
		Value: bson.A{
			bson.D{{Key: path, Value: bson.D{{Key: "$exists", Value: false}}}},
			bson.D{{
				Key: path,
				Value: bson.D{
					{Key: "$type", Value: "null"},
					{Key: "$not", Value: bson.D{{Key: "$type", Value: "array"}}},
				},
			}},
		},
	}}
}

func mongoNullableObjectPredicate(path string) bson.D {
	return bson.D{{
		Key: "$or",
		Value: bson.A{
			bson.D{{Key: path, Value: bson.D{{Key: "$exists", Value: false}}}},
			mongoExactNullPredicate(path),
			mongoTypeGuard(path, "object"),
		},
	}}
}

func mongoExactNullPredicate(path string) bson.D {
	return bson.D{{
		Key: path,
		Value: bson.D{
			{Key: "$type", Value: "null"},
			{Key: "$not", Value: bson.D{{Key: "$type", Value: "array"}}},
		},
	}}
}

func mongoDeletedAtShapePredicate() bson.D {
	return bson.D{{
		Key: "$or",
		Value: bson.A{
			mongoExactNullPredicate(mongoDeletedAtPath),
			mongoTypeGuard(mongoDeletedAtPath, "long"),
		},
	}}
}

func mongoNonNullPredicate(path string) bson.D {
	return bson.D{{
		Key: path,
		Value: bson.D{
			{Key: "$exists", Value: true},
			{Key: "$ne", Value: nil},
		},
	}}
}

func mongoConstant(value bool) bson.D {
	return bson.D{{Key: "$expr", Value: value}}
}

func mongoAnd(predicates []bson.D) bson.D {
	switch len(predicates) {
	case 0:
		return bson.D{}
	case 1:
		return predicates[0]
	default:
		return bson.D{{Key: "$and", Value: mongoDocumentArray(predicates)}}
	}
}

func mongoDocumentArray(documents []bson.D) bson.A {
	result := make(bson.A, len(documents))
	for index, document := range documents {
		result[index] = document
	}
	return result
}
