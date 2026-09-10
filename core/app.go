package core

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/internal/uploads"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/storage"
	"github.com/riducms/ridu/store"
	"golang.org/x/crypto/bcrypt"
)

// App is a resolved Ridu application bound to one document store.
type App struct {
	manifest               schema.Manifest
	local                  *LocalAPI
	auth                   store.AuthStore
	authMaintenance        store.AuthMaintenanceStore
	authByID               map[schema.StableID]schema.Collection
	authBySlug             map[string]schema.Collection
	authOrder              []schema.Collection
	authConfigBySlug       map[string]AuthConfig
	authCreatePolicySet    map[string]bool
	dummyPasswordHashes    map[string][]byte
	passwordWork           *passwordWorkLimiter
	bySlug                 map[string]schema.Collection
	globalsBySlug          map[string]schema.Global
	uploads                uploads.Manager
	tasks                  store.TaskStore
	taskRegistry           map[string]taskRuntime
	health                 store.HealthStore
	readiness              store.ReadinessStore
	migrationReadiness     store.MigrationReadinessStore
	migrationHistoryDigest string
	storageHealth          storage.HealthBackend
	readinessAdmission     chan struct{}
	uploadCleanupAdmission chan struct{}
	uploadCleanupTimeout   time.Duration
	draining               atomic.Bool
	preferences            store.PreferenceStore
	documentLocks          store.DocumentLockStore
	documentStore          store.Store
	previewTokens          *previewTokenRegistry
	adminUserCollection    schema.StableID
	pluginEndpoints        []runtimePluginEndpoint
	customEndpoints        []runtimeEndpoint
	pluginTransports       []runtimePluginTransport
	availableLocales       LocaleAvailability
}

func New(applicationConfig Config, backend store.Store) (*App, error) {
	resolvedConfig, manifest, err := resolveConfig(applicationConfig)
	if err != nil {
		return nil, err
	}

	snapshot := manifest.Snapshot()
	authByID := make(map[schema.StableID]schema.Collection)
	authBySlug := make(map[string]schema.Collection)
	var authOrder []schema.Collection
	bySlug := make(map[string]schema.Collection)
	globalsBySlug := make(map[string]schema.Global)
	uploadsConfigured := false
	versionedCollectionsConfigured := false
	locksConfigured := false
	for _, collection := range snapshot.Collections {
		bySlug[string(collection.Slug)] = collection
		uploadsConfigured = uploadsConfigured || collection.Capabilities.Upload
		versionedCollectionsConfigured = versionedCollectionsConfigured || collection.Capabilities.Versions
		locksConfigured = locksConfigured || collection.Capabilities.Locking
		if collection.Capabilities.Auth {
			authByID[collection.ID] = collection
			authBySlug[string(collection.Slug)] = collection
			authOrder = append(authOrder, collection)
		}
	}
	for _, global := range snapshot.Globals {
		globalsBySlug[string(global.Slug)] = global
	}
	applicationConfig = resolvedConfig
	if uploadsConfigured && applicationConfig.Storage == nil {
		return nil, fmt.Errorf("upload-enabled collections require Config.Storage")
	}
	if uploadsConfigured {
		if err := uploads.ValidateNamespace(applicationConfig.StorageNamespace); err != nil {
			return nil, fmt.Errorf("upload-enabled collections require Config.StorageNamespace: %w", err)
		}
	}
	var uploadObjectLocker store.UploadObjectLocker
	if uploadsConfigured {
		var supported bool
		uploadObjectLocker, supported = backend.(store.UploadObjectLocker)
		if !supported {
			return nil, fmt.Errorf("upload-enabled collections require a store.UploadObjectLocker")
		}
	}
	uploadManager := uploads.Manager{
		Backend: applicationConfig.Storage, Locker: uploadObjectLocker, Namespace: applicationConfig.StorageNamespace,
	}

	var authBackend store.AuthStore
	var authMaintenance store.AuthMaintenanceStore
	if len(authByID) != 0 {
		var supportsAuth bool
		authBackend, supportsAuth = backend.(store.AuthStore)
		if !supportsAuth {
			return nil, fmt.Errorf("auth-enabled collections require a store.AuthStore")
		}
		authMaintenance, _ = backend.(store.AuthMaintenanceStore)
	}

	authoredBySlug := make(map[schema.CollectionSlug]Collection, len(applicationConfig.Collections))
	authoredGlobalsBySlug := make(map[schema.CollectionSlug]Global, len(applicationConfig.Globals))
	authConfigBySlug := make(map[string]AuthConfig, len(authBySlug))
	authCreatePolicySet := make(map[string]bool, len(authBySlug))
	dummyPasswordHashes := make(map[string][]byte, len(authBySlug))
	for _, collection := range applicationConfig.Collections {
		authoredBySlug[collection.Slug] = collection
		if collection.Auth {
			authConfigBySlug[string(collection.Slug)] = collection.AuthConfig
			authCreatePolicySet[string(collection.Slug)] = collection.Access.Create != nil
			resolved := authBySlug[string(collection.Slug)]
			hash, hashError := bcrypt.GenerateFromPassword([]byte("ridu-dummy-password"), resolved.Auth.PasswordBcryptCost)
			if hashError != nil {
				return nil, fmt.Errorf("prepare auth timing defense for %q: %w", collection.Slug, hashError)
			}
			dummyPasswordHashes[string(collection.Slug)] = hash
		}
	}
	for _, global := range applicationConfig.Globals {
		authoredGlobalsBySlug[global.Slug] = global
	}

	var local *LocalAPI
	var application *App
	previewTokens := &previewTokenRegistry{grants: make(map[string]previewTokenGrant)}
	engineCollections := make([]operationengine.Collection, 0, len(snapshot.Collections)+len(snapshot.Globals))
	for _, resolved := range snapshot.Collections {
		authored, exists := authoredBySlug[resolved.Slug]
		if !exists {
			return nil, fmt.Errorf("resolved collection %q has no authored runtime configuration", resolved.ID)
		}
		adapted := adaptCollection(authored, resolved, &local)
		adapted.Bindings, err = lowerFieldGraph(applicationConfig.fieldGraph, "collection", string(resolved.Slug), resolved.Fields, &local)
		if err != nil {
			return nil, err
		}
		engineCollections = append(engineCollections, adapted)
	}
	for _, resolved := range snapshot.Globals {
		authored, exists := authoredGlobalsBySlug[resolved.Slug]
		if !exists {
			return nil, fmt.Errorf("resolved global %q has no authored runtime configuration", resolved.ID)
		}
		adapted := adaptGlobal(authored, resolved, &local)
		adapted.Bindings, err = lowerFieldGraph(applicationConfig.fieldGraph, "global", string(resolved.Slug), resolved.Fields, &local)
		if err != nil {
			return nil, err
		}
		engineCollections = append(engineCollections, adapted)
	}

	engine, err := operationengine.New(operationengine.Config{
		Collections:               engineCollections,
		Store:                     backend,
		AllowIDOnCreate:           snapshot.Application.AllowIDOnCreate,
		PluginValidators:          adaptPluginValidators(applicationConfig.Plugins),
		DispatchAfterCommit:       adaptAfterCommitDispatcher(applicationConfig.AfterCommit),
		BeginPermanentDeleteFence: previewTokens.beginPermanentDeleteFence,
		CleanupPermanentDeletes: func(ctx context.Context, deletes []operationengine.PermanentDelete) error {
			return application.cleanupPermanentUploadDeletes(ctx, deletes)
		},
		ValidateUploadImport: func(ctx context.Context, collection schema.Collection, values store.Values) error {
			return validateImportedUploadObjects(ctx, uploadManager, collection, values)
		},
		RootAfterError: adaptHooks(applicationConfig.Hooks.AfterError, &local),
		Localization:   snapshot.Application.Localization,
	})
	if err != nil {
		return nil, err
	}
	local = &LocalAPI{engine: engine}
	tasks, supportsTasks := backend.(store.TaskStore)
	if (len(applicationConfig.Tasks) != 0 || versionedCollectionsConfigured) && !supportsTasks {
		return nil, fmt.Errorf("configured tasks and scheduled publishing require a store.TaskStore")
	}
	taskRegistry, taskIssues := buildTaskRegistry(applicationConfig.Tasks)
	if len(taskIssues) != 0 {
		return nil, schema.NewValidationError(taskIssues)
	}
	health, _ := backend.(store.HealthStore)
	readiness, _ := backend.(store.ReadinessStore)
	migrationReadiness, _ := backend.(store.MigrationReadinessStore)
	storageHealth, _ := applicationConfig.Storage.(storage.HealthBackend)
	preferences, _ := backend.(store.PreferenceStore)
	var documentLocks store.DocumentLockStore
	if locksConfigured {
		var supported bool
		documentLocks, supported = backend.(store.DocumentLockStore)
		if !supported {
			return nil, fmt.Errorf("document-lock-enabled collections require a store.DocumentLockStore")
		}
	}
	var adminUserCollection schema.StableID
	if snapshot.Application.Admin != nil {
		adminUserCollection = snapshot.Application.Admin.UserCollectionID
	}

	application = &App{
		manifest:               manifest,
		local:                  local,
		auth:                   authBackend,
		authMaintenance:        authMaintenance,
		authByID:               authByID,
		authBySlug:             authBySlug,
		authOrder:              authOrder,
		authConfigBySlug:       authConfigBySlug,
		authCreatePolicySet:    authCreatePolicySet,
		dummyPasswordHashes:    dummyPasswordHashes,
		passwordWork:           globalPasswordWork,
		bySlug:                 bySlug,
		globalsBySlug:          globalsBySlug,
		uploads:                uploadManager,
		tasks:                  tasks,
		taskRegistry:           taskRegistry,
		health:                 health,
		readiness:              readiness,
		migrationReadiness:     migrationReadiness,
		migrationHistoryDigest: executableMigrationHistoryDigest,
		storageHealth:          storageHealth,
		readinessAdmission:     make(chan struct{}, 1),
		uploadCleanupAdmission: make(chan struct{}, committedUploadCleanupWorkers),
		uploadCleanupTimeout:   committedUploadCleanupTimeout,
		preferences:            preferences,
		documentLocks:          documentLocks,
		documentStore:          backend,
		previewTokens:          previewTokens,
		adminUserCollection:    adminUserCollection,
		pluginEndpoints:        append([]runtimePluginEndpoint(nil), applicationConfig.pluginEndpoints...),
		customEndpoints:        runtimeEndpoints(applicationConfig),
		availableLocales:       applicationConfig.Localization.AvailableLocales,
	}
	if versionedCollectionsConfigured {
		application.taskRegistry[builtinPublishTask] = scheduledPublishTaskRuntime(application)
	}
	pluginTransports, err := bindPluginTransports(applicationConfig.Plugins, application)
	if err != nil {
		return nil, err
	}
	application.pluginTransports = pluginTransports
	return application, nil
}

func bindPluginTransports(plugins []Plugin, application *App) ([]runtimePluginTransport, error) {
	var result []runtimePluginTransport
	seen := make(map[string]string)
	for index, plugin := range plugins {
		provider, ok := plugin.(TransportProvider)
		if !ok {
			continue
		}
		transports, err := provider.BindTransports(PluginTransportContext{Manifest: application.manifest, Local: application.local, App: application})
		if err != nil {
			return nil, fmt.Errorf("bind plugin %q transports: %w", plugin.Key(), err)
		}
		for transportIndex, transport := range transports {
			method := strings.ToUpper(strings.TrimSpace(transport.Method))
			path := strings.TrimSpace(transport.Path)
			if !validPluginTransport(method, path) || strings.TrimSpace(transport.Summary) == "" || transport.Handler == nil {
				return nil, schema.NewValidationError([]schema.Issue{{
					Code: "invalid_plugin_transport", Path: fmt.Sprintf("plugins[%d].transports[%d]", index, transportIndex),
					Message: "plugin transports require POST or GET, an unreserved absolute /api path, a summary, and a handler",
				}})
			}
			identity := method + " " + path
			if owner, exists := seen[identity]; exists {
				return nil, schema.NewValidationError([]schema.Issue{{Code: "duplicate_plugin_transport", Path: fmt.Sprintf("plugins[%d].transports[%d]", index, transportIndex), Message: fmt.Sprintf("plugin transport %q is already provided by %q", identity, owner)}})
			}
			seen[identity] = plugin.Key()
			transport.Method = method
			transport.Path = path
			transport.Summary = strings.TrimSpace(transport.Summary)
			result = append(result, runtimePluginTransport{pluginKey: plugin.Key(), transport: transport})
		}
	}
	return result, nil
}

func validPluginTransport(method, path string) bool {
	if method != http.MethodPost && method != http.MethodGet {
		return false
	}
	if path == "" || path != strings.TrimSpace(path) || !strings.HasPrefix(path, "/api/") || strings.ContainsAny(path, " \t\r\n?#*") || strings.Contains(path, "..") || strings.HasSuffix(path, "/") {
		return false
	}
	for _, reserved := range []string{"/api/auth", "/api/collections", "/api/globals", "/api/plugins", "/api/preferences", "/api/schema", "/api/uploads", "/api/access", "/api/preview"} {
		if path == reserved || strings.HasPrefix(path, reserved+"/") {
			return false
		}
	}
	return true
}

func (application *App) Manifest() schema.Manifest { return application.manifest }
func (application *App) Local() *LocalAPI          { return application.local }

func (application *App) collectionByID(id schema.StableID) (schema.Collection, bool) {
	for _, collection := range application.bySlug {
		if collection.ID == id {
			return collection, true
		}
	}
	return schema.Collection{}, false
}
