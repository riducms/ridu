package core

import (
	"context"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/operation"
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
		Access: map[operation.Kind]operationengine.Access{
			operation.Admin:           adaptAccess(authored.Access.Admin, local),
			operation.Create:          adaptAccess(authored.Access.Create, local),
			operation.Duplicate:       adaptAccess(authored.Access.Create, local),
			operation.Read:            adaptAccess(authored.Access.Read, local),
			operation.ReadVersions:    adaptAccess(readVersions, local),
			operation.Update:          adaptAccess(authored.Access.Update, local),
			operation.Publish:         adaptAccess(publish, local),
			operation.Unpublish:       adaptAccess(unpublish, local),
			operation.Delete:          adaptAccess(authored.Access.Delete, local),
			operation.RestoreDeleted:  adaptAccess(authored.Access.Delete, local),
			operation.DeletePermanent: adaptAccess(authored.Access.Delete, local),
			operation.Unlock:          adaptAccess(unlock, local),
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
		Access: map[operation.Kind]operationengine.Access{
			operation.Read:         adaptAccess(authored.Access.Read, local),
			operation.ReadVersions: adaptAccess(readVersions, local),
			operation.Update:       adaptAccess(authored.Access.Update, local),
			operation.Publish:      adaptAccess(publish, local),
			operation.Unpublish:    adaptAccess(unpublish, local),
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
	}
}

func adaptAccess(rule AccessRule, local **LocalAPI) operationengine.Access {
	if rule == nil {
		return nil
	}
	return func(ctx operationengine.Context) (operationengine.Decision, error) {
		collectionID, globalID := resourceIDs(ctx.Collection)
		decision, err := rule(AccessContext{
			Context: ctx.Context, Operation: ctx.Operation, CollectionID: collectionID, GlobalID: globalID,
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

func adaptHooks(hooks []Hook, local **LocalAPI) []operationengine.Hook {
	adapted := make([]operationengine.Hook, len(hooks))
	for index, hook := range hooks {
		current := hook
		adapted[index] = func(ctx operationengine.Context) error {
			collectionID, globalID := resourceIDs(ctx.Collection)
			return current(HookContext{
				Context: ctx.Context, Operation: ctx.Operation, CollectionID: collectionID, GlobalID: globalID,
				Actor: cloneDocument(ctx.Actor), ActorCollection: ctx.ActorCollection, Data: ctx.Data, Document: ctx.Document,
				Original: cloneDocument(ctx.Original), Local: *local,
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
			Operation: ctx.Operation, CollectionID: collectionID, GlobalID: globalID, DocumentID: documentID,
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
