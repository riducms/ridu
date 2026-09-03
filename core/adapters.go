package core

import (
	"context"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func adaptCollection(authored Collection, resolved schema.Collection, local **LocalAPI) operationengine.Collection {
	readVersions := authored.Access.ReadVersions
	if readVersions == nil {
		readVersions = authored.Access.Read
	}
	unlock := authored.Access.Unlock
	if unlock == nil {
		unlock = authored.Access.Update
	}
	publish := authored.Access.Publish
	if publish == nil {
		publish = authored.Access.Update
	}
	unpublish := authored.Access.Unpublish
	if unpublish == nil {
		unpublish = authored.Access.Update
	}
	return operationengine.Collection{
		Key:    string(resolved.Slug),
		Schema: resolved,
		Access: map[operationengine.Kind]operationengine.Access{
			operationengine.Admin:           adaptAccess(authored.Access.Admin, local),
			operationengine.Create:          adaptAccess(authored.Access.Create, local),
			operationengine.Duplicate:       adaptAccess(authored.Access.Create, local),
			operationengine.Read:            adaptAccess(authored.Access.Read, local),
			operationengine.ReadVersions:    adaptAccess(readVersions, local),
			operationengine.Update:          adaptAccess(authored.Access.Update, local),
			operationengine.Publish:         adaptAccess(publish, local),
			operationengine.Unpublish:       adaptAccess(unpublish, local),
			operationengine.Delete:          adaptAccess(authored.Access.Delete, local),
			operationengine.RestoreDeleted:  adaptAccess(authored.Access.Delete, local),
			operationengine.DeletePermanent: adaptAccess(authored.Access.Delete, local),
			operationengine.Unlock:          adaptAccess(unlock, local),
		},
		Hooks: operationengine.Hooks{
			BeforeDuplicate: adaptHooks(authored.Hooks.BeforeDuplicate, local),
			BeforeValidate:  adaptHooks(authored.Hooks.BeforeValidate, local),
			BeforeChange:    adaptHooks(authored.Hooks.BeforeChange, local),
			BeforeOperation: adaptHooks(authored.Hooks.BeforeOperation, local),
			BeforeRead:      adaptHooks(authored.Hooks.BeforeRead, local),
			BeforeDelete:    adaptHooks(authored.Hooks.BeforeDelete, local),
			AfterChange:     adaptHooks(authored.Hooks.AfterChange, local),
			AfterRead:       adaptHooks(authored.Hooks.AfterRead, local),
			AfterDelete:     adaptHooks(authored.Hooks.AfterDelete, local),
			AfterOperation:  adaptHooks(authored.Hooks.AfterOperation, local),
			AfterError:      adaptHooks(authored.Hooks.AfterError, local),
			AfterCommit:     adaptHooks(authored.Hooks.AfterCommit, local),
		},
		Fields:     adaptFieldAccess(authored.FieldAccess, local),
		FieldHooks: adaptFieldHooks(authored.FieldHooks, local),
		Computed:   adaptComputed(authored.Computed, local),
	}
}

func adaptGlobal(authored Global, resolved schema.Global, local **LocalAPI) operationengine.Collection {
	readVersions := authored.Access.ReadVersions
	if readVersions == nil {
		readVersions = authored.Access.Read
	}
	publish := authored.Access.Publish
	if publish == nil {
		publish = authored.Access.Update
	}
	unpublish := authored.Access.Unpublish
	if unpublish == nil {
		unpublish = authored.Access.Update
	}
	return operationengine.Collection{
		Key:    "global:" + string(resolved.Slug),
		Schema: resolved,
		Access: map[operationengine.Kind]operationengine.Access{
			operationengine.Read:         adaptAccess(authored.Access.Read, local),
			operationengine.ReadVersions: adaptAccess(readVersions, local),
			operationengine.Update:       adaptAccess(authored.Access.Update, local),
			operationengine.Publish:      adaptAccess(publish, local),
			operationengine.Unpublish:    adaptAccess(unpublish, local),
		},
		Hooks: operationengine.Hooks{
			BeforeDuplicate: adaptHooks(authored.Hooks.BeforeDuplicate, local),
			BeforeValidate:  adaptHooks(authored.Hooks.BeforeValidate, local),
			BeforeChange:    adaptHooks(authored.Hooks.BeforeChange, local),
			BeforeOperation: adaptHooks(authored.Hooks.BeforeOperation, local),
			BeforeRead:      adaptHooks(authored.Hooks.BeforeRead, local),
			BeforeDelete:    adaptHooks(authored.Hooks.BeforeDelete, local),
			AfterChange:     adaptHooks(authored.Hooks.AfterChange, local),
			AfterRead:       adaptHooks(authored.Hooks.AfterRead, local),
			AfterDelete:     adaptHooks(authored.Hooks.AfterDelete, local),
			AfterOperation:  adaptHooks(authored.Hooks.AfterOperation, local),
			AfterError:      adaptHooks(authored.Hooks.AfterError, local),
			AfterCommit:     adaptHooks(authored.Hooks.AfterCommit, local),
		},
		Fields:     adaptFieldAccess(authored.FieldAccess, local),
		FieldHooks: adaptFieldHooks(authored.FieldHooks, local),
		Computed:   adaptComputed(authored.Computed, local),
	}
}

func adaptComputed(resolvers map[string]Computed, local **LocalAPI) map[string]operationengine.Computed {
	result := make(map[string]operationengine.Computed, len(resolvers))
	for path, resolver := range resolvers {
		current := resolver
		result[path] = func(ctx operationengine.Context, document store.Document) (store.Value, error) {
			collectionID, globalID := resourceIDs(ctx.Collection)
			return current(ComputedContext{Context: ctx.Context, Operation: Operation(ctx.Operation), CollectionID: collectionID, GlobalID: globalID, Actor: cloneDocument(ctx.Actor), ActorCollection: ctx.ActorCollection, Document: store.CloneDocument(document), Local: *local, Locale: ctx.Locale, AllLocales: ctx.AllLocales})
		}
	}
	return result
}

func adaptAccess(rule AccessRule, local **LocalAPI) operationengine.Access {
	if rule == nil {
		return nil
	}
	return func(ctx operationengine.Context) (operationengine.Decision, error) {
		collectionID, globalID := resourceIDs(ctx.Collection)
		decision, err := rule(AccessContext{
			Context: ctx.Context, Operation: Operation(ctx.Operation), CollectionID: collectionID, GlobalID: globalID,
			ID: ctx.ID, Actor: cloneDocument(ctx.Actor), ActorCollection: ctx.ActorCollection, Data: store.CloneValues(ctx.Data), Local: *local, Locale: ctx.Locale, AllLocales: ctx.AllLocales,
		})
		if err != nil {
			return operationengine.Decision{}, err
		}
		adapted := operationengine.Decision{Kind: operationengine.DecisionKind(decision.Kind())}
		if filter, exists := decision.Filter(); exists {
			adapted.Access = &filter
		}
		return adapted, nil
	}
}

func adaptFieldAccess(rules map[string]FieldAccess, local **LocalAPI) map[string]operationengine.FieldRules {
	adapted := make(map[string]operationengine.FieldRules, len(rules))
	for path, fieldRules := range rules {
		adapted[path] = operationengine.FieldRules{
			Create: adaptFieldRule(fieldRules.Create, local),
			Read:   adaptFieldRule(fieldRules.Read, local),
			Update: adaptFieldRule(fieldRules.Update, local),
		}
	}
	return adapted
}

func adaptFieldRule(rule FieldAccessRule, local **LocalAPI) operationengine.FieldAccess {
	if rule == nil {
		return nil
	}
	return func(ctx operationengine.Context) (bool, error) {
		collectionID, globalID := resourceIDs(ctx.Collection)
		return rule(FieldAccessContext{
			Context: ctx.Context, Operation: Operation(ctx.Operation), CollectionID: collectionID, GlobalID: globalID,
			ID: ctx.ID, Path: ctx.FieldPath, RuntimePath: ctx.RuntimePath,
			Actor: cloneDocument(ctx.Actor), ActorCollection: ctx.ActorCollection, Data: store.CloneValues(ctx.Data),
			Value: ctx.Value, SiblingData: store.CloneValues(ctx.SiblingData),
			Document: cloneDocument(ctx.Document), Original: cloneDocument(ctx.Original),
			Local: *local, Locale: ctx.Locale, AllLocales: ctx.AllLocales,
		})
	}
}

func adaptFieldHooks(hooks map[string]CollectionHooks, local **LocalAPI) map[string]operationengine.Hooks {
	result := make(map[string]operationengine.Hooks, len(hooks))
	for path, fieldHooks := range hooks {
		result[path] = operationengine.Hooks{
			BeforeDuplicate: adaptHooks(fieldHooks.BeforeDuplicate, local),
			BeforeValidate:  adaptHooks(fieldHooks.BeforeValidate, local),
			BeforeChange:    adaptHooks(fieldHooks.BeforeChange, local),
			BeforeOperation: adaptHooks(fieldHooks.BeforeOperation, local),
			BeforeRead:      adaptHooks(fieldHooks.BeforeRead, local),
			BeforeDelete:    adaptHooks(fieldHooks.BeforeDelete, local),
			AfterChange:     adaptHooks(fieldHooks.AfterChange, local),
			AfterRead:       adaptHooks(fieldHooks.AfterRead, local),
			AfterDelete:     adaptHooks(fieldHooks.AfterDelete, local),
			AfterOperation:  adaptHooks(fieldHooks.AfterOperation, local),
			AfterError:      adaptHooks(fieldHooks.AfterError, local),
			AfterCommit:     adaptHooks(fieldHooks.AfterCommit, local),
		}
	}
	return result
}

func adaptHooks(hooks []Hook, local **LocalAPI) []operationengine.Hook {
	adapted := make([]operationengine.Hook, len(hooks))
	for index, hook := range hooks {
		current := hook
		adapted[index] = func(ctx operationengine.Context) error {
			collectionID, globalID := resourceIDs(ctx.Collection)
			return current(HookContext{
				Context: ctx.Context, Operation: Operation(ctx.Operation), CollectionID: collectionID, GlobalID: globalID,
				Actor: cloneDocument(ctx.Actor), ActorCollection: ctx.ActorCollection, Data: ctx.Data, Document: ctx.Document,
				Original: cloneDocument(ctx.Original), Local: *local, FieldPath: ctx.FieldPath,
				Error: ctx.Error, Locale: ctx.Locale, AllLocales: ctx.AllLocales,
			})
		}
	}
	return adapted
}

func resourceIDs(resource schema.Collection) (schema.StableID, schema.StableID) {
	if resource.Capabilities.Global {
		return "", resource.ID
	}
	return resource.ID, ""
}

func adaptAfterCommitDispatcher(dispatcher AfterCommitDispatcher) func(operationengine.Context, operationengine.Hook) error {
	if dispatcher == nil {
		return nil
	}
	return func(ctx operationengine.Context, hook operationengine.Hook) error {
		collectionID, globalID := resourceIDs(ctx.Collection)
		documentID := ""
		if ctx.Document != nil {
			documentID = ctx.Document.ID
		}
		return dispatcher.Dispatch(ctx.Context, AfterCommitEffect{
			Operation: Operation(ctx.Operation), CollectionID: collectionID, GlobalID: globalID, DocumentID: documentID,
			Run: func(effectContext context.Context) error { ctx.Context = effectContext; return hook(ctx) },
		})
	}
}

func adaptPluginValidators(plugins []Plugin) map[string]operationengine.PluginValidator {
	validators := make(map[string]operationengine.PluginValidator)
	for _, plugin := range plugins {
		provider, ok := plugin.(FieldValidatorProvider)
		if !ok {
			continue
		}
		for key, validator := range provider.FieldValidators() {
			current := validator
			validators[key] = func(field schema.Field, value store.Value, runtimePath string) []schema.Issue {
				return current(PluginFieldValidationContext{Field: field, RuntimePath: runtimePath, Value: value})
			}
		}
	}
	return validators
}

func cloneDocument(document *store.Document) *store.Document {
	if document == nil {
		return nil
	}
	cloned := store.CloneDocument(*document)
	return &cloned
}
