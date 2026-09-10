package core

import (
	"context"
	"fmt"
	"math"

	"github.com/riducms/ridu/field"
	configresolver "github.com/riducms/ridu/internal/config"
	"github.com/riducms/ridu/internal/embedded"
	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// lowerFieldGraph runs exactly once during application construction. Its output
// owns callback closures and resolved schema metadata, never authoring nodes or
// a request-time graph lookup. The engine owns concrete value traversal.
func lowerFieldGraph(graph configresolver.Graph, resourceKind, resource string, fields []schema.Field, local **LocalAPI) ([]operationengine.FieldBinding, error) {
	byID := make(map[schema.StableID]schema.Field)
	var index func([]schema.Field)
	index = func(fields []schema.Field) {
		for _, f := range fields {
			byID[f.ID] = f
			if f.Nested != nil {
				index(f.Nested.ResolvedFields())
			}
			if f.Blocks != nil {
				for _, block := range f.Blocks.ResolvedTypes() {
					index(block.ResolvedFields())
				}
			}
			embedded.SchemaFields(f, index)
		}
	}
	index(fields)
	var bindings []operationengine.FieldBinding
	for _, occurrence := range graph.Occurrences() {
		if occurrence.ResourceKind != resourceKind || occurrence.Resource != resource {
			continue
		}
		definition, found := graph.Binding(occurrence.ID)
		if !found || !definition.HasBehavior() {
			continue
		}
		unsupported := func(message string) error {
			return schema.NewValidationError([]schema.Issue{{Code: "unsupported_field_policy", Path: occurrence.AuthoredPath, Message: fmt.Sprintf("field %q: %s", occurrence.ResolvedPath, message)}})
		}
		resolved, found := byID[occurrence.SchemaID]
		if !found {
			return nil, unsupported("executable policy has no resolved stored field")
		}
		binding := operationengine.FieldBinding{ID: occurrence.ID, Field: resolved, LocaleOwned: occurrence.LocaleOwner != ""}
		binding.Access = lowerGraphAccess(definition.AccessPolicy(), occurrence.ID, local)
		switch definition.Kind() {
		case field.KindText:
			facade, _ := field.AsText(definition)
			lowerTypedPolicies(&binding, facade.HookPolicy(), facade.ReadHookPolicy(), facade.Validators(), facade.LiveValidators(), stringCodec(), stringCodec(), local)
			lowerDefault(&binding, facade.DefaultCallback(), stringCodec(), local)
		case field.KindNumber:
			facade, _ := field.AsNumber(definition)
			lowerTypedPolicies(&binding, facade.HookPolicy(), facade.ReadHookPolicy(), facade.Validators(), facade.LiveValidators(), numberCodec(), numberCodec(), local)
			lowerDefault(&binding, facade.DefaultCallback(), numberCodec(), local)
		case field.KindTextList:
			facade, _ := field.AsTextList(definition)
			codec := listCodec(stringCodec())
			lowerTypedPolicies(&binding, facade.HookPolicy(), facade.ReadHookPolicy(), facade.Validators(), facade.LiveValidators(), codec, codec, local)
			lowerDefault(&binding, facade.DefaultCallback(), codec, local)
		case field.KindNumberList:
			facade, _ := field.AsNumberList(definition)
			codec := listCodec(numberCodec())
			lowerTypedPolicies(&binding, facade.HookPolicy(), facade.ReadHookPolicy(), facade.Validators(), facade.LiveValidators(), codec, codec, local)
			lowerDefault(&binding, facade.DefaultCallback(), codec, local)
		case field.KindCode:
			facade, _ := field.AsCode(definition)
			lowerTypedPolicies(&binding, facade.HookPolicy(), facade.ReadHookPolicy(), facade.Validators(), facade.LiveValidators(), stringCodec(), stringCodec(), local)
			lowerDefault(&binding, facade.DefaultCallback(), stringCodec(), local)
		case field.KindTextarea:
			facade, _ := field.AsTextarea(definition)
			lowerTypedPolicies(&binding, facade.HookPolicy(), facade.ReadHookPolicy(), facade.Validators(), facade.LiveValidators(), stringCodec(), stringCodec(), local)
			lowerDefault(&binding, facade.DefaultCallback(), stringCodec(), local)
		case field.KindEmail:
			facade, _ := field.AsEmail(definition)
			lowerTypedPolicies(&binding, facade.HookPolicy(), facade.ReadHookPolicy(), facade.Validators(), facade.LiveValidators(), stringCodec(), stringCodec(), local)
			lowerDefault(&binding, facade.DefaultCallback(), stringCodec(), local)
		case field.KindDate:
			facade, _ := field.AsDate(definition)
			lowerTypedPolicies(&binding, facade.HookPolicy(), facade.ReadHookPolicy(), facade.Validators(), facade.LiveValidators(), stringCodec(), stringCodec(), local)
			lowerDefault(&binding, facade.DefaultCallback(), stringCodec(), local)
		case field.KindRadio:
			facade, _ := field.AsRadio(definition)
			lowerTypedPolicies(&binding, facade.HookPolicy(), facade.ReadHookPolicy(), facade.Validators(), facade.LiveValidators(), stringCodec(), stringCodec(), local)
			lowerDefault(&binding, facade.DefaultCallback(), stringCodec(), local)
		case field.KindCheckbox:
			facade, _ := field.AsCheckbox(definition)
			codec := graphCodec[bool]{decode: store.Value.BooleanValue, encode: store.Boolean, code: "invalid_type", expected: "a boolean"}
			lowerTypedPolicies(&binding, facade.HookPolicy(), facade.ReadHookPolicy(), facade.Validators(), facade.LiveValidators(), codec, codec, local)
			lowerDefault(&binding, facade.DefaultCallback(), codec, local)
		case field.KindSelect:
			if facade, err := field.AsMultiSelect(definition); err == nil {
				codec := listCodec(stringCodec())
				lowerTypedPolicies(&binding, facade.HookPolicy(), facade.ReadHookPolicy(), facade.Validators(), facade.LiveValidators(), codec, codec, local)
				lowerDefault(&binding, facade.DefaultCallback(), codec, local)
			} else {
				facade, err := field.AsSelect(definition)
				if err != nil {
					return nil, unsupported(err.Error())
				}
				lowerTypedPolicies(&binding, facade.HookPolicy(), facade.ReadHookPolicy(), facade.Validators(), facade.LiveValidators(), stringCodec(), stringCodec(), local)
				lowerDefault(&binding, facade.DefaultCallback(), stringCodec(), local)
			}
		case field.KindJSON:
			facade, _ := field.AsJSON(definition)
			codec := finiteCodec("", "a finite JSON value")
			lowerTypedPolicies(&binding, facade.HookPolicy(), facade.ReadHookPolicy(), facade.Validators(), facade.LiveValidators(), codec, codec, local)
		case field.KindPoint:
			facade, _ := field.AsPoint(definition)
			codec := finiteCodec(store.ValueList, "a point coordinate pair")
			lowerTypedPolicies(&binding, facade.HookPolicy(), facade.ReadHookPolicy(), facade.Validators(), facade.LiveValidators(), codec, codec, local)
		case field.KindRelationship:
			if facade, err := field.AsPolymorphicRelationships(definition); err == nil {
				codec := finiteCodec(store.ValueList, "a relationship list")
				lowerTypedPolicies(&binding, facade.HookPolicy(), facade.ReadHookPolicy(), facade.Validators(), facade.LiveValidators(), codec, codec, local)
			} else if facade, err := field.AsPolymorphicRelationship(definition); err == nil {
				codec := finiteCodec(store.ValueObject, "a relationship envelope")
				lowerTypedPolicies(&binding, facade.HookPolicy(), facade.ReadHookPolicy(), facade.Validators(), facade.LiveValidators(), codec, codec, local)
			} else if facade, err := field.AsRelationships(definition); err == nil {
				lowerTypedPolicies(&binding, facade.HookPolicy(), facade.ReadHookPolicy(), facade.Validators(), facade.LiveValidators(), listCodec(relationshipCodec()), listCodec(referenceOutputCodec()), local)
			} else {
				facade, err := field.AsRelationship(definition)
				if err != nil {
					return nil, unsupported(err.Error())
				}
				lowerTypedPolicies(&binding, facade.HookPolicy(), facade.ReadHookPolicy(), facade.Validators(), facade.LiveValidators(), relationshipCodec(), referenceOutputCodec(), local)
			}
		case field.KindUpload:
			if facade, err := field.AsUploads(definition); err == nil {
				lowerTypedPolicies(&binding, facade.HookPolicy(), facade.ReadHookPolicy(), facade.Validators(), facade.LiveValidators(), listCodec(relationshipCodec()), listCodec(referenceOutputCodec()), local)
			} else {
				facade, err := field.AsUpload(definition)
				if err != nil {
					return nil, unsupported(err.Error())
				}
				lowerTypedPolicies(&binding, facade.HookPolicy(), facade.ReadHookPolicy(), facade.Validators(), facade.LiveValidators(), relationshipCodec(), referenceOutputCodec(), local)
			}
		case field.KindGroup:
			facade, _ := field.AsGroup(definition)
			codec := finiteCodec(store.ValueObject, "an object")
			lowerTypedPolicies(&binding, facade.HookPolicy(), facade.ReadHookPolicy(), facade.Validators(), facade.LiveValidators(), codec, codec, local)
		case field.KindArray:
			facade, _ := field.AsArray(definition)
			codec := finiteCodec(store.ValueList, "an array")
			lowerTypedPolicies(&binding, facade.HookPolicy(), facade.ReadHookPolicy(), facade.Validators(), facade.LiveValidators(), codec, codec, local)
		case field.KindBlocks:
			facade, _ := field.AsBlocks(definition)
			codec := finiteCodec(store.ValueList, "an array")
			lowerTypedPolicies(&binding, facade.HookPolicy(), facade.ReadHookPolicy(), facade.Validators(), facade.LiveValidators(), codec, codec, local)
		case field.KindPlugin:
			facade, _ := field.AsPlugin(definition)
			codec := finiteCodec("", "a finite plugin value")
			lowerTypedPolicies(&binding, facade.HookPolicy(), facade.ReadHookPolicy(), facade.Validators(), facade.LiveValidators(), codec, codec, local)
		case field.KindJoin:
			facade, _ := field.AsJoin(definition)
			codec := graphCodec[store.Value]{decode: func(value store.Value) (store.Value, bool) { return value, value.Kind() == store.ValueList }, encode: func(value store.Value) store.Value { return value }, code: "invalid_type", expected: "a joined document list"}
			lowerTypedPolicies(&binding, field.Hooks[store.Value]{}, facade.ReadHookPolicy(), nil, nil, codec, codec, local)
		case field.KindVirtual:
			facade, _ := field.AsOutput(definition)
			resolver := facade.Resolver()
			if resolver == nil {
				return nil, unsupported("computed output requires an attached resolver")
			}
			binding.Computed = func(ctx operationengine.Context, document store.Document) (store.Value, error) {
				ctx.ID, ctx.Document = document.ID, &document
				value, err := resolver(operation.ReadContext(graphCallbackContext(ctx, occurrence.ID, local)))
				if result, present := value.Get(); present {
					return result, err
				}
				return store.Null(), err
			}
			codec := finiteCodec("", "a finite output value")
			lowerTypedPolicies(&binding, field.Hooks[store.Value]{}, facade.ReadHookPolicy(), nil, nil, codec, codec, local)
		default:
			return nil, unsupported("this field kind does not support attached callbacks")
		}
		bindings = append(bindings, binding)
	}
	return bindings, nil
}

type graphCodec[T any] struct {
	decode      func(store.Value) (T, bool)
	eventDecode func(store.Value) (T, bool)
	encode      func(T) store.Value
	code        string
	expected    string
}

func (codec graphCodec[T]) value(ctx operationengine.Context, f schema.Field) (operation.Value[T], error) {
	if !ctx.ValuePresent || ctx.Value.Kind() == store.ValueNull {
		return operation.Empty[T](), nil
	}
	value, valid := codec.decode(ctx.Value)
	if !valid {
		return operation.Empty[T](), schema.NewValidationError([]schema.Issue{{Code: codec.code, Path: ctx.RuntimePath, Message: fmt.Sprintf("%s must be %s", f.Admin.Label, codec.expected)}})
	}
	return operation.Present(value), nil
}

func stringCodec() graphCodec[string] {
	return graphCodec[string]{decode: store.Value.StringValue, encode: store.String, code: "invalid_type", expected: "a string"}
}

func numberCodec() graphCodec[float64] {
	return graphCodec[float64]{decode: func(value store.Value) (float64, bool) {
		number, valid := value.NumberValue()
		return number, valid && !math.IsNaN(number) && !math.IsInf(number, 0)
	}, encode: store.Number, code: "invalid_number", expected: "a finite number"}
}

func relationshipCodec() graphCodec[operation.ID] {
	return graphCodec[operation.ID]{decode: func(value store.Value) (operation.ID, bool) {
		id, valid := value.StringValue()
		return operation.ID(id), valid
	}, eventDecode: func(value store.Value) (operation.ID, bool) {
		if document, ok := value.CopyDocument(); ok {
			return operation.ID(document.ID), true
		}
		id, ok := value.StringValue()
		return operation.ID(id), ok
	}, encode: func(value operation.ID) store.Value { return store.String(string(value)) }, code: "invalid_type", expected: "a relationship ID"}
}

func referenceOutputCodec() graphCodec[operation.ReferenceOutput] {
	return graphCodec[operation.ReferenceOutput]{decode: func(value store.Value) (operation.ReferenceOutput, bool) {
		if document, valid := value.CopyDocument(); valid {
			return operation.Populated(document), true
		}
		id, valid := value.StringValue()
		return operation.Unpopulated(operation.ID(id)), valid
	}, encode: func(value operation.ReferenceOutput) store.Value {
		if document, present := value.Document(); present {
			return store.Populated(document)
		}
		return store.String(string(value.ID()))
	}, code: "invalid_type", expected: "a relationship ID or populated document"}
}

func finiteCodec(kind store.ValueKind, expected string) graphCodec[store.Value] {
	return graphCodec[store.Value]{decode: func(value store.Value) (store.Value, bool) {
		if kind != "" {
			return value, value.Kind() == kind
		}
		return value, value.Kind() != "" && value.Kind() != store.ValueDocument
	}, encode: func(value store.Value) store.Value { return value }, code: "invalid_type", expected: expected}
}

func lowerTypedPolicies[W, R any](binding *operationengine.FieldBinding, hooks field.Hooks[W], reads field.ReadHooks[R], validators []field.Validator[W], liveValidators []field.LiveValidator[W], write graphCodec[W], read graphCodec[R], local **LocalAPI) {
	f, id := binding.Field, binding.ID
	adaptContext := func(ctx operationengine.Context) operation.Context { return graphCallbackContext(ctx, id, local) }
	raw := func(callbacks []field.RawTransform) []operationengine.Hook {
		result := make([]operationengine.Hook, len(callbacks))
		for i, callback := range callbacks {
			result[i] = func(ctx operationengine.Context) error {
				value := operation.Empty[store.Value]()
				if ctx.ValuePresent {
					value = operation.Present(ctx.Value)
				}
				change, err := callback(operation.WriteContext(adaptContext(ctx)), value)
				if err == nil {
					applyGraphChange(ctx, f.Name, change, func(value store.Value) store.Value { return value })
				}
				return err
			}
		}
		return result
	}
	transform := func(callbacks []field.Transform[W]) []operationengine.Hook {
		result := make([]operationengine.Hook, len(callbacks))
		for i, callback := range callbacks {
			result[i] = func(ctx operationengine.Context) error {
				value, err := write.value(ctx, f)
				if err != nil {
					return err
				}
				change, err := callback(operation.WriteContext(adaptContext(ctx)), value)
				if err == nil {
					applyGraphChange(ctx, f.Name, change, write.encode)
				}
				return err
			}
		}
		return result
	}
	observe := func(callbacks []field.Observer[W]) []operationengine.Hook {
		result := make([]operationengine.Hook, len(callbacks))
		for i, callback := range callbacks {
			result[i] = func(ctx operationengine.Context) error {
				event := write
				if event.eventDecode != nil {
					event.decode = event.eventDecode
				}
				value, err := event.value(ctx, f)
				if err != nil {
					return err
				}
				return callback(operation.EventContext(adaptContext(ctx)), value)
			}
		}
		return result
	}
	binding.Hooks = operationengine.Hooks{
		BeforeDuplicate: raw(hooks.BeforeDuplicate), BeforeValidate: raw(hooks.BeforeValidate),
		BeforeChange: transform(hooks.BeforeChange), BeforeOperation: transform(hooks.BeforeOperation),
		BeforeDelete: observe(hooks.BeforeDelete), AfterChange: observe(hooks.AfterChange),
		AfterDelete: observe(hooks.AfterDelete), AfterOperation: observe(hooks.AfterOperation), AfterCommit: observe(hooks.AfterCommit),
	}
	for _, callback := range reads.AfterRead {
		binding.Hooks.AfterRead = append(binding.Hooks.AfterRead, func(ctx operationengine.Context) error {
			value, err := read.value(ctx, f)
			if err != nil {
				return err
			}
			change, err := callback(operation.ReadContext(adaptContext(ctx)), value)
			if err == nil {
				applyGraphChange(ctx, f.Name, change, read.encode)
			}
			return err
		})
	}
	for _, callback := range validators {
		binding.Validators = append(binding.Validators, func(ctx operationengine.Context) ([]schema.Issue, error) {
			value, err := write.value(ctx, f)
			if err != nil {
				return nil, err
			}
			issues, err := callback(operation.ValidationContext(adaptContext(ctx)), value)
			if err != nil {
				return nil, err
			}
			result := make([]schema.Issue, len(issues))
			for i, issue := range issues {
				targetField, path, locale, err := operationengine.ResolveIssueTarget(f, ctx, issue.Target)
				if err != nil {
					return nil, err
				}
				collectionID, globalID := resourceIDs(ctx.Collection)
				result[i] = schema.Issue{Code: issue.Code, Path: path, Message: issue.Message, FieldID: targetField.ID, CollectionID: collectionID, GlobalID: globalID, Locale: locale}
			}
			return result, nil
		})
	}
	for _, callback := range liveValidators {
		binding.LiveValidators = append(binding.LiveValidators, func(ctx operationengine.Context) ([]schema.Issue, bool, error) {
			value, err := write.value(ctx, f)
			if err != nil {
				return nil, false, nil
			}
			base := adaptContext(ctx)
			root := base.Root
			if ctx.RootData != nil {
				root = operation.Snapshot(ctx.RootData)
			}
			live := operation.LiveValidationContext{
				Context: base.Context, Operation: base.Operation, CollectionID: base.CollectionID, GlobalID: base.GlobalID,
				ID: base.ID, Actor: base.Actor, Locale: base.Locale, Root: root, Siblings: base.Siblings, Prior: base.Prior,
				Input: operation.Snapshot(ctx.InputSiblingData),
				Local: graphReader{local: local, context: ctx.Context, actor: cloneDocument(ctx.Actor), actorCollection: ctx.ActorCollection, locale: ctx.Locale, live: true},
			}
			issues, err := callback(live, value)
			if err != nil {
				return nil, true, err
			}
			if len(issues) > operationengine.MaxValidationIssues {
				return nil, true, fmt.Errorf("live validation returned too many issues")
			}
			result := make([]schema.Issue, len(issues))
			for i, issue := range issues {
				targetField, path, locale, err := operationengine.ResolveIssueTarget(f, ctx, issue.Target)
				if err != nil {
					return nil, true, err
				}
				result[i] = schema.Issue{Code: issue.Code, Path: path, Message: issue.Message, FieldID: targetField.ID, CollectionID: base.CollectionID, GlobalID: base.GlobalID, Locale: locale}
			}
			return result, true, nil
		})
	}

}

// lowerDefault carries the typed executable policy into private initialization.
// Encoding preserves meaningful zero values; ordinary validation checks constraints.
func lowerDefault[T any](binding *operationengine.FieldBinding, callback field.DefaultFunc[T], codec graphCodec[T], local **LocalAPI) {
	if callback == nil {
		return
	}
	id := binding.ID
	binding.Default = func(ctx operationengine.Context) (store.Value, bool, error) {
		result, err := callback(operation.DefaultContext(graphCallbackContext(ctx, id, local)))
		if err != nil {
			return store.Value{}, false, err
		}
		value, present := result.Get()
		if !present {
			return store.Value{}, false, nil
		}
		return codec.encode(value), true, nil
	}
}

func applyGraphChange[T any](ctx operationengine.Context, name string, change operation.Change[T], encode func(T) store.Value) {
	replacement, replace := change.Replacement()
	if !replace {
		return
	}
	if value, present := replacement.Get(); present {
		ctx.SiblingData[name] = encode(value)
	} else {
		// Clearing the own logical value uses the existing portable empty state;
		// it does not introduce public Unset or a durable presence distinction.
		ctx.SiblingData[name] = store.Null()
	}
}

func lowerGraphAccess(access field.Access, id string, local **LocalAPI) operationengine.FieldRules {
	adapt := func(rule field.AccessRule) operationengine.FieldAccess {
		if rule == nil {
			return nil
		}
		return func(ctx operationengine.Context) (bool, error) {
			return rule(operation.AccessContext(graphCallbackContext(ctx, id, local)))
		}
	}
	return operationengine.FieldRules{Create: adapt(access.Create), Read: adapt(access.Read), Update: adapt(access.Update)}
}

func graphCallbackContext(ctx operationengine.Context, id string, local **LocalAPI) operation.Context {
	collectionID, globalID := resourceIDs(ctx.Collection)
	root := ctx.Data
	if ctx.RootData != nil {
		root = ctx.RootData
	}
	if root == nil && ctx.Document != nil {
		root = ctx.Document.Values
	}
	actor := operation.Actor{Collection: ctx.ActorCollection}
	if ctx.Actor != nil {
		actor.ID = operation.ID(ctx.Actor.ID)
		actor.Data = operation.Snapshot(ctx.Actor.Values)
	}
	return operation.Context{
		Context: ctx.Context, Operation: ctx.Operation, CollectionID: collectionID, GlobalID: globalID,
		OccurrenceID: operation.OccurrenceID(ctx.OccurrenceID), SchemaOccurrenceID: operation.OccurrenceID(id), ID: operation.ID(ctx.ID),
		Actor: actor, Locale: ctx.Locale, AllLocales: ctx.AllLocales, Root: operation.Snapshot(root), Siblings: operation.Snapshot(ctx.SiblingData), Prior: operation.Snapshot(ctx.OriginalSiblingData),
		Local: graphReader{live: ctx.LiveValidation, local: local, context: ctx.Context, actor: cloneDocument(ctx.Actor), actorCollection: ctx.ActorCollection, locale: ctx.Locale},
	}
}

// graphReader carries the original transaction context even if a callback
// supplies context.Background. Its only capability is an ordinary authorized
// exact-locale read; neither actor nor privilege overrides are author inputs.
type graphReader struct {
	live            bool
	local           **LocalAPI
	context         context.Context
	actor           *store.Document
	actorCollection schema.CollectionSlug
	locale          schema.LocaleCode
}

func (reader graphReader) FindByID(caller context.Context, collection schema.CollectionSlug, id operation.ID) (store.Document, error) {
	ctx, cancel := context.WithCancel(reader.context)
	defer cancel()
	if caller != nil {
		if err := caller.Err(); err != nil {
			return store.Document{}, err
		}
		stop := context.AfterFunc(caller, cancel)
		defer stop()
	}
	if reader.live {
		return (*reader.local).engine.LiveRead(ctx, operationengine.CapabilitiesRequest{Collection: string(collection), ID: string(id), Actor: cloneDocument(reader.actor), ActorCollection: reader.actorCollection, Locale: string(reader.locale), DisableFallback: true})
	}
	return (*reader.local).FindWithOptions(ctx, string(collection), string(id), FindOptions{
		Actor: cloneDocument(reader.actor), ActorCollection: reader.actorCollection, Locale: reader.locale, DisableFallback: reader.locale != "",
	})
}

func listCodec[T any](item graphCodec[T]) graphCodec[[]T] {
	decode := func(decodeItem func(store.Value) (T, bool)) func(store.Value) ([]T, bool) {
		return func(value store.Value) ([]T, bool) {
			if value.Kind() != store.ValueList {
				return nil, false
			}
			result := make([]T, value.Len())
			i := 0
			for value := range value.Elements() {
				decoded, ok := decodeItem(value)
				if !ok {
					return nil, false
				}
				result[i] = decoded
				i++
			}
			return result, true
		}
	}
	result := graphCodec[[]T]{decode: decode(item.decode), encode: func(values []T) store.Value {
		result := make([]store.Value, len(values))
		for i, value := range values {
			result[i] = item.encode(value)
		}
		return store.List(result...)
	}, code: "invalid_type", expected: "an array of " + item.expected}
	if item.eventDecode != nil {
		result.eventDecode = decode(item.eventDecode)
	}
	return result
}
