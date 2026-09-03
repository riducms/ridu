package protocol

import (
	"encoding/json"

	"github.com/riducms/ridu/schema"
)

// CurrentVersion is the framework wire-protocol version. It changes
// independently from the schema manifest and project-command protocols.
const CurrentVersion uint32 = 1

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
	ErrorInvalidPreviewToken ErrorCode = "invalid_preview_token"
	ErrorSelectionTooLarge   ErrorCode = "selection_too_large"
)

// ValidationIssue identifies one invalid input path.
type ValidationIssue struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
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
type Pagination struct {
	Page        int  `json:"page"`
	Limit       int  `json:"limit"`
	TotalDocs   int  `json:"totalDocs"`
	TotalPages  int  `json:"totalPages"`
	HasNextPage bool `json:"hasNextPage"`
	HasPrevPage bool `json:"hasPrevPage"`
}

// PageEnvelope contains one page of typed documents.
type PageEnvelope[Document any] struct {
	Docs       []Document `json:"docs"`
	Pagination Pagination `json:"pagination"`
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

// ScheduledPublish is the safe public representation of one queued publish.
// Requesting auth identities are deliberately not exposed.
type ScheduledPublish struct {
	ID               string `json:"id"`
	DocumentID       string `json:"documentId"`
	ExpectedRevision int    `json:"expectedRevision"`
	RunAt            string `json:"runAt"`
	Attempts         int    `json:"attempts"`
	LastError        string `json:"lastError,omitempty"`
	CreatedAt        string `json:"createdAt"`
}

type ScheduledPublishEnvelope struct {
	ScheduledPublish ScheduledPublish `json:"scheduledPublish"`
}

type ScheduledPublishesEnvelope struct {
	ScheduledPublishes []ScheduledPublish `json:"scheduledPublishes"`
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
// AccessCapabilitiesEnvelope are authored or concrete runtime field paths.
type FieldCapabilities struct {
	Read   bool `json:"read"`
	Create bool `json:"create"`
	Update bool `json:"update"`
}

// AccessCapabilitiesEnvelope carries the evaluated access state for the
// current actor and optional document/input snapshot.
type AccessCapabilitiesEnvelope struct {
	Operations OperationCapabilities        `json:"operations"`
	Fields     map[string]FieldCapabilities `json:"fields"`
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
