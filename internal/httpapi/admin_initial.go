package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"runtime/debug"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/schema"
	unicodeencoding "golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

const maxAdminPreparedRouteBytes = 2 << 20

type adminBootstrapMetadata struct {
	DashboardLoaders     []string            `json:"dashboardLoaders"`
	RouteLoaders         map[string]string   `json:"routeLoaders"`
	ViewLoaders          map[string]string   `json:"viewLoaders"`
	ProtocolVersion      uint32              `json:"protocolVersion"`
	BuildID              string              `json:"buildId"`
	DocumentViewRoutes   []string            `json:"documentViewRoutes"`
	ExtensionRoutes      []string            `json:"extensionRoutes"`
	ListCellFields       map[string][]string `json:"listCellFields"`
	ModuleGroups         map[string][]string `json:"moduleGroups"`
	ReplacedCoreViews    []string            `json:"replacedCoreViews"`
	SeedableCoreSurfaces []string            `json:"seedableCoreSurfaces"`
}

type adminPreparedRequestIDContextKey struct{}

type adminPreparedIdentity struct {
	identity        *AuthIdentity
	session         *protocol.AuthSession[map[string]any]
	resolvedSession *AuthSession
}

type adminRouteClassification struct {
	loaders        []string
	kind           protocol.AdminPreparedRouteKindV1
	surface        string
	segments       []string
	location       string
	listCellFields []string
}

func (api *API) adminBootstrapMetadata() (adminBootstrapMetadata, error) {
	// AdminAssets may be application-supplied. Treat its metadata as a build
	// boundary and verify every referenced asset before using it for routing.
	encoded, err := fs.ReadFile(api.config.AdminAssets, "ridu-admin-bootstrap.json")
	if err != nil {
		return adminBootstrapMetadata{}, err
	}
	var metadata adminBootstrapMetadata
	if err := json.Unmarshal(encoded, &metadata); err != nil {
		return adminBootstrapMetadata{}, err
	}
	if metadata.ProtocolVersion != protocol.AdminPreparedRouteStateVersion || metadata.BuildID == "" {
		return adminBootstrapMetadata{}, fmt.Errorf("incompatible admin bootstrap metadata")
	}
	if !validAdminBuildID(metadata.BuildID) {
		return adminBootstrapMetadata{}, fmt.Errorf("invalid admin bootstrap build ID")
	}
	knownGroups := map[string]bool{"entry": true, "document": true, "date": true, "upload-preview": true, "document-api": true, "versions": true, "bulk-upload": true}
	for group, assets := range metadata.ModuleGroups {
		if !knownGroups[group] || !sort.StringsAreSorted(assets) || hasDuplicateStrings(assets) {
			return adminBootstrapMetadata{}, fmt.Errorf("invalid admin bootstrap module group %q", group)
		}
		for _, asset := range assets {
			if !validAdminAssetPath(asset) {
				return adminBootstrapMetadata{}, fmt.Errorf("invalid admin bootstrap asset path")
			}
			info, statError := fs.Stat(api.config.AdminAssets, asset)
			if statError != nil || info.IsDir() {
				return adminBootstrapMetadata{}, fmt.Errorf("admin bootstrap asset is unavailable")
			}
		}
	}
	for group := range knownGroups {
		if len(metadata.ModuleGroups[group]) == 0 {
			return adminBootstrapMetadata{}, fmt.Errorf("admin bootstrap module group %q is unavailable", group)
		}
	}
	if !sort.StringsAreSorted(metadata.SeedableCoreSurfaces) || hasDuplicateStrings(metadata.SeedableCoreSurfaces) || !sort.StringsAreSorted(metadata.ReplacedCoreViews) || hasDuplicateStrings(metadata.ReplacedCoreViews) || !sort.StringsAreSorted(metadata.ExtensionRoutes) || hasDuplicateStrings(metadata.ExtensionRoutes) || !sort.StringsAreSorted(metadata.DocumentViewRoutes) || hasDuplicateStrings(metadata.DocumentViewRoutes) {
		return adminBootstrapMetadata{}, fmt.Errorf("invalid admin bootstrap surface metadata")
	}
	for _, route := range metadata.ExtensionRoutes {
		if !schema.IsValidAdminPluginRoute(route) {
			return adminBootstrapMetadata{}, fmt.Errorf("invalid admin extension route")
		}
	}
	for _, route := range metadata.DocumentViewRoutes {
		target, view, found := strings.Cut(route, ":")
		if !found || target == "" || view == "" || strings.ContainsAny(target, "/:") || strings.ContainsAny(view, "/:") {
			return adminBootstrapMetadata{}, fmt.Errorf("invalid admin document view route")
		}
	}
	for _, surface := range metadata.SeedableCoreSurfaces {
		if !validAdminCoreSurface(surface) {
			return adminBootstrapMetadata{}, fmt.Errorf("invalid seedable admin surface")
		}
	}
	for _, replacement := range metadata.ReplacedCoreViews {
		surface, target, found := strings.Cut(replacement, ":")
		if !found || !validAdminCoreSurface(surface) || target == "" || strings.ContainsAny(target, "/:") {
			return adminBootstrapMetadata{}, fmt.Errorf("invalid replaced admin surface")
		}
	}
	if metadata.BuildID != adminBootstrapBuildID(metadata) {
		return adminBootstrapMetadata{}, fmt.Errorf("admin bootstrap build ID does not match its metadata")
	}
	if !sort.StringsAreSorted(metadata.DashboardLoaders) || hasDuplicateStrings(metadata.DashboardLoaders) {
		return adminBootstrapMetadata{}, fmt.Errorf("dashboard loaders must be sorted and unique")
	}
	for _, key := range metadata.DashboardLoaders {
		if len(key) > 80 || !schema.IsValidCollectionSlug(key) {
			return adminBootstrapMetadata{}, fmt.Errorf("invalid dashboard loader key")
		}
	}
	for route, key := range metadata.RouteLoaders {
		if !slices.Contains(metadata.ExtensionRoutes, route) || len(key) > 80 || !schema.IsValidCollectionSlug(key) {
			return adminBootstrapMetadata{}, fmt.Errorf("invalid custom route loader")
		}
	}
	for view, key := range metadata.ViewLoaders {
		if !slices.Contains(metadata.ReplacedCoreViews, view) || len(key) > 80 || !schema.IsValidCollectionSlug(key) {
			return adminBootstrapMetadata{}, fmt.Errorf("invalid core view loader")
		}
	}
	return metadata, nil
}

func adminBootstrapBuildID(metadata adminBootstrapMetadata) string {
	// BuildID is deliberately omitted from its own digest. The remaining fields
	// are the complete static routing and module-selection contract.
	encoded, _ := json.Marshal(struct {
		DashboardLoaders     []string            `json:"dashboardLoaders"`
		RouteLoaders         map[string]string   `json:"routeLoaders"`
		ViewLoaders          map[string]string   `json:"viewLoaders"`
		ProtocolVersion      uint32              `json:"protocolVersion"`
		DocumentViewRoutes   []string            `json:"documentViewRoutes"`
		ExtensionRoutes      []string            `json:"extensionRoutes"`
		ListCellFields       map[string][]string `json:"listCellFields"`
		ModuleGroups         map[string][]string `json:"moduleGroups"`
		ReplacedCoreViews    []string            `json:"replacedCoreViews"`
		SeedableCoreSurfaces []string            `json:"seedableCoreSurfaces"`
	}{
		DashboardLoaders:     metadata.DashboardLoaders,
		RouteLoaders:         metadata.RouteLoaders,
		ViewLoaders:          metadata.ViewLoaders,
		ProtocolVersion:      metadata.ProtocolVersion,
		DocumentViewRoutes:   metadata.DocumentViewRoutes,
		ExtensionRoutes:      metadata.ExtensionRoutes,
		ListCellFields:       metadata.ListCellFields,
		ModuleGroups:         metadata.ModuleGroups,
		ReplacedCoreViews:    metadata.ReplacedCoreViews,
		SeedableCoreSurfaces: metadata.SeedableCoreSurfaces,
	})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:12])
}

func validAdminCoreSurface(surface string) bool {
	switch surface {
	case "account", "collectionCreate", "collectionEdit", "collectionList", "dashboard", "global", "login", "notFound", "setup":
		return true
	default:
		return false
	}
}

func validAdminBuildID(value string) bool {
	if len(value) != 24 {
		return false
	}
	for _, character := range value {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}

func validAdminAssetPath(value string) bool {
	return strings.HasPrefix(value, "assets/") && path.Clean(value) == value &&
		(strings.HasSuffix(value, ".js") || strings.HasSuffix(value, ".css"))
}

func hasDuplicateStrings(values []string) bool {
	for index := 1; index < len(values); index++ {
		if values[index-1] == values[index] {
			return true
		}
	}
	return false
}

func normalizeAdminRouteIdentity(request *http.Request) (string, string) {
	pathname := request.URL.EscapedPath()
	pathname = "/" + strings.TrimPrefix(pathname, "/")
	if len(pathname) > 1 {
		pathname = strings.TrimSuffix(pathname, "/")
	}
	values := adminSearchValues(request.URL.RawQuery)
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	// URLSearchParams.sort compares UTF-16 code units and preserves the order of
	// repeated values. Matching it keeps server and browser fingerprints equal.
	sort.Slice(keys, func(left, right int) bool { return adminUTF16Less(keys[left], keys[right]) })
	parts := make([]string, 0, len(values))
	for _, key := range keys {
		for _, value := range values[key] {
			parts = append(parts, adminSearchEscape(key)+"="+adminSearchEscape(value))
		}
	}
	search := strings.Join(parts, "&")
	if search != "" {
		search = "?" + search
	}
	return pathname, search
}

func adminUTF16Less(left, right string) bool {
	leftUnits := utf16.Encode([]rune(left))
	rightUnits := utf16.Encode([]rune(right))
	for index := 0; index < len(leftUnits) && index < len(rightUnits); index++ {
		if leftUnits[index] != rightUnits[index] {
			return leftUnits[index] < rightUnits[index]
		}
	}
	return len(leftUnits) < len(rightUnits)
}

func adminSearchValues(rawQuery string) url.Values {
	values := url.Values{}
	if rawQuery == "" {
		return values
	}
	for _, pair := range strings.Split(rawQuery, "&") {
		if pair == "" {
			continue
		}
		key, value, _ := strings.Cut(pair, "=")
		key = adminSearchDecode(key)
		values[key] = append(values[key], adminSearchDecode(value))
	}
	return values
}

func adminSearchDecode(value string) string {
	decoded := make([]byte, 0, len(value))
	for index := 0; index < len(value); index++ {
		switch {
		case value[index] == '+':
			decoded = append(decoded, ' ')
		case value[index] == '%' && index+2 < len(value):
			high, highOK := adminHexNibble(value[index+1])
			low, lowOK := adminHexNibble(value[index+2])
			if highOK && lowOK {
				decoded = append(decoded, high<<4|low)
				index += 2
				continue
			}
			decoded = append(decoded, value[index])
		default:
			decoded = append(decoded, value[index])
		}
	}
	valid, _, _ := transform.Bytes(unicodeencoding.UTF8.NewDecoder(), decoded)
	return string(valid)
}

func adminHexNibble(value byte) (byte, bool) {
	switch {
	case value >= '0' && value <= '9':
		return value - '0', true
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10, true
	case value >= 'A' && value <= 'F':
		return value - 'A' + 10, true
	default:
		return 0, false
	}
}

// adminSearchEscape matches URLSearchParams application/x-www-form-urlencoded
// serialization, which is the browser-side route identity contract.
func adminSearchEscape(value string) string {
	var encoded strings.Builder
	for _, character := range []byte(value) {
		switch {
		case character >= 'a' && character <= 'z',
			character >= 'A' && character <= 'Z',
			character >= '0' && character <= '9',
			character == '*', character == '-', character == '.', character == '_':
			encoded.WriteByte(character)
		case character == ' ':
			encoded.WriteByte('+')
		default:
			const hex = "0123456789ABCDEF"
			encoded.WriteByte('%')
			encoded.WriteByte(hex[character>>4])
			encoded.WriteByte(hex[character&0x0f])
		}
	}
	return encoded.String()
}

func (api *API) prepareAdminRuntime(ctx context.Context, request *http.Request, buildID string) (*protocol.AdminPreparedRuntimeV1, adminPreparedIdentity, string, error) {
	preparedIdentity := api.resolveAdminPreparedIdentity(request)

	// Visibility is identity-bound before any navigation or loader selection.
	// The resulting manifest is presentation metadata, never authorization.
	snapshot := api.config.Manifest.Snapshot()
	if api.config.ManifestForRequest != nil {
		var err error
		snapshot, err = api.config.ManifestForRequest(ctx, preparedIdentity.identity)
		if err != nil {
			return nil, preparedIdentity, "", err
		}
	}
	runtime := &protocol.AdminPreparedRuntimeV1{
		AdminPreparedNavigationV1: protocol.AdminPreparedNavigationV1{
			CollectionOperations: map[string]protocol.OperationCapabilities{},
			GlobalOperations:     map[string]protocol.OperationCapabilities{},
		},
		Manifest: snapshot, Theme: "system",
		Preferences: map[string]json.RawMessage{},
	}
	if snapshot.Application.Admin != nil && api.config.AuthBootstrapAvailable != nil {
		available, err := api.config.AuthBootstrapAvailable(ctx, string(snapshot.Application.Admin.UserCollectionSlug))
		if err != nil {
			return nil, preparedIdentity, "", err
		}
		runtime.AuthBootstrap = available
		if !available {
			runtime.Session = preparedIdentity.session
		}
	} else {
		runtime.Session = preparedIdentity.session
	}
	if snapshot.Application.Localization != nil {
		runtime.ContentLocale = string(snapshot.Application.Localization.DefaultLocale)
	}
	if snapshot.Application.AdminLocalization != nil {
		runtime.AdminLanguage = snapshot.Application.AdminLocalization.DefaultLanguage
		runtime.AdminTimeZone = snapshot.Application.AdminLocalization.DefaultTimeZone
	}

	// Preferences customize this response only after the session was resolved;
	// absent values are retained as explicit nulls for deterministic bootstrap.
	if runtime.Session != nil && api.config.GetPreference != nil {
		for _, key := range []string{"theme", "content-locale", "admin-language", "admin-timezone", "navigation"} {
			value, err := api.config.GetPreference(ctx, preparedIdentity.identity, key)
			if err != nil {
				var operationError *operationengine.Error
				if errors.As(err, &operationError) && operationError.Code == "not_found" {
					runtime.Preferences[key] = json.RawMessage("null")
					continue
				}
				return nil, preparedIdentity, "", err
			}
			runtime.Preferences[key] = append(json.RawMessage(nil), value...)
		}
		var theme string
		if json.Unmarshal(runtime.Preferences["theme"], &theme) == nil && (theme == "system" || theme == "light" || theme == "dark") {
			runtime.Theme = theme
		}
		var locale struct {
			Locale string `json:"locale"`
		}
		if json.Unmarshal(runtime.Preferences["content-locale"], &locale) == nil && adminLocaleAvailable(snapshot, locale.Locale) {
			runtime.ContentLocale = locale.Locale
		}
		_ = json.Unmarshal(runtime.Preferences["admin-language"], &runtime.AdminLanguage)
		_ = json.Unmarshal(runtime.Preferences["admin-timezone"], &runtime.AdminTimeZone)
	}
	if requested := request.URL.Query().Get("locale"); adminLocaleAvailable(snapshot, requested) {
		runtime.ContentLocale = requested
	}

	// Capabilities are advisory navigation state. Every later read and mutation
	// still enters the operation engine with the same actor and locale.
	for _, collection := range snapshot.Collections {
		capabilities, err := api.config.Engine.Capabilities(ctx, operationengine.CapabilitiesRequest{
			Collection: string(collection.Slug), Actor: identityActor(preparedIdentity.identity),
			ActorCollection: identityCollection(preparedIdentity.identity), Locale: runtime.ContentLocale,
		})
		if err != nil {
			return nil, preparedIdentity, "", err
		}
		runtime.CollectionOperations[string(collection.Slug)] = accessCapabilitiesJSON(capabilities).Operations
	}
	for _, global := range snapshot.Globals {
		capabilities, err := api.config.Engine.Capabilities(ctx, operationengine.CapabilitiesRequest{
			Collection: "global:" + string(global.Slug), ID: string(global.Slug), Actor: identityActor(preparedIdentity.identity),
			ActorCollection: identityCollection(preparedIdentity.identity), Locale: runtime.ContentLocale,
		})
		if err != nil {
			return nil, preparedIdentity, "", err
		}
		runtime.GlobalOperations[string(global.Slug)] = accessCapabilitiesJSON(capabilities).Operations
	}
	contextKey := adminContextKey(snapshot, runtime, preparedIdentity.identity, buildID)
	return runtime, preparedIdentity, contextKey, nil
}

func adminLocaleAvailable(snapshot schema.Snapshot, locale string) bool {
	if locale == "" || snapshot.Application.Localization == nil {
		return false
	}
	for _, candidate := range snapshot.Application.Localization.Locales {
		if string(candidate.Code) == locale {
			return true
		}
	}
	return false
}

func (api *API) resolveAdminPreparedIdentity(request *http.Request) adminPreparedIdentity {
	resolveSession := func(token string) adminPreparedIdentity {
		if api.config.Session == nil || strings.TrimSpace(token) == "" {
			return adminPreparedIdentity{}
		}
		session, err := api.config.Session(request.Context(), token)
		if err != nil {
			return adminPreparedIdentity{}
		}
		identity := &AuthIdentity{Collection: session.Collection, Actor: session.User}
		safe := sessionEnvelope(session).Session
		return adminPreparedIdentity{identity: identity, session: &safe, resolvedSession: &session}
	}
	// Preserve transport credential precedence: an explicit Session header does
	// not silently downgrade to a cookie, then Bearer and external auth follow.
	scheme, credential, hasAuthorization := strings.Cut(request.Header.Get("Authorization"), " ")
	if hasAuthorization && strings.EqualFold(scheme, "Session") {
		if resolved := resolveSession(strings.TrimSpace(credential)); resolved.identity != nil {
			return resolved
		}
	} else if cookie, err := request.Cookie(sessionCookie); err == nil {
		if resolved := resolveSession(cookie.Value); resolved.identity != nil {
			return resolved
		}
	}
	if hasAuthorization && strings.EqualFold(scheme, "Bearer") && api.config.AuthenticateAPIKey != nil {
		identity, err := api.config.AuthenticateAPIKey(request.Context(), strings.TrimSpace(credential))
		if err == nil {
			return adminPreparedIdentity{identity: &identity}
		}
	}
	if api.config.AuthenticateExternal != nil {
		identity, err := api.config.AuthenticateExternal(request.Context(), map[string][]string(request.Header.Clone()))
		if err == nil {
			return adminPreparedIdentity{identity: &identity}
		}
	}
	return adminPreparedIdentity{}
}

func adminContextKey(snapshot schema.Snapshot, runtime *protocol.AdminPreparedRuntimeV1, identity *AuthIdentity, buildID string) string {
	actor := "anonymous"
	if identity != nil {
		actor = string(identity.Collection) + ":" + identity.Actor.ID
	}
	sessionID := ""
	if runtime.Session != nil {
		sessionID = runtime.Session.ID
	}
	// Only the digest crosses the reuse boundary. Including both the safe session
	// payload and ID invalidates cached runtime state when identity metadata changes.
	encoded, _ := json.Marshal(struct {
		BuildID       string                                `json:"buildId"`
		Manifest      schema.Snapshot                       `json:"manifest"`
		Actor         string                                `json:"actor"`
		SessionID     string                                `json:"sessionId"`
		Session       *protocol.AuthSession[map[string]any] `json:"session,omitempty"`
		AuthBootstrap bool                                  `json:"authBootstrap"`
	}{buildID, snapshot, actor, sessionID, runtime.Session, runtime.AuthBootstrap})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:16])
}

func classifyAdminRoute(pathname, search string, runtime *protocol.AdminPreparedRuntimeV1, metadata adminBootstrapMetadata) adminRouteClassification {
	routerPath := strings.TrimPrefix(pathname, "/admin")
	if routerPath == "" {
		routerPath = "/"
	}
	segments := strings.Split(strings.Trim(routerPath, "/"), "/")
	if len(segments) == 1 && segments[0] == "" {
		segments = nil
	}
	for index, segment := range segments {
		decoded, err := url.PathUnescape(segment)
		if err != nil {
			return adminRouteClassification{kind: protocol.AdminPreparedRouteNotFound, surface: "notFound"}
		}
		segments[index] = decoded
	}
	authenticated := runtime.Manifest.Application.Admin == nil || runtime.Session != nil && runtime.CollectionOperations[runtime.Session.Collection].Admin
	if authenticated {
		if len(segments) == 1 && strings.EqualFold(segments[0], "login") {
			location := "/admin/"
			if redirect := redirectQuery(search); redirect != "" {
				location = redirect
			}
			return adminRouteClassification{kind: protocol.AdminPreparedRouteDashboard, location: location}
		}
		if len(segments) == 1 && strings.EqualFold(segments[0], "create-first-user") {
			return adminRouteClassification{kind: protocol.AdminPreparedRouteDashboard, location: "/admin/"}
		}
		if adminExtensionRoute(segments, runtime.Manifest, metadata) {
			return adminRouteClassification{kind: protocol.AdminPreparedRouteNotFound, surface: "extension", segments: segments}
		}
		return classifyAuthenticatedAdminRoute(segments)
	}
	if runtime.AuthBootstrap {
		if len(segments) == 1 && strings.EqualFold(segments[0], "create-first-user") {
			return adminRouteClassification{kind: protocol.AdminPreparedRouteSetup, surface: "setup", segments: segments}
		}
		return adminRouteClassification{kind: protocol.AdminPreparedRouteSetup, location: "/admin/create-first-user"}
	}
	if len(segments) == 1 && (strings.EqualFold(segments[0], "login") || strings.EqualFold(segments[0], "forgot-password") || strings.EqualFold(segments[0], "reset-password") || strings.EqualFold(segments[0], "request-verification") || strings.EqualFold(segments[0], "verify-email")) {
		return adminRouteClassification{kind: protocol.AdminPreparedRouteLogin, surface: "login", segments: segments}
	}
	if len(segments) == 1 && strings.EqualFold(segments[0], "create-first-user") {
		return adminRouteClassification{kind: protocol.AdminPreparedRouteLogin, location: "/admin/login"}
	}
	return adminRouteClassification{kind: protocol.AdminPreparedRouteLogin, location: "/admin/login?redirect=" + url.QueryEscape(routerPath+search)}
}

// Match the browser router's literal paths without turning an encoded slash
// inside a parameter into another path segment.
func adminLiteralRouteMatch(segments []string, configured string) bool {
	parts := strings.Split(strings.Trim(configured, "/"), "/")
	if len(parts) != len(segments) {
		return false
	}
	for index, part := range parts {
		if !strings.EqualFold(part, segments[index]) {
			return false
		}
	}
	return true
}

func adminExtensionRoute(segments []string, snapshot schema.Snapshot, metadata adminBootstrapMetadata) bool {
	routes := append([]string(nil), metadata.ExtensionRoutes...)
	for _, plugin := range snapshot.Plugins {
		if plugin.Admin != nil {
			routes = append(routes, plugin.Admin.Routes...)
		}
	}
	for _, configured := range routes {
		if adminLiteralRouteMatch(segments, configured) {
			return true
		}
	}
	var resource, view string
	if len(segments) == 4 && strings.EqualFold(segments[0], "collections") && !strings.EqualFold(segments[2], "create") {
		resource, view = segments[1], segments[3]
	} else if len(segments) == 3 && strings.EqualFold(segments[0], "globals") {
		resource, view = segments[1], segments[2]
	}
	if resource != "" && view != "" {
		for _, configured := range metadata.DocumentViewRoutes {
			target, key, _ := strings.Cut(configured, ":")
			if strings.EqualFold(key, view) && (target == "*" || strings.EqualFold(target, resource)) {
				return true
			}
		}
	}
	return false
}

func classifyAuthenticatedAdminRoute(segments []string) adminRouteClassification {
	if len(segments) == 0 {
		return adminRouteClassification{kind: protocol.AdminPreparedRouteDashboard, surface: "dashboard"}
	}
	if len(segments) == 1 && strings.EqualFold(segments[0], "account") {
		return adminRouteClassification{kind: protocol.AdminPreparedRouteAccount, surface: "account", segments: segments}
	}
	if len(segments) == 2 && strings.EqualFold(segments[0], "account") && strings.EqualFold(segments[1], "security") {
		return adminRouteClassification{kind: protocol.AdminPreparedRouteSecurity, surface: "account", segments: segments}
	}
	if len(segments) >= 2 && strings.EqualFold(segments[0], "collections") {
		if len(segments) == 2 {
			return adminRouteClassification{kind: protocol.AdminPreparedRouteCollectionList, surface: "collectionList", segments: segments}
		}
		if len(segments) == 3 && strings.EqualFold(segments[2], "trash") {
			return adminRouteClassification{kind: protocol.AdminPreparedRouteCollectionTrash, surface: "collectionList", segments: segments}
		}
		if len(segments) == 3 && strings.EqualFold(segments[2], "upload") {
			return adminRouteClassification{kind: protocol.AdminPreparedRouteUpload, surface: "collectionCreate", segments: segments}
		}
		if len(segments) == 3 && strings.EqualFold(segments[2], "create") {
			return adminRouteClassification{kind: protocol.AdminPreparedRouteCollectionCreate, surface: "collectionCreate", segments: segments}
		}
		if len(segments) == 4 && strings.EqualFold(segments[2], "create") && strings.EqualFold(segments[3], "api") {
			return adminRouteClassification{kind: protocol.AdminPreparedRouteCollectionCreate, surface: "collectionCreate", segments: segments}
		}
		if (len(segments) == 4 || len(segments) == 5) && strings.EqualFold(segments[3], "versions") {
			return adminRouteClassification{kind: protocol.AdminPreparedRouteCollectionVersions, surface: "collectionEdit", segments: segments}
		}
		if len(segments) == 3 {
			return adminRouteClassification{kind: protocol.AdminPreparedRouteCollectionDocument, surface: "collectionEdit", segments: segments}
		}
		if len(segments) == 4 && strings.EqualFold(segments[3], "api") {
			return adminRouteClassification{kind: protocol.AdminPreparedRouteCollectionAPI, surface: "collectionEdit", segments: segments}
		}
		return adminRouteClassification{kind: protocol.AdminPreparedRouteNotFound, surface: "notFound", segments: segments}
	}
	if len(segments) >= 2 && strings.EqualFold(segments[0], "globals") {
		if (len(segments) == 3 || len(segments) == 4) && strings.EqualFold(segments[2], "versions") {
			return adminRouteClassification{kind: protocol.AdminPreparedRouteGlobalVersions, surface: "global", segments: segments}
		}
		if len(segments) == 2 {
			return adminRouteClassification{kind: protocol.AdminPreparedRouteGlobalDocument, surface: "global", segments: segments}
		}
		if len(segments) == 3 && strings.EqualFold(segments[2], "api") {
			return adminRouteClassification{kind: protocol.AdminPreparedRouteGlobalAPI, surface: "global", segments: segments}
		}
		return adminRouteClassification{kind: protocol.AdminPreparedRouteNotFound, surface: "notFound", segments: segments}
	}
	return adminRouteClassification{kind: protocol.AdminPreparedRouteNotFound, surface: "notFound", segments: segments}
}

func redirectQuery(search string) string {
	values, _ := url.ParseQuery(strings.TrimPrefix(search, "?"))
	redirect := values.Get("redirect")
	if redirect == "" || !strings.HasPrefix(redirect, "/") || strings.HasPrefix(redirect, "//") || strings.Contains(redirect, `\`) {
		return ""
	}
	target, err := url.Parse(redirect)
	if err != nil || target.IsAbs() || target.Host != "" || !strings.HasPrefix(target.Path, "/") {
		return ""
	}
	switch strings.ToLower(strings.TrimSuffix(target.Path, "/")) {
	case "/login", "/create-first-user", "/forgot-password", "/reset-password", "/request-verification", "/verify-email":
		return ""
	}
	if target.Path == "/" {
		return "/admin/"
	}
	location := "/admin" + target.EscapedPath()
	if target.RawQuery != "" {
		location += "?" + target.RawQuery
	}
	if target.Fragment != "" {
		location += "#" + target.EscapedFragment()
	}
	return location
}

func seedableSurface(metadata adminBootstrapMetadata, route adminRouteClassification) bool {
	if route.kind == protocol.AdminPreparedRouteCustom {
		return true
	}
	if route.surface == "" {
		return true
	}
	for _, candidate := range metadata.SeedableCoreSurfaces {
		if candidate != route.surface {
			continue
		}
		target := "*"
		if len(route.segments) > 1 && (strings.HasPrefix(route.surface, "collection") || route.surface == "global") {
			target = route.segments[1]
		}
		for _, replaced := range metadata.ReplacedCoreViews {
			if route.kind == protocol.AdminPreparedRouteCollectionTrash || route.kind == protocol.AdminPreparedRouteUpload || route.kind == protocol.AdminPreparedRouteCollectionVersions || route.kind == protocol.AdminPreparedRouteGlobalVersions {
				continue
			}
			if replaced == route.surface+":*" || replaced == route.surface+":"+target {
				return false
			}
		}
		return true
	}
	return false
}

func adminRouteHasCoreView(kind protocol.AdminPreparedRouteKindV1) bool {
	switch kind {
	case protocol.AdminPreparedRouteCollectionList, protocol.AdminPreparedRouteCollectionCreate,
		protocol.AdminPreparedRouteCollectionDocument, protocol.AdminPreparedRouteCollectionAPI,
		protocol.AdminPreparedRouteGlobalDocument, protocol.AdminPreparedRouteGlobalAPI, protocol.AdminPreparedRouteNotFound:
		return true
	default:
		return false
	}
}

func selectAdminRouteLoaders(route adminRouteClassification, metadata adminBootstrapMetadata) adminRouteClassification {
	if route.location != "" {
		return route
	}
	var key string
	if route.surface == "extension" {
		for configured, loader := range metadata.RouteLoaders {
			if adminLiteralRouteMatch(route.segments, configured) {
				key = loader
				break
			}
		}
	} else if adminRouteHasCoreView(route.kind) {
		target := route.surface + ":*"
		// A resource-specific replacement wins over the wildcard replacement for
		// the same core surface, matching static admin registration.
		if len(route.segments) > 1 && route.surface != "notFound" {
			specific := route.surface + ":" + route.segments[1]
			if slices.Contains(metadata.ReplacedCoreViews, specific) {
				target = specific
			}
		}
		key = metadata.ViewLoaders[target]
	}
	if key != "" {
		route.kind = protocol.AdminPreparedRouteCustom
		route.loaders = []string{key}
	}
	return route
}

func adminAuthCollection(snapshot schema.Snapshot) *schema.Collection {
	if snapshot.Application.Admin == nil {
		return nil
	}
	for index := range snapshot.Collections {
		if snapshot.Collections[index].Slug == snapshot.Application.Admin.UserCollectionSlug {
			return &snapshot.Collections[index]
		}
	}
	return nil
}

func adminRouteLocale(runtime *protocol.AdminPreparedRuntimeV1, query url.Values) string {
	localization := runtime.Manifest.Application.Localization
	if localization == nil {
		return ""
	}
	requested := query.Get("locale")
	for _, locale := range localization.Locales {
		if string(locale.Code) == requested {
			return requested
		}
	}
	if runtime.ContentLocale != "" {
		return runtime.ContentLocale
	}
	return string(localization.DefaultLocale)
}

func adminInitialFormValues(fields []schema.Field) (map[string]any, bool) {
	values := map[string]any{}
	for _, field := range fields {
		if field.Category == schema.FieldCategoryPresentation {
			continue
		}
		if field.Default != nil {
			value, valid := adminLiteralDefault(field, *field.Default)
			if !valid {
				return nil, false
			}
			values[field.Name] = value
			continue
		}
		if field.Select != nil && field.Select.HasMany && len(field.Select.DefaultValues) != 0 {
			values[field.Name] = append([]string(nil), field.Select.DefaultValues...)
			continue
		}
		if field.Type == schema.FieldTypeGroup && field.Nested != nil {
			nested, deterministic := adminInitialFormValues(field.Nested.Fields)
			if !deterministic {
				return nil, false
			}
			if len(nested) != 0 {
				values[field.Name] = nested
			}
			continue
		}
		// Client-created row keys are intentionally random. A route containing
		// required initial rows cannot prove an exact server/client data identity.
		if field.Type == schema.FieldTypeArray && field.Nested != nil && field.Nested.MinRows > 0 {
			return nil, false
		}
	}
	return values, true
}

func adminLiteralDefault(field schema.Field, encoded string) (any, bool) {
	switch field.Type {
	case schema.FieldTypeTextList, schema.FieldTypeNumberList:
		var value any
		if json.Unmarshal([]byte(encoded), &value) != nil {
			return nil, false
		}
		return value, true
	case schema.FieldTypeNumber:
		value, err := strconv.ParseFloat(encoded, 64)
		return value, err == nil
	case schema.FieldTypeCheckbox:
		value, err := strconv.ParseBool(encoded)
		return value, err == nil
	default:
		return encoded, true
	}
}

func adminStateFingerprint(pathname, search, contextKey, buildID string) string {
	digest := sha256.Sum256([]byte(pathname + "\n" + search + "\n" + contextKey + "\n" + buildID))
	return hex.EncodeToString(digest[:])
}

func setAdminPreparedHeaders(header http.Header) {
	header.Set("Cache-Control", "private, no-store")
	for _, value := range []string{"Accept", "Cookie", "Authorization", "Ridu-Admin-Context"} {
		header.Add("Vary", value)
	}
}

func (api *API) prepareAdminState(request *http.Request, metadata adminBootstrapMetadata, includeRoute bool) protocol.AdminPreparedRouteStateV1 {
	state, runtime, identity, classification := api.prepareAdminStateBase(request, metadata)
	if includeRoute && state.Outcome == protocol.AdminPreparedRoutePrepared && state.Route != nil && runtime != nil {
		api.completeAdminPreparedRoute(&state, request, runtime, identity, classification)
	}
	return state
}

func (api *API) completeAdminPreparedRoute(state *protocol.AdminPreparedRouteStateV1, request *http.Request, runtime *protocol.AdminPreparedRuntimeV1, identity adminPreparedIdentity, classification adminRouteClassification) {
	if len(classification.loaders) != 0 {
		// Loader failures remain per-key results so one dashboard panel cannot
		// suppress successful siblings or the surrounding prepared route.
		state.Loaders = make(map[string]protocol.AdminReadResultV1[json.RawMessage], len(classification.loaders))
		for _, key := range classification.loaders {
			value, err := api.readAdminLoader(request, runtime, identity, key)
			state.Loaders[key] = adminReadResult(api, request, value, err)
		}
	}
	if classification.kind == protocol.AdminPreparedRouteCustom {
		return
	}
	if classification.kind == protocol.AdminPreparedRouteCollectionList || classification.kind == protocol.AdminPreparedRouteCollectionTrash {
		slug := classification.segments[1]
		preferences := &protocol.AdminCollectionListPreferencesV1{
			Workspace: protocol.AdminCollectionListPreferenceV1{Value: json.RawMessage("null")},
			Presets:   protocol.AdminCollectionListPreferenceV1{Value: json.RawMessage("null")},
		}
		if runtime.Session != nil {
			preferences.Workspace = api.readAdminListPreference(request, identity.identity, "collection:"+slug+":workspace")
			preferences.Presets = api.readAdminListPreference(request, identity.identity, "collection:"+slug+":presets")
		}
		data := api.loadAdminCollectionList(request, runtime.Manifest, identity.identity, slug, classification.kind == protocol.AdminPreparedRouteCollectionTrash, adminRouteLocale(runtime, request.URL.Query()), classification.listCellFields, "page", preferences.Workspace.Value)
		data.Preferences = preferences
		state.Route.Data = &data
		return
	}
	data := api.prepareAdminRouteData(request, runtime, identity, classification)
	if adminRouteDataFailed(data) {
		state.Outcome = protocol.AdminPreparedRouteFallback
		state.Route = nil
		state.Loaders = nil
		state.ModuleGroups = []string{"entry"}
		state.Diagnostic = &protocol.AdminPreparedRouteDiagnosticV1{Code: "route_not_preparable", Message: "This route will load using its browser controller."}
		return
	}
	state.Route = data
}

func (api *API) prepareAdminFallbackState(request *http.Request, code, message string) protocol.AdminPreparedRouteStateV1 {
	pathname, search := normalizeAdminRouteIdentity(request)
	state := protocol.AdminPreparedRouteStateV1{
		Version: protocol.AdminPreparedRouteStateVersion, Outcome: protocol.AdminPreparedRouteFallback,
		Pathname: pathname, Search: search, BuildID: "", ModuleGroups: []string{},
		Diagnostic: &protocol.AdminPreparedRouteDiagnosticV1{Code: code, Message: message},
	}
	runtime, _, contextKey, err := api.prepareAdminRuntime(request.Context(), request, "")
	if err != nil {
		requestID, _ := request.Context().Value(adminPreparedRequestIDContextKey{}).(string)
		api.reportAdminPreparedInternalError(request, requestID, err)
		state.Diagnostic = &protocol.AdminPreparedRouteDiagnosticV1{Code: "runtime_failed", Message: "The admin will load using its browser fallback."}
		return state
	}
	state.Runtime = runtime
	state.ContextKey = contextKey
	state.Fingerprint = adminStateFingerprint(pathname, search, contextKey, "")
	if requestedContext := strings.TrimSpace(request.Header.Get("Ridu-Admin-Context")); requestedContext != "" && requestedContext != contextKey {
		state.Outcome = protocol.AdminPreparedRouteReload
		state.Runtime = nil
		state.Diagnostic = &protocol.AdminPreparedRouteDiagnosticV1{Code: "context_changed", Message: "The admin identity changed; reload the document."}
	}
	return state
}

func (api *API) prepareAdminStateSafely(request *http.Request, requestID, buildID string, prepare func() protocol.AdminPreparedRouteStateV1) (state protocol.AdminPreparedRouteStateV1) {
	defer func() {
		if recovered := recover(); recovered != nil {
			api.reportRequestError(request, requestID, errors.New("panic recovered while preparing an admin route"), true, string(debug.Stack()))
			pathname, search := normalizeAdminRouteIdentity(request)
			state = protocol.AdminPreparedRouteStateV1{
				Version: protocol.AdminPreparedRouteStateVersion, Outcome: protocol.AdminPreparedRouteFallback,
				Pathname: pathname, Search: search, BuildID: buildID, ModuleGroups: []string{},
				Diagnostic: &protocol.AdminPreparedRouteDiagnosticV1{Code: "route_prepare_failed", Message: "This route will load using its browser controller."},
			}
		}
	}()
	return prepare()
}

func (api *API) prepareAdminStateBaseSafely(request *http.Request, requestID string, metadata adminBootstrapMetadata) (
	state protocol.AdminPreparedRouteStateV1,
	runtime *protocol.AdminPreparedRuntimeV1,
	identity adminPreparedIdentity,
	classification adminRouteClassification,
) {
	defer func() {
		if recovered := recover(); recovered != nil {
			api.reportRequestError(request, requestID, errors.New("panic recovered while preparing admin runtime state"), true, string(debug.Stack()))
			pathname, search := normalizeAdminRouteIdentity(request)
			state = protocol.AdminPreparedRouteStateV1{
				Version: protocol.AdminPreparedRouteStateVersion, Outcome: protocol.AdminPreparedRouteFallback,
				Pathname: pathname, Search: search, BuildID: metadata.BuildID, ModuleGroups: []string{"entry"},
				Diagnostic: &protocol.AdminPreparedRouteDiagnosticV1{Code: "runtime_failed", Message: "The admin will load using its browser fallback."},
			}
			runtime = nil
			identity = adminPreparedIdentity{}
			classification = adminRouteClassification{}
		}
	}()
	return api.prepareAdminStateBase(request, metadata)
}

func (api *API) prepareAdminStateBase(request *http.Request, metadata adminBootstrapMetadata) (protocol.AdminPreparedRouteStateV1, *protocol.AdminPreparedRuntimeV1, adminPreparedIdentity, adminRouteClassification) {
	pathname, search := normalizeAdminRouteIdentity(request)
	request.URL.RawQuery = strings.TrimPrefix(search, "?")
	state := protocol.AdminPreparedRouteStateV1{
		Version: protocol.AdminPreparedRouteStateVersion, Outcome: protocol.AdminPreparedRouteFallback,
		Pathname: pathname, Search: search, BuildID: metadata.BuildID, ModuleGroups: []string{"entry"},
	}
	runtime, identity, contextKey, err := api.prepareAdminRuntime(request.Context(), request, metadata.BuildID)
	if err != nil {
		requestID, _ := request.Context().Value(adminPreparedRequestIDContextKey{}).(string)
		api.reportAdminPreparedInternalError(request, requestID, err)
		state.Diagnostic = &protocol.AdminPreparedRouteDiagnosticV1{Code: "runtime_failed", Message: "The admin will load using its browser fallback."}
		return state, nil, identity, adminRouteClassification{}
	}
	state.Runtime = runtime
	state.ContextKey = contextKey
	state.Fingerprint = adminStateFingerprint(pathname, search, contextKey, metadata.BuildID)
	// A mismatched context means the browser document belongs to a different
	// identity/build snapshot; sending compact navigation state would mix them.
	if requestedContext := strings.TrimSpace(request.Header.Get("Ridu-Admin-Context")); requestedContext != "" && requestedContext != contextKey {
		state.Outcome = protocol.AdminPreparedRouteReload
		state.Runtime = nil
		state.Diagnostic = &protocol.AdminPreparedRouteDiagnosticV1{Code: "context_changed", Message: "The admin identity changed; reload the document."}
		return state, runtime, identity, adminRouteClassification{}
	}
	classification := classifyAdminRoute(pathname, search, runtime, metadata)
	known := classifyKnownAdminResource(classification, runtime.Manifest)
	if known.kind == classification.kind {
		classification = selectAdminRouteLoaders(classification, metadata)
	} else {
		classification = known
	}
	if classification.kind == protocol.AdminPreparedRouteDashboard {
		classification.loaders = metadata.DashboardLoaders
	}
	if classification.surface == "collectionList" {
		classification.listCellFields = metadata.ListCellFields[classification.segments[1]]
	}
	if classification.location != "" {
		state.Outcome = protocol.AdminPreparedRouteRedirect
		state.Location = classification.location
		return state, runtime, identity, classification
	}
	if !seedableSurface(metadata, classification) {
		state.Outcome = protocol.AdminPreparedRouteFallback
		state.Diagnostic = &protocol.AdminPreparedRouteDiagnosticV1{Code: "surface_not_seedable", Message: "This customized view will load using its browser controller."}
		return state, runtime, identity, classification
	}
	state.ModuleGroups = append(state.ModuleGroups, adminRouteModuleGroups(classification, runtime.Manifest)...)
	state.Outcome = protocol.AdminPreparedRoutePrepared
	state.Route = &protocol.AdminPreparedRouteDataV1{Kind: classification.kind}
	return state, runtime, identity, classification
}

func (api *API) reportAdminPreparedInternalError(request *http.Request, requestID string, err error) {
	status := http.StatusInternalServerError
	var operationError *operationengine.Error
	if errors.As(err, &operationError) {
		status = operationError.Status
	} else if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		status = http.StatusRequestTimeout
	}
	if status >= http.StatusInternalServerError {
		api.reportRequestError(request, requestID, err, false, "")
	}
}

func classifyKnownAdminResource(route adminRouteClassification, snapshot schema.Snapshot) adminRouteClassification {
	if len(route.segments) < 2 {
		return route
	}
	if strings.EqualFold(route.segments[0], "collections") && adminCollection(snapshot, route.segments[1]) == nil {
		return adminRouteClassification{kind: protocol.AdminPreparedRouteNotFound, surface: "notFound", segments: route.segments}
	}
	if strings.EqualFold(route.segments[0], "globals") {
		for _, global := range snapshot.Globals {
			if string(global.Slug) == route.segments[1] {
				return route
			}
		}
		return adminRouteClassification{kind: protocol.AdminPreparedRouteNotFound, surface: "notFound", segments: route.segments}
	}
	return route
}

func adminRouteModuleGroups(route adminRouteClassification, snapshot schema.Snapshot) []string {
	groups := make([]string, 0, 4)
	add := func(group string) {
		for _, existing := range groups {
			if existing == group {
				return
			}
		}
		groups = append(groups, group)
	}
	slug := ""
	if len(route.segments) > 1 {
		slug = route.segments[1]
	}
	switch route.kind {
	case protocol.AdminPreparedRouteSetup:
		if auth := adminAuthCollection(snapshot); auth != nil && adminFieldsUseDate(auth.Fields) {
			add("date")
		}
	case protocol.AdminPreparedRouteCollectionCreate,
		protocol.AdminPreparedRouteCollectionDocument,
		protocol.AdminPreparedRouteCollectionAPI,
		protocol.AdminPreparedRouteGlobalDocument,
		protocol.AdminPreparedRouteGlobalAPI:
		add("document")
		var fields []schema.Field
		usesUploadPreview := false
		if strings.HasPrefix(route.surface, "collection") {
			if collection := adminCollection(snapshot, slug); collection != nil {
				fields = collection.Fields
				if route.kind == protocol.AdminPreparedRouteCollectionDocument && collection.Capabilities.Upload {
					usesUploadPreview = true
				}
			}
		} else {
			for _, global := range snapshot.Globals {
				if string(global.Slug) == slug {
					fields = global.Fields
					break
				}
			}
		}
		if adminFieldsUseDate(fields) {
			add("date")
		}
		if usesUploadPreview {
			add("upload-preview")
		}
		if route.kind == protocol.AdminPreparedRouteCollectionAPI || (len(route.segments) > 2 && strings.EqualFold(route.segments[len(route.segments)-1], "api")) {
			add("document-api")
		}
	case protocol.AdminPreparedRouteCollectionVersions, protocol.AdminPreparedRouteGlobalVersions:
		add("versions")
	case protocol.AdminPreparedRouteUpload:
		add("bulk-upload")
	case protocol.AdminPreparedRouteSecurity:
		add("date")
	case protocol.AdminPreparedRouteAccount:
		if auth := adminAuthCollection(snapshot); auth != nil && adminFieldsUseDate(auth.Fields) {
			add("date")
		}
	}
	return groups
}

func adminFieldsUseDate(fields []schema.Field) bool {
	for _, field := range fields {
		if field.Type == schema.FieldTypeDate {
			return true
		}
		if adminFieldsUseDate(schema.ChildFields(field)) {
			return true
		}
	}
	return false
}

// Reuse only a runtime that this request has independently resolved to the same
// context. This is a transport optimization, not a session or authorization cache.
func adminNavigationState(state protocol.AdminPreparedRouteStateV1, request *http.Request) protocol.AdminPreparedRouteStateV1 {
	if state.Runtime != nil && state.ContextKey != "" && strings.TrimSpace(request.Header.Get("Ridu-Admin-Context")) == state.ContextKey {
		state.Navigation = &state.Runtime.AdminPreparedNavigationV1
		state.Runtime = nil
	}
	return state
}

func marshalBoundedAdminState(state protocol.AdminPreparedRouteStateV1) []byte {
	encoded, err := json.Marshal(state)
	if err == nil && len(encoded) <= maxAdminPreparedRouteBytes {
		return encoded
	}
	// Prepared state is embedded into HTML as well as returned directly. Strip
	// all potentially large data before returning the browser-owned fallback.
	state.Outcome = protocol.AdminPreparedRouteFallback
	state.Runtime = nil
	state.Navigation = nil
	state.Route = nil
	state.Loaders = nil
	state.Location = ""
	state.ModuleGroups = []string{}
	if err != nil {
		state.Diagnostic = &protocol.AdminPreparedRouteDiagnosticV1{Code: "snapshot_invalid", Message: "The prepared route could not be encoded and will use its browser loader."}
	} else {
		state.Diagnostic = &protocol.AdminPreparedRouteDiagnosticV1{Code: "snapshot_too_large", Message: "The prepared route exceeded the safe limit and will use its browser loader."}
	}
	encoded, _ = json.Marshal(state)
	return encoded
}

func adminHTMLPrefix(index []byte, metadata adminBootstrapMetadata, state protocol.AdminPreparedRouteStateV1) ([]byte, []byte, bool) {
	// Split immediately before the framework entry module: the head can flush early, then the
	// snapshot is inserted before that module executes.
	script := bytes.Index(index, []byte("<script type=\"module\""))
	if script < 0 {
		return nil, nil, false
	}
	prefix := append([]byte(nil), index[:script]...)
	suffix := append([]byte(nil), index[script:]...)
	if state.Runtime != nil && (state.Runtime.Theme == "light" || state.Runtime.Theme == "dark") {
		theme := []byte(` data-theme="` + html.EscapeString(state.Runtime.Theme) + `" style="color-scheme: ` + html.EscapeString(state.Runtime.Theme) + `"`)
		prefix = bytes.Replace(prefix, []byte("<html"), append([]byte("<html"), theme...), 1)
	}
	assets := map[string]struct{}{}
	for _, group := range state.ModuleGroups {
		for _, asset := range metadata.ModuleGroups[group] {
			assets[asset] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(assets))
	for asset := range assets {
		ordered = append(ordered, asset)
	}
	sort.Strings(ordered)
	var links strings.Builder
	for _, asset := range ordered {
		href := "/admin/" + strings.TrimPrefix(asset, "/")
		if strings.HasSuffix(asset, ".css") {
			fmt.Fprintf(&links, "\t\t<link rel=\"preload\" as=\"style\" href=\"%s\" />\n", html.EscapeString(href))
		} else if strings.HasSuffix(asset, ".js") {
			fmt.Fprintf(&links, "\t\t<link rel=\"modulepreload\" href=\"%s\" />\n", html.EscapeString(href))
		}
	}
	head := []byte(`<meta name="ridu-admin-build-id" content="` + html.EscapeString(metadata.BuildID) + `" />` + links.String())
	if bytes.Contains(prefix, []byte("</head>")) {
		prefix = bytes.Replace(prefix, []byte("</head>"), append(head, []byte("</head>")...), 1)
	} else {
		// Vite places the entry module inside <head>, so its closing tag is in the
		// suffix. Flush build identity and preloads before that module regardless.
		prefix = append(prefix, head...)
	}
	return prefix, suffix, true
}
