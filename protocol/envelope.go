package protocol

import (
	"encoding/json"

	"github.com/riducms/ridu/schema"
)

// CurrentVersion is the framework wire-protocol version. It changes
// independently from the schema manifest and project-command protocols.
const CurrentVersion uint32 = 1

// AdminPreparedRouteStateVersion is the browser bootstrap snapshot contract.
const AdminPreparedRouteStateVersion uint32 = 1

const AdminPreparedRouteStateMediaType = "application/vnd.ridu.admin-route-state+json"

type AdminPreparedRouteOutcomeV1 string

const (
	AdminPreparedRoutePrepared AdminPreparedRouteOutcomeV1 = "prepared"
	AdminPreparedRouteRedirect AdminPreparedRouteOutcomeV1 = "redirect"
	AdminPreparedRouteReload   AdminPreparedRouteOutcomeV1 = "reload"
	AdminPreparedRouteFallback AdminPreparedRouteOutcomeV1 = "fallback"
)

type AdminPreparedRouteKindV1 string

const (
	AdminPreparedRouteLogin              AdminPreparedRouteKindV1 = "login"
	AdminPreparedRouteSetup              AdminPreparedRouteKindV1 = "setup"
	AdminPreparedRouteDashboard          AdminPreparedRouteKindV1 = "dashboard"
	AdminPreparedRouteCustom             AdminPreparedRouteKindV1 = "custom"
	AdminPreparedRouteCollectionList     AdminPreparedRouteKindV1 = "collection-list"
	AdminPreparedRouteCollectionTrash    AdminPreparedRouteKindV1 = "collection-trash"
	AdminPreparedRouteCollectionCreate   AdminPreparedRouteKindV1 = "collection-create"
	AdminPreparedRouteCollectionDocument AdminPreparedRouteKindV1 = "collection-document"
	AdminPreparedRouteCollectionAPI      AdminPreparedRouteKindV1 = "collection-api"
	AdminPreparedRouteCollectionVersions AdminPreparedRouteKindV1 = "collection-versions"
	AdminPreparedRouteGlobalVersions     AdminPreparedRouteKindV1 = "global-versions"
	AdminPreparedRouteUpload             AdminPreparedRouteKindV1 = "upload"
	AdminPreparedRouteGlobalDocument     AdminPreparedRouteKindV1 = "global-document"
	AdminPreparedRouteGlobalAPI          AdminPreparedRouteKindV1 = "global-api"
	AdminPreparedRouteAccount            AdminPreparedRouteKindV1 = "account"
	AdminPreparedRouteSecurity           AdminPreparedRouteKindV1 = "security"
	AdminPreparedRouteNotFound           AdminPreparedRouteKindV1 = "not-found"
	AdminPreparedRouteError              AdminPreparedRouteKindV1 = "error"
)

type AdminPreparedRouteDataV1 struct {
	Kind     AdminPreparedRouteKindV1                       `json:"kind"`
	Data     *AdminCollectionListDataV1                     `json:"data,omitempty"`
	Document *AdminDocumentDataV1                           `json:"document,omitempty"`
	Create   *AdminCreateDataV1                             `json:"create,omitempty"`
	Versions *AdminVersionsDataV1                           `json:"versions,omitempty"`
	Access   *AdminReadResultV1[AccessCapabilitiesEnvelope] `json:"access,omitempty"`
	Security *AdminSecurityDataV1                           `json:"security,omitempty"`
}

// AdminReadResultV1 preserves independent read failures inside a prepared page.
// Exactly one of Value or Error is present; it is not an SDK request transcript.
type AdminReadResultV1[Value any] struct {
	Value *Value        `json:"value,omitempty"`
	Error *ErrorPayload `json:"error,omitempty"`
}

type AdminDocumentDataV1 struct {
	Document AdminReadResultV1[json.RawMessage]            `json:"document"`
	Access   AdminReadResultV1[AccessCapabilitiesEnvelope] `json:"access"`
}

type AdminCreateDataV1 struct {
	Values map[string]any                                `json:"values"`
	Access AdminReadResultV1[AccessCapabilitiesEnvelope] `json:"access"`
}

type DocumentVersion[Document any] struct {
	ID         string
	DocumentID string
	Revision   int
	Status     string
	Snapshot   Document
	CreatedAt  string
}

type AdminVersionsDataV1 struct {
	History  AdminReadResultV1[[]DocumentVersion[json.RawMessage]] `json:"history"`
	Detail   *AdminReadResultV1[DocumentVersion[json.RawMessage]]  `json:"detail,omitempty"`
	Document AdminDocumentDataV1                                   `json:"document"`
}

type AdminSecurityDataV1 struct {
	Sessions AdminReadResultV1[[]AuthSessionInfo] `json:"sessions"`
	APIKeys  AdminReadResultV1[[]APIKeyInfo]      `json:"apiKeys"`
}

// AdminCollectionListDataV1 is the list read model, independent of SDK method
// names. Counts contains only the status counts read by this response.
type AdminCollectionListDataV1 struct {
	Query       AdminCollectionListQueryV1            `json:"query"`
	Page        *AdminCollectionListPageV1            `json:"page,omitempty"`
	Counts      map[string]AdminCollectionListCountV1 `json:"counts"`
	Preferences *AdminCollectionListPreferencesV1     `json:"preferences,omitempty"`
}

type AdminCollectionListQueryV1 struct {
	Where      json.RawMessage `json:"where,omitempty"`
	CountWhere json.RawMessage `json:"countWhere,omitempty"`
	Locale     string          `json:"locale,omitempty"`
	Trash      bool            `json:"trash"`
}

type AdminCollectionListPageV1 struct {
	Value *CollectionPageEnvelope[json.RawMessage] `json:"value,omitempty"`
	Error *ErrorPayload                            `json:"error,omitempty"`
}

type AdminCollectionListCountV1 struct {
	Value *int          `json:"value,omitempty"`
	Error *ErrorPayload `json:"error,omitempty"`
}

type AdminCollectionListPreferenceV1 struct {
	Value json.RawMessage `json:"value,omitempty"`
	Error *ErrorPayload   `json:"error,omitempty"`
}

type AdminCollectionListPreferencesV1 struct {
	Workspace AdminCollectionListPreferenceV1 `json:"workspace"`
	Presets   AdminCollectionListPreferenceV1 `json:"presets"`
}

// AdminPreparedNavigationV1 contains the runtime values resolved for each route.
// Locale and capabilities can change without changing the identity context.
type AdminPreparedNavigationV1 struct {
	CollectionOperations map[string]OperationCapabilities `json:"collectionOperations"`
	GlobalOperations     map[string]OperationCapabilities `json:"globalOperations"`
	ContentLocale        string                           `json:"contentLocale,omitempty"`
}

// AdminPreparedRuntimeV1 is the complete safe global admin bootstrap. It never
// contains credentials, access predicates, executable hooks, or lock state.
type AdminPreparedRuntimeV1 struct {
	AdminPreparedNavigationV1
	Session       *AuthSession[json.RawMessage] `json:"session,omitempty"`
	AuthBootstrap bool                          `json:"authBootstrapAvailable"`
	Theme         string                        `json:"theme"`
	AdminLanguage string                        `json:"adminLanguage,omitempty"`
	AdminTimeZone string                        `json:"adminTimeZone,omitempty"`
	Preferences   map[string]json.RawMessage    `json:"preferences"`
	Manifest      schema.Snapshot               `json:"manifest"`
	// EncodedManifest optionally carries the canonical encoding of Manifest. A
	// server presenting an immutable manifest encodes it once and reuses the
	// bytes for every response; it is never decoded and must encode Manifest.
	EncodedManifest json.RawMessage `json:"-"`
}

// MarshalJSON writes EncodedManifest in place of encoding Manifest again.
func (runtime AdminPreparedRuntimeV1) MarshalJSON() ([]byte, error) {
	type fields AdminPreparedRuntimeV1
	if runtime.EncodedManifest == nil {
		return json.Marshal(fields(runtime))
	}
	// The shallower Manifest field takes precedence over the embedded one, and
	// both encodings end with the manifest.
	return json.Marshal(struct {
		fields
		Manifest json.RawMessage `json:"manifest"`
	}{fields(runtime), runtime.EncodedManifest})
}

type AdminPreparedRouteDiagnosticV1 struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type AdminPreparedRouteStateV1 struct {
	Version  uint32                      `json:"version"`
	Outcome  AdminPreparedRouteOutcomeV1 `json:"outcome"`
	Pathname string                      `json:"pathname"`
	Search   string                      `json:"search"`
	// ContextKey binds reusable runtime state to the resolved identity, manifest,
	// session and admin build. Fingerprint additionally binds it to this route.
	ContextKey  string `json:"contextKey"`
	Fingerprint string `json:"fingerprint"`
	// BuildID identifies the static bootstrap contract used to prepare this response.
	BuildID      string                                        `json:"buildId"`
	ModuleGroups []string                                      `json:"moduleGroups"`
	Runtime      *AdminPreparedRuntimeV1                       `json:"runtime,omitempty"`
	Navigation   *AdminPreparedNavigationV1                    `json:"navigation,omitempty"`
	Route        *AdminPreparedRouteDataV1                     `json:"route,omitempty"`
	Loaders      map[string]AdminReadResultV1[json.RawMessage] `json:"loaders,omitempty"`
	Location     string                                        `json:"location,omitempty"`
	Diagnostic   *AdminPreparedRouteDiagnosticV1               `json:"diagnostic,omitempty"`
}

// ErrorCode is a stable machine-readable failure category.
type ErrorCode string

const (
	ErrorValidation          ErrorCode = "validation"
	ErrorAccess              ErrorCode = "access_denied"
	ErrorNotFound            ErrorCode = "not_found"
	ErrorConflict            ErrorCode = "conflict"
	ErrorDeleteRestricted    ErrorCode = "delete_restricted"
	ErrorBadRequest          ErrorCode = "bad_request"
	ErrorInternal            ErrorCode = "internal"
	ErrorRateLimited         ErrorCode = "rate_limited"
	ErrorEmailNotVerified    ErrorCode = "email_not_verified"
	ErrorAuthFeatureDisabled ErrorCode = "auth_feature_disabled"
	ErrorInvalidAuthToken    ErrorCode = "invalid_auth_token"
	// ErrorInvalidCredential rejects an explicit Authorization credential. It is
	// distinct from access_denied so clients can discard a stored token without
	// mistaking an ordinary authorization failure for a lost session.
	ErrorInvalidCredential ErrorCode = "invalid_credential"
	// ErrorRejected reports a hook's deliberate refusal. Its message is written
	// for the person who made the request.
	ErrorRejected            ErrorCode = "rejected"
	ErrorInvalidPreviewToken ErrorCode = "invalid_preview_token"
	ErrorSelectionTooLarge   ErrorCode = "selection_too_large"
)

// ValidationIssue identifies one invalid input path.
type ValidationIssue struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
	// Target is an opaque schema/stable-occurrence correlation token for structured field issues.
	Target       string            `json:"target,omitempty"`
	FieldID      schema.StableID   `json:"fieldId,omitempty"`
	CollectionID schema.StableID   `json:"collectionId,omitempty"`
	GlobalID     schema.StableID   `json:"globalId,omitempty"`
	Locale       schema.LocaleCode `json:"locale,omitempty"`
}

// ErrorPayload is the stable structured error carried by ErrorEnvelope.
type ErrorPayload struct {
	Code      ErrorCode         `json:"code"`
	Status    int               `json:"status"`
	Message   string            `json:"message"`
	RequestID string            `json:"requestId,omitempty"`
	Issues    []ValidationIssue `json:"issues"`
	Details   json.RawMessage   `json:"details,omitempty"`
}

// ErrorEnvelope is returned for every failed HTTP operation.
type ErrorEnvelope struct {
	Error ErrorPayload `json:"error"`
}

// Pagination contains stable page metadata independent of a document type.
// HasNextPage is always exact. TotalDocs and TotalPages are present exactly
// when the list counted every match; a list read with pagination=false omits
// both instead of estimating them.
type Pagination struct {
	Page        int  `json:"page"`
	Limit       int  `json:"limit"`
	TotalDocs   *int `json:"totalDocs,omitempty"`
	TotalPages  *int `json:"totalPages,omitempty"`
	HasNextPage bool `json:"hasNextPage"`
	HasPrevPage bool `json:"hasPrevPage"`
}

// PageEnvelope contains one page of typed documents.
type PageEnvelope[Document any] struct {
	Docs       []Document `json:"docs"`
	Pagination Pagination `json:"pagination"`
}

// CollectionPageAccess carries list-scoped access capabilities for the
// collection and every document returned in the page.
type CollectionPageAccess struct {
	Collection AccessCapabilitiesEnvelope            `json:"collection"`
	Documents  map[string]AccessCapabilitiesEnvelope `json:"documents"`
}

// CollectionPageEnvelope enriches one collection page with access metadata
// when the caller explicitly requests it.
type CollectionPageEnvelope[Document any] struct {
	Docs       []Document           `json:"docs"`
	Pagination Pagination           `json:"pagination"`
	Access     CollectionPageAccess `json:"access"`
}

// CountEnvelope is the exact cardinality after caller and access filters.
type CountEnvelope struct {
	TotalDocs int `json:"totalDocs"`
}

// DocumentEnvelope contains one typed document.
type DocumentEnvelope[Document any] struct {
	Doc Document `json:"doc"`
}

// BulkEnvelope contains the documents changed by one atomic bulk operation.
type BulkEnvelope[Document any] struct {
	Docs []Document `json:"docs"`
}

// JoinMutationInput carries explicit inverse-relation deltas. It deliberately
// does not imply that the caller loaded the complete relationship set.
type JoinMutationInput struct {
	Additions []string `json:"additions"`
	Removals  []string `json:"removals"`
}

// JoinMutationEnvelope reports the refreshed source and applied target deltas.
type JoinMutationEnvelope[Document any] struct {
	Doc     Document `json:"doc"`
	Added   int      `json:"added"`
	Removed int      `json:"removed"`
}

// PreferenceEnvelope carries one opaque user-owned preference value.
type PreferenceEnvelope[Value any] struct {
	Value Value `json:"value"`
}

// ScheduledPublication is the safe public representation of one actionable publication change.
// Requesting auth identities are deliberately not exposed.
type ScheduledPublication struct {
	TimeZone         string `json:"timeZone,omitempty"`
	ID               string `json:"id"`
	Action           string `json:"action"`
	DocumentID       string `json:"documentId"`
	ExpectedRevision int    `json:"expectedRevision"`
	RunAt            string `json:"runAt"`
	Attempts         int    `json:"attempts"`
	LastError        string `json:"lastError,omitempty"`
	CreatedAt        string `json:"createdAt"`
}

type ScheduledPublicationEnvelope struct {
	ScheduledPublication ScheduledPublication `json:"scheduledPublication"`
}

type ScheduledPublicationsEnvelope struct {
	ScheduledPublications []ScheduledPublication `json:"scheduledPublications"`
}

// OperationCapabilities is a non-secret summary of operations the current
// actor may attempt for one resource or document. It never contains access
// predicates or executable authorization rules.
type OperationCapabilities struct {
	Admin           bool `json:"admin"`
	Create          bool `json:"create"`
	Read            bool `json:"read"`
	ReadVersions    bool `json:"readVersions"`
	Update          bool `json:"update"`
	Delete          bool `json:"delete"`
	Duplicate       bool `json:"duplicate"`
	Publish         bool `json:"publish"`
	Unpublish       bool `json:"unpublish"`
	RestoreDeleted  bool `json:"restoreDeleted"`
	DeletePermanent bool `json:"deletePermanent"`
	SelectAll       bool `json:"selectAll"`
}

// FieldCapabilities summarizes field visibility and write access. Keys in an
// AccessCapabilitiesEnvelope's Fields are canonical or concrete runtime field
// paths.
type FieldCapabilities struct {
	Read   bool `json:"read"`
	Create bool `json:"create"`
	Update bool `json:"update"`
}

// AccessCapabilitiesEnvelope carries the evaluated access state for the
// current actor and optional document/input snapshot. A field of a block
// definition has Fields entries at its located values and at each placement
// holding them; at any other placement, such as a row the client has yet to
// add, BlockFields gives its capabilities by block slug and
// definition-relative path.
type AccessCapabilitiesEnvelope struct {
	Operations  OperationCapabilities                   `json:"operations"`
	Fields      map[string]FieldCapabilities            `json:"fields"`
	BlockFields map[string]map[string]FieldCapabilities `json:"blockFields,omitempty"`
}

// CollectionSelectionInput carries the active list predicate to the bounded
// server-owned selection resolver.
type CollectionSelectionInput struct {
	Where json.RawMessage `json:"where,omitempty"`
	Trash bool            `json:"trash,omitempty"`
}

// CollectionSelectionItem freezes one read-visible ID and the capabilities
// used only to present its available authoring actions.
type CollectionSelectionItem struct {
	ID     string                     `json:"id"`
	Access AccessCapabilitiesEnvelope `json:"access"`
}

// CollectionSelectionEnvelope contains one complete bounded selection.
type CollectionSelectionEnvelope struct {
	Items     []CollectionSelectionItem `json:"items"`
	TotalDocs int                       `json:"totalDocs"`
}

// DocumentLock is the safe author identity and lease metadata shown in the admin.
type DocumentLock struct {
	DocumentID string `json:"documentId"`
	OwnerID    string `json:"ownerId"`
	OwnerLabel string `json:"ownerLabel"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
	ExpiresAt  string `json:"expiresAt"`
}

// DocumentLockEnvelope carries the current lock and current-actor capabilities.
type DocumentLockEnvelope struct {
	Lock        *DocumentLock `json:"lock"`
	Owned       bool          `json:"owned"`
	Acquired    bool          `json:"acquired"`
	CanTakeOver bool          `json:"canTakeOver"`
}

// DeleteEnvelope confirms a document deletion.
type DeleteEnvelope struct {
	ID      string `json:"id"`
	Deleted bool   `json:"deleted"`
}

// AuthSession contains framework-level session metadata and a typed user.
type AuthSession[User any] struct {
	// ID is the safe, non-secret identifier used for session management.
	ID string `json:"id"`
	// Collection is the auth-enabled collection that owns the user identity.
	Collection string `json:"collection"`
	// User is the current authenticated document.
	User User `json:"user"`
	// ExpiresAt is the session's RFC 3339 expiry timestamp.
	ExpiresAt string `json:"expiresAt"`
}

// AuthSessionInfo is safe device/session metadata. It never exposes a bearer
// token or token digest.
type AuthSessionInfo struct {
	ID         string `json:"id"`
	CreatedAt  string `json:"createdAt"`
	LastSeenAt string `json:"lastSeenAt"`
	ExpiresAt  string `json:"expiresAt"`
	IPAddress  string `json:"ipAddress,omitempty"`
	UserAgent  string `json:"userAgent,omitempty"`
	Current    bool   `json:"current"`
}

// AuthSessionsEnvelope lists the current user's active sessions.
type AuthSessionsEnvelope struct {
	Sessions []AuthSessionInfo `json:"sessions"`
}

// SessionEnvelope contains the current typed auth session.
type SessionEnvelope[User any] struct {
	Session AuthSession[User] `json:"session"`
}

// SessionTransport selects how a login delivers its opaque session token.
type SessionTransport string

const (
	// SessionTransportCookie stores the token in Ridu's HttpOnly session cookie.
	SessionTransportCookie SessionTransport = "cookie"
	// SessionTransportToken returns the token to the client, which presents it as
	// Authorization: Session <token>.
	SessionTransportToken SessionTransport = "token"
)

// LoginRequest authenticates one auth-collection identity.
type LoginRequest struct {
	Email     string           `json:"email"`
	Password  string           `json:"password"`
	Transport SessionTransport `json:"transport,omitempty"`
}

// SessionTokenEnvelope is returned to token-transport clients after login or
// rotation. Session is safe to serialize into rendered pages; Token is the
// bearer secret and must not be.
type SessionTokenEnvelope[User any] struct {
	Session AuthSession[User] `json:"session"`
	Token   string            `json:"token"`
}

// LogoutEnvelope confirms that the current session was cleared.
type LogoutEnvelope struct {
	LoggedOut bool `json:"loggedOut"`
}

// AuthActionEnvelope acknowledges a recovery or verification operation without
// disclosing whether an identity exists.
type AuthActionEnvelope struct {
	Success bool `json:"success"`
}

// AuthBootstrapEnvelope reports whether the configured admin-user collection
// can still accept its one anonymous first-user initialization.
type AuthBootstrapEnvelope struct {
	Available bool `json:"available"`
}

// PreviewToken is a short-lived read-only capability scoped to one resource.
type PreviewToken struct {
	Token      string `json:"token"`
	Resource   string `json:"resource"`
	Slug       string `json:"slug"`
	DocumentID string `json:"documentId"`
	ExpiresAt  string `json:"expiresAt"`
}

// PreviewTokenEnvelope carries a newly minted preview capability exactly once.
type PreviewTokenEnvelope struct {
	PreviewToken PreviewToken `json:"previewToken"`
}

// APIKey contains a newly generated bearer secret. Key is emitted only once.
type APIKey struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Key       string `json:"key"`
	CreatedAt string `json:"createdAt"`
	ExpiresAt string `json:"expiresAt,omitempty"`
}

// APIKeyInfo is safe metadata for an existing API key.
type APIKeyInfo struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	CreatedAt  string `json:"createdAt"`
	LastUsedAt string `json:"lastUsedAt,omitempty"`
	ExpiresAt  string `json:"expiresAt,omitempty"`
}

// APIKeyEnvelope returns a newly minted API key.
type APIKeyEnvelope struct {
	APIKey APIKey `json:"apiKey"`
}

// APIKeysEnvelope lists safe API key metadata.
type APIKeysEnvelope struct {
	APIKeys []APIKeyInfo `json:"apiKeys"`
}

// SchemaEnvelope exposes the resolved declarative schema to trusted tooling
// and the admin. It never contains executable authorization or secrets.
type SchemaEnvelope struct {
	Schema schema.Snapshot `json:"schema"`
}
