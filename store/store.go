package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

const MaxListWindowDocuments = 100

var (
	ErrNotFound         = errors.New("document not found")
	ErrConflict         = errors.New("document conflict")
	ErrAuthInitialized  = errors.New("auth collection is already initialized")
	ErrDeleteRestricted = errors.New("document deletion is restricted by references")
	ErrPopulationLimit  = errors.New("population materialization limit exceeded")
	ErrTaskLeaseLost    = errors.New("task lease was lost")
)

type Status string

const (
	StatusDraft     Status = "draft"
	StatusPublished Status = "published"
)

// Document is the adapter-neutral stored representation. Framework metadata
// remains separate from application field values.
type Document struct {
	ID        string
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
	Status    Status
	Revision  int
	Values    Values
	// LocalizationSources records the locale that supplied each projected
	// localized field path. It is response metadata and is never persisted.
	LocalizationSources map[string]schema.LocaleCode
}

// Values is a detached field-value set.
type Values map[string]Value

// Request identifies one collection operation. Filter is caller-owned while
// Access is the authorization predicate; adapters must apply both atomically.
type Request struct {
	Collection  schema.Collection
	Collections map[schema.StableID]schema.Collection
	ID          string
	Filter      *query.Node
	Access      *query.Node
	Page        int
	Limit       int
	Sort        []query.Sort
	// IndexWindow is set only for a count-free range read over one direct,
	// unique, indexed text field. Adapters must not broaden this range into a
	// count or offset query.
	IndexWindow *IndexWindow
	// Select is nil for all authored fields. A non-nil empty slice returns only
	// document metadata.
	Select           []query.Path
	Populate         []query.Population
	PopulationAccess map[schema.StableID]*query.Node
	// PopulationBudget is shared by every recursive population read that
	// contributes to one response. Official adapters initialize a default when
	// callers omit it so direct store use cannot bypass the materialization cap.
	PopulationBudget *PopulationBudget
	// PublishedOnly restricts every versioned collection read participating in
	// this request, including populated targets. Unversioned collections ignore it.
	PublishedOnly    bool
	Deletion         DeletionMode
	ExpectedRevision int
	// Locales is the complete configured locale order used to decode canonical
	// localized storage. LocaleChain starts with the requested locale and then
	// contains effective fallbacks used by atomic filtering and sorting.
	Locales     []schema.LocaleCode
	LocaleChain []schema.LocaleCode
	AllLocales  bool
	// Lock identifies the row lock required by a semantic read. Ordinary reads
	// use LockNone; relationship validation uses LockReference to keep an
	// accepted target from changing or being deleted before the enclosing write
	// commits. Coordinated framework mutations use LockMutation and acquire all
	// participants in deterministic reference order.
	Lock LockMode
}

// DeletionMode selects active or trashed documents. Active documents are the
// default so existing callers cannot accidentally expose deleted content.
type DeletionMode string

const (
	DeletionActive DeletionMode = ""
	DeletionTrash  DeletionMode = "trash"
	DeletionAll    DeletionMode = "all"
)

// LockMode describes the transaction-scoped row lock required by a store read.
type LockMode string

const (
	LockNone      LockMode = ""
	LockReference LockMode = "reference"
	LockMutation  LockMode = "mutation"
)

type CreateRequest struct {
	Collection schema.Collection
	ID         string
	Values     Values
	Status     Status
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Locales    []schema.LocaleCode
}

type UpdateRequest struct {
	Request
	Values Values
	Status *Status
	// ReplaceValues treats Values as the complete canonical authored document
	// state, including locale maps. Omitted fields and locales are removed. The
	// default remains a patch so ordinary updates preserve omitted values.
	// Framework version restore uses replacement after access, validation, hooks,
	// and reference checks have produced the canonical snapshot candidate.
	ReplaceValues bool
}

type Version struct {
	ID         string
	DocumentID string
	Revision   int
	Status     Status
	Snapshot   Document
	CreatedAt  time.Time
}

// VersionRequest identifies an access-filtered history query. Adapters must
// apply Access to each stored snapshot before returning it.
type VersionRequest struct {
	Collection  schema.Collection
	DocumentID  string
	Access      *query.Node
	Locales     []schema.LocaleCode
	LocaleChain []schema.LocaleCode
	AllLocales  bool
}

// VersionTransaction is required for version-enabled collections. Snapshots
// participate in the same transaction as their document mutation. ListVersions
// must apply access to the stored snapshots before returning them.
type VersionTransaction interface {
	SaveVersion(context.Context, schema.Collection, Document, int) (Version, error)
	ListVersions(context.Context, VersionRequest) ([]Version, error)
	FindVersion(context.Context, schema.Collection, string, int) (Version, error)
}

type Page struct {
	Documents []Document
	Page      int
	Limit     int
	Total     int
}

// DistinctRequest selects the unique scalar values visible through one
// collection query. Filter and Access must be applied together by the adapter;
// neither may be evaluated after values have been selected.
type DistinctRequest struct {
	Collection    schema.Collection
	Field         query.Path
	Filter        *query.Node
	Access        *query.Node
	Page          int
	Limit         int
	PublishedOnly bool
	Deletion      DeletionMode
	Locales       []schema.LocaleCode
	LocaleChain   []schema.LocaleCode
}

// DistinctPage is the adapter-neutral result of a paginated distinct read.
// Values are ordered by the selected field in ascending order.
type DistinctPage struct {
	Values []Value
	Page   int
	Limit  int
	Total  int
}

// DistinctTransaction is the focused optional capability used by
// LocalAPI.Distinct. It deliberately exposes no general aggregation surface.
type DistinctTransaction interface {
	Distinct(context.Context, DistinctRequest) (DistinctPage, error)
}

// ValidateDistinctRequest keeps the initial distinct contract deliberately
// small: one direct scalar, singular relationship, or singular upload field.
// This matches the common Payload findDistinct use without introducing an
// adapter-neutral aggregation language.
func ValidateDistinctRequest(request DistinctRequest) error {
	segments := request.Field.Segments()
	if len(segments) != 1 {
		return fmt.Errorf("distinct field must be a direct field")
	}
	if segments[0] == "id" {
		return nil
	}
	for _, candidate := range request.Collection.Fields {
		if candidate.Name != segments[0] {
			continue
		}
		supported := false
		switch candidate.Type {
		case schema.FieldTypeText, schema.FieldTypeCode, schema.FieldTypeTextarea, schema.FieldTypeEmail,
			schema.FieldTypeDate, schema.FieldTypeNumber, schema.FieldTypeCheckbox, schema.FieldTypeRadio:
			supported = candidate.Category == schema.FieldCategoryScalar
		case schema.FieldTypeSelect:
			supported = candidate.Category == schema.FieldCategoryScalar && candidate.Select != nil && !candidate.Select.HasMany
		case schema.FieldTypeRelationship:
			supported = candidate.Category == schema.FieldCategoryRelationship && candidate.Relationship != nil && !candidate.Relationship.HasMany && !candidate.Relationship.Polymorphic
		case schema.FieldTypeUpload:
			supported = candidate.Category == schema.FieldCategoryUpload && candidate.Upload != nil && !candidate.Upload.HasMany
		}
		if !supported {
			return fmt.Errorf("distinct field %q is not a singular scalar field", request.Field.String())
		}
		return nil
	}
	return fmt.Errorf("distinct field %q is not defined", request.Field.String())
}

// ListPageBounds normalizes ordinary list paging and returns safe half-open
// bounds for total matches. A logical offset beyond total or the int range is
// represented by start == end == total while preserving the requested page.
func ListPageBounds(page, limit, total int) (normalizedPage, normalizedLimit, start, end int) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	if total < 1 {
		return page, limit, 0, 0
	}
	pagesBefore := page - 1
	if pagesBefore > total/limit {
		return page, limit, total, total
	}
	start = pagesBefore * limit
	if start >= total {
		return page, limit, total, total
	}
	end = start + min(limit, total-start)
	return page, limit, start, end
}

// IndexWindow identifies one half-open range over a direct unique index.
// LowerBound is inclusive and UpperBound is exclusive. The unique key is the
// complete deterministic ordering; adapters must not add another sort term.
type IndexWindow struct {
	Path       query.Path
	LowerBound string
	UpperBound string
}

// Window is one count-free bounded unique-index range read. Production
// adapters fetch at most the requested limit plus one overflow sentinel and
// never compute a total match count or use an offset.
type Window struct {
	Documents []Document
	HasMore   bool
}

// WindowTransaction is the optional store capability required by
// LocalAPI.ListWindow. Production implementations must preserve the bounded
// unique-index range semantics enforced by ValidateListWindowRequest.
type WindowTransaction interface {
	ListWindow(context.Context, Request) (Window, error)
}

// ValidateListWindowRequest enforces the deliberately narrow store contract
// used by bounded background reconciliation. A valid request maps to one
// half-open range of a direct unique B-tree key and cannot add predicates that
// would defeat a production adapter's bounded index scan. Non-production
// strict fakes may scan an already-snapshotted map, but must retain and
// materialize only bounded candidates.
func ValidateListWindowRequest(request Request) error {
	if request.Limit < 1 || request.Limit > MaxListWindowDocuments {
		return fmt.Errorf("list window limit must be between 1 and %d", MaxListWindowDocuments)
	}
	window := request.IndexWindow
	if window == nil || window.Path.String() == "" || window.LowerBound == "" || window.UpperBound == "" || window.LowerBound >= window.UpperBound {
		return fmt.Errorf("list window requires a valid non-empty half-open index range")
	}
	if request.ExpectedRevision != 0 {
		return fmt.Errorf("list window does not support expected revisions")
	}
	if request.Deletion != DeletionActive {
		return fmt.Errorf("list window supports active documents only")
	}
	if request.Lock != LockNone {
		return fmt.Errorf("list window does not support document locks")
	}
	if request.ID != "" || request.Page != 0 || request.Filter != nil || request.Access != nil || len(request.Sort) != 0 || len(request.Populate) != 0 || request.PublishedOnly {
		return fmt.Errorf("list window does not support IDs, paging, predicates, custom sorting, population, or published-only reads")
	}
	if request.Collection.Capabilities.Trash || request.Collection.Capabilities.Versions {
		return fmt.Errorf("list window requires an unversioned collection without trash")
	}
	segments := window.Path.Segments()
	if len(segments) != 1 {
		return fmt.Errorf("list window index must be a direct field")
	}
	for _, candidate := range request.Collection.Fields {
		if candidate.Name != segments[0] {
			continue
		}
		if candidate.Category != schema.FieldCategoryScalar || candidate.Type != schema.FieldTypeText || candidate.Localized || !candidate.Unique || !candidate.Index {
			return fmt.Errorf("list window index must be a unique, indexed, non-localized text field")
		}
		return nil
	}
	return fmt.Errorf("list window index field %q is not defined", window.Path.String())
}

// FilteredSelectionRequest identifies read-visible document IDs for one
// bounded, frozen selection. Adapters apply Filter and Access together and
// return IDs in ascending canonical order.
type FilteredSelectionRequest struct {
	Collection  schema.Collection
	Filter      *query.Node
	Access      *query.Node
	Deletion    DeletionMode
	Limit       int
	Locales     []schema.LocaleCode
	LocaleChain []schema.LocaleCode
	AllLocales  bool
}

// FilteredSelection contains at most the requested limit and reports whether
// another matching document existed. Overflow must never be truncated
// silently into a successful selection.
type FilteredSelection struct {
	IDs      []string
	Overflow bool
}

// Store begins write-capable document transactions. All operation-engine
// mutations and reads run through a Transaction; adapters that can provide a
// concurrent read snapshot implement SnapshotStore as well.
type Store interface {
	Begin(context.Context) (Transaction, error)
}

// SnapshotStore begins a transaction whose reads observe one stable database
// snapshot for the transaction lifetime. The operation engine uses this
// capability for read-only lifecycles, and destructive upload reconciliation
// requires it so pagination cannot skip live references.
type SnapshotStore interface {
	BeginSnapshot(context.Context) (Transaction, error)
}

// MaxUploadReferenceCandidates is the largest object-key set one targeted
// upload-reference query may inspect. An upload owns one original object and
// at most 64 configured image variants, so the bound covers one complete
// document while preventing adapters from receiving an unbounded query.
const MaxUploadReferenceCandidates = 65

// UploadReferenceRequest asks whether a bounded set of object keys is still
// named by a current, trashed, or versioned upload document. Collections must
// contain the complete upload-enabled schema visible to the application.
type UploadReferenceRequest struct {
	Collections []schema.Collection
	ObjectKeys  []string
}

// UploadReferenceTransaction performs a targeted upload-reference lookup in
// the transaction's snapshot. Implementations must return only requested keys,
// without applying document access rules; cleanup is framework-owned and must
// account for current, trashed, and immutable version snapshots.
type UploadReferenceTransaction interface {
	ReferencedUploadObjects(context.Context, UploadReferenceRequest) ([]string, error)
}

// UploadObjectLocker serializes creation/adoption and destructive deletion of
// object keys across application processes. Implementations must acquire keys
// in deterministic order and keep them held until the returned release
// function is called. Transactions may implement this contract with locks that
// release automatically at transaction completion and return a no-op release.
// Release must be safe to call once after Context expiry.
type UploadObjectLocker interface {
	LockUploadObjects(context.Context, []string) (release func(), err error)
}

type HealthStore interface {
	Ping(context.Context) error
}

// ReadinessStore proves that the connected database is usable by the exact
// executable manifest and that its migration state is internally complete.
// It does not receive the executable's committed artifact history; production
// runtimes should use MigrationReadinessStore when an adapter provides it.
type ReadinessStore interface {
	Ready(context.Context, schema.Manifest) error
}

// MigrationReadinessStore proves that the connected database's complete,
// ordered migration ledger matches the history embedded in the executable in
// addition to satisfying ordinary manifest readiness.
type MigrationReadinessStore interface {
	ReadyWithMigrationHistory(context.Context, schema.Manifest, string) error
}

// Preference is one opaque, user-owned admin/application setting.
type Preference struct {
	CollectionID schema.StableID
	UserID       string
	Key          string
	Value        json.RawMessage
	UpdatedAt    time.Time
}

// PreferenceStore persists small per-user settings independently from content documents.
type PreferenceStore interface {
	GetPreference(context.Context, schema.StableID, string, string) (Preference, error)
	SetPreference(context.Context, Preference) (Preference, error)
	DeletePreference(context.Context, schema.StableID, string, string) error
	DeletePreferences(context.Context, schema.StableID, string) error
}

// DocumentLock is one expiring exclusive authoring lease.
type DocumentLock struct {
	CollectionID      schema.StableID
	DocumentID        string
	OwnerCollectionID schema.StableID
	OwnerID           string
	OwnerLabel        string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	ExpiresAt         time.Time
}

// DocumentLockStore atomically acquires, refreshes, takes over, and releases authoring locks.
type DocumentLockStore interface {
	FindDocumentLock(context.Context, schema.StableID, string, time.Time) (DocumentLock, error)
	AcquireDocumentLock(context.Context, DocumentLock, time.Time, bool) (DocumentLock, bool, error)
	ReleaseDocumentLock(context.Context, schema.StableID, string, schema.StableID, string) error
}

// DocumentReference identifies one document incarnation for lifecycle cleanup.
// Both values are required because document IDs are not globally unique.
type DocumentReference struct {
	CollectionID schema.StableID
	DocumentID   string
}

// ReferenceDeleteRequest identifies one target whose current incoming
// references must be reconciled before hard deletion. IgnoreOwners may contain
// the target itself and owners already hard-deleted earlier in the same
// transaction. A future batch deletion is not sufficient: adapters must keep
// treating an owner that still exists as a current reference.
type ReferenceDeleteRequest struct {
	Target       DocumentReference
	Collections  map[schema.StableID]schema.Collection
	IgnoreOwners []DocumentReference
}

// ReferenceConstraint identifies the schema boundary that restricted a hard
// delete. Document IDs are intentionally omitted so callers cannot use the
// error as a cross-document existence oracle.
type ReferenceConstraint struct {
	OwnerCollectionID schema.StableID
	FieldID           schema.StableID
}

// DeleteRestrictedError reports all deterministic schema constraints that
// prevented a target hard delete.
type DeleteRestrictedError struct {
	Constraints []ReferenceConstraint
}

func (err *DeleteRestrictedError) Error() string { return ErrDeleteRestricted.Error() }
func (err *DeleteRestrictedError) Is(target error) bool {
	return target == ErrDeleteRestricted
}

// Transaction is deliberately semantic: adapters do not expose generic SQL
// execution through the framework operation path.
type Transaction interface {
	Create(context.Context, CreateRequest) (Document, error)
	Find(context.Context, Request) (Document, error)
	List(context.Context, Request) (Page, error)
	ResolveFilteredSelection(context.Context, FilteredSelectionRequest) (FilteredSelection, error)
	Update(context.Context, UpdateRequest) (Document, error)
	Trash(context.Context, Request) (Document, error)
	Restore(context.Context, Request) (Document, error)
	Delete(context.Context, Request) (Document, error)
	// ApplyReferenceDelete atomically plans incoming current-document
	// references, rejects when any restrict policy matches, and otherwise
	// nullifies/removes those values. Version snapshots remain immutable.
	ApplyReferenceDelete(context.Context, ReferenceDeleteRequest) error
	// DeleteDocumentState idempotently removes framework-owned database state
	// where the document is either the target or the owning principal.
	DeleteDocumentState(context.Context, DocumentReference) error
	Commit(context.Context) error
	Rollback(context.Context) error
}

// AuthCredential is the private local-auth state associated with one document.
// It never crosses an API or manifest boundary.
type AuthCredential struct {
	User                Document
	PasswordHash        []byte
	FailedLoginAttempts int
	LockedUntil         time.Time
	Verified            bool
}

// AuthTokenPurpose separates recovery secrets that must never be accepted by
// another auth flow.
type AuthTokenPurpose string

const (
	AuthTokenPasswordReset AuthTokenPurpose = "password_reset"
	AuthTokenVerifyEmail   AuthTokenPurpose = "verify_email"
)

// AuthToken is a persisted one-way digest of a single-use auth secret.
type AuthToken struct {
	TokenHash    string
	Purpose      AuthTokenPurpose
	CollectionID schema.StableID
	UserID       string
	ExpiresAt    time.Time
	CreatedAt    time.Time
}

// AuthSession is a persisted opaque browser session. TokenHash is a one-way
// digest of the bearer credential; ID is the safe identifier exposed to users
// for session management.
type AuthSession struct {
	ID           string
	TokenHash    string
	CollectionID schema.StableID
	UserID       string
	ExpiresAt    time.Time
	CreatedAt    time.Time
	LastSeenAt   time.Time
	IPAddress    string
	UserAgent    string
}

// AuthAPIKey is a persisted high-entropy bearer credential. TokenHash is never
// returned to callers; ID is the public revocation handle and token prefix.
type AuthAPIKey struct {
	ID           string
	TokenHash    string
	CollectionID schema.StableID
	UserID       string
	Name         string
	CreatedAt    time.Time
	LastUsedAt   time.Time
	ExpiresAt    time.Time
}

// AuthStore is the explicit persistence capability required by auth-enabled
// collections. Implementations must make failed-attempt updates and session
// rotation atomic across concurrent processes.
type AuthStore interface {
	// SetPasswordHash replaces a user-controlled password and atomically revokes
	// every session and API key for that user.
	SetPasswordHash(context.Context, schema.Collection, string, []byte, bool) error
	// ChangePasswordHash replaces a password only while its exact stored hash is
	// the hash verified by the caller. A successful change revokes every session
	// and API key. Exact hash comparison also fences hard-delete/same-ID
	// credential recreation because bcrypt salts make each incarnation unique.
	ChangePasswordHash(context.Context, schema.Collection, string, []byte, []byte) error
	// UpgradePasswordHash raises the hash work factor after a successful login
	// without revoking otherwise-valid sessions. The exact-hash compare-and-set
	// prevents a slow bcrypt upgrade from overwriting a concurrent reset.
	UpgradePasswordHash(context.Context, schema.Collection, string, []byte, []byte) error
	FindAuthCredential(context.Context, schema.Collection, string) (AuthCredential, error)
	RecordFailedLogin(context.Context, schema.StableID, string, time.Time, int, time.Duration) (AuthCredential, error)
	ResetLoginAttempts(context.Context, schema.StableID, string, time.Time) (bool, error)
	// CreateSession atomically verifies the exact password hash observed by
	// password authentication before persisting the new bearer session.
	CreateSession(context.Context, AuthSession, []byte) error
	RotateSession(context.Context, string, AuthSession, time.Time) error
	DeleteSession(context.Context, string) error
	DeleteUserSession(context.Context, schema.StableID, string, string) error
	DeleteUserSessions(context.Context, schema.StableID, string) error
	FindSession(context.Context, string, time.Time) (AuthSession, error)
	ListSessions(context.Context, schema.StableID, string, time.Time) ([]AuthSession, error)
	CreateAuthToken(context.Context, AuthToken) error
	ResetPasswordWithToken(context.Context, schema.StableID, string, []byte, time.Time) (string, error)
	VerifyEmailWithToken(context.Context, schema.StableID, string, time.Time) (string, error)
	// CreateAPIKey inserts the key only while the authorizing session remains
	// active. Password replacement serializes through the same credential row
	// and therefore cannot leave a late-created key behind.
	CreateAPIKey(context.Context, AuthAPIKey, string, time.Time) error
	FindAPIKey(context.Context, string, time.Time) (AuthAPIKey, error)
	TouchAPIKey(context.Context, string, time.Time) error
	ListAPIKeys(context.Context, schema.StableID, string, time.Time) ([]AuthAPIKey, error)
	DeleteAPIKey(context.Context, schema.StableID, string, string) error
	AllowAuthAttempt(context.Context, string, time.Time, time.Duration, int) (bool, error)
}

// AuthTransaction creates private authentication state in the same transaction
// as its owning document. Password hashes never enter document values or hooks.
type AuthTransaction interface {
	CreateAuthCredential(context.Context, schema.Collection, string, []byte, bool) error
}

// AuthBootstrapTransaction is the one-time, transaction-owned first-user
// capability used by anonymous transports for the configured admin-user
// collection when it has no explicit create policy. Implementations must
// serialize contenders, prove that the just-created document is the only
// active document, and create its private credential before allowing the
// transaction to commit.
type AuthBootstrapTransaction interface {
	CreateFirstAuthCredential(context.Context, schema.Collection, string, []byte, bool) error
}

// AuthUnlockStore optionally supports an authorized administrator clearing a
// persisted account lock before its configured duration expires.
type AuthUnlockStore interface {
	ForceUnlock(context.Context, schema.StableID, string) error
}

// AuthUnlockTransaction clears account lockout state inside the same document
// transaction that locked and access-checked the owning auth document.
type AuthUnlockTransaction interface {
	ForceUnlockAuth(context.Context, schema.StableID, string) error
}

type ScheduledPublish struct {
	ID                      string
	CollectionID            schema.StableID
	DocumentID              string
	ExpectedRevision        int
	RunAt                   time.Time
	Attempts                int
	RequestedByCollectionID schema.StableID
	RequestedByUserID       string
	LastError               string
	CreatedAt               time.Time
}

// TaskState is the persisted lifecycle of one durable task. A running task is
// owned only while its lease token and expiry remain current. Terminal tasks
// are retained until RetainUntil so callers can inspect typed output or a
// stable failure without turning the queue into an unbounded log.
type TaskState string

const (
	TaskStateQueued    TaskState = "queued"
	TaskStateRunning   TaskState = "running"
	TaskStateSucceeded TaskState = "succeeded"
	TaskStateFailed    TaskState = "failed"
	TaskStateCanceled  TaskState = "canceled"
)

// TaskBackoff names the deterministic retry schedule captured when a task is
// enqueued. Persisting the policy prevents a deployment-time config change
// from silently changing the behavior of already-durable work.
type TaskBackoff string

const (
	TaskBackoffFixed       TaskBackoff = "fixed"
	TaskBackoffLinear      TaskBackoff = "linear"
	TaskBackoffExponential TaskBackoff = "exponential"
)

// Task is an adapter-neutral durable task record. Input and Output contain
// data only; executable handlers are compiled into the application and are
// selected from its validated registry by Slug.
type Task struct {
	ID             string
	Slug           string
	Queue          string
	ConcurrencyKey string
	Input          json.RawMessage
	Output         json.RawMessage
	State          TaskState
	RunAt          time.Time
	Attempts       int
	MaxAttempts    int
	RetryDelay     time.Duration
	MaxRetryDelay  time.Duration
	Backoff        TaskBackoff
	Timeout        time.Duration
	Retention      time.Duration
	LeaseToken     string
	LeaseExpiresAt *time.Time
	Target         *DocumentReference
	RequestedBy    *DocumentReference
	LastErrorCode  string
	LastError      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	CompletedAt    *time.Time
	RetainUntil    *time.Time
}

// TaskClaim bounds one SKIP LOCKED claim. Queues and Slugs are optional
// allow-lists. A worker normally claims all configured queues so an unknown
// persisted slug is claimed and moved to a stable terminal failure instead of
// executing serialized code or remaining invisibly stuck.
type TaskClaim struct {
	Limit         int
	Queues        []string
	Slugs         []string
	LeaseDuration time.Duration
}

// TaskList limits local inspection to a task slug, optional target, and
// lifecycle states. Limit must be positive and is adapter-bounded.
type TaskList struct {
	Slug   string
	Target *DocumentReference
	States []TaskState
	Limit  int
}

// TaskFailure records one owned attempt. RetryAfter nil moves the task to the
// terminal failed state; otherwise it returns the task to the durable queue.
type TaskFailure struct {
	ID         string
	LeaseToken string
	Code       string
	Message    string
	// RetryAfter is relative to the store's authoritative clock. Nil makes the
	// failure terminal; a non-nil duration returns the task to the queue.
	RetryAfter *time.Duration
}

// TaskStore is the general-purpose durable queue boundary. Every mutation of
// running work is fenced by the opaque LeaseToken returned by ClaimTasks.
// Adapters must use their authoritative clock for due work, lease expiry,
// retries, retention, and pruning; reclaim expired leases; enforce concurrency
// keys atomically; and translate an expired lease or ownership mismatch to
// ErrTaskLeaseLost.
type TaskStore interface {
	EnqueueTask(context.Context, Task) (Task, error)
	FindTask(context.Context, string) (Task, error)
	ListTasks(context.Context, TaskList) ([]Task, error)
	CancelTask(context.Context, string) error
	// DismissTaskForTarget removes one task only when its slug and target match.
	// This powers target-scoped action lists such as scheduled publishing: every
	// listed queued, running, failed, or canceled item remains dismissible while
	// general CancelTask keeps retained cancellation status for typed callers.
	DismissTaskForTarget(context.Context, string, string, DocumentReference) error
	ClaimTasks(context.Context, TaskClaim) ([]Task, error)
	HeartbeatTask(context.Context, string, string, time.Duration) error
	CompleteTask(context.Context, string, string, json.RawMessage) error
	FailTask(context.Context, TaskFailure) error
	ReleaseTask(context.Context, string, string, time.Duration, string, string) error
	PruneTasks(context.Context, int) (int, error)
}
