package mongodb

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoMigrationArtifactLedgerCodecIsStrict(t *testing.T) {
	row := mongoMigrationArtifactLedgerRow{
		Position: 1, Name: "20260831120000.000000000_initial.ridu.json",
		Digest: strings.Repeat("a", 64), ToDigest: strings.Repeat("b", 64),
		PlannerName: mongoDBPlannerName, PlannerVersion: mongoDBPlannerVersion,
		StepCount: 2, AppliedAt: time.Unix(1_800_000_000, 123).UTC(),
	}
	document, err := encodeMongoMigrationArtifactLedger(row)
	if err != nil {
		t.Fatal(err)
	}
	raw := mustMongoMigrationRaw(t, document)
	decoded, err := decodeMongoMigrationArtifactLedger(raw)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Name != row.Name || decoded.Digest != row.Digest || decoded.StepCount != row.StepCount || !decoded.AppliedAt.Equal(row.AppliedAt) {
		t.Fatalf("artifact ledger round trip = %#v", decoded)
	}
	tampered := append(append(bson.D(nil), document...), bson.E{Key: "unreviewed", Value: true})
	if _, err := decodeMongoMigrationArtifactLedger(mustMongoMigrationRaw(t, tampered)); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("artifact ledger unknown-field error = %v", err)
	}
}

func TestMongoMigrationStepLedgerCodecBindsExactIdentityAndState(t *testing.T) {
	completed := time.Unix(1_800_000_001, 0).UTC()
	row := mongoMigrationStepLedgerRow{
		ArtifactName: "20260831120000.000000000_initial.ridu.json", ArtifactDigest: strings.Repeat("a", 64),
		PhaseID: "add-indexes", StepID: "create-title", PhaseMode: ridumigration.PhaseNoTransaction,
		StepKind: ridumigration.StepMongoDBCreateIndex, State: mongoMigrationStepComplete, Attempts: 2,
		Owner: strings.Repeat("1", 32), Fence: strings.Repeat("2", 32),
		UpdatedAt: completed, CompletedAt: &completed,
	}
	document, err := encodeMongoMigrationStepLedger(row)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeMongoMigrationStepLedger(mustMongoMigrationRaw(t, document))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ID != mongoMigrationStepLedgerID(row.ArtifactName, row.PhaseID, row.StepID) || decoded.State != mongoMigrationStepComplete || decoded.Attempts != 2 {
		t.Fatalf("step ledger round trip = %#v", decoded)
	}
	document = append(bson.D(nil), document...)
	for index := range document {
		if document[index].Key == "stepID" {
			document[index].Value = "another-step"
		}
	}
	if _, err := decodeMongoMigrationStepLedger(mustMongoMigrationRaw(t, document)); err == nil || !strings.Contains(err.Error(), "identity") {
		t.Fatalf("step ledger identity error = %v", err)
	}
}

func TestMongoMigrationLedgerContinuityRejectsLineageGaps(t *testing.T) {
	first := mongoMigrationArtifactLedgerRow{
		Position: 1, Name: "001_initial", Digest: strings.Repeat("a", 64), ToDigest: strings.Repeat("b", 64),
	}
	second := mongoMigrationArtifactLedgerRow{
		Position: 2, Name: "002_next", Digest: strings.Repeat("c", 64), PreviousArtifactDigest: first.Digest,
		FromDigest: first.ToDigest, ToDigest: strings.Repeat("d", 64),
	}
	if err := validateMongoMigrationArtifactLedgerContinuity([]mongoMigrationArtifactLedgerRow{first, second}); err != nil {
		t.Fatal(err)
	}
	second.PreviousArtifactDigest = strings.Repeat("e", 64)
	if err := validateMongoMigrationArtifactLedgerContinuity([]mongoMigrationArtifactLedgerRow{first, second}); err == nil || !strings.Contains(err.Error(), "predecessor") {
		t.Fatalf("lineage-gap error = %v", err)
	}
}

func TestMongoMigrationRunnerOptionsStayBoundedByDefault(t *testing.T) {
	bounded, err := normalizeMongoMigrationRunnerOptions(RunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if bounded.leaseWait <= 0 || bounded.leaseDuration <= 0 || bounded.operationTimeout <= 0 {
		t.Fatalf("default MongoDB migration bounds = %#v", bounded)
	}
	unbounded, err := normalizeMongoMigrationRunnerOptions(RunnerOptions{AllowUnbounded: true})
	if err != nil {
		t.Fatal(err)
	}
	if unbounded.leaseWait != 0 || unbounded.operationTimeout != 0 || unbounded.leaseDuration <= 0 {
		t.Fatalf("explicit unbounded MongoDB migration options = %#v", unbounded)
	}
	for _, options := range []RunnerOptions{{LeaseWait: -time.Second}, {LeaseDuration: -time.Second}, {OperationTimeout: -time.Second}} {
		if _, err := normalizeMongoMigrationRunnerOptions(options); err == nil {
			t.Fatalf("negative MongoDB migration option accepted: %#v", options)
		}
	}
}

func TestMongoMigrationPublicBoundariesRejectNilContext(t *testing.T) {
	var backend *Store
	if err := backend.ApplyArtifactsWithOptions(nil, t.TempDir(), RunnerOptions{}); err == nil || !strings.Contains(err.Error(), "context") {
		t.Fatalf("nil apply context error = %v", err)
	}
	if _, err := backend.ArtifactStatus(nil, t.TempDir()); err == nil || !strings.Contains(err.Error(), "context") {
		t.Fatalf("nil status context error = %v", err)
	}
	if err := VerifyArtifactsWithOptions(nil, Config{}, t.TempDir(), RunnerOptions{}); err == nil || !strings.Contains(err.Error(), "context") {
		t.Fatalf("nil verify context error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := backend.ApplyArtifactsWithOptions(canceled, t.TempDir(), RunnerOptions{}); err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("canceled apply context error = %v", err)
	}
}

func TestMongoMigrationVerifierRejectsInvalidOptionsBeforeConnection(t *testing.T) {
	const secret = "must-not-connect-or-leak"
	err := VerifyArtifactsWithOptions(
		context.Background(),
		Config{
			DatabaseURL: "mongodb://" + secret + "@127.0.0.1:1/ridu", AllowInsecureTransport: true,
			ConnectTimeout: time.Millisecond, ServerSelectionTimeout: time.Millisecond,
		},
		filepath.Join("testdata", "historical-v1"),
		RunnerOptions{LeaseWait: -time.Second},
	)
	if err == nil || !strings.Contains(err.Error(), "negative") || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "connection") {
		t.Fatalf("pre-connection option validation error = %v", err)
	}
}

func TestMongoMigrationHeartbeatTreatsExplicitShutdownAsSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	lease := &mongoMigrationLease{}
	if err := lease.heartbeatRefresh(ctx, time.Second); err != nil {
		t.Fatalf("heartbeat refresh after explicit shutdown = %v", err)
	}
}

func TestMongoMigrationHeartbeatTimeoutStartsAfterSemanticFenceLock(t *testing.T) {
	lease := &mongoMigrationLease{}
	lease.mu.Lock()
	const refreshTimeout = 20 * time.Millisecond
	waitStarted := make(chan struct{}, 1)
	coordinated := make(chan bool, 1)
	go func() {
		select {
		case <-waitStarted:
			time.Sleep(3 * refreshTimeout)
			coordinated <- true
		case <-time.After(time.Second):
			coordinated <- false
		}
		lease.mu.Unlock()
	}()
	ctx := mongoHeartbeatLockSignalContext{Context: context.Background(), waitStarted: waitStarted}
	if err := lease.heartbeatRefreshWith(ctx, refreshTimeout, func(ctx context.Context) error {
		return ctx.Err()
	}); err != nil {
		t.Fatalf("heartbeat timeout included semantic fence wait: %v", err)
	}
	if !<-coordinated {
		t.Fatal("heartbeat did not reach the semantic fence before the test released it")
	}
}

type mongoHeartbeatLockSignalContext struct {
	context.Context
	waitStarted chan<- struct{}
}

func (ctx mongoHeartbeatLockSignalContext) Err() error {
	select {
	case ctx.waitStarted <- struct{}{}:
	default:
	}
	return ctx.Context.Err()
}

func TestMongoVerifyIndexesHonorsContextWhileLifecycleIsBusy(t *testing.T) {
	backend := &Store{}
	backend.indexLifecycleMu.Lock()
	defer backend.indexLifecycleMu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := backend.VerifyIndexes(ctx, schema.Manifest{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("bounded VerifyIndexes lifecycle error = %v", err)
	}
}

func TestMongoMigrationCompletedCreateStepBecomesRequiredPhysicalState(t *testing.T) {
	fileName := "001_initial"
	step := mongoDBArtifactReplayStep{
		phaseID: "add-indexes", stepID: "create-title", mode: ridumigration.PhaseNoTransaction,
		kind:  ridumigration.StepMongoDBCreateIndex,
		index: mongoDBPlannedIndex{collection: "z_content", name: "z_title"},
	}
	replay := []mongoDBArtifactReplayPlan{{fileName: fileName, steps: []mongoDBArtifactReplayStep{step}}}
	id := mongoMigrationStepLedgerID(fileName, step.phaseID, step.stepID)
	state := mongoMigrationLedgerState{stepByID: map[string]mongoMigrationStepLedgerRow{id: {State: mongoMigrationStepRunning}}}
	if required := mongoMigrationRequiredIndexes(replay, 0, state); len(required) != 0 {
		t.Fatalf("running step required physical indexes = %#v", required)
	}
	row := state.stepByID[id]
	row.State = mongoMigrationStepComplete
	state.stepByID[id] = row
	required := mongoMigrationRequiredIndexes(replay, 0, state)
	if len(required) != 1 || required[0].collection != step.index.collection || required[0].name != step.index.name {
		t.Fatalf("completed step required physical indexes = %#v", required)
	}
}

func TestMongoMigrationReadinessRejectsLedgerChangeAfterIndexVerification(t *testing.T) {
	artifactDigest := strings.Repeat("a", 64)
	manifestDigest := strings.Repeat("b", 64)
	state := mongoMigrationLedgerState{
		artifacts: []mongoMigrationArtifactLedgerRow{{
			Name: "001_initial", Digest: artifactDigest, ToDigest: manifestDigest, StepCount: 1,
		}},
		steps: []mongoMigrationStepLedgerRow{{
			ArtifactName: "001_initial", ArtifactDigest: artifactDigest, State: mongoMigrationStepComplete, Attempts: 1,
		}},
	}
	if err := validateMongoMigrationReadyState(state, manifestDigest); err != nil {
		t.Fatal(err)
	}
	confirmed := state
	confirmed.steps = append([]mongoMigrationStepLedgerRow(nil), state.steps...)
	confirmed.steps[0].Attempts++
	if mongoMigrationReadyStatesEqual(state, confirmed) {
		t.Fatal("readiness accepted ledger mutation across physical verification")
	}
	confirmed = state
	confirmed.steps = append(confirmed.steps, mongoMigrationStepLedgerRow{
		ArtifactName: "002_pending", ArtifactDigest: strings.Repeat("c", 64), State: mongoMigrationStepRunning,
	})
	if err := validateMongoMigrationReadyState(confirmed, manifestDigest); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("readiness incomplete-work error = %v", err)
	}
}

func TestMongoMigrationReadinessBindsExactOrderedArtifactHistory(t *testing.T) {
	manifestDigest := strings.Repeat("f", 64)
	identities := []ridumigration.ArtifactIdentity{
		{Name: "001_initial", Digest: strings.Repeat("a", 64)},
		{Name: "002_data_only", Digest: strings.Repeat("b", 64)},
		{Name: "003_indexes", Digest: strings.Repeat("c", 64)},
	}
	expectedHistoryDigest, err := ridumigration.DigestArtifactHistory(identities)
	if err != nil {
		t.Fatal(err)
	}
	state := mongoMigrationLedgerState{}
	for index, identity := range identities {
		state.artifacts = append(state.artifacts, mongoMigrationArtifactLedgerRow{
			Position: index + 1, Name: identity.Name, Digest: identity.Digest,
			ToDigest: manifestDigest, StepCount: 1,
		})
		state.steps = append(state.steps, mongoMigrationStepLedgerRow{
			ArtifactName: identity.Name, ArtifactDigest: identity.Digest,
			State: mongoMigrationStepComplete, Attempts: 1,
		})
	}
	if err := validateMongoMigrationReadyStateWithHistory(state, manifestDigest, expectedHistoryDigest); err != nil {
		t.Fatalf("exact MongoDB migration history readiness: %v", err)
	}

	clone := func(source mongoMigrationLedgerState) mongoMigrationLedgerState {
		return mongoMigrationLedgerState{
			artifacts: append([]mongoMigrationArtifactLedgerRow(nil), source.artifacts...),
			steps:     append([]mongoMigrationStepLedgerRow(nil), source.steps...),
		}
	}
	tests := map[string]func(mongoMigrationLedgerState) mongoMigrationLedgerState{
		"missing": func(candidate mongoMigrationLedgerState) mongoMigrationLedgerState {
			candidate.artifacts = candidate.artifacts[:len(candidate.artifacts)-1]
			candidate.steps = candidate.steps[:len(candidate.steps)-1]
			return candidate
		},
		"altered": func(candidate mongoMigrationLedgerState) mongoMigrationLedgerState {
			candidate.artifacts[1].Digest = strings.Repeat("d", 64)
			candidate.steps[1].ArtifactDigest = candidate.artifacts[1].Digest
			return candidate
		},
		"renamed": func(candidate mongoMigrationLedgerState) mongoMigrationLedgerState {
			candidate.artifacts[1].Name = "002_renamed"
			candidate.steps[1].ArtifactName = candidate.artifacts[1].Name
			return candidate
		},
		"reordered": func(candidate mongoMigrationLedgerState) mongoMigrationLedgerState {
			candidate.artifacts[0], candidate.artifacts[1] = candidate.artifacts[1], candidate.artifacts[0]
			candidate.steps[0], candidate.steps[1] = candidate.steps[1], candidate.steps[0]
			return candidate
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			candidate := mutate(clone(state))
			if err := validateMongoMigrationReadyStateWithHistory(candidate, manifestDigest, expectedHistoryDigest); err == nil || !strings.Contains(err.Error(), "history") {
				t.Fatalf("MongoDB readiness accepted %s artifact history: %v", name, err)
			}
		})
	}
}

func mustMongoMigrationRaw(t *testing.T, document bson.D) bson.Raw {
	t.Helper()
	encoded, err := bson.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return bson.Raw(encoded)
}
