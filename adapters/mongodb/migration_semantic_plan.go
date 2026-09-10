package mongodb

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/referenceindex"
	"github.com/riducms/ridu/internal/schemadiff"
	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

const (
	mongoRiskTransformedSchema = "RIDU_MONGODB_TRANSFORMED_SCHEMA_CHANGE"
	mongoRiskRetireResources   = "RIDU_RETIRE_RESOURCE_STATE"
)

type mongoDBSemanticRenamePlan struct {
	intents           []ridumigration.Rename
	collectionMapping map[schema.StableID]schema.StableID
	fieldMapping      map[string]schema.Field
}

type mongoDBIndexDelta struct {
	drops   []mongoDBPlannedIndex
	creates []mongoDBPlannedIndex
}

func requireMongoDBDestructiveApproval(risks []ridumigration.Risk, allowed bool) error {
	if allowed {
		return nil
	}
	var blocked []ridumigration.Risk
	for _, risk := range risks {
		if risk.Level == ridumigration.RiskDestructive {
			blocked = append(blocked, risk)
		}
	}
	if len(blocked) != 0 {
		return &SafetyError{Risks: blocked}
	}
	return nil
}

func buildMongoDBSemanticArtifact(
	ctx context.Context,
	name string,
	before *schema.Manifest,
	after schema.Manifest,
	previousPlannerVersion string,
	contract mongoDBPlannerContract,
	options ArtifactOptions,
) (ridumigration.Artifact, error) {
	if !contract.semantic || contract.version != mongoDBPlannerVersionV2 {
		return ridumigration.Artifact{}, fmt.Errorf("MongoDB planner %q does not implement semantic migrations", contract.version)
	}
	if err := ctx.Err(); err != nil {
		return ridumigration.Artifact{}, err
	}
	if before == nil && (len(options.Renames) != 0 || len(options.DataTransforms) != 0) {
		return ridumigration.Artifact{}, fmt.Errorf("initial MongoDB migration cannot contain renames or data transforms")
	}
	if before != nil && previousPlannerVersion != contract.version && previousPlannerVersion != mongoDBPlannerVersion {
		return ridumigration.Artifact{}, fmt.Errorf("unsupported MongoDB planner transition %q -> %q", previousPlannerVersion, contract.version)
	}
	if err := validateMongoDBTransformDescriptors(options.DataTransforms); err != nil {
		return ridumigration.Artifact{}, err
	}

	artifact, err := ridumigration.NewArtifact(name, ridumigration.Planner{Name: mongoDBPlannerName, Version: contract.version}, before, after)
	if err != nil {
		return ridumigration.Artifact{}, err
	}
	var renamePlan mongoDBSemanticRenamePlan
	var retired []schema.StableID
	var normalizedBefore schema.Snapshot
	if before != nil {
		renamePlan, err = compileMongoDBRenamePlan(*before, after, options.Renames)
		if err != nil {
			return ridumigration.Artifact{}, err
		}
		normalizedBefore, retired, err = normalizeMongoDBSemanticBefore(before.Snapshot(), after.Snapshot(), renamePlan)
		if err != nil {
			return ridumigration.Artifact{}, err
		}
		if err := validateMongoDBRetirement(before.Snapshot(), after.Snapshot(), renamePlan.collectionMapping, retired); err != nil {
			return ridumigration.Artifact{}, err
		}
		for source, target := range renamePlan.collectionMapping {
			if source != target && len(options.DataTransforms) != 0 {
				return ridumigration.Artifact{}, fmt.Errorf("MongoDB collection identity renames cannot share an artifact with data transforms")
			}
		}
		if err := validateMongoDBAdditiveTransition(normalizedBefore, after.Snapshot()); err != nil {
			if len(options.DataTransforms) == 0 {
				return ridumigration.Artifact{}, err
			}
			if transformErr := validateMongoDBTransformedTransition(normalizedBefore, after.Snapshot()); transformErr != nil {
				return ridumigration.Artifact{}, fmt.Errorf("MongoDB data transform cannot authorize transition: %w", transformErr)
			}
			artifact.Risks = append(artifact.Risks, ridumigration.Risk{
				Code: mongoRiskTransformedSchema, Level: ridumigration.RiskDestructive,
				Message: "the compiled data transform is responsible for preserving or deliberately rewriting current MongoDB documents across this otherwise unsupported field change",
			})
		}
	}

	var beforePlans mongoPhysicalIndexPlanSet
	if before != nil {
		beforePlans, err = mongoPhysicalIndexPlans(*before)
		if err != nil {
			return ridumigration.Artifact{}, fmt.Errorf("plan previous MongoDB physical indexes: %w", err)
		}
	}
	afterPlans, err := mongoPhysicalIndexPlans(after)
	if err != nil {
		return ridumigration.Artifact{}, fmt.Errorf("plan MongoDB physical indexes: %w", err)
	}
	if err := validateMongoDBMigrationPlanUnion(beforePlans, afterPlans); err != nil {
		return ridumigration.Artifact{}, err
	}
	delta, err := mongoDBSemanticIndexDelta(beforePlans, afterPlans, renamePlan.collectionMapping, retired)
	if err != nil {
		return ridumigration.Artifact{}, err
	}
	for _, create := range delta.creates {
		if create.definition.unique {
			artifact.Risks = append(artifact.Risks, ridumigration.Risk{
				Code: "RIDU_BUILD_UNIQUE_INDEX", Level: ridumigration.RiskWarning,
				Message: fmt.Sprintf("build unique MongoDB index %s for %s; the migration fails if existing indexed values conflict", create.name, create.description),
			})
		}
	}
	if len(delta.drops)+len(delta.creates) != 0 {
		artifact.Risks = append(artifact.Risks, ridumigration.Risk{
			Code: "RIDU_MONGODB_INDEX_BUILD_NON_TRANSACTIONAL", Level: ridumigration.RiskWarning,
			Message: "MongoDB index reconciliation runs as resumable no-transaction phases around semantic document work; an interrupted artifact may leave a reviewed intermediate index state",
		})
	}
	if len(retired) != 0 {
		artifact.Risks = append(artifact.Risks, ridumigration.Risk{
			Code: mongoRiskRetireResources, Level: ridumigration.RiskDestructive,
			Message: fmt.Sprintf("permanently retire framework-owned current, version, authentication, preference, task, lock, and reference state for removed resources %s", mongoStableIDList(retired)),
		})
	}
	artifact.Risks = normalizeMongoDBRisks(artifact.Risks)
	if err := requireMongoDBDestructiveApproval(artifact.Risks, options.AllowDestructive); err != nil {
		return ridumigration.Artifact{}, err
	}

	artifact.Phases, err = mongoDBSemanticArtifactPhases(artifact.FromDigest, before, after, renamePlan, options.DataTransforms, retired, delta)
	if err != nil {
		return ridumigration.Artifact{}, err
	}
	if err := artifact.Validate(); err != nil {
		return ridumigration.Artifact{}, err
	}
	return artifact, nil
}

func validateMongoDBTransformDescriptors(descriptors []ridumigration.DataTransformDescriptor) error {
	seen := make(map[string]struct{}, len(descriptors))
	for _, descriptor := range descriptors {
		if err := descriptor.Validate(); err != nil {
			return err
		}
		if _, duplicate := seen[descriptor.Name]; duplicate {
			return fmt.Errorf("MongoDB data transform %q is bound more than once", descriptor.Name)
		}
		seen[descriptor.Name] = struct{}{}
	}
	return nil
}

func validateMongoDBDataTransformIdentities(files []migrationartifact.File, additional []ridumigration.DataTransformDescriptor) error {
	checksums := make(map[string]string)
	inspect := func(descriptors []ridumigration.DataTransformDescriptor) error {
		for _, descriptor := range descriptors {
			if previous, exists := checksums[descriptor.Name]; exists && previous != descriptor.Checksum {
				return fmt.Errorf("MongoDB data transform %q changed checksum after immutable history; use a new transform name", descriptor.Name)
			}
			checksums[descriptor.Name] = descriptor.Checksum
		}
		return nil
	}
	for _, file := range files {
		options, err := mongoDBArtifactSemanticOptions(file.Artifact)
		if err != nil {
			return err
		}
		if err := inspect(options.DataTransforms); err != nil {
			return err
		}
	}
	return inspect(additional)
}

func mongoDBArtifactSemanticOptions(artifact ridumigration.Artifact) (ArtifactOptions, error) {
	var options ArtifactOptions
	for _, phase := range artifact.Phases {
		for _, step := range phase.Steps {
			switch step.Kind {
			case ridumigration.StepRenameContent:
				var payload ridumigration.RenamePayload
				if err := json.Unmarshal(step.Payload, &payload); err != nil {
					return ArtifactOptions{}, err
				}
				options.Renames = append(options.Renames, payload.Rename)
			case ridumigration.StepDataTransform:
				var payload ridumigration.DataTransformPayload
				if err := json.Unmarshal(step.Payload, &payload); err != nil {
					return ArtifactOptions{}, err
				}
				options.DataTransforms = append(options.DataTransforms, payload.Transform)
			}
		}
	}
	return options, nil
}

func compileMongoDBRenamePlan(before, after schema.Manifest, intents []ridumigration.Rename) (mongoDBSemanticRenamePlan, error) {
	plan := mongoDBSemanticRenamePlan{
		collectionMapping: make(map[schema.StableID]schema.StableID),
		fieldMapping:      make(map[string]schema.Field),
	}
	if err := validateMongoDBCollectionSlugRewriteOverlap(intents); err != nil {
		return plan, err
	}
	beforeSnapshot, afterSnapshot := before.Snapshot(), after.Snapshot()
	beforeBySlug, afterBySlug := mongoSemanticCollectionsBySlug(beforeSnapshot.Collections), mongoSemanticCollectionsBySlug(afterSnapshot.Collections)
	targets := make(map[schema.StableID]schema.StableID)
	for _, intent := range intents {
		if intent.CollectionBefore == "" || intent.CollectionAfter == "" {
			return plan, fmt.Errorf("MongoDB rename intent requires before and after collection slugs")
		}
		previous, previousOK := beforeBySlug[intent.CollectionBefore]
		next, nextOK := afterBySlug[intent.CollectionAfter]
		if !previousOK || !nextOK {
			return plan, fmt.Errorf("MongoDB rename intent addresses absent collection %q -> %q", intent.CollectionBefore, intent.CollectionAfter)
		}
		collectionIntent := intent.FieldBefore == "" && intent.FieldAfter == ""
		if collectionIntent {
			if existing, duplicate := plan.collectionMapping[previous.ID]; duplicate && existing != next.ID {
				return plan, fmt.Errorf("MongoDB collection rename source %s is ambiguous", previous.ID)
			}
			if existing, duplicate := targets[next.ID]; duplicate && existing != previous.ID {
				return plan, fmt.Errorf("MongoDB collection rename target %s is ambiguous", next.ID)
			}
			if previous.ID == next.ID && previous.Slug == next.Slug {
				return plan, fmt.Errorf("MongoDB collection rename %s does not change identity or slug", previous.ID)
			}
			plan.collectionMapping[previous.ID], targets[next.ID] = next.ID, previous.ID
		}
	}
	candidates := schemadiff.RenameCandidatesWithCollectionMapping(before, after, plan.collectionMapping)
	for _, intent := range intents {
		previous, next := beforeBySlug[intent.CollectionBefore], afterBySlug[intent.CollectionAfter]
		if intent.FieldBefore != intent.FieldAfter && (embedded.DescendantPath(previous.Fields, intent.FieldBefore) || embedded.DescendantPath(next.Fields, intent.FieldAfter)) {
			return plan, fmt.Errorf("embedded field rename %q to %q requires an explicit data migration", intent.FieldBefore, intent.FieldAfter)
		}
		for _, pair := range intent.Fields {
			if pair.Before != pair.After && (embedded.DescendantPath(previous.Fields, pair.Before) || embedded.DescendantPath(next.Fields, pair.After)) {
				return plan, fmt.Errorf("embedded field rename %q to %q requires an explicit data migration", pair.Before, pair.After)
			}
		}

		collectionIntent := intent.FieldBefore == "" && intent.FieldAfter == ""
		if collectionIntent && previous.ID != next.ID {
			candidate, found := mongoCollectionRenameCandidate(candidates, previous.ID, next.ID)
			if !found {
				return plan, fmt.Errorf("MongoDB collection rename %s -> %s is unsupported or ambiguous", previous.ID, next.ID)
			}
			expected := make(map[string]string)
			for _, pair := range candidate.Fields {
				plan.fieldMapping[mongoFieldRenameKey(previous.ID, pair.Before.Path.String())] = pair.After
				if pair.Before.Path.String() != pair.After.Path.String() {
					expected[pair.Before.Path.String()] = pair.After.Path.String()
				}
			}
			if !sameMongoFieldRenameIntent(expected, intent.Fields) {
				return plan, fmt.Errorf("MongoDB collection rename %s -> %s does not contain the exact unambiguous field mapping", previous.ID, next.ID)
			}
		} else if collectionIntent {
			if len(intent.Fields) != 0 {
				return plan, fmt.Errorf("same-identity MongoDB collection slug rename cannot carry field mappings")
			}
		} else {
			if intent.FieldBefore == "" || intent.FieldAfter == "" || len(intent.Fields) != 0 {
				return plan, fmt.Errorf("MongoDB field rename requires one before and after path")
			}
			candidate, found := mongoFieldRenameCandidate(candidates, previous.ID, next.ID, intent.FieldBefore, intent.FieldAfter)
			if !found {
				return plan, fmt.Errorf("MongoDB field rename %s.%s -> %s.%s is unsupported or ambiguous", previous.ID, intent.FieldBefore, next.ID, intent.FieldAfter)
			}
			key := mongoFieldRenameKey(previous.ID, intent.FieldBefore)
			if existing, duplicate := plan.fieldMapping[key]; duplicate && existing.ID != candidate.AfterField.ID {
				return plan, fmt.Errorf("MongoDB field rename source %s.%s is ambiguous", previous.ID, intent.FieldBefore)
			}
			plan.fieldMapping[key] = *candidate.AfterField
		}
		plan.intents = append(plan.intents, intent)
	}
	sort.Slice(plan.intents, func(left, right int) bool {
		return mongoRenameIntentKey(plan.intents[left]) < mongoRenameIntentKey(plan.intents[right])
	})
	return plan, nil
}

func validateMongoDBCollectionSlugRewriteOverlap(intents []ridumigration.Rename) error {
	sources := make(map[schema.CollectionSlug]struct{})
	for _, intent := range intents {
		if intent.FieldBefore == "" && intent.FieldAfter == "" && intent.CollectionBefore != intent.CollectionAfter {
			sources[intent.CollectionBefore] = struct{}{}
		}
	}
	for _, intent := range intents {
		if intent.FieldBefore != "" || intent.FieldAfter != "" || intent.CollectionBefore == intent.CollectionAfter {
			continue
		}
		if _, overlap := sources[intent.CollectionAfter]; overlap {
			return fmt.Errorf("RIDU_COLLECTION_SLUG_REWRITE_OVERLAP_UNSAFE: MongoDB collection rename destination %s is also a source in the same artifact", intent.CollectionAfter)
		}
	}
	return nil
}

func mongoSemanticCollectionsBySlug(collections []schema.Collection) map[schema.CollectionSlug]schema.Collection {
	result := make(map[schema.CollectionSlug]schema.Collection, len(collections))
	for _, collection := range collections {
		result[collection.Slug] = collection
	}
	return result
}

func mongoCollectionRenameCandidate(candidates []schemadiff.RenameCandidate, before, after schema.StableID) (schemadiff.RenameCandidate, bool) {
	var result schemadiff.RenameCandidate
	found := false
	for _, candidate := range candidates {
		if candidate.Kind != schemadiff.RenameCollection || candidate.BeforeCollection.ID != before || candidate.AfterCollection.ID != after {
			continue
		}
		if found {
			return schemadiff.RenameCandidate{}, false
		}
		result, found = candidate, true
	}
	return result, found
}

func mongoFieldRenameCandidate(candidates []schemadiff.RenameCandidate, beforeCollection, afterCollection schema.StableID, beforePath, afterPath string) (schemadiff.RenameCandidate, bool) {
	var result schemadiff.RenameCandidate
	found := false
	for _, candidate := range candidates {
		if candidate.Kind != schemadiff.RenameField || candidate.BeforeCollection.ID != beforeCollection || candidate.AfterCollection.ID != afterCollection ||
			candidate.BeforeField == nil || candidate.AfterField == nil || candidate.BeforeField.Path.String() != beforePath || candidate.AfterField.Path.String() != afterPath {
			continue
		}
		if found {
			return schemadiff.RenameCandidate{}, false
		}
		result, found = candidate, true
	}
	return result, found
}

func sameMongoFieldRenameIntent(expected map[string]string, actual []ridumigration.FieldRename) bool {
	if len(expected) != len(actual) {
		return false
	}
	seen := make(map[string]struct{}, len(actual))
	for _, pair := range actual {
		if expected[pair.Before] != pair.After || pair.Before == pair.After {
			return false
		}
		key := pair.Before + "\x00" + pair.After
		if _, duplicate := seen[key]; duplicate {
			return false
		}
		seen[key] = struct{}{}
	}
	return true
}

func mongoFieldRenameKey(collectionID schema.StableID, path string) string {
	return string(collectionID) + "\x00" + path
}

func mongoRenameIntentKey(intent ridumigration.Rename) string {
	return string(intent.CollectionBefore) + "\x00" + intent.FieldBefore + "\x00" + string(intent.CollectionAfter) + "\x00" + intent.FieldAfter
}

func normalizeMongoDBSemanticBefore(before, after schema.Snapshot, plan mongoDBSemanticRenamePlan) (schema.Snapshot, []schema.StableID, error) {
	normalized := schema.NewManifest(before).Snapshot()
	afterCollections := make(map[schema.StableID]schema.Collection, len(after.Collections))
	for _, collection := range after.Collections {
		afterCollections[collection.ID] = collection
	}
	afterGlobals := make(map[schema.StableID]schema.Collection, len(after.Globals))
	for _, global := range after.Globals {
		afterGlobals[global.ID] = global
	}
	var retired []schema.StableID
	collections := make([]schema.Collection, 0, len(normalized.Collections))
	for _, collection := range normalized.Collections {
		beforeID := collection.ID
		afterID := beforeID
		mappedID := plan.collectionMapping[beforeID]
		if mappedID != "" {
			afterID = mappedID
		}
		target, exists := afterCollections[afterID]
		if !exists {
			retired = append(retired, beforeID)
			continue
		}
		if mappedID != "" {
			collection.ID, collection.Slug = target.ID, target.Slug
		}
		if err := normalizeMongoDBResourceFields(&collection, beforeID, target, plan); err != nil {
			return schema.Snapshot{}, nil, err
		}
		collections = append(collections, collection)
	}
	globals := make([]schema.Collection, 0, len(normalized.Globals))
	for _, global := range normalized.Globals {
		if _, exists := afterGlobals[global.ID]; !exists {
			retired = append(retired, global.ID)
			continue
		}
		globals = append(globals, global)
	}
	normalized.Collections, normalized.Globals = collections, globals
	normalizeMongoDBReferences(&normalized, before, after, plan.collectionMapping)
	sort.Slice(retired, func(left, right int) bool { return retired[left] < retired[right] })
	return normalized, retired, nil
}

func normalizeMongoDBResourceFields(resource *schema.Collection, beforeID schema.StableID, target schema.Collection, plan mongoDBSemanticRenamePlan) error {
	var rewrite func([]schema.Field) ([]schema.Field, error)
	rewrite = func(fields []schema.Field) ([]schema.Field, error) {
		result := append([]schema.Field(nil), fields...)
		for index := range result {
			originalPath := result[index].Path.String()
			if mapped, exists := plan.fieldMapping[mongoFieldRenameKey(beforeID, originalPath)]; exists {
				result[index].ID, result[index].Name, result[index].Path = mapped.ID, mapped.Name, mapped.Path
			}
			if result[index].Nested != nil {
				nested := *result[index].Nested
				children, err := rewrite(nested.ResolvedFields())
				if err != nil {
					return nil, err
				}
				nested.Fields, result[index].Nested = children, &nested
			}
			if result[index].Blocks != nil {
				blocks := *result[index].Blocks
				blocks.Types = append([]schema.BlockType(nil), blocks.ResolvedTypes()...)
				for blockIndex := range blocks.ResolvedTypes() {
					children, err := rewrite(blocks.ResolvedTypes()[blockIndex].ResolvedFields())
					if err != nil {
						return nil, err
					}
					blocks.ResolvedTypes()[blockIndex].Fields = children
				}
				result[index].Blocks = &blocks
			}
			if result[index].Plugin != nil {
				plugin := *result[index].Plugin
				plugin.EmbeddedTrees = append([]schema.EmbeddedTree(nil), plugin.EmbeddedTrees...)
				for ti := range plugin.EmbeddedTrees {
					tree := &plugin.EmbeddedTrees[ti]
					tree.Cases = append([]schema.EmbeddedTreeCase(nil), tree.Cases...)
					for ci := range tree.Cases {
						c := &tree.Cases[ci]
						c.Types = append([]schema.BlockType(nil), c.ResolvedTypes()...)
						for vi := range c.ResolvedTypes() {
							children, err := rewrite(c.ResolvedTypes()[vi].ResolvedFields())
							if err != nil {
								return nil, err
							}
							c.ResolvedTypes()[vi].Fields = children
						}
					}
				}
				result[index].Plugin = &plugin
			}

		}
		return result, nil
	}
	fields, err := rewrite(resource.Fields)
	if err != nil {
		return err
	}
	resource.Fields = fields
	pathMapping := make(map[string]string)
	for key, mapped := range plan.fieldMapping {
		if strings.HasPrefix(key, string(beforeID)+"\x00") {
			pathMapping[strings.TrimPrefix(key, string(beforeID)+"\x00")] = mapped.Path.String()
		}
	}
	for index := range resource.Indexes {
		paths := append([]query.Path(nil), resource.Indexes[index].Fields...)
		for pathIndex, path := range paths {
			if mapped := pathMapping[path.String()]; mapped != "" {
				parsed, parseErr := query.ParsePath(mapped)
				if parseErr != nil {
					return parseErr
				}
				paths[pathIndex] = parsed
			}
		}
		resource.Indexes[index].Fields = paths
	}
	if resource.Auth != nil {
		copy := *resource.Auth
		if mapped := pathMapping[copy.IdentityField]; mapped != "" && !strings.Contains(mapped, ".") {
			copy.IdentityField = mapped
		}
		resource.Auth = &copy
	}
	return nil
}

func normalizeMongoDBReferences(snapshot *schema.Snapshot, before, after schema.Snapshot, mapping map[schema.StableID]schema.StableID) {
	afterSlugByID := make(map[schema.StableID]schema.CollectionSlug)
	_ = before
	for _, collection := range after.Collections {
		afterSlugByID[collection.ID] = collection.Slug
	}
	var visit func([]schema.Field)
	visit = func(fields []schema.Field) {
		for index := range fields {
			field := &fields[index]
			if field.Relationship != nil {
				relationship := *field.Relationship
				if mapped := mapping[relationship.CollectionID]; mapped != "" {
					relationship.CollectionID, relationship.CollectionSlug = mapped, afterSlugByID[mapped]
				}
				relationship.Targets = append([]schema.RelationshipTarget(nil), relationship.Targets...)
				for targetIndex := range relationship.Targets {
					if mapped := mapping[relationship.Targets[targetIndex].CollectionID]; mapped != "" {
						relationship.Targets[targetIndex].CollectionID = mapped
						relationship.Targets[targetIndex].CollectionSlug = afterSlugByID[mapped]
					}
				}
				field.Relationship = &relationship
			}
			if field.Upload != nil {
				upload := *field.Upload
				if mapped := mapping[upload.CollectionID]; mapped != "" {
					upload.CollectionID, upload.CollectionSlug = mapped, afterSlugByID[mapped]
				}
				field.Upload = &upload
			}
			if field.Join != nil {
				join := *field.Join
				if mapped := mapping[join.CollectionID]; mapped != "" {
					join.CollectionID, join.CollectionSlug = mapped, afterSlugByID[mapped]
				}
				field.Join = &join
			}
			if field.Nested != nil {
				visit(field.Nested.ResolvedFields())
			}
			if field.Blocks != nil {
				for blockIndex := range field.Blocks.ResolvedTypes() {
					visit(field.Blocks.ResolvedTypes()[blockIndex].ResolvedFields())
				}
			}
			if field.Plugin != nil {
				for _, tree := range field.Plugin.EmbeddedTrees {
					for _, c := range tree.Cases {
						for _, variant := range c.ResolvedTypes() {
							visit(variant.ResolvedFields())
						}
					}
				}
			}

		}
	}
	for index := range snapshot.Collections {
		visit(snapshot.Collections[index].Fields)
	}
	for index := range snapshot.Globals {
		visit(snapshot.Globals[index].Fields)
	}
	if snapshot.Application.Admin != nil {
		admin := *snapshot.Application.Admin
		if mapped := mapping[admin.UserCollectionID]; mapped != "" {
			admin.UserCollectionID, admin.UserCollectionSlug = mapped, afterSlugByID[mapped]
		}
		snapshot.Application.Admin = &admin
	}
}

func validateMongoDBRetirement(before, after schema.Snapshot, mapping map[schema.StableID]schema.StableID, retired []schema.StableID) error {
	if len(retired) == 0 {
		return nil
	}
	retiredSet := make(map[schema.StableID]struct{}, len(retired))
	for _, id := range retired {
		retiredSet[id] = struct{}{}
	}
	retiredCollection := false
	for _, collection := range before.Collections {
		if _, removed := retiredSet[collection.ID]; removed && (collection.Upload != nil || collection.Capabilities.Upload) {
			return fmt.Errorf("RIDU_UPLOAD_COLLECTION_REMOVAL_UNSAFE: MongoDB artifact cannot retire upload collection %s while external objects may remain", collection.ID)
		}
		if _, removed := retiredSet[collection.ID]; removed {
			retiredCollection = true
		}
	}
	for _, owner := range append(append([]schema.Collection(nil), before.Collections...), before.Globals...) {
		ownerID := owner.ID
		if mapped := mapping[ownerID]; mapped != "" {
			ownerID = mapped
		}
		if _, removed := retiredSet[owner.ID]; removed {
			continue
		}
		if referenceindex.TargetsAnyResource(owner, retired) {
			return fmt.Errorf("RIDU_REFERENCE_SHAPE_DECREASE_UNSAFE: surviving resource %s can retain current or historical references to a retired resource", ownerID)
		}
		if retiredCollection && mongoFieldsContainDeclaredPluginCollectionReferences(owner.Fields) {
			return fmt.Errorf("RIDU_REFERENCE_SHAPE_DECREASE_UNSAFE: surviving resource %s can retain current or historical declared plugin references to a retired resource", ownerID)
		}
	}
	for _, owner := range append(append([]schema.Collection(nil), after.Collections...), after.Globals...) {
		if referenceindex.TargetsAnyResource(owner, retired) {
			return fmt.Errorf("RIDU_REFERENCE_SHAPE_DECREASE_UNSAFE: surviving resource %s still targets a retired resource", owner.ID)
		}
	}
	return nil
}

func mongoFieldsContainDeclaredPluginCollectionReferences(fields []schema.Field) bool {
	for _, field := range fields {
		if field.Plugin != nil && len(field.Plugin.ReferenceKeys) != 0 {
			return true
		}
		if mongoFieldsContainDeclaredPluginCollectionReferences(schema.ChildFields(field)) {
			return true
		}
	}
	return false
}

func validateMongoDBTransformedTransition(before, after schema.Snapshot) error {
	application := after.Application
	application.AllowIDOnCreate = before.Application.AllowIDOnCreate
	if !reflect.DeepEqual(before.Application, application) {
		return fmt.Errorf("data transforms cannot change MongoDB application settings")
	}
	if err := validateMongoDBTransformedResources("collection", before.Collections, after.Collections); err != nil {
		return err
	}
	return validateMongoDBTransformedResources("global", before.Globals, after.Globals)
}

func validateMongoDBTransformedResources(kind string, before, after []schema.Collection) error {
	afterByID := make(map[schema.StableID]schema.Collection, len(after))
	for _, resource := range after {
		afterByID[resource.ID] = resource
	}
	for _, previous := range before {
		current, exists := afterByID[previous.ID]
		if !exists {
			return fmt.Errorf("data transforms cannot retire MongoDB %s %q; use typed resource retirement", kind, previous.ID)
		}
		comparison := current
		comparison.Fields, comparison.Indexes = previous.Fields, previous.Indexes
		comparison.Labels, comparison.Admin, comparison.Endpoints = previous.Labels, previous.Admin, previous.Endpoints
		if !reflect.DeepEqual(previous, comparison) {
			return fmt.Errorf("data transforms cannot change MongoDB %s %q outside fields, indexes, or presentation", kind, previous.ID)
		}
		if previous.Versions != nil && !reflect.DeepEqual(previous.Fields, current.Fields) {
			return fmt.Errorf("data transforms cannot mutate versioned MongoDB %s %q; use explicit field renames that rewrite snapshots", kind, previous.ID)
		}
	}
	return nil
}

func mongoDBSemanticIndexDelta(before, after mongoPhysicalIndexPlanSet, mapping map[schema.StableID]schema.StableID, retired []schema.StableID) (mongoDBIndexDelta, error) {
	retiredSet := make(map[schema.StableID]struct{}, len(retired))
	for _, id := range retired {
		retiredSet[id] = struct{}{}
	}
	previous := mongoDBFlattenedIndexPlans(before)
	next := mongoDBFlattenedIndexPlans(after)
	nextByKey := make(map[string]mongoDBPlannedIndex, len(next))
	for _, planned := range next {
		nextByKey[mongoDBReplayIndexKey(planned.collection, planned.name)] = planned
	}
	matched := make(map[string]struct{})
	var delta mongoDBIndexDelta
	for _, planned := range previous {
		if _, removed := retiredSet[planned.resourceID]; removed && planned.resourceID != "" {
			continue
		}
		if mapped := mapping[planned.resourceID]; mapped != "" {
			planned.resourceID = mapped
			if planned.version {
				planned.collection = physicalVersionCollectionName(mapped)
			} else {
				planned.collection = physicalCollectionName(mapped)
			}
		}
		key := mongoDBReplayIndexKey(planned.collection, planned.name)
		candidate, exists := nextByKey[key]
		if exists {
			left, leftErr := mongoIndexFingerprint([]mongoIndexDefinition{planned.definition})
			right, rightErr := mongoIndexFingerprint([]mongoIndexDefinition{candidate.definition})
			if leftErr != nil || rightErr != nil {
				return delta, fmt.Errorf("fingerprint MongoDB semantic index transition")
			}
			if left == right {
				matched[key] = struct{}{}
				continue
			}
		}
		if planned.resourceID == "" {
			return delta, fmt.Errorf("MongoDB semantic migration cannot remove or change shared system index %q", planned.name)
		}
		delta.drops = append(delta.drops, planned)
	}
	for _, planned := range next {
		key := mongoDBReplayIndexKey(planned.collection, planned.name)
		if _, exists := matched[key]; !exists {
			delta.creates = append(delta.creates, planned)
		}
	}
	sort.Slice(delta.drops, func(left, right int) bool {
		return mongoDBReplayIndexKey(delta.drops[left].collection, delta.drops[left].name) < mongoDBReplayIndexKey(delta.drops[right].collection, delta.drops[right].name)
	})
	sort.Slice(delta.creates, func(left, right int) bool {
		return mongoDBReplayIndexKey(delta.creates[left].collection, delta.creates[left].name) < mongoDBReplayIndexKey(delta.creates[right].collection, delta.creates[right].name)
	})
	return delta, nil
}

func mongoDBSemanticArtifactPhases(fromDigest string, before *schema.Manifest, after schema.Manifest, renames mongoDBSemanticRenamePlan, transforms []ridumigration.DataTransformDescriptor, retired []schema.StableID, delta mongoDBIndexDelta) ([]ridumigration.Phase, error) {
	physical := ridumigration.PhysicalDigestSeed(fromDigest)
	var phases []ridumigration.Phase
	stepNumber := 0
	appendPhase := func(mode ridumigration.PhaseMode, values []struct {
		kind ridumigration.StepKind
		name string
		data any
	}) error {
		steps := make([]ridumigration.Step, 0, len(values))
		for _, value := range values {
			payload, err := ridumigration.MarshalStepPayload(value.data)
			if err != nil {
				return err
			}
			stepNumber++
			steps = append(steps, ridumigration.Step{ID: fmt.Sprintf("step-%04d", stepNumber), Kind: value.kind, ExecutorVersion: 1, Name: value.name, Payload: payload})
		}
		afterDigest, err := ridumigration.PhasePhysicalDigest(physical, mode, steps)
		if err != nil {
			return err
		}
		phases = append(phases, ridumigration.Phase{ID: fmt.Sprintf("phase-%03d", len(phases)+1), Mode: mode, PhysicalContractVersion: ridumigration.PhysicalContractVersion, BeforePhysicalDigest: physical, AfterPhysicalDigest: afterDigest, Steps: steps})
		physical = afterDigest
		return nil
	}
	type phaseValue = struct {
		kind ridumigration.StepKind
		name string
		data any
	}
	if before != nil {
		var ids []schema.StableID
		for source, target := range renames.collectionMapping {
			if source != target {
				ids = append(ids, source)
			}
		}
		sort.Slice(ids, func(left, right int) bool { return ids[left] < ids[right] })
		for _, source := range ids {
			target := renames.collectionMapping[source]
			if err := appendPhase(ridumigration.PhaseNoTransaction, []phaseValue{{
				kind: ridumigration.StepMongoDBRenameResource,
				name: fmt.Sprintf("rename MongoDB resource %s to %s", source, target),
				data: ridumigration.MongoDBRenameResourcePayload{BeforeID: source, AfterID: target},
			}}); err != nil {
				return nil, err
			}
		}
	}
	for _, drop := range delta.drops {
		if err := appendPhase(ridumigration.PhaseNoTransaction, []phaseValue{{
			kind: ridumigration.StepMongoDBDropIndex,
			name: fmt.Sprintf("drop superseded MongoDB index %s for %s", drop.name, drop.description),
			data: ridumigration.MongoDBDropIndexPayload{CollectionID: drop.resourceID, Version: drop.version, Index: drop.name},
		}}); err != nil {
			return nil, err
		}
	}
	var semantic []phaseValue
	for _, intent := range renames.intents {
		semantic = append(semantic, phaseValue{kind: ridumigration.StepRenameContent, name: mongoSemanticRenameName(intent), data: ridumigration.RenamePayload{Rename: intent}})
	}
	for _, transform := range transforms {
		semantic = append(semantic, phaseValue{kind: ridumigration.StepDataTransform, name: "run data transform " + transform.Name, data: ridumigration.DataTransformPayload{Transform: transform}})
	}
	if len(retired) != 0 {
		semantic = append(semantic, phaseValue{kind: ridumigration.StepRetireResources, name: "retire framework state for removed MongoDB resources", data: ridumigration.RetireResourcesPayload{ResourceIDs: append([]schema.StableID(nil), retired...)}})
	}
	if len(semantic) != 0 {
		if err := appendPhase(ridumigration.PhaseTransaction, semantic); err != nil {
			return nil, err
		}
	}
	if len(retired) != 0 {
		if err := appendPhase(ridumigration.PhaseNoTransaction, []phaseValue{{
			kind: ridumigration.StepMongoDBDropResources,
			name: "drop retired MongoDB resource namespaces",
			data: ridumigration.MongoDBDropResourcesPayload{ResourceIDs: append([]schema.StableID(nil), retired...)},
		}}); err != nil {
			return nil, err
		}
	}
	for _, create := range delta.creates {
		if err := appendPhase(ridumigration.PhaseNoTransaction, []phaseValue{{
			kind: ridumigration.StepMongoDBCreateIndex,
			name: fmt.Sprintf("create MongoDB index %s for %s", create.name, create.description),
			data: ridumigration.MongoDBCreateIndexPayload{Collection: create.collection, Index: create.name},
		}}); err != nil {
			return nil, err
		}
	}
	if err := appendPhase(ridumigration.PhaseNoTransaction, []phaseValue{{kind: ridumigration.StepMongoDBAssertSchema, name: "verify resulting MongoDB schema", data: ridumigration.AssertSchemaPayload{}}}); err != nil {
		return nil, err
	}
	return phases, nil
}

func mongoSemanticRenameName(intent ridumigration.Rename) string {
	if intent.FieldBefore != "" {
		return fmt.Sprintf("preserve MongoDB field content %s.%s -> %s.%s", intent.CollectionBefore, intent.FieldBefore, intent.CollectionAfter, intent.FieldAfter)
	}
	return fmt.Sprintf("preserve MongoDB collection content %s -> %s", intent.CollectionBefore, intent.CollectionAfter)
}

func normalizeMongoDBRisks(risks []ridumigration.Risk) []ridumigration.Risk {
	seen := make(map[string]struct{}, len(risks))
	result := make([]ridumigration.Risk, 0, len(risks))
	for _, risk := range risks {
		key := risk.Code + "\x00" + string(risk.Level) + "\x00" + risk.Message
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, risk)
	}
	sort.Slice(result, func(left, right int) bool {
		return string(result[left].Level)+result[left].Code+result[left].Message < string(result[right].Level)+result[right].Code+result[right].Message
	})
	return result
}

func mongoStableIDList(ids []schema.StableID) string {
	values := make([]string, len(ids))
	for index, id := range ids {
		values[index] = string(id)
	}
	return strings.Join(values, ", ")
}
