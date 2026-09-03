package generate

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/riducms/ridu/schema"
)

type openAPIDocument struct {
	OpenAPI    string                    `json:"openapi"`
	Info       openAPIInfo               `json:"info"`
	Paths      map[string]map[string]any `json:"paths"`
	Components openAPIComponents         `json:"components"`
}

type openAPIInfo struct {
	Title   string `json:"title"`
	Version string `json:"version"`
}

type openAPIComponents struct {
	Schemas map[string]openAPISchema `json:"schemas"`
}

type openAPISchema struct {
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties,omitempty"`
	Required   []string       `json:"required,omitempty"`
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
	document := openAPIDocument{
		OpenAPI:    "3.1.0",
		Info:       openAPIInfo{Title: snapshot.Application.Name + " API", Version: fmt.Sprintf("schema-%d", snapshot.Version)},
		Paths:      make(map[string]map[string]any),
		Components: openAPIComponents{Schemas: make(map[string]openAPISchema)},
	}
	addCollectionSelectionSchemas(document.Components.Schemas)
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
	for _, collection := range snapshot.Collections {
		slug := string(collection.Slug)
		name := string(collection.ID)
		localeParameters := localizationParameters(snapshot.Application.Localization, collection.Fields)
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
		if err := addOpenAPIResourceSchema(document.Components.Schemas, name, collection, pluginTypes); err != nil {
			return nil, err
		}
	}
	for _, global := range snapshot.Globals {
		slug, name := string(global.Slug), string(global.ID)
		localeParameters := localizationParameters(snapshot.Application.Localization, global.Fields)
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
		if err := addOpenAPIResourceSchema(document.Components.Schemas, name, global, pluginTypes); err != nil {
			return nil, err
		}
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
		if field.Nested != nil && fieldsUseLocalization(field.Nested.Fields) {
			return true
		}
		if field.Blocks != nil {
			for _, block := range field.Blocks.Types {
				if fieldsUseLocalization(block.Fields) {
					return true
				}
			}
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

func addOpenAPIResourceSchema(schemas map[string]openAPISchema, name string, collection schema.Collection, pluginTypes map[string]json.RawMessage) error {
	fields := collection.Fields
	properties := map[string]any{
		"id":        openAPIProperty{Type: "string"},
		"createdAt": openAPIProperty{Type: "string", Format: "date-time"},
		"updatedAt": openAPIProperty{Type: "string", Format: "date-time"},
	}
	required := []string{"id", "createdAt", "updatedAt"}
	if collection.Capabilities.Trash {
		properties["deletedAt"] = openAPIProperty{Type: "string", Format: "date-time", Nullable: true}
	}
	for _, field := range fields {
		if field.Category == schema.FieldCategoryPresentation && field.Type != schema.FieldTypeJoin && field.Type != schema.FieldTypeVirtual {
			continue
		}
		var property any = openAPIProperty{Type: "string", Nullable: !field.Required}
		if field.Type == schema.FieldTypeGroup {
			property = openAPIProperty{Type: "object", Nullable: !field.Required}
		}
		if (field.Type == schema.FieldTypeSelect || field.Type == schema.FieldTypeRadio) && field.Select != nil {
			selectProperty := openAPIProperty{Type: "string", Nullable: !field.Required}
			for _, choice := range field.Select.Choices {
				selectProperty.Enum = append(selectProperty.Enum, choice.Value)
			}
			if field.Type == schema.FieldTypeSelect && field.Select.HasMany {
				property = map[string]any{"type": "array", "items": selectProperty, "nullable": !field.Required}
			} else {
				property = selectProperty
			}
		}
		if field.Type == schema.FieldTypePoint {
			property = map[string]any{"type": "array", "minItems": 2, "maxItems": 2, "items": openAPIProperty{Type: "number"}, "nullable": !field.Required}
		}
		if field.Type == schema.FieldTypeJoin {
			property = map[string]any{"type": "array", "items": map[string]any{"type": "object"}}
		}
		if field.Type == schema.FieldTypeVirtual && field.Virtual != nil {
			typeName := string(field.Virtual.ValueType)
			if field.Virtual.ValueType == schema.ValueTypeJSON {
				property = map[string]any{}
			} else {
				property = openAPIProperty{Type: typeName, Nullable: !field.Required}
			}
		}
		if field.Type == schema.FieldTypePlugin && field.Plugin != nil && len(pluginTypes[field.Plugin.Key]) != 0 {
			var pluginSchema map[string]any
			if err := json.Unmarshal(pluginTypes[field.Plugin.Key], &pluginSchema); err != nil {
				return fmt.Errorf("decode plugin field %s JSON Schema: %w", field.Plugin.Key, err)
			}
			if !field.Required {
				pluginSchema = map[string]any{"anyOf": []any{pluginSchema, map[string]any{"type": "null"}}}
			}
			property = pluginSchema
		}
		properties[field.Name] = property
		if field.Required || field.Type == schema.FieldTypeJoin {
			required = append(required, field.Name)
		}
	}
	schemas[name] = openAPISchema{Type: "object", Properties: properties, Required: required}
	return nil
}

func operationSpec(summary, operationID, success string) map[string]any {
	return map[string]any{
		"summary":     summary,
		"operationId": operationID,
		"responses": map[string]any{
			success:   map[string]any{"description": "Successful response"},
			"default": map[string]any{"description": "Ridu error envelope"},
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
