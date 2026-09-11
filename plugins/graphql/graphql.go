// Package graphql provides Ridu's optional, manifest-derived GraphQL transport.
// Applications that do not import this package do not link a GraphQL runtime.
package graphql

import (
	"context"
	"fmt"
	"net/http"

	enginegraphql "github.com/graphql-go/graphql"
	"github.com/riducms/ridu"
	riduschema "github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

const Key = "graphql"

const schemaArtifactName = "schema"

// Options controls the exact GraphQL route and resource safeguards. Zero
// values select secure defaults.
type Options struct {
	Path               string
	MaxBodyBytes       int64
	MaxVariableBytes   int
	MaxDepth           int
	MaxAliases         int
	MaxComplexity      int
	MaxListLimit       int
	AllowIntrospection bool
	// Resources optionally renames or suppresses generated surfaces by
	// collection/global slug. Omitted resources use their manifest labels and
	// expose both queries and mutations.
	Resources       map[string]ResourceOptions
	Queries         []ExtensionField
	Mutations       []ExtensionField
	ValidationRules []enginegraphql.ValidationRuleFn
}

// ResourceOptions controls one collection or global's generated GraphQL
// surface without changing its manifest-owned slug or REST contract.
type ResourceOptions struct {
	SingularName     string
	PluralName       string
	DisableQueries   bool
	DisableMutations bool
}

// ExtensionField adds one trusted compiled root field. Resolvers receive the
// authenticated actor and Ridu's public APIs, not a store adapter.
type ExtensionField struct {
	Name        string
	Description string
	Type        enginegraphql.Output
	Args        enginegraphql.FieldConfigArgument
	Cost        int
	Resolve     ExtensionResolver
}

// ExtensionResolver executes one custom query or mutation.
type ExtensionResolver func(ExtensionContext) (interface{}, error)

// ExtensionContext is scoped to one GraphQL field execution.
type ExtensionContext struct {
	Context         context.Context
	Args            map[string]interface{}
	Info            enginegraphql.ResolveInfo
	Actor           *store.Document
	ActorCollection riduschema.CollectionSlug
	Local           *ridu.LocalAPI
	App             *ridu.App
}

type plugin struct{ options Options }

// New enables GraphQL for an application. At most one Options value is used.
func New(options ...Options) ridu.Plugin {
	selected := Options{}
	if len(options) != 0 {
		selected = options[len(options)-1]
	}
	return &plugin{options: normalizedOptions(selected)}
}

func (*plugin) Key() string { return Key }

func (*plugin) Descriptor() ridu.PluginDescriptor {
	return ridu.PluginDescriptor{
		Version: ridu.FrameworkVersion, GoPackage: "github.com/riducms/ridu/plugins/graphql",
		APIVersion: ridu.PluginAPIVersion,
		Ridu:       ridu.RiduCompatibility{Minimum: ridu.FrameworkVersion},
	}
}

func (plugin *plugin) GeneratedArtifacts(ctx ridu.PluginGenerationContext) ([]ridu.PluginGeneratedArtifact, error) {
	sdl, err := GenerateSDL(ctx.Manifest, plugin.options)
	if err != nil {
		return nil, err
	}
	return []ridu.PluginGeneratedArtifact{{Name: schemaArtifactName, Content: []byte(sdl)}}, nil
}

func (plugin *plugin) BindTransports(ctx ridu.PluginTransportContext) ([]ridu.Endpoint, error) {
	executable, err := buildExecutable(ctx.Manifest.Snapshot(), ctx.Local, ctx.App, plugin.options)
	if err != nil {
		return nil, err
	}
	return []ridu.Endpoint{{
		Method: http.MethodPost, Path: plugin.options.Path, Summary: "Execute a GraphQL operation",
		MaxBodyBytes: plugin.options.MaxBodyBytes,
		Handler:      executable.serve,
	}}, nil
}

func normalizedOptions(options Options) Options {
	if options.Path == "" {
		options.Path = "/api/graphql"
	}
	if options.MaxBodyBytes <= 0 {
		options.MaxBodyBytes = 1 << 20
	}
	if options.MaxVariableBytes <= 0 {
		options.MaxVariableBytes = 256 << 10
	}
	if options.MaxDepth <= 0 {
		options.MaxDepth = 12
	}
	if options.MaxAliases <= 0 {
		options.MaxAliases = 100
	}
	if options.MaxComplexity <= 0 {
		options.MaxComplexity = 1000
	}
	if options.MaxListLimit <= 0 {
		options.MaxListLimit = 100
	}
	if options.Resources != nil {
		resources := make(map[string]ResourceOptions, len(options.Resources))
		for slug, resource := range options.Resources {
			resources[slug] = resource
		}
		options.Resources = resources
	}
	options.Queries = cloneExtensionFields(options.Queries)
	options.Mutations = cloneExtensionFields(options.Mutations)
	options.ValidationRules = append([]enginegraphql.ValidationRuleFn(nil), options.ValidationRules...)
	return options
}

func cloneExtensionFields(fields []ExtensionField) []ExtensionField {
	if fields == nil {
		return nil
	}
	cloned := append([]ExtensionField(nil), fields...)
	for index := range cloned {
		if cloned[index].Args == nil {
			continue
		}
		arguments := make(enginegraphql.FieldConfigArgument, len(cloned[index].Args))
		for name, argument := range cloned[index].Args {
			arguments[name] = argument
		}
		cloned[index].Args = arguments
	}
	return cloned
}

type executable struct {
	schema  enginegraphql.Schema
	local   *ridu.LocalAPI
	app     *ridu.App
	options Options
}

func buildExecutable(snapshot riduschema.Snapshot, local *ridu.LocalAPI, app *ridu.App, options Options) (*executable, error) {
	if local == nil {
		return nil, fmt.Errorf("GraphQL requires a local API")
	}
	builder := newSchemaBuilder(snapshot, local, app, options)
	schema, err := builder.build()
	if err != nil {
		return nil, err
	}
	return &executable{schema: schema, local: local, app: app, options: options}, nil
}

var _ ridu.DescriptorProvider = (*plugin)(nil)
var _ ridu.GenerationProvider = (*plugin)(nil)
var _ ridu.TransportProvider = (*plugin)(nil)
