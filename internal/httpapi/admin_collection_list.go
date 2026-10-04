package httpapi

import (
	"context"
	"encoding/json"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

// adminCollectionList serves browser reads independently of build metadata and
// the embedding size limit. Only the mounted list asks for this read model.
func (api *API) adminCollectionList(writer http.ResponseWriter, request *http.Request, requestID string) {
	setAdminPreparedHeaders(writer.Header())
	if request.Method == http.MethodHead {
		writer.WriteHeader(http.StatusOK)
		return
	}
	if request.Method != http.MethodGet {
		api.methodNotAllowed(writer, requestID, http.MethodGet, http.MethodHead)
		return
	}
	slug, err := url.PathUnescape(strings.TrimPrefix(request.URL.EscapedPath(), "/api/admin/collection-list/"))
	if err != nil || slug == "" || strings.Contains(slug, "/") {
		api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: "invalid collection list path"})
		return
	}
	identity := api.resolveAdminPreparedIdentity(request).identity
	snapshot := api.config.Manifest.Snapshot()
	if api.config.ManifestForRequest != nil {
		snapshot, err = api.config.ManifestForRequest(request.Context(), identity)
		if err != nil {
			api.writeError(writer, requestID, err)
			return
		}
	}
	collection := adminCollection(snapshot, slug)
	if collection == nil {
		api.writeError(writer, requestID, &operationengine.Error{Code: "not_found", Status: 404, Message: "collection was not found"})
		return
	}
	part := request.URL.Query().Get("part")
	if part != "page" && part != "counts" {
		api.writeError(writer, requestID, &operationengine.Error{Code: "bad_request", Status: 400, Message: "list part must be page or counts"})
		return
	}
	data := api.loadAdminCollectionList(request, snapshot, identity, slug, request.URL.Query().Get("trash") == "true", request.URL.Query().Get("locale"), nil, part, nil)
	writeJSON(writer, http.StatusOK, data)
}

// loadAdminCollectionList owns list query planning for cold documents, router
// navigation, browser fallback, refresh and focused retry. Each read still has
// its own ordinary engine lifecycle; this is not a batch transaction or cache.
func (api *API) loadAdminCollectionList(request *http.Request, snapshot schema.Snapshot, identity *AuthIdentity, slug string, trash bool, locale string, listCellFields []string, part string, workspace json.RawMessage) protocol.AdminCollectionListDataV1 {
	values := request.URL.Query()
	collection := adminCollection(snapshot, slug)
	data := protocol.AdminCollectionListDataV1{Query: protocol.AdminCollectionListQueryV1{Locale: locale, Trash: trash}, Counts: map[string]protocol.AdminCollectionListCountV1{}}
	limit := 10
	var saved struct {
		Limit int `json:"limit"`
	}
	if json.Unmarshal(workspace, &saved) == nil && validListLimit(saved.Limit) {
		limit = saved.Limit
	}
	if parsed, valid := adminIntegerQuery(values.Get("limit")); valid && validListLimit(parsed) {
		limit = parsed
	}
	page, _ := adminIntegerQuery(values.Get("page"))
	if page < 1 {
		page = 1
	}
	// Hierarchy mode uses one capped page instead of user pagination so ordinary parent/child sets
	// are read together. The 100-document bound still applies to larger hierarchies.
	if !trash && collection.Admin.ParentField != "" && values.Get("view") == "hierarchy" {
		page, limit = 1, 100
	}
	parameters := url.Values{"page": {strconv.Itoa(page)}, "limit": {strconv.Itoa(limit)}, "include-access": {"true"}}
	if trash {
		parameters.Set("trash", "true")
	}
	if locale != "" {
		parameters.Set("locale", locale)
	}
	where := api.adminListWhere(snapshot, slug, values, nil)
	allStatuses := ""
	if countWhere := api.adminListWhere(snapshot, slug, values, &allStatuses); countWhere != nil {
		data.Query.CountWhere, _ = json.Marshal(countWhere)
	}
	if where != nil {
		encoded, _ := json.Marshal(where)
		data.Query.Where = encoded
		parameters.Set("where", string(encoded))
	}
	if part != "counts" {
		if sort := values.Get("sort"); sort != "" && adminListSortAllowed(*collection, sort) {
			parameters.Set("sort", sort)
		}
		if population := adminListPopulation(snapshot, *collection, workspace, values, listCellFields); population != nil {
			encoded, _ := json.Marshal(population)
			parameters.Set("populate", string(encoded))
		}
		options, err := decodeListQuery(parameters, *collection, true)
		var result protocol.CollectionPageEnvelope[json.RawMessage]
		if err == nil {
			result, err = api.readCollectionPage(request.Context(), slug, identity, options)
		}
		data.Page = &protocol.AdminCollectionListPageV1{}
		if err != nil {
			data.Page.Error = api.adminReadError(request, err)
		} else {
			data.Page.Value = &result
		}
	}
	if part != "page" {
		statusField, statuses := adminStatusField(snapshot, slug)
		if statusField != "" {
			for _, status := range append([]string{""}, statuses...) {
				countParameters := url.Values{}
				if trash {
					countParameters.Set("trash", "true")
				}
				if locale != "" {
					countParameters.Set("locale", locale)
				}
				if where := api.adminListWhere(snapshot, slug, values, &status); where != nil {
					encoded, _ := json.Marshal(where)
					countParameters.Set("where", string(encoded))
				}
				options, err := decodeListQuery(countParameters, *collection, false)
				var count int
				if err == nil {
					count, err = api.readCollectionCount(request.Context(), slug, identity, options)
				}
				entry := protocol.AdminCollectionListCountV1{}
				if err != nil {
					entry.Error = api.adminReadError(request, err)
				} else {
					entry.Value = &count
				}
				data.Counts[status] = entry
			}
		}
	}
	return data
}

func (api *API) readAdminListPreference(request *http.Request, identity *AuthIdentity, key string) protocol.AdminCollectionListPreferenceV1 {
	if identity == nil || api.config.GetPreference == nil {
		return protocol.AdminCollectionListPreferenceV1{Value: json.RawMessage("null")}
	}
	value, err := api.config.GetPreference(request.Context(), identity, key)
	if err != nil {
		return protocol.AdminCollectionListPreferenceV1{Error: api.adminReadError(request, err)}
	}
	if len(value) == 0 {
		value = json.RawMessage("null")
	}
	return protocol.AdminCollectionListPreferenceV1{Value: value}
}

func (api *API) adminReadError(request *http.Request, err error) *protocol.ErrorPayload {
	requestID, _ := request.Context().Value(adminPreparedRequestIDContextKey{}).(string)
	if requestID == "" {
		requestID = newRequestID()
	}
	payload := publicErrorPayload(requestID, err)
	if payload.Status >= 500 {
		api.reportRequestError(request, requestID, err, false, "")
	}
	return &payload
}

func (api *API) readCollectionPage(ctx context.Context, slug string, identity *AuthIdentity, options listQuery) (protocol.CollectionPageEnvelope[json.RawMessage], error) {
	result, err := api.config.Engine.Execute(ctx, operationengine.Request{
		Operation: operation.Read, Collection: slug, Filter: options.filter,
		Draft: options.draft,
		Page:  options.page, Limit: options.limit, Actor: identityActor(identity), ActorCollection: identityCollection(identity), Sort: options.sort,
		Select: options.selectFields, OutputFields: options.outputFields, Populate: options.populate, TrashOnly: options.trashOnly,
		IncludeAccess: options.includeAccess, Locale: options.locale, FallbackLocales: options.fallbackLocales,
		DisableFallback: options.disableFallback, AllLocales: options.allLocales,
	})
	if err != nil {
		return protocol.CollectionPageEnvelope[json.RawMessage]{}, err
	}
	page := result.Page
	totalPages := 0
	if page.Total > 0 {
		totalPages = (page.Total + page.Limit - 1) / page.Limit
	}
	docs := make([]json.RawMessage, len(page.Documents))
	for index, document := range page.Documents {
		docs[index] = documentJSON(document)
	}
	value := protocol.CollectionPageEnvelope[json.RawMessage]{Docs: docs, Pagination: protocol.Pagination{
		Page: page.Page, Limit: page.Limit, TotalDocs: page.Total, TotalPages: totalPages, HasNextPage: page.Page < totalPages, HasPrevPage: page.Page > 1,
	}}
	if options.includeAccess {
		value.Access = protocol.CollectionPageAccess{Collection: accessCapabilitiesJSON(result.PageAccess.Collection), Documents: map[string]protocol.AccessCapabilitiesEnvelope{}}
		for id, access := range result.PageAccess.Documents {
			value.Access.Documents[id] = accessCapabilitiesJSON(access)
		}
	}
	return value, nil
}

func (api *API) readCollectionCount(ctx context.Context, slug string, identity *AuthIdentity, options listQuery) (int, error) {
	result, err := api.config.Engine.Execute(ctx, operationengine.Request{
		Operation: operation.Read, Collection: slug, Filter: options.filter, Page: 1, Limit: 1,
		Draft: options.draft,
		Actor: identityActor(identity), ActorCollection: identityCollection(identity), TrashOnly: options.trashOnly,
		Locale: options.locale, FallbackLocales: options.fallbackLocales, DisableFallback: options.disableFallback, AllLocales: options.allLocales,
	})
	if err != nil {
		return 0, err
	}
	return result.Page.Total, nil
}

func validListLimit(value int) bool { return value == 10 || value == 25 || value == 50 || value == 100 }

func adminIntegerQuery(value string) (int, bool) {
	if strings.TrimSpace(value) == "" {
		return 0, false
	}
	number, err := adminJavaScriptNumber(value)
	if err != nil || math.IsInf(number, 0) || math.IsNaN(number) || math.Trunc(number) != number || number > math.MaxInt || number < math.MinInt {
		return 0, false
	}
	return int(number), true
}

func (api *API) adminListWhere(snapshot schema.Snapshot, slug string, values url.Values, statusOverride *string) any {
	parts := make([]map[string]any, 0, 4)
	collection := adminCollection(snapshot, slug)
	if collection == nil {
		return nil
	}
	if search := strings.TrimSpace(values.Get("q")); search != "" {
		if field := adminTitleField(*collection); field != nil && !field.QueryRestricted {
			parts = append(parts, map[string]any{field.Name: map[string]any{"like": search}})
		}
	}
	status := values.Get("status")
	if statusOverride != nil {
		status = *statusOverride
	}
	if status != "" {
		if field, options := adminStatusField(snapshot, slug); field != "" && containsString(options, status) {
			parts = append(parts, map[string]any{field: map[string]any{"equals": status}})
		}
	}
	if folder := values.Get("folder"); folder != "" {
		if field := adminFieldByPath(collection.Fields, collection.Admin.FolderField); field != nil && !field.QueryRestricted {
			parts = append(parts, map[string]any{field.Path.String(): map[string]any{"equals": folder}})
		}
	}
	var filters [][]struct {
		Field    string `json:"field"`
		Operator string `json:"operator"`
		Value    any    `json:"value"`
	}
	filterFields := adminFilterableFields(collection.Fields, false)
	for _, name := range []string{"id", "createdAt", "updatedAt", "_status"} {
		if name == "_status" && collection.Versions == nil {
			continue
		}
		path, _ := query.ParsePath(name)
		kind := schema.FieldTypeText
		if name == "createdAt" || name == "updatedAt" {
			kind = schema.FieldTypeDate
		}
		if name == "_status" {
			kind = schema.FieldTypeSelect
		}
		filterFields = append(filterFields, schema.Field{Name: name, Path: path, Type: kind})
	}
	if json.Unmarshal([]byte(values.Get("filters")), &filters) == nil {
		groups := make([]map[string]any, 0, len(filters))
		for _, group := range filters {
			conditions := make([]map[string]any, 0, len(group))
			for _, filter := range group {
				field := adminFieldByPath(filterFields, filter.Field)
				if field == nil || !adminFilterOperatorAllowed(*field, filter.Operator) {
					continue
				}
				value, valid := adminFilterValue(*field, filter.Operator, filter.Value)
				if !valid {
					continue
				}
				conditions = append(conditions, map[string]any{filter.Field: map[string]any{filter.Operator: value}})
			}
			if len(conditions) == 1 {
				groups = append(groups, conditions[0])
			} else if len(conditions) > 1 {
				groups = append(groups, map[string]any{"and": conditions})
			}
		}
		if len(groups) == 1 {
			parts = append(parts, groups[0])
		} else if len(groups) > 1 {
			parts = append(parts, map[string]any{"or": groups})
		}
	}

	if len(parts) == 0 {
		return nil
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return map[string]any{"and": parts}
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func adminFieldByPath(fields []schema.Field, path string) *schema.Field {
	for index := range fields {
		if fields[index].Path.String() == path || fields[index].Path.String() == "" && fields[index].Name == path {
			return &fields[index]
		}
	}
	return nil
}

func adminFilterableFields(fields []schema.Field, inheritedRestricted bool) []schema.Field {
	result := make([]schema.Field, 0, len(fields))
	for _, field := range fields {
		// A restricted container makes every descendant path restricted even when
		// the leaf itself does not repeat QueryRestricted in the manifest.
		restricted := inheritedRestricted || field.QueryRestricted
		switch field.Type {
		case schema.FieldTypeGroup, schema.FieldTypeArray:
			if field.Nested != nil {
				result = append(result, adminFilterableFields(field.Nested.Fields, restricted)...)
			}
			continue
		case schema.FieldTypeBlocks:
			if field.Blocks != nil {
				for _, block := range field.Blocks.ResolvedTypes() {
					result = append(result, adminFilterableFields(block.Fields, restricted)...)
				}
			}
			continue
		}
		if restricted || field.Virtual != nil || field.Join != nil || field.Plugin != nil || field.Type == schema.FieldTypeUI || field.Type == schema.FieldTypeJSON || field.Type == schema.FieldTypePoint || field.Type == schema.FieldTypeCode {
			continue
		}
		result = append(result, field)
	}
	return result
}

func adminFilterOperatorAllowed(field schema.Field, operator string) bool {
	var allowed []string
	switch field.Type {
	case schema.FieldTypeTextList, schema.FieldTypeNumberList:
		allowed = []string{"in", "exists"}
	case schema.FieldTypeNumber, schema.FieldTypeDate:
		allowed = []string{"equals", "notEquals", "greaterThan", "greaterThanEqual", "lessThan", "lessThanEqual", "exists"}
	case schema.FieldTypeText, schema.FieldTypeTextarea, schema.FieldTypeEmail:
		allowed = []string{"like", "equals", "notEquals", "contains", "exists"}
	default:
		allowed = []string{"equals", "notEquals", "exists"}
	}
	return containsString(allowed, operator)
}

func adminFilterValue(field schema.Field, operator string, raw any) (any, bool) {
	value := ""
	switch typed := raw.(type) {
	case string:
		value = typed
	case float64:
		if typed == 0 {
			typed = 0
		}
		encoded, err := json.Marshal(typed)
		if err != nil {
			return nil, false
		}
		value = string(encoded)
	case bool:
		value = strconv.FormatBool(typed)
	}
	if operator == "exists" {
		return value != "false", true
	}
	if value == "" && field.Type != schema.FieldTypeTextList {
		return nil, false
	}
	var normalized any = value
	if field.Type == schema.FieldTypeNumber || field.Type == schema.FieldTypeNumberList {
		number, err := adminJavaScriptNumber(value)
		if err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
			return nil, false
		}
		normalized = number
	} else if field.Type == schema.FieldTypeCheckbox {
		normalized = value == "true"
	}
	if operator == "in" {
		return []any{normalized}, true
	}
	return normalized, true
}

func adminJavaScriptNumber(value string) (float64, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, nil
	}
	lower := strings.ToLower(trimmed)
	var base int
	switch {
	case strings.HasPrefix(lower, "0x"):
		base = 16
	case strings.HasPrefix(lower, "0o"):
		base = 8
	case strings.HasPrefix(lower, "0b"):
		base = 2
	default:
		return strconv.ParseFloat(trimmed, 64)
	}
	integer, valid := new(big.Int).SetString(lower[2:], base)
	if !valid {
		return 0, strconv.ErrSyntax
	}
	number, _ := new(big.Float).SetInt(integer).Float64()
	return number, nil
}

func adminCollection(snapshot schema.Snapshot, slug string) *schema.Collection {
	for index := range snapshot.Collections {
		if string(snapshot.Collections[index].Slug) == slug {
			return &snapshot.Collections[index]
		}
	}
	return nil
}

func adminTitleField(collection schema.Collection) *schema.Field {
	for index := range collection.Fields {
		if collection.Admin.UseAsTitle != "" && collection.Fields[index].Name == collection.Admin.UseAsTitle {
			return &collection.Fields[index]
		}
	}
	for _, name := range []string{"title", "name"} {
		for index := range collection.Fields {
			if collection.Fields[index].Name == name {
				return &collection.Fields[index]
			}
		}
	}
	for index := range collection.Fields {
		if collection.Fields[index].Type == schema.FieldTypeText {
			return &collection.Fields[index]
		}
	}
	for index := range collection.Fields {
		switch collection.Fields[index].Type {
		case schema.FieldTypeEmail, schema.FieldTypeSelect, schema.FieldTypeRadio:
			return &collection.Fields[index]
		}
	}
	return nil
}

func adminListColumnFields(collection schema.Collection) []schema.Field {
	var result []schema.Field
	var appendFields func([]schema.Field, bool)
	appendFields = func(fields []schema.Field, inheritedRestricted bool) {
		for _, field := range fields {
			field.QueryRestricted = field.QueryRestricted || inheritedRestricted
			if field.Type == schema.FieldTypeUI {
				continue
			}
			if field.Type == schema.FieldTypeGroup {
				if field.Nested != nil {
					appendFields(field.Nested.Fields, field.QueryRestricted)
				}
				continue
			}
			result = append(result, field)
		}
	}
	appendFields(collection.Fields, false)
	return result
}

func adminListSortAllowed(collection schema.Collection, raw string) bool {
	name := strings.TrimPrefix(raw, "-")
	if name == "id" || name == "createdAt" || name == "updatedAt" || name == "_status" && collection.Versions != nil {
		return true
	}
	fields := adminListColumnFields(collection)
	if title := adminTitleField(collection); title != nil {
		fields = append([]schema.Field{*title}, fields...)
	}
	field := adminFieldByPath(fields, name)
	if field == nil || field.QueryRestricted || field.Virtual != nil || field.Join != nil || field.Plugin != nil {
		return false
	}
	switch field.Type {
	case schema.FieldTypeTextList, schema.FieldTypeNumberList, schema.FieldTypeArray,
		schema.FieldTypeBlocks, schema.FieldTypeGroup, schema.FieldTypeJSON,
		schema.FieldTypePoint, schema.FieldTypeCode:
		return false
	default:
		return true
	}
}

func adminListPopulation(snapshot schema.Snapshot, collection schema.Collection, workspace json.RawMessage, query url.Values, listCellFields []string) map[string]any {
	fields := adminListColumnFields(collection)

	// Custom cells receive document values directly, not the standard relationship-label population.
	customPaths := make(map[string]bool, len(listCellFields))
	for _, name := range listCellFields {
		for _, field := range collection.Fields {
			if field.Path.String() == name || field.Name == name {
				customPaths[field.Path.String()] = true
				break
			}
		}
	}
	available := make([]schema.Field, 0, len(fields))
	for _, field := range fields {
		if !customPaths[field.Path.String()] {
			available = append(available, field)
		}
	}
	defaults := make([]string, 0, 2)
	for _, configured := range collection.Admin.DefaultColumns {
		if field := adminFieldByPath(available, configured); field != nil {
			defaults = append(defaults, field.Path.String())
		}
	}
	if len(defaults) == 0 {
		for index := 0; index < len(available) && index < 1; index++ {
			defaults = append(defaults, available[index].Path.String())
		}
	}
	columns := defaults
	// Precedence mirrors the mounted list controller: saved workspace selection
	// replaces defaults, and an explicit URL selection replaces both.
	if len(workspace) != 0 {
		var decoded map[string]json.RawMessage
		var storedColumns []any
		if json.Unmarshal(workspace, &decoded) == nil && json.Unmarshal(decoded["columns"], &storedColumns) == nil && storedColumns != nil {
			columns = nil
			for _, value := range storedColumns {
				entry, ok := value.(map[string]any)
				if !ok {
					continue
				}
				name, _ := entry["path"].(string)
				active, _ := entry["active"].(bool)
				if active && adminFieldByPath(available, name) != nil {
					columns = append(columns, name)
				}
			}
		}
	}
	if _, present := query["columns"]; present {
		columns = adminEligibleColumnNames(uniqueNonEmptyStrings(strings.Split(query.Get("columns"), ",")), available)
	}
	population := map[string]any{}
	for _, name := range columns {
		field := adminFieldByPath(available, name)
		if field == nil {
			continue
		}
		targets := adminPopulationTargets(*field)
		if len(targets) == 0 {
			continue
		}
		selectFields := map[string]any{"id": true}
		for _, target := range targets {
			if targetCollection := adminCollection(snapshot, target); targetCollection != nil {
				if title := adminTitleField(*targetCollection); title != nil {
					selectFields[title.Name] = true
				}
			}
		}
		population[field.Path.String()] = map[string]any{"select": selectFields}
	}
	if len(population) == 0 {
		return nil
	}
	return population
}

func adminEligibleColumnNames(names []string, fields []schema.Field) []string {
	result := make([]string, 0, len(names))
	for _, name := range names {
		if adminFieldByPath(fields, name) != nil {
			result = append(result, name)
		}
	}
	return result
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func adminPopulationTargets(field schema.Field) []string {
	if field.Relationship != nil {
		if len(field.Relationship.Targets) != 0 {
			targets := make([]string, len(field.Relationship.Targets))
			for index, target := range field.Relationship.Targets {
				targets[index] = string(target.CollectionSlug)
			}
			return targets
		}
		if field.Relationship.CollectionSlug != "" {
			return []string{string(field.Relationship.CollectionSlug)}
		}
	}
	if field.Upload != nil {
		return []string{string(field.Upload.CollectionSlug)}
	}
	return nil
}

func adminStatusField(snapshot schema.Snapshot, slug string) (string, []string) {
	for _, collection := range snapshot.Collections {
		if string(collection.Slug) != slug {
			continue
		}
		for _, field := range collection.Fields {
			if field.Name == "status" && field.Type == schema.FieldTypeSelect && field.Select != nil && !field.QueryRestricted {
				values := make([]string, len(field.Select.Options))
				for index, option := range field.Select.Options {
					values[index] = option.Value
				}
				return field.Name, values
			}
		}
	}
	return "", nil
}
