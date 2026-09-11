// Package mcp exposes explicitly selected Ridu content reads through Model
// Context Protocol without bypassing the ordinary authorization and operation
// engine. It is optional compiled application code, not part of Ridu core.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

const (
	// Key is the stable compiled plugin identity.
	Key         = "mcp"
	defaultPath = "/api/mcp"
)

var toolNameCharacters = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

// Resource opts one manifest collection or global into MCP discovery.
// Description overrides manifest presentation text when clients need more
// task-specific guidance.
type Resource struct {
	Slug        schema.CollectionSlug
	Description string
}

// Config controls the bounded, read-only MCP surface. Omitted resources are
// not exposed even when the authenticated actor could read them over REST.
type Config struct {
	Path         string
	MaxBodyBytes int64
	DefaultLimit int
	MaxLimit     int
	Collections  []Resource
	Globals      []Resource
}

// Plugin is one configured MCP transport.
type Plugin struct{ config Config }

// New enables MCP with defensive copies of all resource declarations.
func New(config Config) *Plugin {
	config.Collections = append([]Resource(nil), config.Collections...)
	config.Globals = append([]Resource(nil), config.Globals...)
	return &Plugin{config: normalizedConfig(config)}
}

func (*Plugin) Key() string { return Key }

func (*Plugin) Descriptor() ridu.PluginDescriptor {
	return ridu.PluginDescriptor{
		Version: ridu.FrameworkVersion, GoPackage: "github.com/riducms/ridu/plugins/mcp",
		APIVersion: ridu.PluginAPIVersion,
		Ridu:       ridu.RiduCompatibility{Minimum: ridu.FrameworkVersion},
	}
}

func (plugin *Plugin) BindTransports(binding ridu.PluginTransportContext) ([]ridu.Endpoint, error) {
	if binding.Local == nil {
		return nil, fmt.Errorf("MCP requires a local API")
	}
	snapshot := binding.Manifest.Snapshot()
	server, err := plugin.server(snapshot)
	if err != nil {
		return nil, err
	}
	handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, &mcpsdk.StreamableHTTPOptions{
		Stateless:                    true,
		JSONResponse:                 true,
		MaxRequestBodyBytes:          plugin.config.MaxBodyBytes,
		PropagateRequestCancellation: true,
	})

	return []ridu.Endpoint{{
		Method: http.MethodPost, Path: plugin.config.Path, Summary: "Execute an authenticated Ridu MCP request",
		MaxBodyBytes: plugin.config.MaxBodyBytes,
		Handler: func(endpoint ridu.EndpointContext) {
			if endpoint.Actor == nil {
				endpoint.Writer.Header().Set("WWW-Authenticate", `Bearer realm="ridu-mcp"`)
				http.Error(endpoint.Writer, "MCP authentication required", http.StatusUnauthorized)
				return
			}
			state := callState{
				local: endpoint.Local, actor: store.CloneDocument(*endpoint.Actor), actorCollection: endpoint.ActorCollection,
				reportError: endpoint.ReportError,
			}
			request := endpoint.Request.WithContext(context.WithValue(endpoint.Request.Context(), callStateKey{}, state))
			handler.ServeHTTP(endpoint.Writer, request)
		},
	}}, nil
}

func normalizedConfig(config Config) Config {
	if strings.TrimSpace(config.Path) == "" {
		config.Path = defaultPath
	}
	if config.MaxBodyBytes <= 0 {
		config.MaxBodyBytes = 1 << 20
	}
	if config.DefaultLimit <= 0 {
		config.DefaultLimit = 20
	}
	if config.MaxLimit <= 0 {
		config.MaxLimit = 100
	}
	return config
}

func (plugin *Plugin) server(snapshot schema.Snapshot) (*mcpsdk.Server, error) {
	if !strings.HasPrefix(plugin.config.Path, "/api/") || strings.HasSuffix(plugin.config.Path, "/") {
		return nil, fmt.Errorf("MCP path %q must be an absolute /api path without a trailing slash", plugin.config.Path)
	}
	if plugin.config.DefaultLimit > plugin.config.MaxLimit {
		return nil, fmt.Errorf("MCP default limit %d exceeds maximum %d", plugin.config.DefaultLimit, plugin.config.MaxLimit)
	}

	server := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name: "ridu", Title: snapshot.Application.Name + " MCP", Version: ridu.FrameworkVersion,
		Description: "Access-controlled content reads from a Ridu application.",
	}, &mcpsdk.ServerOptions{
		Instructions: "Use only the explicitly exposed read-only tools. Ridu applies the authenticated user's collection, document, and field access rules to every result.",
		Capabilities: &mcpsdk.ServerCapabilities{},
	})

	toolNames := make(map[string]string)
	collections := collectionsBySlug(snapshot.Collections)
	for index, resource := range plugin.config.Collections {
		collection, exists := collections[resource.Slug]
		if !exists {
			return nil, fmt.Errorf("MCP collections[%d] refers to unknown collection %q", index, resource.Slug)
		}
		name := toolName("find", "collection", resource.Slug)
		if err := reserveToolName(toolNames, name, "collection "+string(resource.Slug)); err != nil {
			return nil, err
		}
		plugin.addCollectionTool(server, name, collection, resource.Description)
	}
	globals := collectionsBySlug(snapshot.Globals)
	for index, resource := range plugin.config.Globals {
		global, exists := globals[resource.Slug]
		if !exists {
			return nil, fmt.Errorf("MCP globals[%d] refers to unknown global %q", index, resource.Slug)
		}
		name := toolName("find", "global", resource.Slug)
		if err := reserveToolName(toolNames, name, "global "+string(resource.Slug)); err != nil {
			return nil, err
		}
		plugin.addGlobalTool(server, name, global, resource.Description)
	}
	return server, nil
}

func collectionsBySlug(collections []schema.Collection) map[schema.CollectionSlug]schema.Collection {
	result := make(map[schema.CollectionSlug]schema.Collection, len(collections))
	for _, collection := range collections {
		result[collection.Slug] = collection
	}
	return result
}

func toolName(operation, kind string, slug schema.CollectionSlug) string {
	cleaned := toolNameCharacters.ReplaceAllString(string(slug), "_")
	return "ridu_" + operation + "_" + kind + "_" + cleaned
}

func reserveToolName(names map[string]string, name, owner string) error {
	if previous, exists := names[name]; exists {
		return fmt.Errorf("MCP tool name %q collides between %s and %s", name, previous, owner)
	}
	names[name] = owner
	return nil
}

type collectionInput struct {
	Page            int      `json:"page,omitempty" jsonschema:"One-based result page; defaults to 1."`
	Limit           int      `json:"limit,omitempty" jsonschema:"Maximum documents returned; bounded by the server."`
	Select          []string `json:"select,omitempty" jsonschema:"Optional canonical field paths to return."`
	Locale          string   `json:"locale,omitempty" jsonschema:"Optional configured content locale."`
	DisableFallback bool     `json:"disableFallback,omitempty" jsonschema:"Require exact localized values without fallbacks."`
	AllLocales      bool     `json:"allLocales,omitempty" jsonschema:"Return locale-keyed values for localized fields."`
	Draft           *bool    `json:"draft,omitempty" jsonschema:"Include drafts when true or require published documents when false."`
}

type globalInput struct {
	Select          []string `json:"select,omitempty" jsonschema:"Optional canonical field paths to return."`
	Locale          string   `json:"locale,omitempty" jsonschema:"Optional configured content locale."`
	DisableFallback bool     `json:"disableFallback,omitempty" jsonschema:"Require exact localized values without fallbacks."`
	AllLocales      bool     `json:"allLocales,omitempty" jsonschema:"Return locale-keyed values for localized fields."`
	Draft           *bool    `json:"draft,omitempty" jsonschema:"Include a draft when true or require the published value when false."`
}

type documentOutput struct {
	ID                  string                       `json:"id"`
	CreatedAt           time.Time                    `json:"createdAt"`
	UpdatedAt           time.Time                    `json:"updatedAt"`
	Status              store.Status                 `json:"_status,omitempty"`
	Revision            int                          `json:"_revision,omitempty"`
	Values              map[string]any               `json:"values"`
	LocalizationSources map[string]schema.LocaleCode `json:"localizationSources,omitempty"`
}

type collectionOutput struct {
	Documents []documentOutput `json:"docs"`
	Page      int              `json:"page"`
	Limit     int              `json:"limit"`
	Total     int              `json:"totalDocs"`
	HasNext   bool             `json:"hasNextPage"`
	HasPrev   bool             `json:"hasPrevPage"`
}

type globalOutput struct {
	Document documentOutput `json:"document"`
}

func (plugin *Plugin) addCollectionTool(server *mcpsdk.Server, name string, collection schema.Collection, configuredDescription string) {
	description := strings.TrimSpace(configuredDescription)
	if description == "" {
		description = strings.TrimSpace(collection.Admin.Description)
	}
	if description == "" {
		description = "List access-visible " + collection.Labels.Plural + "."
	}
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name: name, Title: "Find " + collection.Labels.Plural, Description: description,
		Annotations: &mcpsdk.ToolAnnotations{Title: "Find " + collection.Labels.Plural, ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: boolPointer(false)},
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, input collectionInput) (*mcpsdk.CallToolResult, collectionOutput, error) {
		state, err := requestCallState(ctx)
		if err != nil {
			return nil, collectionOutput{}, err
		}
		limit, err := plugin.limit(input.Limit)
		if err != nil {
			return nil, collectionOutput{}, err
		}
		selectPaths, err := parseSelect(input.Select)
		if err != nil {
			return nil, collectionOutput{}, err
		}
		page, err := state.local.List(ctx, string(collection.Slug), ridu.ListOptions{
			Page: input.Page, Limit: limit, Select: selectPaths, Draft: input.Draft,
			Actor: &state.actor, ActorCollection: state.actorCollection,
			Locale: schema.LocaleCode(input.Locale), DisableFallback: input.DisableFallback, AllLocales: input.AllLocales,
		})
		if err != nil {
			return nil, collectionOutput{}, state.publicError(err)
		}
		documents := make([]documentOutput, len(page.Documents))
		for index, document := range page.Documents {
			documents[index], err = outputDocument(document)
			if err != nil {
				return nil, collectionOutput{}, state.publicError(err)
			}
		}
		return nil, collectionOutput{
			Documents: documents, Page: page.Page, Limit: page.Limit, Total: page.Total,
			HasNext: page.Page*page.Limit < page.Total, HasPrev: page.Page > 1,
		}, nil
	})
}

func (plugin *Plugin) addGlobalTool(server *mcpsdk.Server, name string, global schema.Global, configuredDescription string) {
	description := strings.TrimSpace(configuredDescription)
	if description == "" {
		description = strings.TrimSpace(global.Admin.Description)
	}
	if description == "" {
		description = "Read the access-visible " + global.Labels.Singular + " global."
	}
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name: name, Title: "Read " + global.Labels.Singular, Description: description,
		Annotations: &mcpsdk.ToolAnnotations{Title: "Read " + global.Labels.Singular, ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: boolPointer(false)},
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, input globalInput) (*mcpsdk.CallToolResult, globalOutput, error) {
		state, err := requestCallState(ctx)
		if err != nil {
			return nil, globalOutput{}, err
		}
		selectPaths, err := parseSelect(input.Select)
		if err != nil {
			return nil, globalOutput{}, err
		}
		document, err := state.local.Global(ctx, string(global.Slug), ridu.FindOptions{
			Select: selectPaths, Draft: input.Draft, Actor: &state.actor, ActorCollection: state.actorCollection,
			Locale: schema.LocaleCode(input.Locale), DisableFallback: input.DisableFallback, AllLocales: input.AllLocales,
		})
		if err != nil {
			return nil, globalOutput{}, state.publicError(err)
		}
		output, err := outputDocument(document)
		if err != nil {
			return nil, globalOutput{}, state.publicError(err)
		}
		return nil, globalOutput{Document: output}, nil
	})
}

func (plugin *Plugin) limit(requested int) (int, error) {
	if requested < 0 {
		return 0, fmt.Errorf("limit must not be negative")
	}
	if requested == 0 {
		return plugin.config.DefaultLimit, nil
	}
	if requested > plugin.config.MaxLimit {
		return 0, fmt.Errorf("limit must not exceed %d", plugin.config.MaxLimit)
	}
	return requested, nil
}

func parseSelect(encoded []string) ([]query.Path, error) {
	if encoded == nil {
		return nil, nil
	}
	paths := make([]query.Path, len(encoded))
	for index, value := range encoded {
		path, err := query.ParsePath(value)
		if err != nil {
			return nil, fmt.Errorf("select[%d]: %w", index, err)
		}
		paths[index] = path
	}
	return paths, nil
}

func outputDocument(document store.Document) (documentOutput, error) {
	values := make(map[string]any, len(document.Values))
	encoded, err := json.Marshal(document.Values)
	if err != nil {
		return documentOutput{}, fmt.Errorf("encode MCP document %q: %w", document.ID, err)
	}
	if err := json.Unmarshal(encoded, &values); err != nil {
		return documentOutput{}, fmt.Errorf("normalize MCP document %q: %w", document.ID, err)
	}
	return documentOutput{
		ID: document.ID, CreatedAt: document.CreatedAt, UpdatedAt: document.UpdatedAt,
		Status: document.Status, Revision: document.Revision, Values: values,
		LocalizationSources: cloneLocalizationSources(document.LocalizationSources),
	}, nil
}

func cloneLocalizationSources(sources map[string]schema.LocaleCode) map[string]schema.LocaleCode {
	if sources == nil {
		return nil
	}
	cloned := make(map[string]schema.LocaleCode, len(sources))
	for path, locale := range sources {
		cloned[path] = locale
	}
	return cloned
}

type callStateKey struct{}

type callState struct {
	local           *ridu.LocalAPI
	actor           store.Document
	actorCollection schema.CollectionSlug
	reportError     func(error, string)
}

func requestCallState(ctx context.Context) (callState, error) {
	state, ok := ctx.Value(callStateKey{}).(callState)
	if !ok || state.local == nil || state.actor.ID == "" {
		return callState{}, fmt.Errorf("MCP request authentication context is unavailable")
	}
	return state, nil
}

func (state callState) publicError(err error) error {
	var operationError *ridu.OperationError
	if errors.As(err, &operationError) {
		return fmt.Errorf("%s: %s", operationError.Code, operationError.Message)
	}
	if state.reportError != nil {
		state.reportError(err, "mcp_operation")
	}
	return fmt.Errorf("content operation failed")
}

func boolPointer(value bool) *bool { return &value }

var _ ridu.DescriptorProvider = (*Plugin)(nil)
var _ ridu.TransportProvider = (*Plugin)(nil)
