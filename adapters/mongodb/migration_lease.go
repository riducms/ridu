package mongodb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	defaultMongoMigrationLeaseWait        = 30 * time.Second
	defaultMongoMigrationLeaseDuration    = 30 * time.Second
	defaultMongoMigrationOperationTimeout = 30 * time.Minute
	mongoMigrationLeaseID                 = "single-migrator"
)

var errMongoMigrationLeaseLost = errors.New("MongoDB migration lease was lost")

// RunnerOptions bounds MongoDB migration coordination and index builds.
// AllowUnbounded permits a zero wait or operation timeout; lease expiry is
// always bounded so a crashed migrator cannot permanently block recovery.
type RunnerOptions struct {
	AllowMaintenance bool
	AllowUnbounded   bool
	LeaseWait        time.Duration
	LeaseDuration    time.Duration
	OperationTimeout time.Duration
}

type normalizedMongoMigrationRunnerOptions struct {
	leaseWait        time.Duration
	leaseDuration    time.Duration
	operationTimeout time.Duration
}

type mongoMigrationLeaseRecord struct {
	Owner     string
	Fence     string
	ExpiresAt time.Time
	UpdatedAt time.Time
}

type mongoMigrationLease struct {
	backend  *Store
	owner    string
	fence    string
	duration time.Duration
	mu       sync.Mutex
}

func normalizeMongoMigrationRunnerOptions(value RunnerOptions) (normalizedMongoMigrationRunnerOptions, error) {
	if value.LeaseWait < 0 || value.LeaseDuration < 0 || value.OperationTimeout < 0 {
		return normalizedMongoMigrationRunnerOptions{}, fmt.Errorf("MongoDB migration timeouts cannot be negative")
	}
	result := normalizedMongoMigrationRunnerOptions{
		leaseWait: value.LeaseWait, leaseDuration: value.LeaseDuration, operationTimeout: value.OperationTimeout,
	}
	if result.leaseDuration == 0 {
		result.leaseDuration = defaultMongoMigrationLeaseDuration
	}
	if !value.AllowUnbounded {
		if result.leaseWait == 0 {
			result.leaseWait = defaultMongoMigrationLeaseWait
		}
		if result.operationTimeout == 0 {
			result.operationTimeout = defaultMongoMigrationOperationTimeout
		}
	}
	if result.leaseDuration < 100*time.Millisecond {
		return normalizedMongoMigrationRunnerOptions{}, fmt.Errorf("MongoDB migration lease duration must be at least 100ms")
	}
	return result, nil
}

func newMongoMigrationToken() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("create MongoDB migration lease identity: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}

func decodeMongoMigrationLease(raw bson.Raw) (mongoMigrationLeaseRecord, error) {
	if err := requireExactKeys(raw, "MongoDB migration lease", "_id", "codec", "owner", "fence", "expiresAt", "updatedAt"); err != nil {
		return mongoMigrationLeaseRecord{}, err
	}
	id, idOK := raw.Lookup("_id").StringValueOK()
	codec, codecOK := raw.Lookup("codec").Int32OK()
	owner, ownerOK := raw.Lookup("owner").StringValueOK()
	fence, fenceOK := raw.Lookup("fence").StringValueOK()
	expiresAt, expiresAtOK := raw.Lookup("expiresAt").Int64OK()
	updatedAt, updatedAtOK := raw.Lookup("updatedAt").Int64OK()
	if !idOK || id != mongoMigrationLeaseID || !codecOK || codec != mongoMigrationLedgerCodecVersion ||
		!ownerOK || !fenceOK || !expiresAtOK || !updatedAtOK || !validMongoMigrationToken(owner) || !validMongoMigrationToken(fence) {
		return mongoMigrationLeaseRecord{}, fmt.Errorf("stored MongoDB migration lease has invalid field types or identity")
	}
	record := mongoMigrationLeaseRecord{Owner: owner, Fence: fence, ExpiresAt: decodeTime(expiresAt), UpdatedAt: decodeTime(updatedAt)}
	if record.UpdatedAt.IsZero() || !record.ExpiresAt.After(record.UpdatedAt) {
		return mongoMigrationLeaseRecord{}, fmt.Errorf("stored MongoDB migration lease has an invalid expiry")
	}
	return record, nil
}

func mongoMigrationLeaseFilter(owner, fence string, allowExpired bool) bson.D {
	conditions := bson.A{}
	if owner != "" && fence != "" {
		conditions = append(conditions, bson.D{{Key: "owner", Value: owner}, {Key: "fence", Value: fence}})
	}
	if allowExpired {
		conditions = append(conditions, bson.D{{Key: "$expr", Value: bson.D{{Key: "$lte", Value: bson.A{"$expiresAt", mongoTaskServerNowNanosExpression()}}}}})
	}
	filter := bson.D{{Key: "_id", Value: mongoMigrationLeaseID}}
	if len(conditions) == 1 {
		filter = append(filter, conditions[0].(bson.D)...)
	} else if len(conditions) != 0 {
		filter = append(filter, bson.E{Key: "$or", Value: conditions})
	}
	return filter
}

func mongoMigrationLeaseUpdate(owner, fence string, duration time.Duration) mongo.Pipeline {
	return mongo.Pipeline{bson.D{{Key: "$set", Value: bson.D{
		{Key: "codec", Value: mongoMigrationLedgerCodecVersion},
		{Key: "owner", Value: mongoTaskLiteral(owner)},
		{Key: "fence", Value: mongoTaskLiteral(fence)},
		{Key: "expiresAt", Value: mongoTaskServerDeadlineExpression(duration)},
		{Key: "updatedAt", Value: mongoTaskServerNowNanosExpression()},
	}}}}
}

func mongoMigrationLeaseCreateUpdate(owner, fence string, duration time.Duration) mongo.Pipeline {
	return append(
		mongoMigrationLeaseUpdate(owner, fence, duration),
		bson.D{{Key: "$unset", Value: "creationNonce"}},
	)
}

func (backend *Store) acquireMongoMigrationLease(ctx context.Context, wait, duration time.Duration) (*mongoMigrationLease, error) {
	if err := backend.prepareSystemOperation(ctx, "migration lease"); err != nil {
		return nil, err
	}
	owner, err := newMongoMigrationToken()
	if err != nil {
		return nil, err
	}
	fence, err := newMongoMigrationToken()
	if err != nil {
		return nil, err
	}
	waitContext := ctx
	cancel := func() {}
	if wait > 0 {
		waitContext, cancel = context.WithTimeout(ctx, wait)
	}
	defer cancel()
	collection := backend.mongoMigrationCollection(mongoMigrationLeaseCollectionName)
	for {
		// The nonce equality is copied into a genuine upsert but cannot match an
		// existing strict lease because the pipeline removes it. This gives the
		// initial create server-time expiry without putting $expr in an upsert
		// predicate (which MongoDB rejects).
		raw, updateErr := collection.FindOneAndUpdate(
			waitContext,
			bson.D{{Key: "_id", Value: mongoMigrationLeaseID}, {Key: "creationNonce", Value: owner}},
			mongoMigrationLeaseCreateUpdate(owner, fence, duration),
			options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
		).Raw()
		if updateErr == nil {
			return newAcquiredMongoMigrationLease(backend, owner, fence, duration, raw)
		}
		if !mongo.IsDuplicateKeyError(updateErr) {
			return nil, fmt.Errorf("acquire MongoDB migration lease: %w", translateMongoError(waitContext, updateErr))
		}
		stored, readErr := collection.FindOne(waitContext, bson.D{{Key: "_id", Value: mongoMigrationLeaseID}}).Raw()
		if readErr == nil {
			record, decodeErr := decodeMongoMigrationLease(stored)
			if decodeErr != nil {
				return nil, decodeErr
			}
			raw, updateErr = collection.FindOneAndUpdate(
				waitContext,
				bson.D{
					{Key: "_id", Value: mongoMigrationLeaseID}, {Key: "codec", Value: mongoMigrationLedgerCodecVersion},
					{Key: "owner", Value: record.Owner}, {Key: "fence", Value: record.Fence},
					{Key: "$expr", Value: bson.D{{Key: "$lte", Value: bson.A{"$expiresAt", mongoTaskServerNowNanosExpression()}}}},
				},
				mongoMigrationLeaseUpdate(owner, fence, duration),
				options.FindOneAndUpdate().SetReturnDocument(options.After),
			).Raw()
			if updateErr == nil {
				return newAcquiredMongoMigrationLease(backend, owner, fence, duration, raw)
			}
			if !errors.Is(updateErr, mongo.ErrNoDocuments) {
				return nil, fmt.Errorf("take over expired MongoDB migration lease: %w", translateMongoError(waitContext, updateErr))
			}
		} else if !errors.Is(readErr, mongo.ErrNoDocuments) {
			return nil, fmt.Errorf("inspect occupied MongoDB migration lease: %w", translateMongoError(waitContext, readErr))
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-waitContext.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, fmt.Errorf("wait for MongoDB migration lease: %w", waitContext.Err())
		case <-timer.C:
		}
	}
}

func newAcquiredMongoMigrationLease(backend *Store, owner, fence string, duration time.Duration, raw bson.Raw) (*mongoMigrationLease, error) {
	record, err := decodeMongoMigrationLease(raw)
	if err != nil {
		return nil, err
	}
	if record.Owner != owner || record.Fence != fence {
		return nil, fmt.Errorf("MongoDB migration lease acquisition returned another owner")
	}
	return &mongoMigrationLease{backend: backend, owner: owner, fence: fence, duration: duration}, nil
}

func (lease *mongoMigrationLease) refresh(ctx context.Context) error {
	lease.mu.Lock()
	defer lease.mu.Unlock()
	return lease.refreshUnlocked(ctx, lease.backend.mongoMigrationCollection(mongoMigrationLeaseCollectionName))
}

type mongoFindOneAndUpdater interface {
	FindOneAndUpdate(context.Context, any, any, ...options.Lister[options.FindOneAndUpdateOptions]) *mongo.SingleResult
}

func (lease *mongoMigrationLease) refreshUnlocked(ctx context.Context, collection mongoFindOneAndUpdater) error {
	filter := mongoMigrationLeaseFilter(lease.owner, lease.fence, false)
	filter = append(filter, bson.E{Key: "$expr", Value: bson.D{{Key: "$gt", Value: bson.A{"$expiresAt", mongoTaskServerNowNanosExpression()}}}})
	raw, err := collection.FindOneAndUpdate(ctx, filter, mongoMigrationLeaseUpdate(lease.owner, lease.fence, lease.duration), options.FindOneAndUpdate().SetReturnDocument(options.After)).Raw()
	if errors.Is(err, mongo.ErrNoDocuments) {
		return errMongoMigrationLeaseLost
	}
	if err != nil {
		return fmt.Errorf("refresh MongoDB migration lease: %w", translateMongoError(ctx, err))
	}
	record, err := decodeMongoMigrationLease(raw)
	if err != nil {
		return err
	}
	if record.Owner != lease.owner || record.Fence != lease.fence {
		return errMongoMigrationLeaseLost
	}
	return nil
}

func (lease *mongoMigrationLease) transaction(ctx context.Context, operation func(context.Context) error) error {
	lease.mu.Lock()
	defer lease.mu.Unlock()
	return lease.backend.runSystemTransaction(ctx, func(transaction *documentTransaction) error {
		sessionContext, leave, err := transaction.enter(ctx, true)
		if err != nil {
			return err
		}
		defer leave()
		if err := lease.refreshUnlocked(sessionContext, lease.backend.mongoMigrationCollection(mongoMigrationLeaseCollectionName)); err != nil {
			return err
		}
		return operation(sessionContext)
	})
}

func (lease *mongoMigrationLease) release() {
	lease.mu.Lock()
	defer lease.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), defaultCloseTimeout)
	defer cancel()
	_, _ = lease.backend.mongoMigrationCollection(mongoMigrationLeaseCollectionName).DeleteOne(ctx, bson.D{
		{Key: "_id", Value: mongoMigrationLeaseID}, {Key: "owner", Value: lease.owner}, {Key: "fence", Value: lease.fence},
	})
}
