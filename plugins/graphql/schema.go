package graphql

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	enginegraphql "github.com/graphql-go/graphql"
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

var graphQLNamePattern = regexp.MustCompile(`^[_A-Za-z][_0-9A-Za-z]*$`)

type resource struct {
	schema.Collection
	global bool
	name   string
	plural string
}

type schemaBuilder struct {
	snapshot   schema.Snapshot
	local      *ridu.LocalAPI
	app        *ridu.App
	options    Options
	json       *enginegraphql.Scalar
	locale     *enginegraphql.Enum
	objects    map[schema.StableID]*enginegraphql.Object
	resources  map[schema.StableID]resource
	unions     map[string]*enginegraphql.Union
	enums      map[string]*enginegraphql.Enum
	where      map[schema.StableID]*enginegraphql.InputObject
	types      map[string]struct{}
	permission *enginegraphql.Object
	adminSlug  schema.CollectionSlug
}

func newSchemaBuilder(snapshot schema.Snapshot, local *ridu.LocalAPI, app *ridu.App, options Options) *schemaBuilder {
	return &schemaBuilder{
		snapshot: snapshot, local: local, app: app, options: options, json: jsonScalar(),
		objects: make(map[schema.StableID]*enginegraphql.Object), resources: make(map[schema.StableID]resource), unions: make(map[string]*enginegraphql.Union), enums: make(map[string]*enginegraphql.Enum), where: make(map[schema.StableID]*enginegraphql.InputObject), types: make(map[string]struct{}),
	}
}

func (builder *schemaBuilder) build() (enginegraphql.Schema, error) {
	resources := make([]resource, 0, len(builder.snapshot.Collections)+len(builder.snapshot.Globals))
	for _, collection := range builder.snapshot.Collections {
		name, plural := builder.resourceNames(collection)
		resources = append(resources, resource{Collection: collection, name: name, plural: plural})
	}
	for _, global := range builder.snapshot.Globals {
		name, plural := builder.resourceNames(global)
		resources = append(resources, resource{Collection: global, global: true, name: name, plural: plural})
	}
	for index := range resources {
		if !resources[index].global && resources[index].plural == resources[index].name {
			resources[index].plural = "all" + resources[index].plural
		}
	}
	for _, current := range resources {
		builder.resources[current.ID] = current
		if builder.snapshot.Application.Admin != nil && current.ID == builder.snapshot.Application.Admin.UserCollectionID {
			builder.adminSlug = current.Slug
		}
	}
	for slug := range builder.options.Resources {
		known := false
		for _, current := range resources {
			if string(current.Slug) == slug {
				known = true
				break
			}
		}
		if !known {
			return enginegraphql.Schema{}, fmt.Errorf("GraphQL resource options target unknown slug %q", slug)
		}
	}
	if builder.snapshot.Application.Localization != nil {
		values := enginegraphql.EnumValueConfigMap{}
		for _, locale := range builder.snapshot.Application.Localization.Locales {
			name := enumName(string(locale.Code))
			if _, exists := values[name]; exists {
				return enginegraphql.Schema{}, fmt.Errorf("locales %q produce duplicate GraphQL enum name %q", locale.Code, name)
			}
			values[name] = &enginegraphql.EnumValueConfig{Value: string(locale.Code), Description: locale.Label}
		}
		builder.locale = enginegraphql.NewEnum(enginegraphql.EnumConfig{Name: "RiduLocale", Values: values})
	}
	for index := range resources {
		current := resources[index]
		if !graphQLNamePattern.MatchString(current.plural) || strings.HasPrefix(current.plural, "__") {
			return enginegraphql.Schema{}, fmt.Errorf("resource %q produces invalid GraphQL plural name %q", current.Slug, current.plural)
		}
		if err := validateGraphQLFields(current.Fields, true); err != nil {
			return enginegraphql.Schema{}, fmt.Errorf("resource %q: %w", current.Slug, err)
		}
		if err := builder.reserveType(current.name, "resource "+string(current.Slug)); err != nil {
			return enginegraphql.Schema{}, err
		}
		resourceCopy := current
		builder.objects[current.ID] = enginegraphql.NewObject(enginegraphql.ObjectConfig{
			Name: current.name,
			Fields: enginegraphql.FieldsThunk(func() enginegraphql.Fields {
				return builder.outputFields(resourceCopy, current.name, current.Fields, true)
			}),
		})
	}

	queryFields := enginegraphql.Fields{}
	mutationFields := enginegraphql.Fields{}
	for _, current := range resources {
		generatedQueries := enginegraphql.Fields{}
		generatedMutations := enginegraphql.Fields{}
		if current.global {
			if err := builder.addGlobal(current, generatedQueries, generatedMutations); err != nil {
				return enginegraphql.Schema{}, err
			}
		} else if err := builder.addCollection(current, generatedQueries, generatedMutations); err != nil {
			return enginegraphql.Schema{}, err
		}
		settings := builder.options.Resources[string(current.Slug)]
		if !settings.DisableQueries {
			if err := mergeGeneratedFields(queryFields, generatedQueries); err != nil {
				return enginegraphql.Schema{}, err
			}
		}
		if !settings.DisableMutations {
			if err := mergeGeneratedFields(mutationFields, generatedMutations); err != nil {
				return enginegraphql.Schema{}, err
			}
		}
	}
	if err := builder.addPreferences(queryFields, mutationFields); err != nil {
		return enginegraphql.Schema{}, err
	}
	if err := builder.addExtensions(queryFields, mutationFields); err != nil {
		return enginegraphql.Schema{}, err
	}
	if len(queryFields) == 0 {
		queryFields["ridu"] = &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.String), Resolve: func(enginegraphql.ResolveParams) (interface{}, error) { return "ok", nil }}
	}
	queryRoot := enginegraphql.NewObject(enginegraphql.ObjectConfig{Name: "Query", Fields: queryFields})
	config := enginegraphql.SchemaConfig{Query: queryRoot}
	if len(mutationFields) != 0 {
		config.Mutation = enginegraphql.NewObject(enginegraphql.ObjectConfig{Name: "Mutation", Fields: mutationFields})
	}
	result, err := enginegraphql.NewSchema(config)
	if err != nil {
		return enginegraphql.Schema{}, fmt.Errorf("build GraphQL schema: %w", err)
	}
	return result, nil
}

func (builder *schemaBuilder) resourceNames(collection schema.Collection) (string, string) {
	name := typeName(collection.Labels.Singular)
	plural := typeName(collection.Labels.Plural)
	settings := builder.options.Resources[string(collection.Slug)]
	if settings.SingularName != "" {
		name = settings.SingularName
	}
	if settings.PluralName != "" {
		plural = settings.PluralName
	}
	return name, plural
}

func mergeGeneratedFields(destination, generated enginegraphql.Fields) error {
	for _, name := range sortedFieldNamesFromConfig(generated) {
		if err := addRootField(destination, name, generated[name]); err != nil {
			return err
		}
	}
	return nil
}

func sortedFieldNamesFromConfig(fields enginegraphql.Fields) []string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (builder *schemaBuilder) addPreferences(queries, mutations enginegraphql.Fields) error {
	if builder.adminSlug == "" {
		return nil
	}
	args := enginegraphql.FieldConfigArgument{"key": &enginegraphql.ArgumentConfig{Type: enginegraphql.NewNonNull(enginegraphql.String)}}
	if err := addRootField(queries, "preference", &enginegraphql.Field{Type: builder.json, Args: args, Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
		request := requestFromContext(params)
		if request.actor == nil || request.actorCollection != builder.adminSlug {
			return nil, transportError(&ridu.OperationError{Code: "access_denied", Status: 403, Message: "preferences require an admin identity"})
		}
		value, err := builder.app.Preference(params.Context, &ridu.AuthIdentity{Collection: request.actorCollection, Actor: *request.actor}, fmt.Sprint(params.Args["key"]))
		if err != nil {
			return nil, transportError(err)
		}
		var decoded interface{}
		if err := json.Unmarshal(value, &decoded); err != nil {
			return nil, transportError(err)
		}
		return decoded, nil
	}}); err != nil {
		return err
	}
	if err := addRootField(mutations, "setPreference", &enginegraphql.Field{Type: builder.json, Args: mergeArgs(args, enginegraphql.FieldConfigArgument{"value": &enginegraphql.ArgumentConfig{Type: enginegraphql.NewNonNull(builder.json)}}), Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
		request := requestFromContext(params)
		if request.actor == nil || request.actorCollection != builder.adminSlug {
			return nil, transportError(&ridu.OperationError{Code: "access_denied", Status: 403, Message: "preferences require an admin identity"})
		}
		encoded, err := json.Marshal(params.Args["value"])
		if err != nil {
			return nil, clientError(err)
		}
		value, err := builder.app.SetPreference(params.Context, &ridu.AuthIdentity{Collection: request.actorCollection, Actor: *request.actor}, fmt.Sprint(params.Args["key"]), encoded)
		if err != nil {
			return nil, transportError(err)
		}
		var decoded interface{}
		if err := json.Unmarshal(value, &decoded); err != nil {
			return nil, transportError(err)
		}
		return decoded, nil
	}}); err != nil {
		return err
	}
	for _, mutation := range []struct {
		name string
		args enginegraphql.FieldConfigArgument
		run  func(context.Context, *ridu.AuthIdentity, map[string]interface{}) error
	}{
		{name: "deletePreference", args: args, run: func(ctx context.Context, identity *ridu.AuthIdentity, args map[string]interface{}) error {
			return builder.app.DeletePreference(ctx, identity, fmt.Sprint(args["key"]))
		}},
		{name: "resetPreferences", args: enginegraphql.FieldConfigArgument{}, run: func(ctx context.Context, identity *ridu.AuthIdentity, _ map[string]interface{}) error {
			return builder.app.ResetPreferences(ctx, identity)
		}},
	} {
		mutation := mutation
		if err := addRootField(mutations, mutation.name, &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.Boolean), Args: mutation.args, Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
			request := requestFromContext(params)
			if request.actor == nil || request.actorCollection != builder.adminSlug {
				return nil, transportError(&ridu.OperationError{Code: "access_denied", Status: 403, Message: "preferences require an admin identity"})
			}
			if err := mutation.run(params.Context, &ridu.AuthIdentity{Collection: request.actorCollection, Actor: *request.actor}, params.Args); err != nil {
				return nil, transportError(err)
			}
			return true, nil
		}}); err != nil {
			return err
		}
	}
	return nil
}

func (builder *schemaBuilder) addExtensions(queries, mutations enginegraphql.Fields) error {
	for _, group := range []struct {
		fields []ExtensionField
		root   enginegraphql.Fields
	}{{fields: builder.options.Queries, root: queries}, {fields: builder.options.Mutations, root: mutations}} {
		for index, extension := range group.fields {
			if extension.Type == nil || extension.Resolve == nil {
				return fmt.Errorf("GraphQL extension field %d requires a type and resolver", index)
			}
			extension := extension
			if err := addRootField(group.root, extension.Name, &enginegraphql.Field{
				Type: extension.Type, Args: extension.Args, Description: extension.Description,
				Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
					request := requestFromContext(params)
					result, err := extension.Resolve(ExtensionContext{
						Context: params.Context, Args: params.Args, Info: params.Info,
						Actor: request.actor, ActorCollection: request.actorCollection, Local: builder.local, App: builder.app,
					})
					if err != nil {
						return nil, transportError(err)
					}
					return result, nil
				},
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (builder *schemaBuilder) addDocumentAccess(current resource, queries enginegraphql.Fields) error {
	args := enginegraphql.FieldConfigArgument{}
	collection := string(current.Slug)
	id := ""
	if current.global {
		collection = "global:" + collection
		id = string(current.Slug)
	} else {
		args["id"] = &enginegraphql.ArgumentConfig{Type: enginegraphql.NewNonNull(enginegraphql.ID)}
	}
	return addRootField(queries, "docAccess"+current.name, &enginegraphql.Field{
		Type: builder.documentAccessType(current), Args: args,
		Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
			request := requestFromContext(params)
			documentID := id
			if !current.global {
				documentID = fmt.Sprint(params.Args["id"])
			}
			capabilities, err := builder.local.Capabilities(params.Context, collection, documentID, ridu.CapabilityOptions{Actor: request.actor, ActorCollection: request.actorCollection})
			if err != nil {
				return nil, transportError(err)
			}
			return capabilityMap(current, capabilities), nil
		},
	})
}

func (builder *schemaBuilder) documentAccessType(current resource) *enginegraphql.Object {
	fields := enginegraphql.Fields{
		"read":   &enginegraphql.Field{Type: builder.permissionType()},
		"update": &enginegraphql.Field{Type: builder.permissionType()},
		"fields": &enginegraphql.Field{Type: builder.fieldAccessType(current.name+"DocAccessFields", current.Fields)},
	}
	if !current.global {
		fields["create"] = &enginegraphql.Field{Type: builder.permissionType()}
		fields["delete"] = &enginegraphql.Field{Type: builder.permissionType()}
		fields["duplicate"] = &enginegraphql.Field{Type: builder.permissionType()}
	}
	if current.Versions != nil {
		fields["readVersions"] = &enginegraphql.Field{Type: builder.permissionType()}
		if current.Versions.Drafts {
			fields["publish"] = &enginegraphql.Field{Type: builder.permissionType()}
			fields["unpublish"] = &enginegraphql.Field{Type: builder.permissionType()}
		}
	}
	if current.Capabilities.Trash {
		fields["restoreDeleted"] = &enginegraphql.Field{Type: builder.permissionType()}
		fields["deletePermanent"] = &enginegraphql.Field{Type: builder.permissionType()}
	}
	if current.Auth != nil && current.Auth.MaxLoginAttempts > 0 {
		fields["unlock"] = &enginegraphql.Field{Type: builder.permissionType()}
	}
	return enginegraphql.NewObject(enginegraphql.ObjectConfig{Name: current.name + "DocAccess", Fields: fields})
}

func (builder *schemaBuilder) permissionType() *enginegraphql.Object {
	if builder.permission == nil {
		builder.permission = enginegraphql.NewObject(enginegraphql.ObjectConfig{Name: "RiduPermission", Fields: enginegraphql.Fields{
			"permission": &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.Boolean)},
		}})
	}
	return builder.permission
}

func (builder *schemaBuilder) fieldAccessType(name string, fields []schema.Field) *enginegraphql.Object {
	result := enginegraphql.Fields{}
	for _, field := range fields {
		if field.Type == schema.FieldTypeUI {
			continue
		}
		fieldTypeName := name + typeName(field.Name)
		operations := enginegraphql.Fields{
			"read": &enginegraphql.Field{Type: builder.permissionType()}, "create": &enginegraphql.Field{Type: builder.permissionType()}, "update": &enginegraphql.Field{Type: builder.permissionType()},
		}
		if field.Nested != nil && len(field.Nested.ResolvedFields()) != 0 {
			operations["fields"] = &enginegraphql.Field{Type: builder.fieldAccessType(fieldTypeName+"Fields", field.Nested.ResolvedFields())}
		}
		result[fieldName(field.Name)] = &enginegraphql.Field{Type: enginegraphql.NewObject(enginegraphql.ObjectConfig{Name: fieldTypeName, Fields: operations})}
	}
	return enginegraphql.NewObject(enginegraphql.ObjectConfig{Name: name, Fields: result})
}

func capabilityMap(current resource, capabilities ridu.AccessCapabilities) map[string]interface{} {
	permission := func(value bool) map[string]interface{} { return map[string]interface{}{"permission": value} }
	operations := capabilities.Operations
	result := map[string]interface{}{
		"read": permission(operations.Read), "update": permission(operations.Update),
		"fields": fieldCapabilityMap(current.Fields, capabilities.Fields),
	}
	if !current.global {
		result["create"] = permission(operations.Create)
		result["delete"] = permission(operations.Delete)
		result["duplicate"] = permission(operations.Duplicate)
	}
	if current.Versions != nil {
		result["readVersions"] = permission(operations.ReadVersions)
		if current.Versions.Drafts {
			result["publish"] = permission(operations.Publish)
			result["unpublish"] = permission(operations.Unpublish)
		}
	}
	if current.Capabilities.Trash {
		result["restoreDeleted"] = permission(operations.RestoreDeleted)
		result["deletePermanent"] = permission(operations.DeletePermanent)
	}
	if current.Auth != nil && current.Auth.MaxLoginAttempts > 0 {
		result["unlock"] = permission(operations.Update)
	}
	return result
}

func fieldCapabilityMap(fields []schema.Field, capabilities map[string]ridu.FieldCapabilities) map[string]interface{} {
	result := make(map[string]interface{}, len(fields))
	permission := func(value bool) map[string]interface{} { return map[string]interface{}{"permission": value} }
	for _, field := range fields {
		if field.Type == schema.FieldTypeUI {
			continue
		}
		current := capabilities[field.Path.String()]
		value := map[string]interface{}{
			"read": permission(current.Read), "create": permission(current.Create), "update": permission(current.Update),
		}
		if field.Nested != nil && len(field.Nested.ResolvedFields()) != 0 {
			value["fields"] = fieldCapabilityMap(field.Nested.ResolvedFields(), capabilities)
		}
		result[fieldName(field.Name)] = value
	}
	return result
}

func (builder *schemaBuilder) addCollection(current resource, queries, mutations enginegraphql.Fields) error {
	object := builder.objects[current.ID]
	where := builder.whereInput(current)
	var create *enginegraphql.InputObject
	if current.Upload == nil {
		// GraphQL's JSON transport cannot supply upload bytes. Do not publish a
		// create mutation whose resolver can only reject the collection.
		create = builder.dataInput(current, true)
	}
	update := builder.dataInput(current, false)
	page := builder.pageType(current, object)
	count := enginegraphql.NewObject(enginegraphql.ObjectConfig{Name: current.name + "Count", Fields: enginegraphql.Fields{
		"totalDocs": &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.Int)},
	}})

	if err := addRootField(queries, current.name, &enginegraphql.Field{
		Type: object, Args: mergeArgs(enginegraphql.FieldConfigArgument{"id": &enginegraphql.ArgumentConfig{Type: enginegraphql.NewNonNull(enginegraphql.ID)}}, builder.readLocaleArgs()),
		Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
			request := requestFromContext(params)
			document, err := builder.local.Find(params.Context, string(current.Slug), fmt.Sprint(params.Args["id"]), ridu.FindOptions{
				Actor: request.actor, ActorCollection: request.actorCollection, Populate: builder.populationsFor(current.Fields, params.Info),
				OutputFields: builder.outputFieldsFor(current.Fields, params.Info),
				Draft:        boolPointerArg(params.Args, "draft"),
				TrashOnly:    boolArg(params.Args, "trash"), Locale: localeArg(params.Args), FallbackLocales: fallbackArgs(params.Args), DisableFallback: boolArg(params.Args, "disableFallback"), AllLocales: boolArg(params.Args, "allLocales"),
			})
			if err != nil {
				return nil, transportError(err)
			}
			return documentMapForArgs(document, params.Args), nil
		},
	}); err != nil {
		return err
	}
	if err := builder.addDocumentAccess(current, queries); err != nil {
		return err
	}
	listArgs := mergeArgs(enginegraphql.FieldConfigArgument{
		"where": &enginegraphql.ArgumentConfig{Type: where}, "sort": &enginegraphql.ArgumentConfig{Type: enginegraphql.NewList(enginegraphql.NewNonNull(enginegraphql.String))},
		"page": &enginegraphql.ArgumentConfig{Type: enginegraphql.Int}, "limit": &enginegraphql.ArgumentConfig{Type: enginegraphql.Int},
		"trash": &enginegraphql.ArgumentConfig{Type: enginegraphql.Boolean},
	}, builder.readLocaleArgs())
	if err := addRootField(queries, current.plural, &enginegraphql.Field{Type: enginegraphql.NewNonNull(page), Args: listArgs, Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
		request := requestFromContext(params)
		whereExpression, err := whereExpression(current, params.Args["where"])
		if err != nil {
			return nil, clientError(err)
		}
		sorts, err := sortArgs(params.Args)
		if err != nil {
			return nil, clientError(err)
		}
		pageResult, err := builder.local.List(params.Context, string(current.Slug), ridu.ListOptions{
			Where: whereExpression, Page: intArg(params.Args, "page", 1), Limit: boundedLimit(params.Args, builder.options.MaxListLimit), Sort: sorts,
			Populate: builder.populationsFor(current.Fields, params.Info), Actor: request.actor, ActorCollection: request.actorCollection, TrashOnly: boolArg(params.Args, "trash"),
			OutputFields: builder.outputFieldsFor(current.Fields, params.Info),
			Draft:        boolPointerArg(params.Args, "draft"),
			Locale:       localeArg(params.Args), FallbackLocales: fallbackArgs(params.Args), DisableFallback: boolArg(params.Args, "disableFallback"), AllLocales: boolArg(params.Args, "allLocales"),
		})
		if err != nil {
			return nil, transportError(err)
		}
		return pageMapForArgs(pageResult, params.Args), nil
	}}); err != nil {
		return err
	}
	countArgs := mergeArgs(enginegraphql.FieldConfigArgument{"where": &enginegraphql.ArgumentConfig{Type: where}}, builder.readLocaleArgs())
	if err := addRootField(queries, "count"+current.plural, &enginegraphql.Field{Type: enginegraphql.NewNonNull(count), Args: countArgs, Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
		request := requestFromContext(params)
		whereExpression, err := whereExpression(current, params.Args["where"])
		if err != nil {
			return nil, clientError(err)
		}
		result, err := builder.local.List(params.Context, string(current.Slug), ridu.ListOptions{Where: whereExpression, Page: 1, Limit: 1, Actor: request.actor, ActorCollection: request.actorCollection, OutputFields: []query.Path{}, Draft: boolPointerArg(params.Args, "draft"), TrashOnly: boolArg(params.Args, "trash"), Locale: localeArg(params.Args), FallbackLocales: fallbackArgs(params.Args), DisableFallback: boolArg(params.Args, "disableFallback"), AllLocales: boolArg(params.Args, "allLocales")})
		if err != nil {
			return nil, transportError(err)
		}
		return map[string]interface{}{"totalDocs": result.Total}, nil
	}}); err != nil {
		return err
	}

	writeLocale := builder.writeLocaleArgs()
	writeArgs := mergeArgs(writeLocale)
	if current.Versions != nil {
		writeArgs["draft"] = &enginegraphql.ArgumentConfig{Type: enginegraphql.Boolean}
	}
	if current.Upload == nil {
		createArgs := mergeArgs(writeArgs)
		if create != nil {
			createArgs["data"] = &enginegraphql.ArgumentConfig{Type: enginegraphql.NewNonNull(create)}
		}
		if err := addRootField(mutations, "create"+current.name, &enginegraphql.Field{Type: object, Args: createArgs, Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
			if err := primitiveListInputError(params, current.Fields); err != nil {
				return nil, clientError(err)
			}
			request := requestFromContext(params)
			var document store.Document
			var err error
			if current.Auth != nil {
				document, err = builder.app.CreateAuthUserForTransport(params.Context, string(current.Slug), valuesArg(params.Args, "data", current.Fields), dataStringArg(params.Args, "data", "password"), builder.mutationOptions(current, params, request, 0))
			} else {
				document, err = builder.local.Create(params.Context, string(current.Slug), valuesArg(params.Args, "data", current.Fields), builder.mutationOptions(current, params, request, 0))
			}
			if err != nil {
				return nil, transportError(err)
			}
			return documentMapForArgs(document, params.Args), nil
		}}); err != nil {
			return err
		}
	}
	identityArgs := mergeArgs(enginegraphql.FieldConfigArgument{"id": &enginegraphql.ArgumentConfig{Type: enginegraphql.NewNonNull(enginegraphql.ID)}}, writeLocale)
	updateArgs := mergeArgs(identityArgs, enginegraphql.FieldConfigArgument{"expectedRevision": &enginegraphql.ArgumentConfig{Type: enginegraphql.Int}})
	if update != nil {
		updateArgs["data"] = &enginegraphql.ArgumentConfig{Type: enginegraphql.NewNonNull(update)}
	}
	if err := addRootField(mutations, "update"+current.name, &enginegraphql.Field{Type: object, Args: updateArgs, Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
		if err := primitiveListInputError(params, current.Fields); err != nil {
			return nil, clientError(err)
		}
		request := requestFromContext(params)
		document, err := builder.local.Update(params.Context, string(current.Slug), fmt.Sprint(params.Args["id"]), valuesArg(params.Args, "data", current.Fields), builder.mutationOptions(current, params, request, intArg(params.Args, "expectedRevision", 0)))
		if err != nil {
			return nil, transportError(err)
		}
		return documentMapForArgs(document, params.Args), nil
	}}); err != nil {
		return err
	}
	if err := addRootField(mutations, "delete"+current.name, &enginegraphql.Field{Type: object, Args: identityArgs, Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
		request := requestFromContext(params)
		document, err := builder.local.Delete(params.Context, string(current.Slug), fmt.Sprint(params.Args["id"]), builder.mutationOptions(current, params, request, 0))
		if err != nil {
			return nil, transportError(err)
		}
		return documentMapForArgs(document, params.Args), nil
	}}); err != nil {
		return err
	}
	duplicateArgs := mergeArgs(identityArgs)
	if update != nil {
		duplicateArgs["data"] = &enginegraphql.ArgumentConfig{Type: update}
	}
	if err := addRootField(mutations, "duplicate"+current.name, &enginegraphql.Field{Type: object, Args: duplicateArgs, Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
		if err := primitiveListInputError(params, current.Fields); err != nil {
			return nil, clientError(err)
		}
		request := requestFromContext(params)
		options := builder.mutationOptions(current, params, request, 0)
		var document store.Document
		var err error
		if current.Upload != nil {
			if builder.app == nil {
				return nil, fmt.Errorf("GraphQL upload duplication requires a bound application")
			}
			document, err = builder.app.Duplicate(params.Context, string(current.Slug), fmt.Sprint(params.Args["id"]), valuesArg(params.Args, "data", current.Fields), options)
		} else {
			document, err = builder.local.Duplicate(params.Context, string(current.Slug), fmt.Sprint(params.Args["id"]), valuesArg(params.Args, "data", current.Fields), options)
		}
		if err != nil {
			return nil, transportError(err)
		}
		return documentMapForArgs(document, params.Args), nil
	}}); err != nil {
		return err
	}
	if current.Capabilities.Trash {
		if err := builder.addTrashMutations(current, object, identityArgs, mutations); err != nil {
			return err
		}
	}
	if current.Versions != nil {
		if err := builder.addCollectionVersions(current, object, queries, mutations); err != nil {
			return err
		}
	}
	if current.Auth != nil {
		return builder.addAuth(current, queries, mutations)
	}
	return nil
}

func (builder *schemaBuilder) mutationOptions(current resource, params enginegraphql.ResolveParams, request requestState, expectedRevision int) ridu.MutationOptions {
	return ridu.MutationOptions{
		Actor: request.actor, ActorCollection: request.actorCollection, ExpectedRevision: expectedRevision,
		Populate: builder.populationsFor(current.Fields, params.Info), Locale: localeArg(params.Args),
		OutputFields:    builder.outputFieldsFor(current.Fields, params.Info),
		Draft:           boolPointerArg(params.Args, "draft"),
		FallbackLocales: fallbackArgs(params.Args), DisableFallback: boolArg(params.Args, "disableFallback"), AllLocales: boolArg(params.Args, "allLocales"),
	}
}

func versionFindOptions(params enginegraphql.ResolveParams, request requestState) ridu.FindOptions {
	return ridu.FindOptions{
		Actor: request.actor, ActorCollection: request.actorCollection, Locale: localeArg(params.Args),
		FallbackLocales: fallbackArgs(params.Args), DisableFallback: boolArg(params.Args, "disableFallback"), AllLocales: boolArg(params.Args, "allLocales"),
	}
}

func (builder *schemaBuilder) addTrashMutations(current resource, object *enginegraphql.Object, identityArgs enginegraphql.FieldConfigArgument, mutations enginegraphql.Fields) error {
	if err := addRootField(mutations, "restoreDeleted"+current.name, &enginegraphql.Field{Type: object, Args: identityArgs, Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
		request := requestFromContext(params)
		document, err := builder.local.RestoreDeleted(params.Context, string(current.Slug), fmt.Sprint(params.Args["id"]), builder.mutationOptions(current, params, request, 0))
		if err != nil {
			return nil, transportError(err)
		}
		return documentMapForArgs(document, params.Args), nil
	}}); err != nil {
		return err
	}
	return addRootField(mutations, "deletePermanent"+current.name, &enginegraphql.Field{Type: object, Args: identityArgs, Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
		request := requestFromContext(params)
		document, err := builder.local.DeletePermanent(params.Context, string(current.Slug), fmt.Sprint(params.Args["id"]), builder.mutationOptions(current, params, request, 0))
		if err != nil {
			return nil, transportError(err)
		}
		return documentMapForArgs(document, params.Args), nil
	}})
}

func (builder *schemaBuilder) addCollectionVersions(current resource, object *enginegraphql.Object, queries, mutations enginegraphql.Fields) error {
	version, page := builder.versionTypes(current, object)
	readArgs := mergeArgs(enginegraphql.FieldConfigArgument{
		"id": &enginegraphql.ArgumentConfig{Type: enginegraphql.NewNonNull(enginegraphql.ID)}, "revision": &enginegraphql.ArgumentConfig{Type: enginegraphql.NewNonNull(enginegraphql.Int)},
	}, builder.readLocaleArgs())
	if err := addRootField(queries, "version"+current.name, &enginegraphql.Field{Type: version, Args: readArgs, Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
		request := requestFromContext(params)
		result, err := builder.local.Version(params.Context, string(current.Slug), fmt.Sprint(params.Args["id"]), intArg(params.Args, "revision", 0), versionFindOptions(params, request))
		if err != nil {
			return nil, transportError(err)
		}
		return versionMap(result), nil
	}}); err != nil {
		return err
	}
	listArgs := mergeArgs(enginegraphql.FieldConfigArgument{
		"id": &enginegraphql.ArgumentConfig{Type: enginegraphql.NewNonNull(enginegraphql.ID)}, "page": &enginegraphql.ArgumentConfig{Type: enginegraphql.Int}, "limit": &enginegraphql.ArgumentConfig{Type: enginegraphql.Int},
	}, builder.readLocaleArgs())
	if err := addRootField(queries, "versions"+current.plural, &enginegraphql.Field{Type: enginegraphql.NewNonNull(page), Args: listArgs, Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
		request := requestFromContext(params)
		versions, err := builder.local.Versions(params.Context, string(current.Slug), fmt.Sprint(params.Args["id"]), versionFindOptions(params, request))
		if err != nil {
			return nil, transportError(err)
		}
		return versionPageMap(versions, intArg(params.Args, "page", 1), boundedLimit(params.Args, builder.options.MaxListLimit)), nil
	}}); err != nil {
		return err
	}
	restoreArgs := mergeArgs(enginegraphql.FieldConfigArgument{
		"id": &enginegraphql.ArgumentConfig{Type: enginegraphql.NewNonNull(enginegraphql.ID)}, "revision": &enginegraphql.ArgumentConfig{Type: enginegraphql.NewNonNull(enginegraphql.Int)},
		"expectedRevision": &enginegraphql.ArgumentConfig{Type: enginegraphql.Int}, "draft": &enginegraphql.ArgumentConfig{Type: enginegraphql.Boolean},
	}, builder.writeLocaleArgs())
	if err := addRootField(mutations, "restoreVersion"+current.name, &enginegraphql.Field{Type: object, Args: restoreArgs, Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
		request := requestFromContext(params)
		restore := builder.local.Restore
		if boolArg(params.Args, "draft") {
			restore = builder.local.RestoreAsDraft
		}
		document, err := restore(params.Context, string(current.Slug), fmt.Sprint(params.Args["id"]), intArg(params.Args, "revision", 0), builder.mutationOptions(current, params, request, intArg(params.Args, "expectedRevision", 0)))
		if err != nil {
			return nil, transportError(err)
		}
		return documentMapForArgs(document, params.Args), nil
	}}); err != nil {
		return err
	}
	if current.Versions.Drafts {
		for _, action := range []struct {
			name string
			run  func(params enginegraphql.ResolveParams, request requestState) (store.Document, error)
		}{
			{name: "publish", run: func(params enginegraphql.ResolveParams, request requestState) (store.Document, error) {
				return builder.local.Publish(params.Context, string(current.Slug), fmt.Sprint(params.Args["id"]), builder.mutationOptions(current, params, request, intArg(params.Args, "expectedRevision", 0)))
			}},
			{name: "unpublish", run: func(params enginegraphql.ResolveParams, request requestState) (store.Document, error) {
				return builder.local.Unpublish(params.Context, string(current.Slug), fmt.Sprint(params.Args["id"]), builder.mutationOptions(current, params, request, intArg(params.Args, "expectedRevision", 0)))
			}},
		} {
			action := action
			args := mergeArgs(enginegraphql.FieldConfigArgument{"id": &enginegraphql.ArgumentConfig{Type: enginegraphql.NewNonNull(enginegraphql.ID)}, "expectedRevision": &enginegraphql.ArgumentConfig{Type: enginegraphql.Int}}, builder.writeLocaleArgs())
			if err := addRootField(mutations, action.name+current.name, &enginegraphql.Field{Type: object, Args: args, Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
				document, err := action.run(params, requestFromContext(params))
				if err != nil {
					return nil, transportError(err)
				}
				return documentMapForArgs(document, params.Args), nil
			}}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (builder *schemaBuilder) versionTypes(current resource, object *enginegraphql.Object) (*enginegraphql.Object, *enginegraphql.Object) {
	version := enginegraphql.NewObject(enginegraphql.ObjectConfig{Name: current.name + "Version", Fields: enginegraphql.Fields{
		"id": &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.ID)}, "documentID": &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.ID)},
		"revision": &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.Int)}, "status": &enginegraphql.Field{Type: enginegraphql.String},
		"createdAt": &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.String)}, "snapshot": &enginegraphql.Field{Type: enginegraphql.NewNonNull(object)},
	}})
	page := enginegraphql.NewObject(enginegraphql.ObjectConfig{Name: current.plural + "VersionsPage", Fields: enginegraphql.Fields{
		"docs": &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.NewList(enginegraphql.NewNonNull(version)))},
		"page": &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.Int)}, "limit": &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.Int)},
		"totalDocs": &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.Int)}, "totalPages": &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.Int)},
	}})
	return version, page
}

func (builder *schemaBuilder) addAuth(current resource, queries, mutations enginegraphql.Fields) error {
	object := builder.objects[current.ID]
	sessionType := enginegraphql.NewObject(enginegraphql.ObjectConfig{Name: current.name + "Auth", Fields: enginegraphql.Fields{
		"token": &enginegraphql.Field{Type: enginegraphql.String}, "refreshedToken": &enginegraphql.Field{Type: enginegraphql.String}, "collection": &enginegraphql.Field{Type: enginegraphql.String},
		"expiresAt": &enginegraphql.Field{Type: enginegraphql.String}, "exp": &enginegraphql.Field{Type: enginegraphql.Float},
		"user": &enginegraphql.Field{Type: object},
	}})
	if err := addRootField(queries, "me"+current.name, &enginegraphql.Field{Type: sessionType, Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
		request := requestFromContext(params)
		if request.actor == nil || request.actorCollection != current.Slug {
			return map[string]interface{}{"collection": string(current.Slug), "user": nil}, nil
		}
		return map[string]interface{}{"collection": string(current.Slug), "user": documentMap(*request.actor)}, nil
	}}); err != nil {
		return err
	}
	if err := addRootField(queries, "initialized"+current.name, &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.Boolean), Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
		initialized, err := builder.app.AuthInitialized(params.Context, string(current.Slug))
		if err != nil {
			return nil, transportError(err)
		}
		return initialized, nil
	}}); err != nil {
		return err
	}
	identityName := fieldName(current.Auth.IdentityField)
	loginArgs := enginegraphql.FieldConfigArgument{
		identityName: &enginegraphql.ArgumentConfig{Type: enginegraphql.NewNonNull(enginegraphql.String)},
		"password":   &enginegraphql.ArgumentConfig{Type: enginegraphql.NewNonNull(enginegraphql.String)},
	}
	if err := addRootField(mutations, "login"+current.name, &enginegraphql.Field{Type: sessionType, Args: loginArgs, Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
		identity := fmt.Sprint(params.Args[identityName])
		request := requestFromContext(params)
		if request.admitAuth != nil {
			if err := request.admitAuth(params.Context, string(current.Slug), identity); err != nil {
				return nil, transportError(err)
			}
		}
		session, err := builder.app.LoginWithOptions(params.Context, string(current.Slug), identity, fmt.Sprint(params.Args["password"]), ridu.LoginOptions{
			IPAddress: request.clientIP,
			UserAgent: request.userAgent,
		})
		if err != nil {
			return nil, transportError(err)
		}
		return sessionMap(session), nil
	}}); err != nil {
		return err
	}
	refreshField := func() *enginegraphql.Field {
		return &enginegraphql.Field{Type: sessionType, Args: enginegraphql.FieldConfigArgument{"token": &enginegraphql.ArgumentConfig{Type: enginegraphql.String}}, Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
			request := requestFromContext(params)
			token := stringArg(params.Args, "token", request.token)
			session, err := builder.app.RotateSession(params.Context, token)
			if err != nil {
				return nil, transportError(err)
			}
			return sessionMap(session), nil
		}}
	}
	if err := addRootField(mutations, "refresh"+current.name, refreshField()); err != nil {
		return err
	}
	if err := addRootField(mutations, "refreshToken"+current.name, refreshField()); err != nil {
		return err
	}
	if err := addRootField(mutations, "logout"+current.name, &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.Boolean), Args: enginegraphql.FieldConfigArgument{"token": &enginegraphql.ArgumentConfig{Type: enginegraphql.String}}, Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
		request := requestFromContext(params)
		if err := builder.app.Logout(params.Context, stringArg(params.Args, "token", request.token)); err != nil {
			return nil, transportError(err)
		}
		return true, nil
	}}); err != nil {
		return err
	}
	if current.Auth.MaxLoginAttempts > 0 {
		if err := addRootField(mutations, "unlock"+current.name, &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.Boolean), Args: enginegraphql.FieldConfigArgument{identityName: &enginegraphql.ArgumentConfig{Type: enginegraphql.NewNonNull(enginegraphql.String)}}, Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
			request := requestFromContext(params)
			identity := &ridu.AuthIdentity{Collection: request.actorCollection}
			if request.actor != nil {
				identity.Actor = *request.actor
			} else {
				identity = nil
			}
			if err := builder.app.UnlockAuthUser(params.Context, string(current.Slug), fmt.Sprint(params.Args[identityName]), identity); err != nil {
				return nil, transportError(err)
			}
			return true, nil
		}}); err != nil {
			return err
		}
	}
	if current.Auth.PasswordReset {
		if err := addRootField(mutations, "forgotPassword"+current.name, &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.Boolean), Args: enginegraphql.FieldConfigArgument{identityName: &enginegraphql.ArgumentConfig{Type: enginegraphql.NewNonNull(enginegraphql.String)}}, Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
			identity := fmt.Sprint(params.Args[identityName])
			request := requestFromContext(params)
			if request.admitAuth != nil {
				if err := request.admitAuth(params.Context, string(current.Slug)+":forgot-password", identity); err != nil {
					return nil, transportError(err)
				}
			}
			if err := builder.app.RequestPasswordReset(params.Context, string(current.Slug), identity); err != nil {
				return nil, transportError(err)
			}
			return true, nil
		}}); err != nil {
			return err
		}
		if err := addRootField(mutations, "resetPassword"+current.name, &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.Boolean), Args: enginegraphql.FieldConfigArgument{"token": &enginegraphql.ArgumentConfig{Type: enginegraphql.NewNonNull(enginegraphql.String)}, "password": &enginegraphql.ArgumentConfig{Type: enginegraphql.NewNonNull(enginegraphql.String)}}, Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
			request := requestFromContext(params)
			if request.admitAuth != nil {
				if err := request.admitAuth(params.Context, string(current.Slug)+":reset-password", ""); err != nil {
					return nil, transportError(err)
				}
			}
			if err := builder.app.ResetPassword(params.Context, string(current.Slug), fmt.Sprint(params.Args["token"]), fmt.Sprint(params.Args["password"])); err != nil {
				return nil, transportError(err)
			}
			return true, nil
		}}); err != nil {
			return err
		}
	}
	if current.Auth.VerifyEmail {
		verificationRequestField := func() *enginegraphql.Field {
			return &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.Boolean), Args: enginegraphql.FieldConfigArgument{identityName: &enginegraphql.ArgumentConfig{Type: enginegraphql.NewNonNull(enginegraphql.String)}}, Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
				identity := fmt.Sprint(params.Args[identityName])
				request := requestFromContext(params)
				if request.admitAuth != nil {
					if err := request.admitAuth(params.Context, string(current.Slug)+":request-verification", identity); err != nil {
						return nil, transportError(err)
					}
				}
				if err := builder.app.RequestVerification(params.Context, string(current.Slug), identity); err != nil {
					return nil, transportError(err)
				}
				return true, nil
			}}
		}
		if err := addRootField(mutations, "requestVerification"+current.name, verificationRequestField()); err != nil {
			return err
		}
		if err := addRootField(mutations, "resendVerification"+current.name, verificationRequestField()); err != nil {
			return err
		}
		return addRootField(mutations, "verifyEmail"+current.name, &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.Boolean), Args: enginegraphql.FieldConfigArgument{"token": &enginegraphql.ArgumentConfig{Type: enginegraphql.NewNonNull(enginegraphql.String)}}, Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
			request := requestFromContext(params)
			if request.admitAuth != nil {
				if err := request.admitAuth(params.Context, string(current.Slug)+":verify", ""); err != nil {
					return nil, transportError(err)
				}
			}
			if err := builder.app.VerifyEmail(params.Context, string(current.Slug), fmt.Sprint(params.Args["token"])); err != nil {
				return nil, transportError(err)
			}
			return true, nil
		}})
	}
	return nil
}

func (builder *schemaBuilder) addGlobal(current resource, queries, mutations enginegraphql.Fields) error {
	object := builder.objects[current.ID]
	if err := addRootField(queries, current.name, &enginegraphql.Field{Type: object, Args: builder.readLocaleArgs(), Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
		request := requestFromContext(params)
		document, err := builder.local.Global(params.Context, string(current.Slug), ridu.FindOptions{
			Actor: request.actor, ActorCollection: request.actorCollection, Populate: builder.populationsFor(current.Fields, params.Info), Locale: localeArg(params.Args),
			OutputFields:    builder.outputFieldsFor(current.Fields, params.Info),
			Draft:           boolPointerArg(params.Args, "draft"),
			FallbackLocales: fallbackArgs(params.Args), DisableFallback: boolArg(params.Args, "disableFallback"), AllLocales: boolArg(params.Args, "allLocales"),
		})
		if err != nil {
			return nil, transportError(err)
		}
		return documentMapForArgs(document, params.Args), nil
	}}); err != nil {
		return err
	}
	if err := builder.addDocumentAccess(current, queries); err != nil {
		return err
	}
	input := builder.dataInput(current, false)
	updateArgs := mergeArgs(enginegraphql.FieldConfigArgument{
		"expectedRevision": &enginegraphql.ArgumentConfig{Type: enginegraphql.Int},
	}, builder.writeLocaleArgs())
	if input != nil {
		updateArgs["data"] = &enginegraphql.ArgumentConfig{Type: enginegraphql.NewNonNull(input)}
	}
	if err := addRootField(mutations, "update"+current.name, &enginegraphql.Field{Type: object, Args: updateArgs, Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
		if err := primitiveListInputError(params, current.Fields); err != nil {
			return nil, clientError(err)
		}
		request := requestFromContext(params)
		document, err := builder.local.UpdateGlobal(params.Context, string(current.Slug), valuesArg(params.Args, "data", current.Fields), builder.mutationOptions(current, params, request, intArg(params.Args, "expectedRevision", 0)))
		if err != nil {
			return nil, transportError(err)
		}
		return documentMapForArgs(document, params.Args), nil
	}}); err != nil {
		return err
	}
	if current.Versions != nil {
		return builder.addGlobalVersions(current, object, queries, mutations)
	}
	return nil
}

func (builder *schemaBuilder) addGlobalVersions(current resource, object *enginegraphql.Object, queries, mutations enginegraphql.Fields) error {
	version, page := builder.versionTypes(current, object)
	if err := addRootField(queries, "version"+current.name, &enginegraphql.Field{Type: version, Args: mergeArgs(enginegraphql.FieldConfigArgument{"revision": &enginegraphql.ArgumentConfig{Type: enginegraphql.NewNonNull(enginegraphql.Int)}}, builder.readLocaleArgs()), Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
		request := requestFromContext(params)
		result, err := builder.local.GlobalVersion(params.Context, string(current.Slug), intArg(params.Args, "revision", 0), versionFindOptions(params, request))
		if err != nil {
			return nil, transportError(err)
		}
		return versionMap(result), nil
	}}); err != nil {
		return err
	}
	if err := addRootField(queries, "versions"+current.plural, &enginegraphql.Field{Type: enginegraphql.NewNonNull(page), Args: mergeArgs(enginegraphql.FieldConfigArgument{"page": &enginegraphql.ArgumentConfig{Type: enginegraphql.Int}, "limit": &enginegraphql.ArgumentConfig{Type: enginegraphql.Int}}, builder.readLocaleArgs()), Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
		request := requestFromContext(params)
		versions, err := builder.local.GlobalVersions(params.Context, string(current.Slug), versionFindOptions(params, request))
		if err != nil {
			return nil, transportError(err)
		}
		return versionPageMap(versions, intArg(params.Args, "page", 1), boundedLimit(params.Args, builder.options.MaxListLimit)), nil
	}}); err != nil {
		return err
	}
	restoreArgs := mergeArgs(enginegraphql.FieldConfigArgument{"revision": &enginegraphql.ArgumentConfig{Type: enginegraphql.NewNonNull(enginegraphql.Int)}, "expectedRevision": &enginegraphql.ArgumentConfig{Type: enginegraphql.Int}, "draft": &enginegraphql.ArgumentConfig{Type: enginegraphql.Boolean}}, builder.writeLocaleArgs())
	if err := addRootField(mutations, "restoreVersion"+current.name, &enginegraphql.Field{Type: object, Args: restoreArgs, Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
		request := requestFromContext(params)
		restore := builder.local.RestoreGlobal
		if boolArg(params.Args, "draft") {
			restore = builder.local.RestoreGlobalAsDraft
		}
		document, err := restore(params.Context, string(current.Slug), intArg(params.Args, "revision", 0), builder.mutationOptions(current, params, request, intArg(params.Args, "expectedRevision", 0)))
		if err != nil {
			return nil, transportError(err)
		}
		return documentMapForArgs(document, params.Args), nil
	}}); err != nil {
		return err
	}
	if current.Versions.Drafts {
		for _, action := range []struct {
			name string
			run  func(enginegraphql.ResolveParams, requestState) (store.Document, error)
		}{
			{name: "publish", run: func(params enginegraphql.ResolveParams, request requestState) (store.Document, error) {
				return builder.local.PublishGlobal(params.Context, string(current.Slug), builder.mutationOptions(current, params, request, intArg(params.Args, "expectedRevision", 0)))
			}},
			{name: "unpublish", run: func(params enginegraphql.ResolveParams, request requestState) (store.Document, error) {
				return builder.local.UnpublishGlobal(params.Context, string(current.Slug), builder.mutationOptions(current, params, request, intArg(params.Args, "expectedRevision", 0)))
			}},
		} {
			action := action
			if err := addRootField(mutations, action.name+current.name, &enginegraphql.Field{Type: object, Args: mergeArgs(enginegraphql.FieldConfigArgument{"expectedRevision": &enginegraphql.ArgumentConfig{Type: enginegraphql.Int}}, builder.writeLocaleArgs()), Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
				document, err := action.run(params, requestFromContext(params))
				if err != nil {
					return nil, transportError(err)
				}
				return documentMapForArgs(document, params.Args), nil
			}}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (builder *schemaBuilder) outputFields(current resource, parent string, fields []schema.Field, document bool) enginegraphql.Fields {
	result := enginegraphql.Fields{}
	if document {
		result["id"] = &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.ID)}
		result["createdAt"] = &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.String)}
		result["updatedAt"] = &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.String)}
		result["_status"] = &enginegraphql.Field{Type: enginegraphql.String}
		result["_revision"] = &enginegraphql.Field{Type: enginegraphql.Int}
	}
	for _, field := range fields {
		if field.Type == schema.FieldTypeUI {
			continue
		}
		name := fieldName(field.Name)
		if _, exists := result[name]; exists {
			continue
		}
		if field.Type == schema.FieldTypeJoin && field.Join != nil {
			result[name] = builder.joinOutputField(current, parent, field)
			continue
		}
		result[name] = &enginegraphql.Field{Type: builder.outputType(current, parent, field)}
	}
	return result
}

func (builder *schemaBuilder) joinOutputField(sourceResource resource, parent string, field schema.Field) *enginegraphql.Field {
	target, available := builder.resources[field.Join.CollectionID]
	object := builder.objects[field.Join.CollectionID]
	if !available || object == nil {
		return &enginegraphql.Field{Type: builder.json}
	}
	typeName := parent + typeName(field.Name) + "Join"
	resultType := enginegraphql.NewObject(enginegraphql.ObjectConfig{Name: typeName, Fields: enginegraphql.Fields{
		"docs":        &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.NewList(enginegraphql.NewNonNull(object)))},
		"hasNextPage": &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.Boolean)},
		"totalDocs":   &enginegraphql.Field{Type: enginegraphql.Int},
	}})
	args := enginegraphql.FieldConfigArgument{
		"count": &enginegraphql.ArgumentConfig{Type: enginegraphql.Boolean}, "limit": &enginegraphql.ArgumentConfig{Type: enginegraphql.Int},
		"page": &enginegraphql.ArgumentConfig{Type: enginegraphql.Int}, "sort": &enginegraphql.ArgumentConfig{Type: enginegraphql.String},
		"where": &enginegraphql.ArgumentConfig{Type: builder.whereInput(target)},
	}
	return &enginegraphql.Field{Type: resultType, Args: args, Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
		source, _ := params.Source.(map[string]interface{})
		id := fmt.Sprint(source["id"])
		if id == "" {
			return map[string]interface{}{"docs": []interface{}{}, "hasNextPage": false}, nil
		}
		where, err := whereExpression(target, params.Args["where"])
		if err != nil {
			return nil, clientError(err)
		}
		var sorts []query.Sort
		if rawSort := stringArg(params.Args, "sort", ""); rawSort != "" {
			sorts, err = sortArgs(map[string]interface{}{"sort": []interface{}{rawSort}})
			if err != nil {
				return nil, clientError(err)
			}
		}
		limit := boundedLimit(params.Args, builder.options.MaxListLimit)
		if field.Join.Limit > 0 && limit > field.Join.Limit {
			limit = field.Join.Limit
		}
		request := requestFromContext(params)
		options := ridu.ListOptions{
			Where: where, Page: intArg(params.Args, "page", 1), Limit: limit, Sort: sorts,
			Populate: builder.populationsFor(target.Fields, params.Info), Actor: request.actor, ActorCollection: request.actorCollection,
			OutputFields:    builder.outputFieldsFor(target.Fields, params.Info),
			Locale:          schema.LocaleCode(stringMapValue(source, "__riduLocale")),
			FallbackLocales: localeCodesValue(source["__riduFallbackLocales"]),
			DisableFallback: boolMapValue(source, "__riduDisableFallback"), AllLocales: boolMapValue(source, "__riduAllLocales"),
		}
		page, err := builder.local.ListJoin(params.Context, string(sourceResource.Slug), id, field.Path.String(), options)
		if err != nil {
			return nil, transportError(err)
		}
		docs := make([]interface{}, len(page.Documents))
		for index, document := range page.Documents {
			docs[index] = documentMapWithLocalization(document, options.Locale, options.FallbackLocales, options.DisableFallback, options.AllLocales)
		}
		result := map[string]interface{}{"docs": docs, "hasNextPage": page.Page*page.Limit < page.Total}
		if boolArg(params.Args, "count") {
			result["totalDocs"] = page.Total
		}
		return result, nil
	}}
}

func (builder *schemaBuilder) outputType(current resource, parent string, field schema.Field) enginegraphql.Output {
	switch field.Type {
	case schema.FieldTypeNumber:
		return enginegraphql.Float
	case schema.FieldTypeTextList:
		return enginegraphql.NewList(enginegraphql.NewNonNull(enginegraphql.String))
	case schema.FieldTypeNumberList:
		return enginegraphql.NewList(enginegraphql.NewNonNull(enginegraphql.Float))
	case schema.FieldTypeCheckbox:
		return enginegraphql.Boolean
	case schema.FieldTypeSelect, schema.FieldTypeRadio:
		option := builder.selectEnum(parent, field)
		if field.Type == schema.FieldTypeSelect && field.Select != nil && field.Select.HasMany {
			return enginegraphql.NewList(enginegraphql.NewNonNull(option))
		}
		return option
	case schema.FieldTypePoint:
		return enginegraphql.NewList(enginegraphql.NewNonNull(enginegraphql.Float))
	case schema.FieldTypeJSON, schema.FieldTypePlugin:
		return builder.json
	case schema.FieldTypeJoin:
		if field.Join != nil {
			if target := builder.objects[field.Join.CollectionID]; target != nil {
				return enginegraphql.NewList(target)
			}
		}
		return builder.json
	case schema.FieldTypeVirtual:
		if field.Virtual == nil {
			return builder.json
		}
		switch field.Virtual.ValueType {
		case schema.ValueTypeString:
			return enginegraphql.String
		case schema.ValueTypeNumber:
			return enginegraphql.Float
		case schema.ValueTypeBoolean:
			return enginegraphql.Boolean
		default:
			return builder.json
		}
	case schema.FieldTypeBlocks:
		if field.Blocks == nil {
			return builder.json
		}
		return enginegraphql.NewList(builder.blockUnion(current, parent, field))
	case schema.FieldTypeGroup, schema.FieldTypeArray:
		name := parent + typeName(field.Name)
		childFields := field.Nested.ResolvedFields()
		object := enginegraphql.NewObject(enginegraphql.ObjectConfig{Name: name, Fields: enginegraphql.FieldsThunk(func() enginegraphql.Fields {
			fields := builder.outputFields(current, name, childFields, false)
			if field.Type == schema.FieldTypeArray {
				fields["_key"] = &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.String)}
			}
			return fields
		})})
		if field.Type == schema.FieldTypeArray {
			return enginegraphql.NewList(object)
		}
		return object
	case schema.FieldTypeRelationship:
		if field.Relationship != nil && !field.Relationship.Polymorphic {
			if target := builder.objects[field.Relationship.CollectionID]; target != nil {
				if field.Relationship.HasMany {
					return enginegraphql.NewList(target)
				}
				return target
			}
		}
		if field.Relationship != nil && field.Relationship.Polymorphic {
			wrapper := builder.polymorphicOutputType(parent, field)
			if field.Relationship.HasMany {
				return enginegraphql.NewList(wrapper)
			}
			return wrapper
		}
		return builder.json
	case schema.FieldTypeUpload:
		if field.Upload != nil {
			if target := builder.objects[field.Upload.CollectionID]; target != nil {
				if field.Upload.HasMany {
					return enginegraphql.NewList(target)
				}
				return target
			}
		}
		return builder.json
	default:
		return enginegraphql.String
	}
}

func (builder *schemaBuilder) polymorphicOutputType(parent string, field schema.Field) *enginegraphql.Object {
	baseName := parent + typeName(field.Name)
	enum := builder.relationshipTargetEnum(baseName+"RelationTo", field.Relationship.Targets)
	targets := make([]*enginegraphql.Object, 0, len(field.Relationship.Targets))
	bySlug := make(map[string]*enginegraphql.Object, len(field.Relationship.Targets))
	for _, target := range field.Relationship.Targets {
		object := builder.objects[target.CollectionID]
		if object != nil {
			targets = append(targets, object)
			bySlug[string(target.CollectionSlug)] = object
		}
	}
	union := enginegraphql.NewUnion(enginegraphql.UnionConfig{Name: baseName + "Value", Types: targets, ResolveType: func(params enginegraphql.ResolveTypeParams) *enginegraphql.Object {
		value, _ := params.Value.(map[string]interface{})
		collection, _ := value["__collection"].(string)
		return bySlug[collection]
	}})
	return enginegraphql.NewObject(enginegraphql.ObjectConfig{Name: baseName + "Relationship", Fields: enginegraphql.Fields{
		"relationTo": &enginegraphql.Field{Type: enginegraphql.NewNonNull(enum)},
		"value": &enginegraphql.Field{Type: union, Resolve: func(params enginegraphql.ResolveParams) (interface{}, error) {
			source, _ := params.Source.(map[string]interface{})
			document, ok := source["id"].(map[string]interface{})
			if !ok {
				return nil, nil
			}
			cloned := make(map[string]interface{}, len(document)+1)
			for key, value := range document {
				cloned[key] = value
			}
			cloned["__collection"], _ = source["relationTo"].(string)
			return cloned, nil
		}},
	}})
}

func (builder *schemaBuilder) relationshipTargetEnum(name string, targets []schema.RelationshipTarget) *enginegraphql.Enum {
	values := enginegraphql.EnumValueConfigMap{}
	for _, target := range targets {
		values[enumName(string(target.CollectionSlug))] = &enginegraphql.EnumValueConfig{Value: string(target.CollectionSlug)}
	}
	return enginegraphql.NewEnum(enginegraphql.EnumConfig{Name: name, Values: values})
}

func (builder *schemaBuilder) blockUnion(current resource, parent string, field schema.Field) *enginegraphql.Union {
	name := parent + typeName(field.Name) + "Block"
	if existing := builder.unions[name]; existing != nil {
		return existing
	}
	objects := make([]*enginegraphql.Object, 0, len(field.Blocks.ResolvedTypes()))
	bySlug := make(map[string]*enginegraphql.Object, len(field.Blocks.ResolvedTypes()))
	for _, block := range field.Blocks.ResolvedTypes() {
		blockCopy := block
		objectName := name + typeName(block.Slug)
		object := enginegraphql.NewObject(enginegraphql.ObjectConfig{Name: objectName, Fields: enginegraphql.FieldsThunk(func() enginegraphql.Fields {
			fields := builder.outputFields(current, objectName, blockCopy.ResolvedFields(), false)
			fields["_key"] = &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.String)}
			fields["blockType"] = &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.String)}
			return fields
		})})
		objects = append(objects, object)
		bySlug[block.Slug] = object
	}
	union := enginegraphql.NewUnion(enginegraphql.UnionConfig{Name: name, Types: objects, ResolveType: func(params enginegraphql.ResolveTypeParams) *enginegraphql.Object {
		value, _ := params.Value.(map[string]interface{})
		blockType, _ := value["blockType"].(string)
		return bySlug[blockType]
	}})
	builder.unions[name] = union
	return union
}

func (builder *schemaBuilder) dataInput(current resource, create bool) *enginegraphql.InputObject {
	suffix := "UpdateInput"
	if create {
		suffix = "CreateInput"
	}
	name := current.name + suffix
	fields := builder.inputFields(current, name, current.Fields, create)
	if create && current.Auth != nil {
		fields["password"] = &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.NewNonNull(enginegraphql.String)}
	}
	if len(fields) == 0 {
		return nil
	}
	return enginegraphql.NewInputObject(enginegraphql.InputObjectConfig{Name: name, Fields: fields})
}

func (builder *schemaBuilder) inputFields(current resource, parent string, fields []schema.Field, create bool) enginegraphql.InputObjectConfigFieldMap {
	result := enginegraphql.InputObjectConfigFieldMap{}
	for _, field := range fields {
		if field.Type == schema.FieldTypeUI || field.Type == schema.FieldTypeJoin || field.Type == schema.FieldTypeVirtual || field.Category == schema.FieldCategoryUpload {
			continue
		}
		input := builder.inputType(current, parent, field, create)
		if create && (field.Required || primitiveListNeedsValue(field)) && !fieldHasDefault(field) {
			input = enginegraphql.NewNonNull(input)
		}
		result[fieldName(field.Name)] = &enginegraphql.InputObjectFieldConfig{Type: input}
	}
	return result
}

func (builder *schemaBuilder) inputType(current resource, parent string, field schema.Field, create bool) enginegraphql.Input {
	switch field.Type {
	case schema.FieldTypeNumber:
		return enginegraphql.Float
	case schema.FieldTypeTextList:
		return enginegraphql.NewList(enginegraphql.NewNonNull(enginegraphql.String))
	case schema.FieldTypeNumberList:
		return enginegraphql.NewList(enginegraphql.NewNonNull(enginegraphql.Float))
	case schema.FieldTypeCheckbox:
		return enginegraphql.Boolean
	case schema.FieldTypeSelect, schema.FieldTypeRadio:
		option := builder.selectEnum(parent, field)
		if field.Type == schema.FieldTypeSelect && field.Select != nil && field.Select.HasMany {
			return enginegraphql.NewList(enginegraphql.NewNonNull(option))
		}
		return option
	case schema.FieldTypePoint:
		return enginegraphql.NewList(enginegraphql.NewNonNull(enginegraphql.Float))
	case schema.FieldTypeJSON, schema.FieldTypeBlocks, schema.FieldTypePlugin:
		return builder.json
	case schema.FieldTypeGroup, schema.FieldTypeArray:
		name := parent + typeName(field.Name)
		childFields := field.Nested.ResolvedFields()
		fields := builder.inputFields(current, name, childFields, create)
		if field.Type == schema.FieldTypeArray {
			// Optional on creation, and supplied on retained rows during updates.
			// The local operation engine owns identity generation and validation.
			fields["_key"] = &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.String}
		}
		input := enginegraphql.NewInputObject(enginegraphql.InputObjectConfig{Name: name, Fields: fields})
		if field.Type == schema.FieldTypeArray {
			return enginegraphql.NewList(input)
		}
		return input
	case schema.FieldTypeRelationship:
		if field.Relationship != nil && field.Relationship.Polymorphic {
			baseName := parent + typeName(field.Name)
			input := enginegraphql.NewInputObject(enginegraphql.InputObjectConfig{Name: baseName + "RelationshipInput", Fields: enginegraphql.InputObjectConfigFieldMap{
				"relationTo": &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.NewNonNull(builder.relationshipTargetEnum(baseName+"RelationshipInputRelationTo", field.Relationship.Targets))},
				"value":      &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.NewNonNull(enginegraphql.ID)},
			}})
			if field.Relationship.HasMany {
				return enginegraphql.NewList(enginegraphql.NewNonNull(input))
			}
			return input
		}
		if field.Relationship != nil && field.Relationship.HasMany {
			return enginegraphql.NewList(enginegraphql.NewNonNull(enginegraphql.ID))
		}
		return enginegraphql.ID
	case schema.FieldTypeUpload:
		if field.Upload != nil && field.Upload.HasMany {
			return enginegraphql.NewList(enginegraphql.NewNonNull(enginegraphql.ID))
		}
		return enginegraphql.ID
	default:
		return enginegraphql.String
	}
}

func fieldHasDefault(field schema.Field) bool {
	return field.Default != nil || field.DynamicDefault ||
		field.Select != nil && field.Select.HasMany && len(field.Select.DefaultValues) != 0 ||
		field.Text != nil && field.Text.Slug != nil
}

func (builder *schemaBuilder) selectEnum(parent string, field schema.Field) *enginegraphql.Enum {
	name := parent + typeName(field.Name) + "Option"
	if existing := builder.enums[name]; existing != nil {
		return existing
	}
	values := enginegraphql.EnumValueConfigMap{}
	if field.Select != nil {
		for _, option := range field.Select.Options {
			values[enumName(option.Value)] = &enginegraphql.EnumValueConfig{Value: option.Value, Description: option.Label}
		}
	}
	result := enginegraphql.NewEnum(enginegraphql.EnumConfig{Name: name, Values: values})
	builder.enums[name] = result
	return result
}

func validateGraphQLFields(fields []schema.Field, document bool) error {
	seen := make(map[string]string, len(fields)+5)
	if document {
		for _, name := range []string{"id", "createdAt", "updatedAt", "_status", "_revision"} {
			seen[name] = "document metadata"
		}
	}
	for _, field := range fields {
		if field.Type == schema.FieldTypeUI {
			continue
		}
		name := fieldName(field.Name)
		if owner, exists := seen[name]; exists {
			return fmt.Errorf("fields %q and %q produce duplicate GraphQL field name %q", owner, field.Name, name)
		}
		if !graphQLNamePattern.MatchString(name) || strings.HasPrefix(name, "__") {
			return fmt.Errorf("field %q produces invalid GraphQL name %q", field.Name, name)
		}
		seen[name] = field.Name
		if field.Nested != nil {
			if err := validateGraphQLFields(field.Nested.ResolvedFields(), false); err != nil {
				return err
			}
		}
		if field.Blocks != nil {
			blockNames := make(map[string]string, len(field.Blocks.ResolvedTypes()))
			for _, block := range field.Blocks.ResolvedTypes() {
				blockName := typeName(block.Slug)
				if owner, exists := blockNames[blockName]; exists {
					return fmt.Errorf("blocks %q and %q produce duplicate GraphQL type name %q", owner, block.Slug, blockName)
				}
				blockNames[blockName] = block.Slug
				if err := validateGraphQLFields(block.ResolvedFields(), false); err != nil {
					return err
				}
			}
		}
		if field.Select != nil {
			options := make(map[string]string, len(field.Select.Options))
			for _, option := range field.Select.Options {
				optionName := enumName(option.Value)
				if owner, exists := options[optionName]; exists {
					return fmt.Errorf("select options %q and %q produce duplicate GraphQL enum name %q", owner, option.Value, optionName)
				}
				options[optionName] = option.Value
			}
		}
	}
	return nil
}

func (builder *schemaBuilder) whereInput(current resource) *enginegraphql.InputObject {
	if existing := builder.where[current.ID]; existing != nil {
		return existing
	}
	name := current.name + "Where"
	var input *enginegraphql.InputObject
	input = enginegraphql.NewInputObject(enginegraphql.InputObjectConfig{Name: name, Fields: enginegraphql.InputObjectConfigFieldMapThunk(func() enginegraphql.InputObjectConfigFieldMap {
		result := enginegraphql.InputObjectConfigFieldMap{
			"and": &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.NewList(enginegraphql.NewNonNull(input))},
			"or":  &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.NewList(enginegraphql.NewNonNull(input))},
			"not": &enginegraphql.InputObjectFieldConfig{Type: input},
			"AND": &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.NewList(enginegraphql.NewNonNull(input))},
			"OR":  &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.NewList(enginegraphql.NewNonNull(input))},
			"NOT": &enginegraphql.InputObjectFieldConfig{Type: input},
			"id":  &enginegraphql.InputObjectFieldConfig{Type: builder.stringOperators(current.name + "IDWhere")},
		}
		for _, candidate := range flattenWhereFields(current.Fields) {
			field := candidate.field
			var fieldType enginegraphql.Input
			typeBase := current.name + typeName(strings.ReplaceAll(candidate.path, ".", " ")) + "Where"
			switch field.Type {
			case schema.FieldTypeNumber:
				fieldType = builder.numberOperators(typeBase)
			case schema.FieldTypeTextList:
				fieldType = builder.primitiveListOperators(typeBase, enginegraphql.String)
			case schema.FieldTypeNumberList:
				fieldType = builder.primitiveListOperators(typeBase, enginegraphql.Float)
			case schema.FieldTypeCheckbox:
				fieldType = builder.booleanOperators(typeBase)
			case schema.FieldTypeSelect, schema.FieldTypeRadio:
				option := builder.selectEnum(current.name+typeName(strings.Join(field.Path.Segments()[:len(field.Path.Segments())-1], " ")), field)
				if field.Type == schema.FieldTypeSelect && field.Select != nil && field.Select.HasMany {
					fieldType = builder.multiSelectOperators(typeBase, option)
				} else {
					fieldType = builder.enumOperators(typeBase, option)
				}
			case schema.FieldTypeText, schema.FieldTypeTextarea, schema.FieldTypeEmail, schema.FieldTypeCode, schema.FieldTypeDate, schema.FieldTypeRelationship, schema.FieldTypeUpload:
				fieldType = builder.stringOperators(typeBase)
			}
			if fieldType != nil {
				result[candidate.name] = &enginegraphql.InputObjectFieldConfig{Type: fieldType}
			}
		}
		return result
	})})
	builder.where[current.ID] = input
	return input
}

func (builder *schemaBuilder) enumOperators(name string, value *enginegraphql.Enum) *enginegraphql.InputObject {
	return enginegraphql.NewInputObject(enginegraphql.InputObjectConfig{Name: name, Fields: enginegraphql.InputObjectConfigFieldMap{
		"equals": &enginegraphql.InputObjectFieldConfig{Type: value}, "not_equals": &enginegraphql.InputObjectFieldConfig{Type: value},
		"in": &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.NewList(enginegraphql.NewNonNull(value))}, "not_in": &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.NewList(enginegraphql.NewNonNull(value))},
		"exists": &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.Boolean},
	}})
}

func (builder *schemaBuilder) multiSelectOperators(name string, value *enginegraphql.Enum) *enginegraphql.InputObject {
	return enginegraphql.NewInputObject(enginegraphql.InputObjectConfig{Name: name, Fields: enginegraphql.InputObjectConfigFieldMap{
		"contains": &enginegraphql.InputObjectFieldConfig{Type: value},
		"exists":   &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.Boolean},
	}})
}

func (builder *schemaBuilder) primitiveListOperators(name string, value enginegraphql.Input) *enginegraphql.InputObject {
	return enginegraphql.NewInputObject(enginegraphql.InputObjectConfig{Name: name, Fields: enginegraphql.InputObjectConfigFieldMap{
		"in":     {Type: enginegraphql.NewList(enginegraphql.NewNonNull(value))},
		"not_in": {Type: enginegraphql.NewList(enginegraphql.NewNonNull(value))},
		"exists": {Type: enginegraphql.Boolean},
	}})
}

func (builder *schemaBuilder) stringOperators(name string) *enginegraphql.InputObject {
	return enginegraphql.NewInputObject(enginegraphql.InputObjectConfig{Name: name, Fields: enginegraphql.InputObjectConfigFieldMap{
		"equals": &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.String}, "not_equals": &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.String},
		"in": &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.NewList(enginegraphql.NewNonNull(enginegraphql.String))}, "not_in": &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.NewList(enginegraphql.NewNonNull(enginegraphql.String))},
		"contains": &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.String}, "like": &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.String},
		"greater_than": &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.String}, "greater_than_equal": &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.String},
		"less_than": &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.String}, "less_than_equal": &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.String},
		"exists": &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.Boolean},
	}})
}

func (builder *schemaBuilder) numberOperators(name string) *enginegraphql.InputObject {
	return enginegraphql.NewInputObject(enginegraphql.InputObjectConfig{Name: name, Fields: enginegraphql.InputObjectConfigFieldMap{
		"equals": &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.Float}, "not_equals": &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.Float},
		"in": &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.NewList(enginegraphql.NewNonNull(enginegraphql.Float))}, "not_in": &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.NewList(enginegraphql.NewNonNull(enginegraphql.Float))},
		"greater_than": &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.Float}, "greater_than_equal": &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.Float},
		"less_than": &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.Float}, "less_than_equal": &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.Float},
		"exists": &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.Boolean},
	}})
}

func (builder *schemaBuilder) booleanOperators(name string) *enginegraphql.InputObject {
	return enginegraphql.NewInputObject(enginegraphql.InputObjectConfig{Name: name, Fields: enginegraphql.InputObjectConfigFieldMap{
		"equals": &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.Boolean}, "not_equals": &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.Boolean}, "exists": &enginegraphql.InputObjectFieldConfig{Type: enginegraphql.Boolean},
	}})
}

func (builder *schemaBuilder) pageType(current resource, object *enginegraphql.Object) *enginegraphql.Object {
	return enginegraphql.NewObject(enginegraphql.ObjectConfig{Name: current.plural + "Page", Fields: enginegraphql.Fields{
		"docs": &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.NewList(enginegraphql.NewNonNull(object)))},
		"page": &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.Int)}, "limit": &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.Int)},
		"totalDocs": &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.Int)}, "totalPages": &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.Int)},
		"hasNextPage": &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.Boolean)}, "hasPrevPage": &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.Boolean)},
		"nextPage": &enginegraphql.Field{Type: enginegraphql.Int}, "prevPage": &enginegraphql.Field{Type: enginegraphql.Int}, "pagingCounter": &enginegraphql.Field{Type: enginegraphql.NewNonNull(enginegraphql.Int)},
	}})
}

func (builder *schemaBuilder) readLocaleArgs() enginegraphql.FieldConfigArgument {
	result := enginegraphql.FieldConfigArgument{"draft": &enginegraphql.ArgumentConfig{Type: enginegraphql.Boolean}, "trash": &enginegraphql.ArgumentConfig{Type: enginegraphql.Boolean}}
	return mergeArgs(result, builder.writeLocaleArgs())
}

func (builder *schemaBuilder) writeLocaleArgs() enginegraphql.FieldConfigArgument {
	if builder.locale == nil {
		return enginegraphql.FieldConfigArgument{}
	}
	return enginegraphql.FieldConfigArgument{
		"locale":          &enginegraphql.ArgumentConfig{Type: builder.locale},
		"fallbackLocale":  &enginegraphql.ArgumentConfig{Type: enginegraphql.NewList(enginegraphql.NewNonNull(builder.locale))},
		"disableFallback": &enginegraphql.ArgumentConfig{Type: enginegraphql.Boolean},
	}
}

func (builder *schemaBuilder) reserveType(name, owner string) error {
	if !graphQLNamePattern.MatchString(name) || strings.HasPrefix(name, "__") {
		return fmt.Errorf("%s produces invalid GraphQL name %q", owner, name)
	}
	if _, exists := builder.types[name]; exists {
		return fmt.Errorf("%s produces duplicate GraphQL type name %q", owner, name)
	}
	builder.types[name] = struct{}{}
	return nil
}

func addRootField(fields enginegraphql.Fields, name string, field *enginegraphql.Field) error {
	if !graphQLNamePattern.MatchString(name) || strings.HasPrefix(name, "__") {
		return fmt.Errorf("invalid GraphQL root field name %q", name)
	}
	if _, exists := fields[name]; exists {
		return fmt.Errorf("duplicate GraphQL root field name %q", name)
	}
	fields[name] = field
	return nil
}

func mergeArgs(groups ...enginegraphql.FieldConfigArgument) enginegraphql.FieldConfigArgument {
	result := enginegraphql.FieldConfigArgument{}
	for _, group := range groups {
		for name, value := range group {
			result[name] = value
		}
	}
	return result
}

func typeName(value string) string {
	words := strings.FieldsFunc(value, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	var result strings.Builder
	for _, word := range words {
		runes := []rune(word)
		if len(runes) == 0 {
			continue
		}
		result.WriteRune(unicode.ToUpper(runes[0]))
		result.WriteString(string(runes[1:]))
	}
	name := result.String()
	if name == "" || unicode.IsDigit([]rune(name)[0]) {
		name = "Ridu" + name
	}
	return name
}

func fieldName(value string) string {
	name := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			return r
		}
		return '_'
	}, value)
	if name == "" || unicode.IsDigit([]rune(name)[0]) {
		name = "_" + name
	}
	return name
}

func enumName(value string) string { return strings.ToUpper(fieldName(value)) }

func sortArgs(args map[string]interface{}) ([]query.Sort, error) {
	raw, _ := args["sort"].([]interface{})
	result := make([]query.Sort, 0, len(raw))
	for _, item := range raw {
		value := fmt.Sprint(item)
		direction := query.Ascending
		if strings.HasPrefix(value, "-") {
			direction = query.Descending
			value = strings.TrimPrefix(value, "-")
		}
		path, err := query.ParsePath(value)
		if err != nil {
			return nil, fmt.Errorf("invalid sort field %q: %w", value, err)
		}
		term, err := query.NewSort(path, direction)
		if err != nil {
			return nil, err
		}
		result = append(result, term)
	}
	return result, nil
}

func sortedKeys(values map[string]interface{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
