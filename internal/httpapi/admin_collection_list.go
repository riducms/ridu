package httpapi

import (
	"context"
	"encoding/json"
	"maps"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/riducms/ridu/internal/membership"
	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
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
	snapshot, _, err := api.presentedManifest(request.Context(), identity)
	if err != nil {
		api.writeError(writer, requestID, err)
		return
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
	extensions := adminListExtensions{cells: map[string][]string{}, fields: uniqueNonEmptyStrings(strings.Split(request.URL.Query().Get("fields"), ","))}
	for _, cell := range uniqueNonEmptyStrings(strings.Split(request.URL.Query().Get("cells"), ",")) {
		extensions.cells[cell] = nil
	}
	data := api.loadAdminCollectionList(request, snapshot, identity, slug, request.URL.Query().Get("trash") == "true", request.URL.Query().Get("locale"), extensions, part, nil)
	writeJSON(writer, http.StatusOK, data)
}

// adminListExtensions describes the custom cells and results layout registered for
// one collection's list. The prepared route reads them from build metadata. Browser
// reads don't depend on build metadata, so the admin sends its custom cells' fields
// and every field its extensions declare instead.
type adminListExtensions struct {
	// cells maps each custom cell's field to the other fields that cell reads.
	cells map[string][]string
	// fields are read whichever columns are active: the results layout's
	// declaration, or everything a browser read's extensions declare.
	fields []string
}

func (extensions adminListExtensions) cellFields() []string {
	return slices.Sorted(maps.Keys(extensions.cells))
}

// declaredFields returns the layout's fields and those of every custom cell whose
// column is active, matching the fields the mounted list sends.
func (extensions adminListExtensions) declaredFields(collection schema.Collection, columns []string) []string {
	declared := slices.Clone(extensions.fields)
	fields := adminListColumnFields(collection)
	for cell, cellFields := range extensions.cells {
		if field := adminFieldByPath(fields, cell); field != nil && slices.Contains(columns, field.Path.String()) {
			declared = append(declared, cellFields...)
		}
	}
	return declared
}

// loadAdminCollectionList owns list query planning for cold documents, router
// navigation, browser fallback, refresh and focused retry. Each read still has
// its own ordinary engine lifecycle; this is not a batch transaction or cache.
func (api *API) loadAdminCollectionList(request *http.Request, snapshot schema.Snapshot, identity *AuthIdentity, slug string, trash bool, locale string, extensions adminListExtensions, part string, workspace json.RawMessage) protocol.AdminCollectionListDataV1 {
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
	hierarchy := !trash && collection.Admin.ParentField != "" && values.Get("view") == "hierarchy"
	if hierarchy {
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
		columns := adminListColumns(*collection, workspace, values, extensions.cellFields())
		declared := extensions.declaredFields(*collection, columns)
		// The admin reads again when these don't cover its own columns, as when a
		// navigation resolved another tab's saved layout.
		data.Query.Columns, data.Query.Fields = columns, slices.Sorted(slices.Values(uniqueNonEmptyStrings(declared)))
		if population := adminListPopulation(snapshot, *collection, columns, extensions.cellFields(), declared); population != nil {
			encoded, _ := json.Marshal(population)
			parameters.Set("populate", string(encoded))
		}
		if selection := adminListSelection(*collection, columns, hierarchy, declared); selection != nil {
			encoded, _ := json.Marshal(selection)
			parameters.Set("select", string(encoded))
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
		Page:  options.page, Limit: options.limit, SkipTotal: options.skipTotal, Actor: identityActor(identity), ActorCollection: identityCollection(identity), Sort: options.sort,
		Select: options.selectFields, OutputFields: options.outputFields, Populate: options.populate, TrashOnly: options.trashOnly,
		IncludeAccess: options.includeAccess, Locale: options.locale, FallbackLocales: options.fallbackLocales,
		DisableFallback: options.disableFallback, AllLocales: options.allLocales,
	})
	if err != nil {
		return protocol.CollectionPageEnvelope[json.RawMessage]{}, err
	}
	page := result.Page
	docs := make([]json.RawMessage, len(page.Documents))
	for index, document := range page.Documents {
		docs[index] = documentJSON(document)
	}
	value := protocol.CollectionPageEnvelope[json.RawMessage]{Docs: docs, Pagination: paginationJSON(*page)}
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
	return *result.Page.Total, nil
}

// paginationJSON reports totals only for a counted page; HasNextPage comes
// from the adapter in both modes.
func paginationJSON(page store.Page) protocol.Pagination {
	pagination := protocol.Pagination{Page: page.Page, Limit: page.Limit, HasNextPage: page.HasNextPage, HasPrevPage: page.Page > 1}
	if page.Total != nil {
		total := *page.Total
		totalPages := (total + page.Limit - 1) / page.Limit
		pagination.TotalDocs, pagination.TotalPages = &total, &totalPages
	}
	return pagination
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
	var filterFields []schema.Field
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
				if field == nil {
					field = adminFilterableField(collection.Fields, filter.Field)
				}
				if field == nil || !adminFilterOperatorAllowed(*field, filter.Operator) {
					continue
				}
				value, valid := adminFilterValue(*field, filter.Operator, filter.Value)
				if !valid {
					continue
				}
				if filter.Operator == "notIn" {
					// The query language negates membership rather than naming its opposite.
					conditions = append(conditions, map[string]any{"not": map[string]any{filter.Field: map[string]any{"in": value}}})
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

// adminFilterableField resolves a list filter's field from its canonical path
// through groups, arrays and block variants, creating placement views only
// along that path. A restricted container makes every descendant path
// restricted even when the leaf itself does not repeat QueryRestricted.
func adminFilterableField(fields []schema.Field, path string) *schema.Field {
	segments := strings.Split(path, ".")
	restricted := false
	for index := 0; index < len(segments); {
		var found *schema.Field
		for candidate := range fields {
			if fields[candidate].Name == segments[index] {
				found = &fields[candidate]
				break
			}
		}
		if found == nil {
			return nil
		}
		restricted = restricted || found.QueryRestricted
		index++
		switch {
		case found.Type == schema.FieldTypeGroup || found.Type == schema.FieldTypeArray:
			if found.Nested == nil || index == len(segments) {
				return nil
			}
			fields = found.Nested.ResolvedFields()
		case found.Type == schema.FieldTypeBlocks:
			if found.Blocks == nil || index == len(segments) {
				return nil
			}
			fields = nil
			for _, block := range found.Blocks.ResolvedTypes() {
				if block.Slug == segments[index] {
					fields = block.ResolvedFields()
				}
			}
			index++
		case index < len(segments) || restricted || found.Virtual != nil || found.Join != nil || found.Plugin != nil || found.Type == schema.FieldTypeUI || found.Type == schema.FieldTypeJSON || found.Type == schema.FieldTypePoint || found.Type == schema.FieldTypeCode:
			return nil
		default:
			return found
		}
	}
	return nil
}

// adminFilterOperatorAllowed mirrors the admin's operator menu. Lists, has-many
// fields and polymorphic relationships offer membership only.
func adminFilterOperatorAllowed(field schema.Field, operator string) bool {
	var allowed []string
	switch {
	case membership.KindOf(field) != membership.None:
		allowed = []string{"in", "notIn", "exists"}
	case field.Type == schema.FieldTypeNumber || field.Type == schema.FieldTypeDate:
		allowed = []string{"equals", "notEquals", "greaterThan", "greaterThanEqual", "lessThan", "lessThanEqual", "exists"}
	case field.Type == schema.FieldTypeText || field.Type == schema.FieldTypeTextarea || field.Type == schema.FieldTypeEmail:
		allowed = []string{"like", "equals", "notEquals", "contains", "exists"}
	default:
		allowed = []string{"equals", "notEquals", "exists"}
	}
	return containsString(allowed, operator)
}

// adminFilterCandidates reads the candidates of a membership condition: one
// string or a list of them. Number lists compare numbers, and a polymorphic
// relationship's candidate "collection:id" becomes a {relationTo, id}
// reference; collection slugs never contain a colon.
func adminFilterCandidates(field schema.Field, raw any) ([]any, bool) {
	var items []any
	switch typed := raw.(type) {
	case string:
		items = []any{typed}
	case []any:
		items = typed
	default:
		return nil, false
	}
	kind := membership.KindOf(field)
	candidates := make([]any, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok || text == "" {
			return nil, false
		}
		switch kind {
		case membership.Numbers:
			number, err := adminJavaScriptNumber(text)
			if err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
				return nil, false
			}
			candidates = append(candidates, number)
		case membership.References:
			relationTo, id, found := strings.Cut(text, ":")
			if !found || relationTo == "" || id == "" {
				return nil, false
			}
			candidates = append(candidates, map[string]any{"relationTo": relationTo, "id": id})
		default:
			candidates = append(candidates, text)
		}
	}
	return candidates, len(candidates) > 0
}

func adminFilterValue(field schema.Field, operator string, raw any) (any, bool) {
	if operator == "in" || operator == "notIn" {
		return adminFilterCandidates(field, raw)
	}
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
	if value == "" {
		return nil, false
	}
	var normalized any = value
	if field.Type == schema.FieldTypeNumber {
		number, err := adminJavaScriptNumber(value)
		if err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
			return nil, false
		}
		normalized = number
	} else if field.Type == schema.FieldTypeCheckbox {
		normalized = value == "true"
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
	// The first field named title or name, matching the admin's displayed title.
	for index := range collection.Fields {
		if collection.Fields[index].Name == "title" || collection.Fields[index].Name == "name" {
			return &collection.Fields[index]
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

// adminListColumnFields mirrors the admin's list columns: group leaves become
// columns, while UI fields and blocks inside groups never do.
func adminListColumnFields(collection schema.Collection) []schema.Field {
	var result []schema.Field
	var appendFields func([]schema.Field, bool, bool)
	appendFields = func(fields []schema.Field, inheritedRestricted, nested bool) {
		for _, field := range fields {
			field.QueryRestricted = field.QueryRestricted || inheritedRestricted
			if field.Type == schema.FieldTypeUI || nested && field.Type == schema.FieldTypeBlocks {
				continue
			}
			if field.Type == schema.FieldTypeGroup {
				if field.Nested != nil {
					appendFields(field.Nested.Fields, field.QueryRestricted, true)
				}
				continue
			}
			result = append(result, field)
		}
	}
	appendFields(collection.Fields, false, false)
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

// adminListColumns resolves the active column paths in the mounted list's
// precedence: the configured defaults plus every custom cell, replaced by a saved
// workspace selection, replaced in turn by an explicit URL selection.
func adminListColumns(collection schema.Collection, workspace json.RawMessage, query url.Values, cellFields []string) []string {
	fields := adminListColumnFields(collection)
	resolve := func(names []string) []string {
		paths := make([]string, 0, len(names))
		for _, name := range names {
			if field := adminFieldByPath(fields, name); field != nil {
				paths = append(paths, field.Path.String())
			}
		}
		return uniqueNonEmptyStrings(paths)
	}
	columns := resolve(collection.Admin.DefaultColumns)
	if len(columns) == 0 && len(fields) != 0 {
		columns = []string{fields[0].Path.String()}
	}
	columns = resolve(append(columns, cellFields...))
	if len(workspace) != 0 {
		var decoded map[string]json.RawMessage
		var storedColumns []any
		if json.Unmarshal(workspace, &decoded) == nil && json.Unmarshal(decoded["columns"], &storedColumns) == nil && storedColumns != nil {
			var active []string
			for _, value := range storedColumns {
				entry, ok := value.(map[string]any)
				if !ok {
					continue
				}
				name, _ := entry["path"].(string)
				if selected, _ := entry["active"].(bool); selected {
					active = append(active, name)
				}
			}
			columns = resolve(active)
		}
	}
	if _, present := query["columns"]; present {
		columns = resolve(strings.Split(query.Get("columns"), ","))
	}
	return columns
}

// adminListPopulation reads the ID and title of the documents that relationship
// and upload columns name. Declared fields get the same, so their shape doesn't
// depend on whether their column is visible.
func adminListPopulation(snapshot schema.Snapshot, collection schema.Collection, columns, cellFields, declared []string) map[string]any {
	fields := adminListColumnFields(collection)
	columns = slices.Clone(columns)
	for _, name := range declared {
		if field, exists := topLevelField(collection, name); exists && adminFieldByPath(fields, field.Path.String()) != nil {
			columns = append(columns, field.Path.String())
		}
	}

	// Custom cells receive document values directly, not the standard relationship-label population.
	customPaths := make(map[string]bool, len(cellFields))
	for _, name := range cellFields {
		for _, field := range collection.Fields {
			if field.Path.String() == name || field.Name == name {
				customPaths[field.Path.String()] = true
				break
			}
		}
	}
	population := map[string]any{}
	for _, name := range columns {
		field := adminFieldByPath(fields, name)
		if field == nil || customPaths[field.Path.String()] {
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

// adminListSelection names the top-level fields a list page renders: the title,
// the active columns (a group's column reads its group), the parent field in a
// hierarchy and the fields list extensions declare. Filters, sorting and access
// are store predicates, so they need no returned values. Metadata is always
// returned. Nil reads complete documents when the set exceeds the select limit.
func adminListSelection(collection schema.Collection, columns []string, hierarchy bool, declared []string) map[string]bool {
	selection := map[string]bool{}
	add := func(name string) {
		if field, exists := topLevelField(collection, name); exists && selectableField(field) {
			selection[name] = true
		}
	}
	if title := adminTitleField(collection); title != nil {
		add(title.Name)
	}
	fields := adminListColumnFields(collection)
	for _, column := range columns {
		if field := adminFieldByPath(fields, column); field != nil && field.Path.Len() != 0 {
			add(field.Path.Segment(0))
		}
	}
	if hierarchy {
		add(collection.Admin.ParentField)
	}
	for _, name := range declared {
		add(name)
	}
	if len(selection) > maxSelectionFields {
		return nil
	}
	return selection
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
