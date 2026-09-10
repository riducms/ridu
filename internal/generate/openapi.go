package generate

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/riducms/ridu/internal/blocktypes"
	"github.com/riducms/ridu/schema"
)

type openAPIDocument struct {
	BlockRegistry []schema.BlockType        `json:"x-ridu-blocks,omitempty"`
	OpenAPI       string                    `json:"openapi"`
	Info          openAPIInfo               `json:"info"`
	Paths         map[string]map[string]any `json:"paths"`
	Components    openAPIComponents         `json:"components"`
}

type openAPIInfo struct {
	Title   string `json:"title"`
	Version string `json:"version"`
}

type openAPIComponents struct {
	Schemas map[string]openAPISchema `json:"schemas"`
}

type openAPISchema struct {
	Type                 string         `json:"type,omitempty"`
	AllOf                []any          `json:"allOf,omitempty"`
	Properties           map[string]any `json:"properties,omitempty"`
	Required             []string       `json:"required,omitempty"`
	Description          string         `json:"description,omitempty"`
	AdditionalProperties *bool          `json:"additionalProperties,omitempty"`
}

type openAPIProperty struct {
	Type     string   `json:"type,omitempty"`
	Format   string   `json:"format,omitempty"`
	Nullable bool     `json:"nullable,omitempty"`
	Enum     []string `json:"enum,omitempty"`
	Ref      string   `json:"$ref,omitempty"`
}

func openAPI(manifest schema.Manifest) ([]byte, error) {
	snapshot := manifest.Snapshot()
	catalog, err := blocktypes.Build(snapshot)
	if err != nil {
		return nil, err
	}
	generator := openAPIBlockGenerator{blocks: catalog}
	document := openAPIDocument{
		BlockRegistry: snapshot.Blocks,
		OpenAPI:       "3.1.0",
		Info:          openAPIInfo{Title: snapshot.Application.Name + " API", Version: fmt.Sprintf("schema-%d", snapshot.Version)},
		Paths:         make(map[string]map[string]any),
		Components:    openAPIComponents{Schemas: make(map[string]openAPISchema)},
	}
	addCollectionSelectionSchemas(document.Components.Schemas)
	addTransportSchemas(document.Components.Schemas)
	addLiveValidationSchemas(document.Components.Schemas)
	pluginTypes := make(map[string]json.RawMessage)
	hasAPIKeys := false
	for _, plugin := range snapshot.Plugins {
		for _, fieldType := range plugin.FieldTypes {
			pluginTypes[fieldType.Key] = fieldType.JSONSchema
		}
		for _, endpoint := range plugin.Endpoints {
			path := "/api/plugins/" + plugin.Key + "/" + endpoint.Path
			if document.Paths[path] == nil {
				document.Paths[path] = make(map[string]any)
			}
			document.Paths[path][strings.ToLower(endpoint.Method)] = operationSpec(endpoint.Summary, plugin.Key+"-"+strings.ToLower(endpoint.Method)+"-"+strings.ReplaceAll(endpoint.Path, "/", "-"), "200")
		}
	}
	if err := generator.addEmbeddedSchemas(document.Components.Schemas, snapshot, pluginTypes); err != nil {
		return nil, err
	}
	if err := generator.addBlockSchemas(document.Components.Schemas, pluginTypes, snapshot.Application.Localization != nil); err != nil {
		return nil, err
	}
	for _, collection := range snapshot.Collections {
		slug := string(collection.Slug)
		name := string(collection.ID)
		localeParameters := localizationParameters(snapshot.Application.Localization, collection.Fields)
		document.Paths["/api/collections/"+slug+"/validate"] = map[string]any{"post": liveValidationOperation(name, snapshot.Application.Localization, false)}
		listSpec := operationSpec("List "+collection.Labels.Plural, "list"+name, "200")
		listSpec["parameters"] = append(depthParameters(), localeParameters...)
		createSpec := operationSpec("Create "+collection.Labels.Singular, "create"+name, "201")
		createSpec["parameters"] = append(createDraftParameters(collection), localeParameters...)
		document.Paths["/api/collections/"+slug] = map[string]any{
			"get": listSpec, "post": createSpec,
		}
		findSpec := operationSpec("Find "+collection.Labels.Singular, "find"+name, "200")
		findSpec["parameters"] = append(depthParameters(), localeParameters...)
		updateSpec := operationSpec("Update "+collection.Labels.Singular, "update"+name, "200")
		updateSpec["parameters"] = localeParameters
		deleteSpec := operationSpec("Delete "+collection.Labels.Singular, "delete"+name, "200")
		deleteSpec["parameters"] = localeParameters
		document.Paths["/api/collections/"+slug+"/{id}"] = map[string]any{
			"parameters": []map[string]any{{
				"name": "id", "in": "path", "required": true,
				"schema": map[string]string{"type": "string"},
			}},
			"get":    findSpec,
			"patch":  updateSpec,
			"delete": deleteSpec,
		}
		bulkSpec := operationSpec("Bulk mutate "+collection.Labels.Plural, "bulk"+name, "200")
		bulkSpec["parameters"] = localeParameters
		document.Paths["/api/collections/"+slug+"/bulk"] = map[string]any{"post": bulkSpec}
		selectionSpec := collectionSelectionOperationSpec(collection.Labels.Plural, name)
		selectionSpec["parameters"] = localeParameters
		document.Paths["/api/access/collections/"+slug+"/selection"] = map[string]any{
			"post": selectionSpec,
		}
		duplicateSpec := operationSpec("Duplicate "+collection.Labels.Singular, "duplicate"+name, "201")
		duplicateSpec["parameters"] = localeParameters
		document.Paths["/api/collections/"+slug+"/{id}/duplicate"] = map[string]any{"post": duplicateSpec}
		copyLocaleSpec := operationSpec("Copy localized values between locales", "copyLocale"+name, "200")
		document.Paths["/api/collections/"+slug+"/{id}/copy-locale"] = map[string]any{"post": copyLocaleSpec}
		for _, field := range collection.Fields {
			if field.Type != schema.FieldTypeJoin {
				continue
			}
			document.Components.Schemas["JoinMutationInput"] = openAPISchema{
				Type: "object",
				Properties: map[string]any{
					"additions": map[string]any{"type": "array", "items": map[string]string{"type": "string"}},
					"removals":  map[string]any{"type": "array", "items": map[string]string{"type": "string"}},
				},
				Required: []string{"additions", "removals"},
			}
			responseSchema := "JoinMutation" + name
			document.Components.Schemas[responseSchema] = openAPISchema{
				Type: "object",
				Properties: map[string]any{
					"doc":     map[string]string{"$ref": "#/components/schemas/" + name},
					"added":   map[string]string{"type": "integer"},
					"removed": map[string]string{"type": "integer"},
				},
				Required: []string{"doc", "added", "removed"},
			}
			joinSpec := joinMutationOperationSpec(collection.Labels.Singular, name, responseSchema)
			joinSpec["parameters"] = localeParameters
			document.Paths["/api/collections/"+slug+"/{id}/joins/{field}"] = map[string]any{
				"parameters": []map[string]any{
					{"name": "id", "in": "path", "required": true, "schema": map[string]string{"type": "string"}},
					{"name": "field", "in": "path", "required": true, "schema": map[string]string{"type": "string"}},
				},
				"patch": joinSpec,
			}
			break
		}
		if collection.Capabilities.Auth {
			document.Components.Schemas["AuthBootstrapEnvelope"] = openAPISchema{
				Type: "object", Properties: map[string]any{"available": map[string]string{"type": "boolean"}}, Required: []string{"available"},
			}
			bootstrapSpec := operationSpec("Check first-user setup availability for "+collection.Labels.Plural, "authBootstrap"+name, "200")
			bootstrapResponses := bootstrapSpec["responses"].(map[string]any)
			bootstrapResponses["200"] = map[string]any{
				"description": "Successful response",
				"content": map[string]any{
					"application/json": map[string]any{"schema": map[string]string{"$ref": "#/components/schemas/AuthBootstrapEnvelope"}},
				},
			}
			document.Paths["/api/auth/"+slug+"/bootstrap"] = map[string]any{
				"get": bootstrapSpec,
			}
			createAuthSpec := operationSpec("Create "+collection.Labels.Singular+" with credentials", "createAuth"+name, "201")
			createAuthSpec["parameters"] = localeParameters
			document.Paths["/api/auth/"+slug+"/create-user"] = map[string]any{
				"post": createAuthSpec,
			}
			document.Paths["/api/auth/"+slug+"/login"] = map[string]any{
				"post": operationSpec("Login to "+collection.Labels.Plural, "login"+name, "200"),
			}
			if collection.Auth.PasswordReset {
				document.Paths["/api/auth/"+slug+"/forgot-password"] = map[string]any{"post": operationSpec("Request a password reset", "requestPasswordReset"+name, "200")}
				document.Paths["/api/auth/"+slug+"/reset-password"] = map[string]any{"post": operationSpec("Reset a password", "resetPassword"+name, "200")}
			}
			if collection.Auth.VerifyEmail {
				document.Paths["/api/auth/"+slug+"/request-verification"] = map[string]any{"post": operationSpec("Request email verification", "requestVerification"+name, "200")}
				document.Paths["/api/auth/"+slug+"/verify"] = map[string]any{"post": operationSpec("Verify an email address", "verifyEmail"+name, "200")}
			}
			hasAPIKeys = hasAPIKeys || collection.Auth.APIKeys
			if collection.Auth.MaxLoginAttempts > 0 {
				document.Paths["/api/auth/"+slug+"/{id}/unlock"] = map[string]any{
					"parameters": []map[string]any{{"name": "id", "in": "path", "required": true, "schema": map[string]string{"type": "string"}}},
					"post":       operationSpec("Force unlock "+collection.Labels.Singular, "forceUnlock"+name, "200"),
				}
			}
		}
		if collection.Capabilities.Trash {
			emptyTrashSpec := operationSpec("Empty "+collection.Labels.Plural+" trash", "emptyTrash"+name, "200")
			emptyTrashSpec["parameters"] = append([]map[string]any{{
				"name": "trash", "in": "query", "required": true,
				"schema": map[string]any{"type": "boolean", "enum": []bool{true}},
			}}, localeParameters...)
			restoreDeletedSpec := operationSpec("Restore "+collection.Labels.Singular, "restoreDeleted"+name, "200")
			restoreDeletedSpec["parameters"] = localeParameters
			deletePermanentSpec := operationSpec("Permanently delete "+collection.Labels.Singular, "deletePermanent"+name, "200")
			deletePermanentSpec["parameters"] = localeParameters
			document.Paths["/api/collections/"+slug]["delete"] = emptyTrashSpec
			document.Paths["/api/collections/"+slug+"/{id}/restore-deleted"] = map[string]any{"post": restoreDeletedSpec}
			document.Paths["/api/collections/"+slug+"/{id}/permanent"] = map[string]any{"delete": deletePermanentSpec}
		}
		if collection.Upload != nil {
			remoteUploadSpec := operationSpec("Create "+collection.Labels.Singular+" from a public URL", "remoteUpload"+name, "201")
			remoteUploadSpec["parameters"] = localeParameters
			document.Paths["/api/collections/"+slug+"/remote-upload"] = map[string]any{
				"post": remoteUploadSpec,
			}
			document.Paths["/api/collections/"+slug+"/{id}/image"] = map[string]any{
				"patch": operationSpec("Regenerate "+collection.Labels.Singular+" image sizes", "updateUploadImage"+name, "200"),
			}
		}
		if collection.Versions != nil {
			versionBase := "/api/collections/" + slug + "/{id}"
			versionsSpec := operationSpec("List "+collection.Labels.Singular+" versions", "versions"+name, "200")
			versionsSpec["parameters"] = localeParameters
			document.Paths[versionBase+"/versions"] = map[string]any{"get": versionsSpec}
			versionSpec := operationSpec("Read "+collection.Labels.Singular+" version", "version"+name, "200")
			versionSpec["parameters"] = localeParameters
			document.Paths[versionBase+"/versions/{revision}"] = map[string]any{
				"parameters": []map[string]any{{"name": "revision", "in": "path", "required": true, "schema": map[string]string{"type": "integer"}}},
				"get":        versionSpec,
			}
			publishSpec := operationSpec("Publish "+collection.Labels.Singular, "publish"+name, "200")
			publishSpec["parameters"] = localeParameters
			unpublishSpec := operationSpec("Unpublish "+collection.Labels.Singular, "unpublish"+name, "200")
			unpublishSpec["parameters"] = localeParameters
			document.Paths[versionBase+"/publish"] = map[string]any{"post": publishSpec}
			document.Paths[versionBase+"/unpublish"] = map[string]any{"post": unpublishSpec}
			restoreSpec := operationSpec("Restore "+collection.Labels.Singular, "restore"+name, "200")
			restoreSpec["parameters"] = append(restoreVersionParameters(), localeParameters...)
			document.Paths[versionBase+"/restore/{revision}"] = map[string]any{
				"post": restoreSpec,
			}
			document.Paths[versionBase+"/schedule"] = map[string]any{
				"get":  operationSpec("List scheduled publishes for "+collection.Labels.Singular, "scheduledPublishes"+name, "200"),
				"post": operationSpec("Schedule "+collection.Labels.Singular+" publish", "schedulePublish"+name, "201"),
			}
			document.Paths[versionBase+"/schedule/{jobId}"] = map[string]any{
				"parameters": []map[string]any{{"name": "jobId", "in": "path", "required": true, "schema": map[string]string{"type": "string"}}},
				"delete":     operationSpec("Cancel "+collection.Labels.Singular+" scheduled publish", "cancelScheduledPublish"+name, "200"),
			}
		}
		if err := generator.addOpenAPIResourceSchema(document.Components.Schemas, name, collection, pluginTypes); err != nil {
			return nil, err
		}
		docResponse := openAPIDocumentEnvelope(name)
		addOpenAPIJSONResponse(createSpec, "201", docResponse)
		for _, operation := range []map[string]any{findSpec, updateSpec, deleteSpec} {
			addOpenAPIJSONResponse(operation, "200", docResponse)
		}
		addOpenAPIJSONResponse(listSpec, "200", map[string]any{"type": "object", "properties": map[string]any{
			"docs":       map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/" + name}},
			"pagination": map[string]any{"$ref": "#/components/schemas/Pagination"},
		}, "required": []string{"docs", "pagination"}})
		if err := generator.addInputResourceSchemas(document.Components.Schemas, name, collection, pluginTypes, snapshot.Application.AllowIDOnCreate); err != nil {
			return nil, err
		}
		createSpec["requestBody"] = openAPIJSONRequest(name + "Create")
		if collection.Upload != nil {
			createSpec["requestBody"] = map[string]any{"required": true, "content": map[string]any{"multipart/form-data": map[string]any{"schema": map[string]any{"type": "object", "properties": map[string]any{"file": map[string]any{"type": "string", "format": "binary"}, "data": map[string]any{"type": "string", "contentMediaType": "application/json", "contentSchema": map[string]any{"$ref": "#/components/schemas/" + name + "Create"}}}, "required": []string{"file"}}}}}
		}
		updateSpec["requestBody"] = openAPIJSONRequest(name + "Update")
	}
	for _, global := range snapshot.Globals {
		slug, name := string(global.Slug), string(global.ID)
		localeParameters := localizationParameters(snapshot.Application.Localization, global.Fields)
		document.Paths["/api/globals/"+slug+"/validate"] = map[string]any{"post": liveValidationOperation(name, snapshot.Application.Localization, true)}
		readSpec := operationSpec("Read "+global.Labels.Singular, "read"+name, "200")
		readSpec["parameters"] = append(depthParameters(), localeParameters...)
		updateSpec := operationSpec("Update "+global.Labels.Singular, "update"+name, "200")
		updateSpec["parameters"] = localeParameters
		document.Paths["/api/globals/"+slug] = map[string]any{
			"get":   readSpec,
			"patch": updateSpec,
		}
		document.Paths["/api/globals/"+slug+"/copy-locale"] = map[string]any{"post": operationSpec("Copy localized global values between locales", "copyLocale"+name, "200")}
		if global.Versions != nil {
			versionsSpec := operationSpec("List "+global.Labels.Singular+" versions", "versions"+name, "200")
			versionsSpec["parameters"] = localeParameters
			document.Paths["/api/globals/"+slug+"/versions"] = map[string]any{"get": versionsSpec}
			versionSpec := operationSpec("Read "+global.Labels.Singular+" version", "version"+name, "200")
			versionSpec["parameters"] = localeParameters
			document.Paths["/api/globals/"+slug+"/versions/{revision}"] = map[string]any{
				"parameters": []map[string]any{{"name": "revision", "in": "path", "required": true, "schema": map[string]string{"type": "integer"}}},
				"get":        versionSpec,
			}
			publishSpec := operationSpec("Publish "+global.Labels.Singular, "publish"+name, "200")
			publishSpec["parameters"] = localeParameters
			unpublishSpec := operationSpec("Unpublish "+global.Labels.Singular, "unpublish"+name, "200")
			unpublishSpec["parameters"] = localeParameters
			document.Paths["/api/globals/"+slug+"/publish"] = map[string]any{"post": publishSpec}
			document.Paths["/api/globals/"+slug+"/unpublish"] = map[string]any{"post": unpublishSpec}
			restoreSpec := operationSpec("Restore "+global.Labels.Singular, "restore"+name, "200")
			restoreSpec["parameters"] = append(restoreVersionParameters(), localeParameters...)
			document.Paths["/api/globals/"+slug+"/restore/{revision}"] = map[string]any{
				"post": restoreSpec,
			}
		}
		if err := generator.addOpenAPIResourceSchema(document.Components.Schemas, name, global, pluginTypes); err != nil {
			return nil, err
		}
		addOpenAPIJSONResponse(readSpec, "200", openAPIDocumentEnvelope(name))
		addOpenAPIJSONResponse(updateSpec, "200", openAPIDocumentEnvelope(name))
		if err := generator.addInputResourceSchemas(document.Components.Schemas, name, global, pluginTypes, false); err != nil {
			return nil, err
		}
		updateSpec["requestBody"] = openAPIJSONRequest(name + "Update")
	}
	document.Paths["/api/auth/me"] = map[string]any{"get": operationSpec("Current session", "currentSession", "200")}
	document.Paths["/api/auth/refresh"] = map[string]any{"post": operationSpec("Rotate current session", "refreshSession", "200")}
	document.Paths["/api/auth/logout"] = map[string]any{"post": operationSpec("Logout", "logout", "200")}
	document.Paths["/api/auth/logout-all"] = map[string]any{"post": operationSpec("Logout all sessions", "logoutAll", "200")}
	document.Paths["/api/auth/change-password"] = map[string]any{"post": operationSpec("Change the current password", "changePassword", "200")}
	document.Paths["/api/preferences"] = map[string]any{"delete": operationSpec("Reset current user preferences", "resetPreferences", "200")}
	document.Paths["/api/preferences/{key}"] = map[string]any{
		"parameters": []map[string]any{{"name": "key", "in": "path", "required": true, "schema": map[string]string{"type": "string"}}},
		"get":        operationSpec("Read a current user preference", "getPreference", "200"), "put": operationSpec("Set a current user preference", "setPreference", "200"), "delete": operationSpec("Delete a current user preference", "deletePreference", "200"),
	}
	document.Paths["/api/auth/sessions"] = map[string]any{"get": operationSpec("List active sessions", "listAuthSessions", "200")}
	document.Paths["/api/auth/sessions/{id}"] = map[string]any{
		"parameters": []map[string]any{{
			"name": "id", "in": "path", "required": true,
			"schema": map[string]string{"type": "string"},
		}},
		"delete": operationSpec("Revoke an active session", "revokeAuthSession", "200"),
	}
	if hasAPIKeys {
		document.Paths["/api/auth/api-keys"] = map[string]any{
			"get":  operationSpec("List API keys", "listAPIKeys", "200"),
			"post": operationSpec("Create an API key", "createAPIKey", "201"),
		}
		document.Paths["/api/auth/api-keys/{id}"] = map[string]any{
			"parameters": []map[string]any{{"name": "id", "in": "path", "required": true, "schema": map[string]string{"type": "string"}}},
			"delete":     operationSpec("Revoke an API key", "revokeAPIKey", "200"),
		}
	}
	addCustomEndpointPaths(document.Paths, snapshot)
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func addCustomEndpointPaths(paths map[string]map[string]any, snapshot schema.Snapshot) {
	add := func(mount, owner string, endpoints []schema.Endpoint) {
		for _, endpoint := range endpoints {
			path, _ := openAPIEndpointPath(mount, endpoint.Path)
			path = equivalentOpenAPIPath(paths, path)
			if paths[path] == nil {
				paths[path] = make(map[string]any)
			}
			summary := endpoint.Summary
			if summary == "" {
				summary = "Custom " + endpoint.Method + " endpoint"
			}
			operationID := customEndpointOperationID(owner, endpoint)
			methodKey := strings.ToLower(endpoint.Method)
			if endpoint.Method == "CONNECT" {
				methodKey = "x-ridu-connect"
			}
			operation := customEndpointOperationSpec(summary, operationID)
			if parameters := openAPIParametersForPath(path); len(parameters) != 0 {
				operation["parameters"] = parameters
			}
			paths[path][methodKey] = operation
		}
	}
	add("/api", "root", snapshot.Application.Endpoints)
	for _, collection := range snapshot.Collections {
		add("/api/collections/"+string(collection.Slug), "collection-"+string(collection.Slug), collection.Endpoints)
	}
	for _, global := range snapshot.Globals {
		add("/api/globals/"+string(global.Slug), "global-"+string(global.Slug), global.Endpoints)
	}
}

func equivalentOpenAPIPath(paths map[string]map[string]any, candidate string) string {
	if paths[candidate] != nil {
		return candidate
	}
	shape := openAPIPathShape(candidate)
	match := ""
	for existing := range paths {
		if openAPIPathShape(existing) == shape && (match == "" || existing < match) {
			match = existing
		}
	}
	if match != "" {
		return match
	}
	return candidate
}

func openAPIPathShape(path string) string {
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for index, segment := range segments {
		if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
			segments[index] = "{}"
		}
	}
	return "/" + strings.Join(segments, "/")
}

func openAPIParametersForPath(path string) []map[string]any {
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	parameters := make([]map[string]any, 0)
	for _, segment := range segments {
		if !strings.HasPrefix(segment, "{") || !strings.HasSuffix(segment, "}") {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(segment, "{"), "}")
		parameters = append(parameters, map[string]any{
			"name": name, "in": "path", "required": true,
			"schema": map[string]string{"type": "string"},
		})
	}
	return parameters
}

func openAPIEndpointPath(mount, endpointPath string) (string, []map[string]any) {
	if endpointPath == "/" {
		return mount, nil
	}
	segments := strings.Split(strings.TrimPrefix(endpointPath, "/"), "/")
	parameters := make([]map[string]any, 0)
	for index, segment := range segments {
		if !strings.HasPrefix(segment, ":") {
			continue
		}
		name := strings.TrimPrefix(segment, ":")
		segments[index] = "{" + name + "}"
		parameters = append(parameters, map[string]any{
			"name": name, "in": "path", "required": true,
			"schema": map[string]string{"type": "string"},
		})
	}
	return mount + "/" + strings.Join(segments, "/"), parameters
}

func endpointOperationSuffix(path string) string {
	if path == "/" {
		return "root"
	}
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for index, segment := range segments {
		if strings.HasPrefix(segment, ":") {
			segments[index] = "param-" + strings.TrimPrefix(segment, ":")
		}
	}
	return strings.Join(segments, "-")
}

func customEndpointOperationID(owner string, endpoint schema.Endpoint) string {
	digest := sha256.Sum256([]byte(owner + "\x00" + endpoint.Method + "\x00" + endpoint.Path))
	return fmt.Sprintf("custom-%s-%s-%s-%x", owner, strings.ToLower(endpoint.Method), endpointOperationSuffix(endpoint.Path), digest[:6])
}

func customEndpointOperationSpec(summary, operationID string) map[string]any {
	return map[string]any{
		"summary":     summary,
		"operationId": operationID,
		"responses": map[string]any{
			"2XX":     map[string]any{"description": "Endpoint-defined successful response"},
			"default": map[string]any{"description": "Endpoint-defined response"},
		},
	}
}

func depthParameters() []map[string]any {
	return []map[string]any{{
		"name": "depth", "in": "query", "required": false,
		"description": "Expand readable relationships recursively.",
		"schema":      map[string]any{"type": "integer", "minimum": 0, "maximum": 5, "default": 0},
	}}
}

func localizationParameters(settings *schema.LocalizationSettings, fields []schema.Field) []map[string]any {
	if settings == nil || !fieldsUseLocalization(fields) {
		return nil
	}
	locales := make([]string, 0, len(settings.Locales)+1)
	for _, locale := range settings.Locales {
		locales = append(locales, string(locale.Code))
	}
	locales = append(locales, "all")
	return []map[string]any{
		{
			"name": "locale", "in": "query", "required": false,
			"description": "Select one content locale, or all locales as locale-keyed values.",
			"schema":      map[string]any{"type": "string", "enum": locales, "default": string(settings.DefaultLocale)},
		},
		{
			"name": "fallback-locale", "in": "query", "required": false,
			"description": "Override fallback with a locale or comma-separated chain; false disables fallback.",
			"schema":      map[string]any{"type": "string"},
		},
	}
}

func fieldsUseLocalization(fields []schema.Field) bool {
	for _, field := range fields {
		if field.Localized {
			return true
		}
		if fieldsUseLocalization(schema.ChildFields(field)) {
			return true
		}
	}
	return false
}

func restoreVersionParameters() []map[string]any {
	return []map[string]any{
		{"name": "revision", "in": "path", "required": true, "schema": map[string]string{"type": "integer"}},
		{"name": "draft", "in": "query", "required": false, "schema": map[string]any{"type": "boolean", "default": false}},
	}
}

func createDraftParameters(collection schema.Collection) []map[string]any {
	if collection.Versions == nil || collection.Upload != nil {
		return nil
	}
	return []map[string]any{{
		"name": "draft", "in": "query", "required": false,
		"description": "Create the document as a draft when true or publish it atomically when false.",
		"schema":      map[string]any{"type": "boolean"},
	}}
}

func (generator openAPIBlockGenerator) addOpenAPIResourceSchema(schemas map[string]openAPISchema, name string, collection schema.Collection, pluginTypes map[string]json.RawMessage) error {
	fields := collection.Fields
	properties := map[string]any{
		"id":        openAPIProperty{Type: "string"},
		"createdAt": openAPIProperty{Type: "string", Format: "date-time"},
		"updatedAt": openAPIProperty{Type: "string", Format: "date-time"},
	}
	required := []string{"id", "createdAt", "updatedAt"}
	if collection.Capabilities.Trash {
		properties["deletedAt"] = map[string]any{"type": []string{"string", "null"}, "format": "date-time"}
	}
	for _, field := range fields {
		if field.Category == schema.FieldCategoryPresentation && field.Type != schema.FieldTypeJoin && field.Type != schema.FieldTypeVirtual {
			continue
		}
		property, err := generator.fieldSchema(field, pluginTypes)
		if err != nil {
			return err
		}

		properties[field.Name] = property
	}
	schemas[name] = openAPISchema{Type: "object", Properties: properties, Required: required}
	return nil
}

// openAPIFieldSchema follows the manifest tree, including nested block variants.
// Optional values use JSON Schema null unions, as required by OpenAPI 3.1.
// The default mode describes responses: authored fields can be omitted by read
// access or selection. Input mode reuses the tree with authoring requiredness,
// canonical references and per-locale values.
type openAPIBlockGenerator struct {
	blocks *blocktypes.Catalog
	input  bool
	update bool
}

func openAPIFieldSchema(field schema.Field, pluginTypes map[string]json.RawMessage) (map[string]any, error) {
	return (openAPIBlockGenerator{}).fieldSchema(field, pluginTypes)
}

func (generator openAPIBlockGenerator) fieldSchema(field schema.Field, pluginTypes map[string]json.RawMessage) (map[string]any, error) {
	single, err := generator.fieldSchemaForLocaleMode(field, pluginTypes, false)
	if err != nil || generator.input || !fieldsUseLocalization([]schema.Field{field}) {
		return single, err
	}
	all, err := generator.fieldSchemaForLocaleMode(field, pluginTypes, true)
	if err != nil {
		return nil, err
	}
	// Build each response mode once, rather than multiplying alternatives at
	// every nested field. A localized container owns its descendants' locale.
	return map[string]any{"anyOf": []any{single, all}}, nil
}

func (generator openAPIBlockGenerator) fieldSchemaForLocaleMode(field schema.Field, pluginTypes map[string]json.RawMessage, allLocales bool) (map[string]any, error) {
	childAllLocales := allLocales && !field.Localized
	property := map[string]any{"type": "string"}
	switch field.Type {
	case schema.FieldTypeText, schema.FieldTypeTextarea, schema.FieldTypeCode:
		var minimum, maximum *int
		switch {
		case field.Text != nil:
			minimum, maximum = field.Text.MinLength, field.Text.MaxLength
		case field.Textarea != nil:
			minimum, maximum = field.Textarea.MinLength, field.Textarea.MaxLength
		case field.Code != nil:
			minimum, maximum = field.Code.MinLength, field.Code.MaxLength
		}
		// Read transforms preserve the logical type but do not rerun write
		// admission. Length constraints describe submitted values only.
		if generator.input {
			if minimum != nil {
				property["minLength"] = *minimum
			}
			if field.Required && (minimum == nil || *minimum < 1) {
				property["minLength"] = 1
			}
			if maximum != nil {
				property["maxLength"] = *maximum
			}
		}
	case schema.FieldTypeNumber:
		property["type"] = "number"
		if generator.input && field.Number != nil {
			if field.Number.Min != nil {
				property["minimum"] = *field.Number.Min
			}
			if field.Number.Max != nil {
				property["maximum"] = *field.Number.Max
			}
		}
	case schema.FieldTypeTextList, schema.FieldTypeNumberList:
		item := map[string]any{"type": "string"}
		if field.Type == schema.FieldTypeNumberList {
			item["type"] = "number"
		}
		if generator.input {
			if field.Type == schema.FieldTypeTextList && field.Text != nil {
				if field.Text.MinLength != nil {
					item["minLength"] = *field.Text.MinLength
				}
				if field.Text.MaxLength != nil {
					item["maxLength"] = *field.Text.MaxLength
				}
			}
			if field.Type == schema.FieldTypeNumberList && field.Number != nil {
				if field.Number.Min != nil {
					item["minimum"] = *field.Number.Min
				}
				if field.Number.Max != nil {
					item["maximum"] = *field.Number.Max
				}
			}
		}
		property = map[string]any{"type": "array", "items": item}
		if generator.input && field.List != nil {
			openAPIRowBounds(property, field.Required, field.List.MinRows, field.List.MaxRows)
		}
	case schema.FieldTypeCheckbox:
		property["type"] = "boolean"
	case schema.FieldTypeJSON:
		property = map[string]any{}
	case schema.FieldTypeDate:
		if field.Date == nil {
			property["format"] = "date"
		} else {
			switch field.Date.Format {
			case schema.DateOnly:
				property["format"] = "date"
			case schema.DateTime:
				property["format"] = "date-time"
			case schema.TimeOnly:
				// Local wall-clock values are not RFC3339 times (no offset).
				property["pattern"] = `^(?:[01]?[0-9]|2[0-3]):[0-5][0-9](?::[0-5][0-9](?:[.,][0-9]+)?)?$`
			}
		}
	case schema.FieldTypeEmail:
		property["format"] = "email"
	case schema.FieldTypeSelect, schema.FieldTypeRadio:
		if field.Select != nil {
			values := make([]string, len(field.Select.Options))
			for i, option := range field.Select.Options {
				values[i] = option.Value
			}
			property["enum"] = values
			if field.Select.HasMany {
				property = map[string]any{"type": "array", "items": property}
			}
		}
	case schema.FieldTypePoint:
		property = map[string]any{"type": "array", "minItems": 2, "maxItems": 2, "items": map[string]any{"type": "number"}}
	case schema.FieldTypeGroup, schema.FieldTypeArray:
		if field.Nested == nil {
			return nil, fmt.Errorf("field %s lacks nested metadata", field.Name)
		}
		object, err := generator.fieldsObject(field.Nested.ResolvedFields(), pluginTypes, childAllLocales)
		if err != nil {
			return nil, err
		}
		property = object
		if field.Type == schema.FieldTypeGroup && field.Localized {
			// A locale map must use the all-locales alternative, rather than
			// passing as undeclared properties of the single-locale group.
			object["additionalProperties"] = false
		}
		if field.Type == schema.FieldTypeArray {
			object["properties"].(map[string]any)["_key"] = map[string]any{"type": "string", "minLength": 1, "pattern": `\S`}
			if !generator.input {
				object["required"] = []string{"_key"}
			}
			var items any = object
			if generator.update {
				createGenerator := generator
				createGenerator.update = false
				createObject, err := createGenerator.fieldsObject(field.Nested.ResolvedFields(), pluginTypes, false)
				if err != nil {
					return nil, err
				}
				createObject["properties"].(map[string]any)["_key"] = map[string]any{"type": "string", "minLength": 1, "pattern": `\S`}
				object["required"] = []string{"_key"}
				items = map[string]any{"anyOf": []any{createObject, object}}
			}
			property = map[string]any{"type": "array", "items": items}
			if generator.input {
				openAPIRowBounds(property, field.Required, field.Nested.MinRows, field.Nested.MaxRows)
			}
		}
	case schema.FieldTypeBlocks:
		if field.Blocks == nil {
			return nil, fmt.Errorf("field %s lacks block metadata", field.Name)
		}
		variants := make([]any, 0, len(field.Blocks.ResolvedTypes()))
		mapping := make(map[string]string, len(field.Blocks.ResolvedTypes()))
		for index, block := range field.Blocks.ResolvedTypes() {
			if generator.blocks != nil {
				name := generator.blocks.Fields[field.ID].Variants[index]
				if generator.input {
					name += "Input"
				} else if childAllLocales {
					name += "AllLocales"
				}
				if generator.update {
					base := generator.blocks.Fields[field.ID].Variants[index]
					variants = append(variants, map[string]any{"anyOf": []any{map[string]any{"$ref": "#/components/schemas/" + base + "Input"}, map[string]any{"$ref": "#/components/schemas/" + base + "Update"}}})
				} else {
					mapping[block.Slug] = "#/components/schemas/" + name
					variants = append(variants, map[string]any{"$ref": mapping[block.Slug]})
				}
				continue
			}
			variant, err := generator.fieldsObject(block.ResolvedFields(), pluginTypes, childAllLocales)
			if err != nil {
				return nil, err
			}
			properties := variant["properties"].(map[string]any)
			properties["blockType"] = map[string]any{"type": "string", "const": block.Slug}
			properties["_key"] = map[string]any{"type": "string", "minLength": 1, "pattern": `\S`, "description": "Stable row identity. May be omitted on input; materialized by the server."}
			required, _ := variant["required"].([]string)
			required = append(required, "blockType")
			if !generator.input {
				required = append(required, "_key")
			}
			variant["required"] = required
			variants = append(variants, variant)
		}
		discriminator := map[string]any{"propertyName": "blockType"}
		if len(mapping) > 0 {
			discriminator["mapping"] = mapping
		}
		items := map[string]any{"oneOf": variants}
		// Input and retained-row branches overlap for complete keyed rows.
		// Their grouped anyOf is authoritative; an OpenAPI discriminator mapping
		// cannot select one of these two shapes from blockType alone.
		if !generator.update {
			items["discriminator"] = discriminator
		}
		property = map[string]any{"type": "array", "items": items}
		if generator.input {
			openAPIRowBounds(property, field.Required, field.Blocks.MinRows, field.Blocks.MaxRows)
		}
	case schema.FieldTypeRelationship, schema.FieldTypeUpload:
		// Reads may populate references; writes use their string IDs.
		property = map[string]any{"anyOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "object"}}}
		if field.Relationship != nil && field.Relationship.Polymorphic {
			property = map[string]any{"type": "object", "properties": map[string]any{"relationTo": map[string]any{"type": "string"}, "id": map[string]any{"anyOf": []any{map[string]any{"type": "string"}, map[string]any{"type": "object"}}}}, "required": []string{"relationTo", "id"}}
		}
		if generator.input {
			property = map[string]any{"type": "string"}
			if field.Relationship != nil && field.Relationship.Polymorphic {
				targets := make([]string, len(field.Relationship.Targets))
				for index, target := range field.Relationship.Targets {
					targets[index] = string(target.CollectionSlug)
				}
				property = map[string]any{"type": "object", "properties": map[string]any{"relationTo": map[string]any{"type": "string", "enum": targets}, "id": map[string]any{"type": "string"}}, "required": []string{"relationTo", "id"}}
			}
		}
		if field.Relationship != nil && field.Relationship.HasMany || field.Upload != nil && field.Upload.HasMany {
			property = map[string]any{"type": "array", "items": property}
		}
	case schema.FieldTypeJoin:
		return map[string]any{"type": "array", "items": map[string]any{"type": "object"}}, nil
	case schema.FieldTypeVirtual:
		if field.Virtual != nil {
			property["type"] = string(field.Virtual.ValueType)
			if field.Virtual.ValueType == schema.ValueTypeJSON {
				property = map[string]any{}
			}
		}
	case schema.FieldTypePlugin:
		property = map[string]any{}
		if field.Plugin != nil && len(pluginTypes[field.Plugin.Key]) != 0 {
			if err := json.Unmarshal(pluginTypes[field.Plugin.Key], &property); err != nil {
				return nil, fmt.Errorf("decode plugin field %s JSON Schema: %w", field.Plugin.Key, err)
			}
		}
		if field.Plugin != nil && len(field.Plugin.EmbeddedTrees) > 0 {
			property = generator.embeddedFieldSchema(field, property, allLocales)
		}
	}
	if (field.Type == schema.FieldTypeDate || field.Type == schema.FieldTypeEmail) && !field.Required {
		// Optional date and email controls preserve explicitly empty wire values.
		property = map[string]any{"anyOf": []any{property, map[string]any{"const": ""}}}
	}
	if !field.Required && (!generator.input || !generatedInputRequired(field)) && len(property) > 0 {
		property = map[string]any{"anyOf": []any{property, map[string]any{"type": "null"}}}
	}
	if allLocales && field.Localized {
		property = map[string]any{"type": "object", "additionalProperties": property, "description": "Values keyed by locale code when locale=all."}
	}
	return property, nil
}

func (generator openAPIBlockGenerator) fieldsObject(fields []schema.Field, pluginTypes map[string]json.RawMessage, allLocales bool) (map[string]any, error) {
	properties := make(map[string]any)
	required := []string{}
	for _, field := range fields {
		if generator.input && (field.Category == schema.FieldCategoryPresentation || field.Category == schema.FieldCategoryUpload && field.Upload == nil) {
			continue
		}
		if field.Category == schema.FieldCategoryPresentation && field.Type != schema.FieldTypeJoin && field.Type != schema.FieldTypeVirtual {
			continue
		}
		property, err := generator.fieldSchemaForLocaleMode(field, pluginTypes, allLocales)
		if err != nil {
			return nil, err
		}
		properties[field.Name] = property
		if generator.input && !generator.update && generatedInputRequired(field) && !generatedFieldHasDefault(field) {
			required = append(required, field.Name)
		}
	}
	object := map[string]any{"type": "object", "properties": properties}
	if generator.input {
		object["additionalProperties"] = false
	}
	if len(required) > 0 {
		object["required"] = required
	}
	return object, nil
}

func openAPIRowBounds(property map[string]any, required bool, minimum, maximum int) {
	if required && minimum < 1 {
		minimum = 1
	}
	if minimum > 0 {
		property["minItems"] = minimum
	}
	if maximum > 0 {
		property["maxItems"] = maximum
	}
}

func operationSpec(summary, operationID, success string) map[string]any {
	return map[string]any{
		"summary":     summary,
		"operationId": operationID,
		"responses": map[string]any{
			success:   map[string]any{"description": "Successful response"},
			"default": map[string]any{"description": "Ridu error envelope", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/ErrorEnvelope"}}}},
		},
	}
}

func joinMutationOperationSpec(label, name, responseSchema string) map[string]any {
	spec := operationSpec("Mutate "+label+" inverse relationship", "mutateJoin"+name, "200")
	spec["requestBody"] = map[string]any{
		"required": true,
		"content": map[string]any{
			"application/json": map[string]any{"schema": map[string]string{"$ref": "#/components/schemas/JoinMutationInput"}},
		},
	}
	responses := spec["responses"].(map[string]any)
	responses["200"] = map[string]any{
		"description": "Successful response",
		"content": map[string]any{
			"application/json": map[string]any{"schema": map[string]string{"$ref": "#/components/schemas/" + responseSchema}},
		},
	}
	return spec
}

func addCollectionSelectionSchemas(schemas map[string]openAPISchema) {
	operationProperties := map[string]any{}
	operationRequired := []string{
		"admin", "create", "read", "readVersions", "update", "delete", "duplicate",
		"publish", "unpublish", "restoreDeleted", "deletePermanent", "selectAll",
	}
	for _, name := range operationRequired {
		operationProperties[name] = map[string]string{"type": "boolean"}
	}
	schemas["OperationCapabilities"] = openAPISchema{Type: "object", Properties: operationProperties, Required: operationRequired}
	schemas["FieldCapabilities"] = openAPISchema{
		Type: "object",
		Properties: map[string]any{
			"read": map[string]string{"type": "boolean"}, "create": map[string]string{"type": "boolean"}, "update": map[string]string{"type": "boolean"},
		},
		Required: []string{"read", "create", "update"},
	}
	schemas["AccessCapabilities"] = openAPISchema{
		Type: "object",
		Properties: map[string]any{
			"operations": map[string]string{"$ref": "#/components/schemas/OperationCapabilities"},
			"fields": map[string]any{
				"type": "object", "additionalProperties": map[string]string{"$ref": "#/components/schemas/FieldCapabilities"},
			},
		},
		Required: []string{"operations", "fields"},
	}
	schemas["CollectionSelectionInput"] = openAPISchema{
		Type: "object",
		Properties: map[string]any{
			"where": map[string]string{"type": "object"}, "trash": map[string]string{"type": "boolean"},
		},
	}
	schemas["CollectionSelectionItem"] = openAPISchema{
		Type: "object",
		Properties: map[string]any{
			"id": map[string]string{"type": "string"}, "access": map[string]string{"$ref": "#/components/schemas/AccessCapabilities"},
		},
		Required: []string{"id", "access"},
	}
	schemas["CollectionSelectionEnvelope"] = openAPISchema{
		Type: "object",
		Properties: map[string]any{
			"items":     map[string]any{"type": "array", "items": map[string]string{"$ref": "#/components/schemas/CollectionSelectionItem"}},
			"totalDocs": map[string]string{"type": "integer"},
		},
		Required: []string{"items", "totalDocs"},
	}
}

func collectionSelectionOperationSpec(label, name string) map[string]any {
	spec := operationSpec("Resolve filtered "+label+" selection", "resolveSelection"+name, "200")
	spec["requestBody"] = map[string]any{
		"required": true,
		"content": map[string]any{
			"application/json": map[string]any{"schema": map[string]string{"$ref": "#/components/schemas/CollectionSelectionInput"}},
		},
	}
	responses := spec["responses"].(map[string]any)
	responses["200"] = map[string]any{
		"description": "Successful response",
		"content": map[string]any{
			"application/json": map[string]any{"schema": map[string]string{"$ref": "#/components/schemas/CollectionSelectionEnvelope"}},
		},
	}
	return spec
}

func (generator openAPIBlockGenerator) addBlockSchemas(schemas map[string]openAPISchema, pluginTypes map[string]json.RawMessage, localized bool) error {
	closed := false
	for _, variant := range generator.blocks.Variants {
		inputGenerator := generator
		inputGenerator.input = true
		object, err := inputGenerator.fieldsObject(variant.Block.ResolvedFields(), pluginTypes, false)
		if err != nil {
			return err
		}
		properties := object["properties"].(map[string]any)
		properties[variant.Discriminator] = map[string]any{"type": "string", "const": variant.Block.Slug}
		properties[variant.Identity] = map[string]any{"type": "string", "minLength": 1, "pattern": `\S`}
		required, _ := object["required"].([]string)
		schemas[variant.Name+"Input"] = openAPISchema{Type: "object", Properties: properties, Required: append(required, variant.Discriminator), AdditionalProperties: &closed}
		inputGenerator.update = true
		updateObject, err := inputGenerator.fieldsObject(variant.Block.ResolvedFields(), pluginTypes, false)
		if err != nil {
			return err
		}
		updateProperties := updateObject["properties"].(map[string]any)
		updateProperties[variant.Discriminator] = properties[variant.Discriminator]
		updateProperties[variant.Identity] = properties[variant.Identity]
		schemas[variant.Name+"Update"] = openAPISchema{Type: "object", Properties: updateProperties, Required: []string{variant.Discriminator, variant.Identity}, AdditionalProperties: &closed, Description: "Patch an existing row identified by _key. Omitted children are retained; a new key must satisfy the create contract at runtime."}
		modes := []bool{false}
		if localized {
			modes = append(modes, true)
		}
		for _, all := range modes {
			name := variant.Name
			if all {
				name += "AllLocales"
			}
			object, err := generator.fieldsObject(variant.Block.ResolvedFields(), pluginTypes, all)
			if err != nil {
				return err
			}
			properties := object["properties"].(map[string]any)
			properties[variant.Discriminator] = map[string]any{"type": "string", "const": variant.Block.Slug}
			properties[variant.Identity] = map[string]any{"type": "string", "minLength": 1, "pattern": `\S`}
			schemas[name] = openAPISchema{Type: "object", Properties: properties, Required: []string{variant.Discriminator, variant.Identity}}
		}
	}
	return nil
}

func containsBlockFields(fields []schema.Field) bool {
	for _, field := range fields {
		if field.Blocks != nil || len(schema.EmbeddedBlocks(field)) > 0 {
			return true
		}
		if containsBlockFields(schema.ChildFields(field)) {
			return true
		}
	}
	return false
}

func openAPIJSONRequest(name string) map[string]any {
	return map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/" + name}}}}
}

func openAPIDocumentEnvelope(name string) map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"doc": map[string]any{"$ref": "#/components/schemas/" + name}}, "required": []string{"doc"}}
}

func addOpenAPIJSONResponse(operation map[string]any, status string, response map[string]any) {
	operation["responses"].(map[string]any)[status] = map[string]any{"description": "Successful response", "content": map[string]any{"application/json": map[string]any{"schema": response}}}
}

func addTransportSchemas(schemas map[string]openAPISchema) {
	schemas["Pagination"] = openAPISchema{Type: "object", Properties: map[string]any{
		"page": map[string]any{"type": "integer"}, "limit": map[string]any{"type": "integer"},
		"totalDocs": map[string]any{"type": "integer"}, "totalPages": map[string]any{"type": "integer"},
		"hasNextPage": map[string]any{"type": "boolean"}, "hasPrevPage": map[string]any{"type": "boolean"},
	}, Required: []string{"page", "limit", "totalDocs", "totalPages", "hasNextPage", "hasPrevPage"}}
	schemas["ValidationIssue"] = openAPISchema{
		Type: "object", Properties: map[string]any{
			"code":         map[string]any{"type": "string"},
			"path":         map[string]any{"type": "string"},
			"message":      map[string]any{"type": "string"},
			"target":       map[string]any{"type": "string", "description": "Opaque occurrence identity for correlating field issues across repeated rows."},
			"fieldId":      map[string]any{"type": "string"},
			"collectionId": map[string]any{"type": "string"},
			"globalId":     map[string]any{"type": "string"},
			"locale":       map[string]any{"type": "string", "description": "Exact content locale addressed by the issue; never a fallback locale."},
		}, Required: []string{"code", "path", "message"},
	}
	schemas["ErrorPayload"] = openAPISchema{
		Type: "object", Properties: map[string]any{
			"code":      map[string]any{"type": "string"},
			"status":    map[string]any{"type": "integer"},
			"message":   map[string]any{"type": "string"},
			"requestId": map[string]any{"type": "string"},
			"issues":    map[string]any{"type": "array", "items": map[string]any{"$ref": "#/components/schemas/ValidationIssue"}},
			"details":   map[string]any{},
		}, Required: []string{"code", "status", "message", "issues"},
	}
	schemas["ErrorEnvelope"] = openAPISchema{
		Type: "object", Properties: map[string]any{"error": map[string]any{"$ref": "#/components/schemas/ErrorPayload"}}, Required: []string{"error"},
	}
}

func (generator openAPIBlockGenerator) addInputResourceSchemas(schemas map[string]openAPISchema, name string, collection schema.Collection, pluginTypes map[string]json.RawMessage, allowID bool) error {
	generator.input = true
	object, err := generator.fieldsObject(collection.Fields, pluginTypes, false)
	if err != nil {
		return err
	}
	properties := object["properties"].(map[string]any)
	required, _ := object["required"].([]string)
	if collection.Capabilities.Auth {
		properties["email"] = map[string]any{"type": "string", "format": "email"}
		properties["password"] = map[string]any{"type": "string"}
		required = append(required, "email", "password")
	}
	generator.update = true
	updateObject, err := generator.fieldsObject(collection.Fields, pluginTypes, false)
	if err != nil {
		return err
	}
	update := updateObject["properties"].(map[string]any)
	if collection.Capabilities.Auth {
		update["email"] = properties["email"]
		update["password"] = properties["password"]
	}
	if allowID {
		properties["id"] = map[string]any{"type": "string"}
	}
	closed := false
	schemas[name+"Create"] = openAPISchema{Type: "object", Properties: properties, Required: required, AdditionalProperties: &closed}
	schemas[name+"Update"] = openAPISchema{Type: "object", Properties: update, AdditionalProperties: &closed}
	return nil
}
