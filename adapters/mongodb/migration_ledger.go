package mongodb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

const (
	mongoMigrationArtifactCollectionName = "z_ridu_migrations"
	mongoMigrationStepCollectionName     = "z_ridu_migration_steps"
	mongoMigrationLeaseCollectionName    = "z_ridu_migration_lock"
	mongoMigrationLedgerCodecVersion     = int32(1)
	mongoMigrationStepRunning            = "running"
	mongoMigrationStepComplete           = "complete"
)

var (
	mongoMigrationDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	mongoMigrationIDPattern     = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
)

// MigrationStatus describes one immutable MongoDB artifact relative to the
// database ledger.
type MigrationStatus struct {
	Name     string                 `json:"name"`
	Checksum string                 `json:"checksum"`
	Version  uint32                 `json:"version"`
	Applied  bool                   `json:"applied"`
	Phases   []MigrationPhaseStatus `json:"phases,omitempty"`
}

// MigrationPhaseStatus describes one immutable execution boundary.
type MigrationPhaseStatus struct {
	ID    string                  `json:"id"`
	Mode  ridumigration.PhaseMode `json:"mode"`
	State string                  `json:"state"`
	Steps []MigrationStepStatus   `json:"steps"`
}

// MigrationStepStatus describes one durable MongoDB executor invocation.
type MigrationStepStatus struct {
	ID    string                 `json:"id"`
	Kind  ridumigration.StepKind `json:"kind"`
	State string                 `json:"state"`
}

type mongoMigrationArtifactLedgerRow struct {
	Position               int
	Name                   string
	Digest                 string
	PreviousArtifactDigest string
	FromDigest             string
	ToDigest               string
	PlannerName            string
	PlannerVersion         string
	StepCount              int
	AppliedAt              time.Time
}

type mongoMigrationStepLedgerRow struct {
	ID             string
	ArtifactName   string
	ArtifactDigest string
	PhaseID        string
	StepID         string
	PhaseMode      ridumigration.PhaseMode
	StepKind       ridumigration.StepKind
	State          string
	Attempts       int
	Owner          string
	Fence          string
	UpdatedAt      time.Time
	CompletedAt    *time.Time
}

type mongoMigrationLedgerState struct {
	artifacts []mongoMigrationArtifactLedgerRow
	steps     []mongoMigrationStepLedgerRow
	stepByID  map[string]mongoMigrationStepLedgerRow
}

func (backend *Store) mongoMigrationCollection(name string) *mongo.Collection {
	return backend.database.Collection(
		name,
		options.Collection().
			SetReadConcern(readconcern.Majority()).
			SetReadPreference(readpref.Primary()).
			SetWriteConcern(writeconcern.Majority()),
	)
}

func encodeMongoMigrationArtifactLedger(row mongoMigrationArtifactLedgerRow) (bson.D, error) {
	if err := validateMongoMigrationArtifactLedgerRow(row); err != nil {
		return nil, err
	}
	appliedAt, err := encodeTime(row.AppliedAt)
	if err != nil {
		return nil, fmt.Errorf("encode MongoDB migration appliedAt: %w", err)
	}
	return bson.D{
		{Key: "_id", Value: row.Name},
		{Key: "codec", Value: mongoMigrationLedgerCodecVersion},
		{Key: "position", Value: int64(row.Position)},
		{Key: "name", Value: row.Name},
		{Key: "artifactDigest", Value: row.Digest},
		{Key: "previousArtifactDigest", Value: row.PreviousArtifactDigest},
		{Key: "fromDigest", Value: row.FromDigest},
		{Key: "toDigest", Value: row.ToDigest},
		{Key: "plannerName", Value: row.PlannerName},
		{Key: "plannerVersion", Value: row.PlannerVersion},
		{Key: "stepCount", Value: int64(row.StepCount)},
		{Key: "appliedAt", Value: appliedAt},
	}, nil
}

func decodeMongoMigrationArtifactLedger(raw bson.Raw) (mongoMigrationArtifactLedgerRow, error) {
	if err := requireExactKeys(
		raw,
		"MongoDB migration artifact ledger",
		"_id", "codec", "position", "name", "artifactDigest", "previousArtifactDigest",
		"fromDigest", "toDigest", "plannerName", "plannerVersion", "stepCount", "appliedAt",
	); err != nil {
		return mongoMigrationArtifactLedgerRow{}, err
	}
	codec, ok := raw.Lookup("codec").Int32OK()
	if !ok || codec != mongoMigrationLedgerCodecVersion {
		return mongoMigrationArtifactLedgerRow{}, fmt.Errorf("stored MongoDB migration artifact ledger has an unsupported codec version")
	}
	id, idOK := raw.Lookup("_id").StringValueOK()
	name, nameOK := raw.Lookup("name").StringValueOK()
	digest, digestOK := raw.Lookup("artifactDigest").StringValueOK()
	previous, previousOK := raw.Lookup("previousArtifactDigest").StringValueOK()
	from, fromOK := raw.Lookup("fromDigest").StringValueOK()
	to, toOK := raw.Lookup("toDigest").StringValueOK()
	plannerName, plannerNameOK := raw.Lookup("plannerName").StringValueOK()
	plannerVersion, plannerVersionOK := raw.Lookup("plannerVersion").StringValueOK()
	position, positionOK := raw.Lookup("position").Int64OK()
	stepCount, stepCountOK := raw.Lookup("stepCount").Int64OK()
	appliedAt, appliedAtOK := raw.Lookup("appliedAt").Int64OK()
	if !idOK || !nameOK || id != name || !digestOK || !previousOK || !fromOK || !toOK ||
		!plannerNameOK || !plannerVersionOK || !positionOK || position > math.MaxInt ||
		!stepCountOK || stepCount > math.MaxInt || !appliedAtOK {
		return mongoMigrationArtifactLedgerRow{}, fmt.Errorf("stored MongoDB migration artifact ledger has invalid field types or identity")
	}
	row := mongoMigrationArtifactLedgerRow{
		Position: int(position), Name: name, Digest: digest, PreviousArtifactDigest: previous,
		FromDigest: from, ToDigest: to, PlannerName: plannerName, PlannerVersion: plannerVersion,
		StepCount: int(stepCount), AppliedAt: decodeTime(appliedAt),
	}
	if err := validateMongoMigrationArtifactLedgerRow(row); err != nil {
		return mongoMigrationArtifactLedgerRow{}, fmt.Errorf("stored MongoDB migration artifact ledger: %w", err)
	}
	return row, nil
}

func validateMongoMigrationArtifactLedgerRow(row mongoMigrationArtifactLedgerRow) error {
	if row.Position <= 0 {
		return fmt.Errorf("position must be positive")
	}
	if !validMongoMigrationText(row.Name, 255) {
		return fmt.Errorf("name is invalid")
	}
	if !validMongoMigrationDigest(row.Digest, false) || !validMongoMigrationDigest(row.PreviousArtifactDigest, true) ||
		!validMongoMigrationDigest(row.FromDigest, true) || !validMongoMigrationDigest(row.ToDigest, false) {
		return fmt.Errorf("digest lineage is invalid")
	}
	if row.PlannerName != mongoDBPlannerName {
		return fmt.Errorf("planner name %q is not %q", row.PlannerName, mongoDBPlannerName)
	}
	if _, supported := mongoDBPlannerContractFor(row.PlannerVersion); !supported {
		return fmt.Errorf("planner version %q is unsupported", row.PlannerVersion)
	}
	if row.StepCount <= 0 {
		return fmt.Errorf("step count must be positive")
	}
	if row.AppliedAt.IsZero() {
		return fmt.Errorf("appliedAt is required")
	}
	return nil
}

func mongoMigrationStepLedgerID(artifactName, phaseID, stepID string) string {
	digest := sha256.Sum256([]byte("ridu-mongodb-migration-step-v1\x00" + artifactName + "\x00" + phaseID + "\x00" + stepID))
	return "step_" + hex.EncodeToString(digest[:])
}

func encodeMongoMigrationStepLedger(row mongoMigrationStepLedgerRow) (bson.D, error) {
	if row.ID == "" {
		row.ID = mongoMigrationStepLedgerID(row.ArtifactName, row.PhaseID, row.StepID)
	}
	if err := validateMongoMigrationStepLedgerRow(row); err != nil {
		return nil, err
	}
	updatedAt, err := encodeTime(row.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("encode MongoDB migration step updatedAt: %w", err)
	}
	var completedAt any
	if row.CompletedAt != nil {
		encoded, encodeErr := encodeTime(*row.CompletedAt)
		if encodeErr != nil {
			return nil, fmt.Errorf("encode MongoDB migration step completedAt: %w", encodeErr)
		}
		completedAt = encoded
	}
	return bson.D{
		{Key: "_id", Value: row.ID},
		{Key: "codec", Value: mongoMigrationLedgerCodecVersion},
		{Key: "artifactName", Value: row.ArtifactName},
		{Key: "artifactDigest", Value: row.ArtifactDigest},
		{Key: "phaseID", Value: row.PhaseID},
		{Key: "stepID", Value: row.StepID},
		{Key: "phaseMode", Value: string(row.PhaseMode)},
		{Key: "stepKind", Value: string(row.StepKind)},
		{Key: "state", Value: row.State},
		{Key: "attempts", Value: int64(row.Attempts)},
		{Key: "owner", Value: row.Owner},
		{Key: "fence", Value: row.Fence},
		{Key: "updatedAt", Value: updatedAt},
		{Key: "completedAt", Value: completedAt},
	}, nil
}

func decodeMongoMigrationStepLedger(raw bson.Raw) (mongoMigrationStepLedgerRow, error) {
	if err := requireExactKeys(
		raw,
		"MongoDB migration step ledger",
		"_id", "codec", "artifactName", "artifactDigest", "phaseID", "stepID", "phaseMode",
		"stepKind", "state", "attempts", "owner", "fence", "updatedAt", "completedAt",
	); err != nil {
		return mongoMigrationStepLedgerRow{}, err
	}
	codec, ok := raw.Lookup("codec").Int32OK()
	if !ok || codec != mongoMigrationLedgerCodecVersion {
		return mongoMigrationStepLedgerRow{}, fmt.Errorf("stored MongoDB migration step ledger has an unsupported codec version")
	}
	id, idOK := raw.Lookup("_id").StringValueOK()
	artifactName, artifactNameOK := raw.Lookup("artifactName").StringValueOK()
	artifactDigest, artifactDigestOK := raw.Lookup("artifactDigest").StringValueOK()
	phaseID, phaseIDOK := raw.Lookup("phaseID").StringValueOK()
	stepID, stepIDOK := raw.Lookup("stepID").StringValueOK()
	phaseMode, phaseModeOK := raw.Lookup("phaseMode").StringValueOK()
	stepKind, stepKindOK := raw.Lookup("stepKind").StringValueOK()
	state, stateOK := raw.Lookup("state").StringValueOK()
	attempts, attemptsOK := raw.Lookup("attempts").Int64OK()
	owner, ownerOK := raw.Lookup("owner").StringValueOK()
	fence, fenceOK := raw.Lookup("fence").StringValueOK()
	updatedAt, updatedAtOK := raw.Lookup("updatedAt").Int64OK()
	if !idOK || !artifactNameOK || !artifactDigestOK || !phaseIDOK || !stepIDOK || !phaseModeOK ||
		!stepKindOK || !stateOK || !attemptsOK || attempts > math.MaxInt || !ownerOK || !fenceOK || !updatedAtOK {
		return mongoMigrationStepLedgerRow{}, fmt.Errorf("stored MongoDB migration step ledger has invalid field types")
	}
	var completedAt *time.Time
	completedValue := raw.Lookup("completedAt")
	if completedValue.Type != bson.TypeNull {
		encoded, completedOK := completedValue.Int64OK()
		if !completedOK {
			return mongoMigrationStepLedgerRow{}, fmt.Errorf("stored MongoDB migration step ledger has invalid completedAt")
		}
		decoded := decodeTime(encoded)
		completedAt = &decoded
	}
	row := mongoMigrationStepLedgerRow{
		ID: id, ArtifactName: artifactName, ArtifactDigest: artifactDigest, PhaseID: phaseID, StepID: stepID,
		PhaseMode: ridumigration.PhaseMode(phaseMode), StepKind: ridumigration.StepKind(stepKind), State: state,
		Attempts: int(attempts), Owner: owner, Fence: fence, UpdatedAt: decodeTime(updatedAt), CompletedAt: completedAt,
	}
	if err := validateMongoMigrationStepLedgerRow(row); err != nil {
		return mongoMigrationStepLedgerRow{}, fmt.Errorf("stored MongoDB migration step ledger: %w", err)
	}
	return row, nil
}

func validateMongoMigrationStepLedgerRow(row mongoMigrationStepLedgerRow) error {
	if !validMongoMigrationText(row.ArtifactName, 255) || !validMongoMigrationDigest(row.ArtifactDigest, false) {
		return fmt.Errorf("artifact identity is invalid")
	}
	if !mongoMigrationIDPattern.MatchString(row.PhaseID) || !mongoMigrationIDPattern.MatchString(row.StepID) {
		return fmt.Errorf("phase or step identity is invalid")
	}
	if row.ID != mongoMigrationStepLedgerID(row.ArtifactName, row.PhaseID, row.StepID) {
		return fmt.Errorf("ID does not match artifact/phase/step identity")
	}
	if row.PhaseMode != ridumigration.PhaseTransaction && row.PhaseMode != ridumigration.PhaseBatch && row.PhaseMode != ridumigration.PhaseNoTransaction {
		return fmt.Errorf("phase mode %q is invalid", row.PhaseMode)
	}
	if row.StepKind == "" {
		return fmt.Errorf("step kind is required")
	}
	if row.State != mongoMigrationStepRunning && row.State != mongoMigrationStepComplete {
		return fmt.Errorf("state %q is invalid", row.State)
	}
	if row.Attempts <= 0 || !validMongoMigrationToken(row.Owner) || !validMongoMigrationToken(row.Fence) || row.UpdatedAt.IsZero() {
		return fmt.Errorf("attempt, owner, fence, or updatedAt is invalid")
	}
	if row.State == mongoMigrationStepRunning && row.CompletedAt != nil {
		return fmt.Errorf("running step has completedAt")
	}
	if row.State == mongoMigrationStepComplete && (row.CompletedAt == nil || row.CompletedAt.IsZero()) {
		return fmt.Errorf("complete step has no completedAt")
	}
	return nil
}

func validMongoMigrationDigest(value string, allowEmpty bool) bool {
	return allowEmpty && value == "" || mongoMigrationDigestPattern.MatchString(value)
}

func validMongoMigrationText(value string, maximum int) bool {
	return value != "" && len(value) <= maximum && utf8.ValidString(value) && !strings.ContainsRune(value, '\x00') && strings.TrimSpace(value) == value
}

func validMongoMigrationToken(value string) bool {
	if len(value) != 32 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}

func (backend *Store) readMongoMigrationLedgerState(ctx context.Context) (mongoMigrationLedgerState, error) {
	artifacts, err := backend.readMongoMigrationArtifactLedger(ctx)
	if err != nil {
		return mongoMigrationLedgerState{}, err
	}
	steps, err := backend.readMongoMigrationStepLedger(ctx)
	if err != nil {
		return mongoMigrationLedgerState{}, err
	}
	state := mongoMigrationLedgerState{artifacts: artifacts, steps: steps, stepByID: make(map[string]mongoMigrationStepLedgerRow, len(steps))}
	for _, row := range steps {
		if _, duplicate := state.stepByID[row.ID]; duplicate {
			return mongoMigrationLedgerState{}, fmt.Errorf("MongoDB migration step ledger repeats identity %s", row.ID)
		}
		state.stepByID[row.ID] = row
	}
	if err := validateMongoMigrationArtifactLedgerContinuity(artifacts); err != nil {
		return mongoMigrationLedgerState{}, err
	}
	return state, nil
}

func (backend *Store) readMongoMigrationArtifactLedger(ctx context.Context) ([]mongoMigrationArtifactLedgerRow, error) {
	cursor, err := backend.mongoMigrationCollection(mongoMigrationArtifactCollectionName).Find(
		ctx,
		bson.D{},
		options.Find().SetSort(bson.D{{Key: "position", Value: int32(1)}, {Key: "_id", Value: int32(1)}}),
	)
	if err != nil {
		return nil, fmt.Errorf("read MongoDB migration artifact ledger: %w", translateMongoError(ctx, err))
	}
	defer func() {
		closeContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultCloseTimeout)
		defer cancel()
		_ = cursor.Close(closeContext)
	}()
	var rows []mongoMigrationArtifactLedgerRow
	for cursor.Next(ctx) {
		row, decodeErr := decodeMongoMigrationArtifactLedger(cursor.Current)
		if decodeErr != nil {
			return nil, decodeErr
		}
		rows = append(rows, row)
	}
	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("read MongoDB migration artifact ledger: %w", translateMongoError(ctx, err))
	}
	return rows, nil
}

func (backend *Store) readMongoMigrationStepLedger(ctx context.Context) ([]mongoMigrationStepLedgerRow, error) {
	cursor, err := backend.mongoMigrationCollection(mongoMigrationStepCollectionName).Find(
		ctx,
		bson.D{},
		options.Find().SetSort(bson.D{{Key: "artifactName", Value: int32(1)}, {Key: "phaseID", Value: int32(1)}, {Key: "stepID", Value: int32(1)}}),
	)
	if err != nil {
		return nil, fmt.Errorf("read MongoDB migration step ledger: %w", translateMongoError(ctx, err))
	}
	defer func() {
		closeContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultCloseTimeout)
		defer cancel()
		_ = cursor.Close(closeContext)
	}()
	var rows []mongoMigrationStepLedgerRow
	for cursor.Next(ctx) {
		row, decodeErr := decodeMongoMigrationStepLedger(cursor.Current)
		if decodeErr != nil {
			return nil, decodeErr
		}
		rows = append(rows, row)
	}
	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("read MongoDB migration step ledger: %w", translateMongoError(ctx, err))
	}
	return rows, nil
}

func validateMongoMigrationArtifactLedgerContinuity(rows []mongoMigrationArtifactLedgerRow) error {
	for index, row := range rows {
		if row.Position != index+1 {
			return fmt.Errorf("MongoDB migration artifact ledger position %d is not continuous after %d", row.Position, index)
		}
		if index == 0 {
			if row.PreviousArtifactDigest != "" || row.FromDigest != "" {
				return fmt.Errorf("MongoDB migration artifact ledger does not start from empty history")
			}
			continue
		}
		previous := rows[index-1]
		if row.Name <= previous.Name {
			return fmt.Errorf("MongoDB migration artifact ledger is reordered at %s after %s", row.Name, previous.Name)
		}
		if row.PreviousArtifactDigest != previous.Digest {
			return fmt.Errorf("MongoDB migration artifact ledger predecessor is discontinuous between %s and %s", previous.Name, row.Name)
		}
		if row.FromDigest != previous.ToDigest {
			return fmt.Errorf("MongoDB migration artifact ledger manifest lineage is discontinuous between %s and %s", previous.Name, row.Name)
		}
	}
	return nil
}

func validateMongoMigrationLedgerAgainstFiles(files []migrationartifact.File, state mongoMigrationLedgerState) error {
	if len(state.artifacts) > len(files) {
		return fmt.Errorf("database contains %d applied MongoDB migrations but the directory contains only %d", len(state.artifacts), len(files))
	}
	expectedSteps := make(map[string]mongoMigrationStepLedgerRow)
	orderedSteps := make(map[string][]string)
	for _, file := range files {
		for _, phase := range file.Artifact.Phases {
			for _, step := range phase.Steps {
				id := mongoMigrationStepLedgerID(file.Name, phase.ID, step.ID)
				expectedSteps[id] = mongoMigrationStepLedgerRow{
					ID: id, ArtifactName: file.Name, ArtifactDigest: file.Digest,
					PhaseID: phase.ID, StepID: step.ID, PhaseMode: phase.Mode, StepKind: step.Kind,
				}
				orderedSteps[file.Name] = append(orderedSteps[file.Name], id)
			}
		}
	}
	for index, row := range state.artifacts {
		file := files[index]
		if row.Name != file.Name {
			return fmt.Errorf("MongoDB migration history diverged at %s; database records %s", file.Name, row.Name)
		}
		if row.Digest != file.Digest {
			return fmt.Errorf("MongoDB migration %s changed after application", file.Name)
		}
		if row.PreviousArtifactDigest != file.Artifact.PreviousArtifactDigest {
			return fmt.Errorf("MongoDB migration %s predecessor differs from the database ledger", file.Name)
		}
		if row.FromDigest != file.Artifact.FromDigest || row.ToDigest != file.Artifact.ToDigest {
			return fmt.Errorf("MongoDB migration %s manifest lineage differs from the database ledger", file.Name)
		}
		if row.PlannerName != file.Artifact.Planner.Name || row.PlannerVersion != file.Artifact.Planner.Version {
			return fmt.Errorf("MongoDB migration %s planner provenance differs from the database ledger", file.Name)
		}
		if row.StepCount != len(orderedSteps[file.Name]) {
			return fmt.Errorf("MongoDB migration %s step count differs from the database ledger", file.Name)
		}
	}
	for _, row := range state.steps {
		expected, exists := expectedSteps[row.ID]
		if !exists {
			return fmt.Errorf("MongoDB migration step ledger contains uncommitted identity %s/%s/%s", row.ArtifactName, row.PhaseID, row.StepID)
		}
		if row.ArtifactName != expected.ArtifactName || row.ArtifactDigest != expected.ArtifactDigest ||
			row.PhaseID != expected.PhaseID || row.StepID != expected.StepID || row.PhaseMode != expected.PhaseMode || row.StepKind != expected.StepKind {
			return fmt.Errorf("MongoDB migration step ledger identity differs for %s/%s/%s", expected.ArtifactName, expected.PhaseID, expected.StepID)
		}
	}
	for fileIndex, file := range files {
		ids := orderedSteps[file.Name]
		rows := make([]mongoMigrationStepLedgerRow, 0, len(ids))
		for _, id := range ids {
			if row, exists := state.stepByID[id]; exists {
				rows = append(rows, row)
			} else {
				rows = append(rows, mongoMigrationStepLedgerRow{})
			}
		}
		if fileIndex < len(state.artifacts) {
			for index, row := range rows {
				if row.State != mongoMigrationStepComplete {
					return fmt.Errorf("MongoDB migration %s is complete but step %s is not complete", file.Name, ids[index])
				}
			}
			continue
		}
		if fileIndex > len(state.artifacts) {
			for _, row := range rows {
				if row.State != "" {
					return fmt.Errorf("MongoDB migration %s has step state before earlier history completed", file.Name)
				}
			}
			continue
		}
		missing := false
		running := false
		for index, row := range rows {
			if row.State == "" {
				missing = true
				continue
			}
			if missing {
				return fmt.Errorf("MongoDB migration %s step ledger has a gap before %s", file.Name, ids[index])
			}
			if running {
				return fmt.Errorf("MongoDB migration %s has step state after its running boundary", file.Name)
			}
			if row.State == mongoMigrationStepRunning {
				running = true
			}
		}
	}
	return nil
}

func mongoMigrationStatuses(files []migrationartifact.File, state mongoMigrationLedgerState) []MigrationStatus {
	statuses := make([]MigrationStatus, len(files))
	for fileIndex, file := range files {
		status := MigrationStatus{Name: file.Name, Checksum: file.Digest, Version: file.Artifact.Version, Applied: fileIndex < len(state.artifacts)}
		for _, phase := range file.Artifact.Phases {
			phaseStatus := MigrationPhaseStatus{ID: phase.ID, Mode: phase.Mode, State: "pending"}
			completed := 0
			running := false
			for _, step := range phase.Steps {
				row := state.stepByID[mongoMigrationStepLedgerID(file.Name, phase.ID, step.ID)]
				stepState := row.State
				if stepState == "" {
					stepState = "pending"
				}
				if stepState == mongoMigrationStepComplete {
					completed++
				}
				if stepState == mongoMigrationStepRunning {
					running = true
				}
				phaseStatus.Steps = append(phaseStatus.Steps, MigrationStepStatus{ID: step.ID, Kind: step.Kind, State: stepState})
			}
			if completed == len(phase.Steps) {
				phaseStatus.State = mongoMigrationStepComplete
			} else if completed != 0 || running {
				phaseStatus.State = mongoMigrationStepRunning
			}
			status.Phases = append(status.Phases, phaseStatus)
		}
		statuses[fileIndex] = status
	}
	return statuses
}

func countMongoMigrationArtifactSteps(artifact ridumigration.Artifact) int {
	count := 0
	for _, phase := range artifact.Phases {
		count += len(phase.Steps)
	}
	return count
}

func mongoMigrationFilesByName(files []migrationartifact.File) map[string]migrationartifact.File {
	result := make(map[string]migrationartifact.File, len(files))
	for _, file := range files {
		result[file.Name] = file
	}
	return result
}

func sortMongoMigrationStepRows(rows []mongoMigrationStepLedgerRow) {
	sort.Slice(rows, func(left, right int) bool {
		if rows[left].ArtifactName != rows[right].ArtifactName {
			return rows[left].ArtifactName < rows[right].ArtifactName
		}
		if rows[left].PhaseID != rows[right].PhaseID {
			return rows[left].PhaseID < rows[right].PhaseID
		}
		return rows[left].StepID < rows[right].StepID
	})
}
