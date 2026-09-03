package ridu

import (
	"time"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// Public authoring and runtime contracts live in core. These aliases keep the
// root package as the ergonomic application-facing entry point.
type (
	AccessContext                = core.AccessContext
	AccessDecision               = core.AccessDecision
	AccessDecisionKind           = core.AccessDecisionKind
	AccessRule                   = core.AccessRule
	AfterCommitDispatcher        = core.AfterCommitDispatcher
	AfterCommitEffect            = core.AfterCommitEffect
	AdminConfig                  = core.AdminConfig
	AdminLocalizationConfig      = core.AdminLocalizationConfig
	AdminLanguage                = core.AdminLanguage
	AdminTimeZone                = core.AdminTimeZone
	AdminPluginMetadata          = core.AdminPluginMetadata
	DescriptorProvider           = core.DescriptorProvider
	DistinctOptions              = core.DistinctOptions
	GenerationProvider           = core.GenerationProvider
	EndpointProvider             = core.EndpointProvider
	Endpoint                     = core.Endpoint
	EndpointContext              = core.EndpointContext
	EndpointHandler              = core.EndpointHandler
	TransportProvider            = core.TransportProvider
	App                          = core.App
	AuditEvent                   = core.AuditEvent
	AuthConfig                   = core.AuthConfig
	PasswordPolicy               = core.PasswordPolicy
	PasswordResetConfig          = core.PasswordResetConfig
	PasswordResetNotification    = core.PasswordResetNotification
	VerifyEmailConfig            = core.VerifyEmailConfig
	VerifyEmailNotification      = core.VerifyEmailNotification
	AuthSession                  = core.AuthSession
	AuthSessionInfo              = core.AuthSessionInfo
	AuthIdentity                 = core.AuthIdentity
	LoginOptions                 = core.LoginOptions
	APIKey                       = core.APIKey
	APIKeyInfo                   = core.APIKeyInfo
	AuthOperation                = core.AuthOperation
	AuthContext                  = core.AuthContext
	AuthAccessRule               = core.AuthAccessRule
	AuthAccess                   = core.AuthAccess
	AuthHook                     = core.AuthHook
	AuthHooks                    = core.AuthHooks
	AuthStrategyContext          = core.AuthStrategyContext
	AuthStrategyResult           = core.AuthStrategyResult
	AuthStrategy                 = core.AuthStrategy
	Collection                   = core.Collection
	CollectionIndex              = core.CollectionIndex
	CapabilityOptions            = core.CapabilityOptions
	CollectionAdmin              = core.CollectionAdmin
	LivePreviewConfig            = core.LivePreviewConfig
	PreviewBreakpoint            = core.PreviewBreakpoint
	LocalizationConfig           = core.LocalizationConfig
	Locale                       = core.Locale
	CollectionAccess             = core.CollectionAccess
	CollectionHooks              = core.CollectionHooks
	DocumentLockConfig           = core.DocumentLockConfig
	CollectionLabels             = core.CollectionLabels
	Global                       = core.Global
	GlobalAdmin                  = core.GlobalAdmin
	GlobalAccess                 = core.GlobalAccess
	Config                       = core.Config
	ConfigTransformer            = core.ConfigTransformer
	Computed                     = core.Computed
	ComputedContext              = core.ComputedContext
	ExecuteOption                = core.ExecuteOption
	FieldAccess                  = core.FieldAccess
	FieldAccessContext           = core.FieldAccessContext
	FieldAccessRule              = core.FieldAccessRule
	FieldValidatorProvider       = core.FieldValidatorProvider
	FindOptions                  = core.FindOptions
	FieldCapabilities            = core.FieldCapabilities
	HandlerOptions               = core.HandlerOptions
	Hook                         = core.Hook
	HookContext                  = core.HookContext
	ImageSize                    = core.ImageSize
	ImportOptions                = core.ImportOptions
	JoinMutationResult           = core.JoinMutationResult
	ListOptions                  = core.ListOptions
	ListWindowOptions            = core.ListWindowOptions
	LocaleOptions                = core.LocaleOptions
	LocalAPI                     = core.LocalAPI
	MutationOptions              = core.MutationOptions
	Operation                    = core.Operation
	OperationCapabilities        = core.OperationCapabilities
	OperationError               = core.OperationError
	AccessCapabilities           = core.AccessCapabilities
	Plugin                       = core.Plugin
	PluginDescriptor             = core.PluginDescriptor
	PluginGenerationContext      = core.PluginGenerationContext
	PluginGeneratedArtifact      = core.PluginGeneratedArtifact
	PluginEndpoint               = core.PluginEndpoint
	PluginEndpointContext        = core.PluginEndpointContext
	PluginEndpointHandler        = core.PluginEndpointHandler
	PluginTransport              = core.PluginTransport
	PluginTransportContext       = core.PluginTransportContext
	PluginFieldType              = core.PluginFieldType
	PluginHookContribution       = core.PluginHookContribution
	PluginDatabaseAdapter        = core.PluginDatabaseAdapter
	PluginDatabaseContribution   = core.PluginDatabaseContribution
	PluginMigration              = core.PluginMigration
	HookProvider                 = core.HookProvider
	RiduCompatibility            = core.RiduCompatibility
	PluginFieldValidationContext = core.PluginFieldValidationContext
	PluginFieldValidator         = core.PluginFieldValidator
	ReconcileResult              = core.ReconcileResult
	RequestObservation           = core.RequestObservation
	RequestErrorEvent            = core.RequestErrorEvent
	ReadinessCheck               = core.ReadinessCheck
	ServerOptions                = core.ServerOptions
	StorageFactory               = core.StorageFactory
	StoreFactory                 = core.StoreFactory
	TaskDefinition               = core.TaskDefinition
	TaskContext                  = core.TaskContext
	TaskOption                   = core.TaskOption
	TaskReconciler               = core.TaskReconciler
	TaskEnqueueOptions           = core.TaskEnqueueOptions
	TaskError                    = core.TaskError
	TaskErrorCode                = core.TaskErrorCode
	TaskRunSummary               = core.TaskRunSummary
	TaskBackoff                  = core.TaskBackoff
	TaskState                    = store.TaskState
	UploadConfig                 = core.UploadConfig
	UploadInput                  = core.UploadInput
	VersionConfig                = core.VersionConfig
)

const (
	AccessAllow = core.AccessAllow
	AccessDeny  = core.AccessDeny
	AccessWhere = core.AccessWhere

	AdminPluginAPIVersion = core.AdminPluginAPIVersion
	PluginAPIVersion      = core.PluginAPIVersion

	OperationCreate          = core.OperationCreate
	OperationDuplicate       = core.OperationDuplicate
	OperationAdmin           = core.OperationAdmin
	OperationRead            = core.OperationRead
	OperationReadVersions    = core.OperationReadVersions
	OperationUpdate          = core.OperationUpdate
	OperationDelete          = core.OperationDelete
	OperationRestoreDeleted  = core.OperationRestoreDeleted
	OperationDeletePermanent = core.OperationDeletePermanent
	OperationPublish         = core.OperationPublish
	OperationUnpublish       = core.OperationUnpublish

	FrameworkVersion = core.FrameworkVersion

	PluginDatabaseAdapterPostgres = core.PluginDatabaseAdapterPostgres
	PluginDatabaseAdapterSQLite   = core.PluginDatabaseAdapterSQLite

	AuthOperationLogin             = core.AuthOperationLogin
	AuthOperationLogout            = core.AuthOperationLogout
	AuthOperationRefresh           = core.AuthOperationRefresh
	AuthOperationPasswordReset     = core.AuthOperationPasswordReset
	AuthOperationEmailVerification = core.AuthOperationEmailVerification
	AuthOperationAPIKey            = core.AuthOperationAPIKey
	AuthOperationExternalStrategy  = core.AuthOperationExternalStrategy

	TaskBackoffFixed       = core.TaskBackoffFixed
	TaskBackoffLinear      = core.TaskBackoffLinear
	TaskBackoffExponential = core.TaskBackoffExponential
	TaskStateQueued        = store.TaskStateQueued
	TaskStateRunning       = store.TaskStateRunning
	TaskStateSucceeded     = store.TaskStateSucceeded
	TaskStateFailed        = store.TaskStateFailed
	TaskStateCanceled      = store.TaskStateCanceled

	TaskErrorNotRegistered = core.TaskErrorNotRegistered
	TaskErrorUnavailable   = core.TaskErrorUnavailable
	TaskErrorInvalidInput  = core.TaskErrorInvalidInput
	TaskErrorInvalidOutput = core.TaskErrorInvalidOutput
	TaskErrorNotFound      = core.TaskErrorNotFound
	TaskErrorStoreFailed   = core.TaskErrorStoreFailed
	TaskErrorLeaseLost     = core.TaskErrorLeaseLost
)

type (
	TaskHandler[Input, Output any] = core.TaskHandler[Input, Output]
	TypedTask[Input, Output any]   = core.TypedTask[Input, Output]
	TaskReceipt[Output any]        = core.TaskReceipt[Output]
	TaskResult[Output any]         = core.TaskResult[Output]
)

const MaxTaskPayloadBytes = core.MaxTaskPayloadBytes

func NewTask[Input, Output any](slug string, handler TaskHandler[Input, Output], options ...TaskOption) TypedTask[Input, Output] {
	return core.NewTask(slug, handler, options...)
}

func TaskQueue(queue string) TaskOption { return core.TaskQueue(queue) }

func TaskRetries(maxAttempts int, delay, maxDelay time.Duration, backoff TaskBackoff) TaskOption {
	return core.TaskRetries(maxAttempts, delay, maxDelay, backoff)
}

func TaskTimeout(timeout time.Duration) TaskOption { return core.TaskTimeout(timeout) }

func TaskRetention(retention time.Duration) TaskOption { return core.TaskRetention(retention) }

func TaskAdmissionReconciler(reconcile TaskReconciler) TaskOption {
	return core.TaskAdmissionReconciler(reconcile)
}

func RetryTask(code string, cause error) error { return core.RetryTask(code, cause) }

func RetryTaskAfter(code string, cause error, delay time.Duration) error {
	return core.RetryTaskAfter(code, cause, delay)
}

func AbortTask(code string, cause error) error { return core.AbortTask(code, cause) }

// Allow authorizes an operation without adding a document filter.
func Allow() AccessDecision { return core.Allow() }

// Deny rejects an operation.
func Deny() AccessDecision { return core.Deny() }

// Where authorizes only documents matching expression.
func Where(expression query.Expression) AccessDecision { return core.Where(expression) }

// Resolve validates executable configuration and returns its canonical manifest.
func Resolve(applicationConfig Config) (schema.Manifest, error) {
	return core.Resolve(applicationConfig)
}

// New builds an application runtime using backend after resolving configuration.
func New(applicationConfig Config, backend store.Store) (*App, error) {
	return core.New(applicationConfig, backend)
}

// WithUploadStorage configures the upload storage factory used by Execute.
func WithUploadStorage(factory StorageFactory) ExecuteOption {
	return core.WithUploadStorage(factory)
}

// WithStore configures the singular document-store adapter lazily.
func WithStore(factory StoreFactory) ExecuteOption {
	return core.WithStore(factory)
}

// WithProjectMigrations registers an adapter-owned compiled migration driver
// for checksum-bound application data callbacks.
func WithProjectMigrations(driver migration.ProjectDriver) ExecuteOption {
	return core.WithProjectMigrations(driver)
}

// WithAddress configures the HTTP listen address. The default is :8080.
func WithAddress(address string) ExecuteOption {
	return core.WithAddress(address)
}

// WithHandlerOptions configures HTTP security, limits, and observation hooks.
func WithHandlerOptions(options HandlerOptions) ExecuteOption {
	return core.WithHandlerOptions(options)
}

// WithServerOptions customizes production socket and graceful-drain bounds.
func WithServerOptions(options ServerOptions) ExecuteOption {
	return core.WithServerOptions(options)
}

// Execute resolves configuration, initializes plugins, and runs configured services.
func Execute(applicationConfig Config, options ...ExecuteOption) error {
	return core.Execute(applicationConfig, options...)
}
