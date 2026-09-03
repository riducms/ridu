// Package mongodb provides Ridu's official MongoDB document store.
package mongodb

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

const (
	defaultConnectTimeout         = 10 * time.Second
	defaultServerSelectionTimeout = 10 * time.Second
	defaultMaxConnectionIdleTime  = 30 * time.Minute
	defaultMaxPoolSize            = 10
	defaultCloseTimeout           = 10 * time.Second
)

var databaseNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Config controls MongoDB connection behavior. DatabaseURL is runtime-only
// infrastructure and must never be copied into a schema manifest or artifact.
type Config struct {
	DatabaseURL string
	// AllowInsecureTransport permits plaintext or certificate verification
	// bypasses for an explicitly selected local development fixture.
	AllowInsecureTransport bool
	ApplicationName        string
	ConnectTimeout         time.Duration
	ServerSelectionTimeout time.Duration
	MaxConnectionIdleTime  time.Duration
	MaxPoolSize            uint64
	MinPoolSize            uint64
}

// Store is one replica-set-backed MongoDB database with immutable migration
// ledger readiness and transactional document storage.
type Store struct {
	client   *mongo.Client
	database *mongo.Database
	now      func() time.Time

	closeMu  sync.Mutex
	closed   bool
	closeErr error

	indexLifecycleMu            sync.Mutex
	indexesMu                   sync.RWMutex
	verifiedIndexes             map[schema.StableID]mongoVerifiedIndexPlan
	verifiedVersionIndexes      map[schema.StableID]bool
	verifiedReferenceIndexes    bool
	verifiedPreferenceIndexes   bool
	verifiedDocumentLockIndexes bool
	verifiedTaskIndexes         bool
	verifiedAuthIndexes         bool
	verifiedUploadLockIndexes   bool

	uploadLockRetry time.Duration
	uploadLockWait  time.Duration

	uploadLockLifecycleMu     sync.Mutex
	uploadLockLifecycleCtx    context.Context
	uploadLockLifecycleCancel context.CancelFunc
	uploadLockOperations      sync.WaitGroup
}

var _ store.ReadinessStore = (*Store)(nil)
var _ store.MigrationReadinessStore = (*Store)(nil)

type mongoTopology struct {
	SetName                      string `bson:"setName"`
	Message                      string `bson:"msg"`
	IsWritablePrimary            bool   `bson:"isWritablePrimary"`
	ReadOnly                     bool   `bson:"readOnly"`
	LogicalSessionTimeoutMinutes *int64 `bson:"logicalSessionTimeoutMinutes"`
}

// Open connects to a TLS-protected MongoDB replica set using safe defaults.
func Open(ctx context.Context, databaseURL string) (*Store, error) {
	return OpenWithConfig(ctx, Config{DatabaseURL: databaseURL})
}

// OpenWithConfig validates the credential-bearing URL without echoing it,
// connects, and proves transaction-capable replica-set topology. It does not
// create collections, indexes, or migration state.
func OpenWithConfig(ctx context.Context, config Config) (*Store, error) {
	if ctx == nil {
		return nil, fmt.Errorf("MongoDB connection context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	clientOptions, databaseName, err := normalizedClientOptions(config)
	if err != nil {
		return nil, err
	}
	client, err := mongo.Connect(clientOptions)
	if err != nil {
		return nil, redactedConnectionError(ctx, "connect")
	}
	cleanup := func() {
		closeContext, cancel := context.WithTimeout(context.Background(), defaultCloseTimeout)
		defer cancel()
		_ = client.Disconnect(closeContext)
	}
	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		cleanup()
		return nil, redactedConnectionError(ctx, "ping")
	}
	var topology mongoTopology
	if err := client.Database("admin").RunCommand(ctx, bson.D{{Key: "hello", Value: 1}}).Decode(&topology); err != nil {
		cleanup()
		return nil, redactedConnectionError(ctx, "inspect topology")
	}
	if err := validateMongoTopology(topology); err != nil {
		cleanup()
		return nil, err
	}
	uploadLockLifecycleCtx, uploadLockLifecycleCancel := context.WithCancel(context.Background())
	return &Store{
		client: client, database: client.Database(databaseName), now: time.Now,
		verifiedIndexes:           make(map[schema.StableID]mongoVerifiedIndexPlan),
		verifiedVersionIndexes:    make(map[schema.StableID]bool),
		uploadLockRetry:           defaultUploadLockRetry,
		uploadLockWait:            defaultUploadLockWait,
		uploadLockLifecycleCtx:    uploadLockLifecycleCtx,
		uploadLockLifecycleCancel: uploadLockLifecycleCancel,
	}, nil
}

func validateMongoTopology(topology mongoTopology) error {
	if topology.Message == "isdbgrid" {
		return fmt.Errorf("MongoDB sharded clusters are not supported by the initial adapter; use a replica set")
	}
	if topology.SetName == "" || topology.LogicalSessionTimeoutMinutes == nil || !topology.IsWritablePrimary || topology.ReadOnly {
		return fmt.Errorf("MongoDB transactions require a writable replica-set primary with logical sessions enabled")
	}
	return nil
}

func normalizedClientOptions(config Config) (*options.ClientOptions, string, error) {
	if strings.TrimSpace(config.DatabaseURL) == "" {
		return nil, "", fmt.Errorf("MongoDB database URL is required")
	}
	for _, duration := range []struct {
		name  string
		value time.Duration
	}{
		{"connect timeout", config.ConnectTimeout},
		{"server selection timeout", config.ServerSelectionTimeout},
		{"maximum connection idle time", config.MaxConnectionIdleTime},
	} {
		if duration.value < 0 {
			return nil, "", fmt.Errorf("MongoDB %s cannot be negative", duration.name)
		}
	}
	if config.MaxPoolSize == 0 {
		config.MaxPoolSize = defaultMaxPoolSize
	}
	if config.MinPoolSize > config.MaxPoolSize {
		return nil, "", fmt.Errorf("MongoDB minimum pool size %d exceeds maximum pool size %d", config.MinPoolSize, config.MaxPoolSize)
	}
	databaseName, err := databaseNameFromURL(config.DatabaseURL)
	if err != nil {
		return nil, "", err
	}
	clientOptions := options.Client().ApplyURI(config.DatabaseURL)
	if err := clientOptions.Validate(); err != nil {
		return nil, "", errors.New("invalid MongoDB connection configuration")
	}
	if !config.AllowInsecureTransport && (clientOptions.TLSConfig == nil || clientOptions.TLSConfig.InsecureSkipVerify) {
		return nil, "", fmt.Errorf("MongoDB transport must verify TLS; configure TLS in the database URL, or explicitly allow insecure transport for local development")
	}
	applicationName := strings.TrimSpace(config.ApplicationName)
	if applicationName == "" {
		applicationName = "ridu"
	}
	if !utf8.ValidString(applicationName) || strings.ContainsRune(applicationName, '\x00') || len(applicationName) > 128 {
		return nil, "", fmt.Errorf("MongoDB application name must contain at most 128 valid UTF-8 non-NUL bytes")
	}
	connectTimeout := config.ConnectTimeout
	if connectTimeout == 0 {
		connectTimeout = defaultConnectTimeout
	}
	serverSelectionTimeout := config.ServerSelectionTimeout
	if serverSelectionTimeout == 0 {
		serverSelectionTimeout = defaultServerSelectionTimeout
	}
	maxConnectionIdleTime := config.MaxConnectionIdleTime
	if maxConnectionIdleTime == 0 {
		maxConnectionIdleTime = defaultMaxConnectionIdleTime
	}
	clientOptions.
		SetAppName(applicationName).
		SetConnectTimeout(connectTimeout).
		SetServerSelectionTimeout(serverSelectionTimeout).
		SetMaxConnIdleTime(maxConnectionIdleTime).
		SetMaxPoolSize(config.MaxPoolSize).
		SetMinPoolSize(config.MinPoolSize)
	if err := clientOptions.Validate(); err != nil {
		return nil, "", errors.New("invalid MongoDB connection configuration")
	}
	return clientOptions, databaseName, nil
}

func databaseNameFromURL(databaseURL string) (string, error) {
	parsed, err := url.Parse(databaseURL)
	if err != nil || (parsed.Scheme != "mongodb" && parsed.Scheme != "mongodb+srv") || parsed.Host == "" {
		return "", errors.New("invalid MongoDB connection configuration")
	}
	databaseName, err := url.PathUnescape(strings.TrimPrefix(parsed.EscapedPath(), "/"))
	if err != nil || databaseName == "" || strings.Contains(databaseName, "/") || len(databaseName) > 63 || !databaseNamePattern.MatchString(databaseName) {
		return "", fmt.Errorf("MongoDB database URL must select a database whose name contains only letters, numbers, underscores, or hyphens")
	}
	return databaseName, nil
}

func redactedConnectionError(ctx context.Context, action string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf("%s MongoDB: connection failed", action)
}

// Close releases the MongoDB client. It is idempotent and uses an internal
// bound so it satisfies the framework runtime's context-free close contract.
func (backend *Store) Close() error {
	if backend == nil {
		return nil
	}
	backend.closeMu.Lock()
	defer backend.closeMu.Unlock()
	if backend.closed {
		return backend.closeErr
	}
	backend.closed = true
	if backend.client == nil {
		return nil
	}
	backend.stopMongoUploadLockOperations()
	ctx, cancel := context.WithTimeout(context.Background(), defaultCloseTimeout)
	defer cancel()
	if err := backend.client.Disconnect(ctx); err != nil {
		backend.closeErr = fmt.Errorf("close MongoDB: connection close failed")
	}
	return backend.closeErr
}

// Ping proves that the connected replica set can serve a primary read.
func (backend *Store) Ping(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("MongoDB ping context is required")
	}
	if backend == nil || backend.client == nil {
		return fmt.Errorf("MongoDB database is unavailable")
	}
	backend.closeMu.Lock()
	closed := backend.closed
	backend.closeMu.Unlock()
	if closed {
		return fmt.Errorf("MongoDB database is closed")
	}
	if err := backend.client.Ping(ctx, readpref.Primary()); err != nil {
		return redactedConnectionError(ctx, "ping")
	}
	return nil
}

var _ store.HealthStore = (*Store)(nil)
