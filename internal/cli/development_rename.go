package cli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/riducms/ridu/adapters/mongodb"
	"github.com/riducms/ridu/adapters/postgres"
	"github.com/riducms/ridu/adapters/sqlite"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/projectfile"
	"github.com/riducms/ridu/internal/schemadiff"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

// developmentRenames settles a rename `ridu dev` detects in a saved config.
// With a terminal it asks and can apply the rename itself. Without one it
// rejects every reload until the rename is resolved some other way: the old
// name restored, or the rename migrated with ridu migrate.
type developmentRenames struct {
	// input is nil when ridu dev cannot ask: without a terminal it must never
	// block on input that cannot arrive, and without a migrations directory
	// there is nowhere to record the decision.
	input        *bufio.Reader
	prompt       io.Writer
	databaseURL  string
	databasePath string
	output       *cliOutput
	now          func() time.Time
	// stopServer stops the development server that is still running the
	// previous config. Every adapter stores some content under field names, so
	// a write from that server during or after the rename would put content
	// back under the old name. It is nil until a server is running.
	stopServer func()
	// declined is the config whose renames the developer declined during the
	// current reload. One reload prepares the config more than once when
	// generated Go changes, and must not ask again each time.
	declined *schema.Manifest
	// stillHas replaces the database check in tests that have no database.
	stillHas func(ctx context.Context, baseline, current schema.Manifest) (bool, error)
}

// beginReload forgets the answers given during the previous reload, so a
// declined rename that still blocks schema sync is asked about again on the
// next save.
func (renames *developmentRenames) beginReload() {
	renames.declined = nil
}

func newDevelopmentRenames(definition projectfile.File, options Options, databaseURL, databasePath string, prompt io.Writer, output *cliOutput) *developmentRenames {
	renames := &developmentRenames{prompt: prompt, databaseURL: databaseURL, databasePath: databasePath, output: output, now: time.Now}
	if options.Interactive && options.Stdin != nil && definition.Migrations != "" {
		renames.input = bufio.NewReader(options.Stdin)
	}
	return renames
}

// developmentRenameHeldError carries the message for a rename that waits for
// the developer and that ridu dev could not ask about.
type developmentRenameHeldError struct{ err error }

func (held developmentRenameHeldError) Error() string { return held.err.Error() }
func (held developmentRenameHeldError) Unwrap() error { return held.err }

// baselinePath is where ridu dev records the schema it last brought this
// development database to. The generated schema file cannot serve: ridu
// generate rewrites it whenever the config changes, which would forget a
// rename the database has not been through.
func (renames *developmentRenames) baselinePath(definition projectfile.File) string {
	identity := sha256.Sum256([]byte(string(definition.Database) + "\x00" + renames.databaseURL + "\x00" + renames.databasePath))
	return definition.Absolute(filepath.Join(".ridu", "development", hex.EncodeToString(identity[:8])+".schema.json"))
}

// baseline returns the schema the development database is believed to have:
// the one ridu dev last synchronized it to or, before any was recorded, the
// generated schema file.
func (renames *developmentRenames) baseline(definition projectfile.File) (schema.Manifest, bool) {
	if manifest, exists, err := schemadiff.ReadManifest(renames.baselinePath(definition)); err == nil && exists {
		return manifest, true
	}
	manifest, exists, err := schemadiff.ReadManifest(definition.Absolute(definition.Schema))
	return manifest, err == nil && exists
}

// recordSynchronized notes that the development database now has manifest.
func (renames *developmentRenames) recordSynchronized(definition projectfile.File, manifest schema.Manifest) error {
	encoded, err := manifest.Bytes()
	if err != nil {
		return err
	}
	path := renames.baselinePath(definition)
	if existing, err := os.ReadFile(path); err == nil && bytes.Equal(existing, encoded) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(encoded); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), path)
}

// waiting reports whether the development database still has baseline's
// schema, so a rename from it still waits for a decision. A database that
// moved on some other way, such as ridu migrate up or a new empty file, waits
// for nothing.
func (renames *developmentRenames) waiting(ctx context.Context, definition projectfile.File, baseline, current schema.Manifest) (bool, error) {
	if renames.stillHas != nil {
		return renames.stillHas(ctx, baseline, current)
	}
	target, err := renames.openTarget(ctx, definition.Database)
	if err != nil {
		return false, fmt.Errorf("open the development database: %w", err)
	}
	defer target.close()
	directory := ""
	if definition.Migrations != "" {
		directory = definition.Absolute(definition.Migrations)
	}
	has, err := target.has(ctx, directory, baseline, current)
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	return has, err
}

// resolve settles every rename the new config suggests against the schema the
// development database has. It reports true when it asked and ordinary schema
// sync may continue: either each rename was applied through a migration this
// call created, or the developer declined them all. It reports false when
// nothing waits for a decision. fresh reports whether the config the
// questions were about is still the newest one; an answer to a config that
// has since changed is discarded with errDevelopmentSourceChanged. Any other
// error rejects the reload before generation, and the same question comes up
// again on the next save.
func (renames *developmentRenames) resolve(ctx context.Context, definition projectfile.File, current schema.Manifest, fresh func() bool) (bool, error) {
	previous, exists := renames.baseline(definition)
	if !exists {
		return false, nil
	}
	candidates := schemadiff.RenameCandidates(previous, current)
	if len(candidates) == 0 {
		return false, nil
	}
	if renames.declined != nil && renames.declined.Equal(current) {
		return true, nil
	}
	waiting, err := renames.waiting(ctx, definition, previous, current)
	if err != nil || !waiting {
		return false, err
	}
	if renames.input == nil {
		for _, candidate := range candidates {
			renames.output.Warn("possible "+renameDescription(candidate), nil)
		}
		return false, developmentRenameHeldError{developmentSchemaWarningError(definition.Database)}
	}
	if definition.Database == projectfile.DatabaseSQLite {
		for _, candidate := range candidates {
			if candidate.Kind == schemadiff.RenameCollection {
				return false, fmt.Errorf("SQLite cannot rename collection %q to %q in place; keep the old slug, or move its documents with a registered data transform", candidate.BeforeCollection.Slug, candidate.AfterCollection.Slug)
			}
		}
	}
	var accepted []schemadiff.RenameCandidate
	for _, candidate := range candidates {
		preserve, err := renames.confirm(ctx, fmt.Sprintf("Detected %s. Preserve its existing data as a rename? [y/n] ", renameDescription(candidate)))
		if err != nil {
			return false, err
		}
		if preserve {
			accepted = append(accepted, candidate)
		}
	}
	name := ""
	if len(accepted) == len(candidates) {
		if name, err = renames.migrationName(ctx, developmentRenameName(accepted)); err != nil {
			return false, err
		}
	}
	if fresh != nil && !fresh() {
		// The config changed while the question waited, so the answers describe
		// a change that may no longer exist.
		return false, errDevelopmentSourceChanged
	}
	if len(accepted) == 0 {
		renames.output.Info(developmentRenameDeclined(definition.Database))
		renames.declined = &current
		return true, nil
	}
	if len(accepted) != len(candidates) {
		// The migration would drop the declined fields' data, which is a
		// reviewed destructive change, not a development prompt.
		if definition.Database == projectfile.DatabaseSQLite {
			return false, fmt.Errorf("some renames were declined, so their old values would be dropped in the same migration; on SQLite that needs a registered data transform, or make the changes in separate saves")
		}
		return false, fmt.Errorf("some renames were declined, so their old values would be dropped in the same migration; review that with ridu migrate create --allow-destructive")
	}
	if err := renames.migrate(ctx, definition, previous, current, accepted, name); err != nil {
		return false, err
	}
	// The content is under the new names now, whatever happens to this reload.
	if err := renames.recordSynchronized(definition, current); err != nil {
		renames.output.Warn("record the renamed development schema", err)
	}
	return true, nil
}

// developmentRenameDeclined says what declining a rename leaves behind, which
// differs by how each adapter's development sync treats a removed field.
func developmentRenameDeclined(adapter projectfile.DatabaseAdapter) string {
	switch adapter {
	case projectfile.DatabasePostgres:
		return "Not a rename. PostgreSQL schema sync never drops a column, so it stays paused, and asks again on each save, until a reviewed migration removes the old one or the old name is restored"
	case projectfile.DatabaseMongoDB:
		return "Not a rename. Documents keep their old values under the old name; an index on the removed field needs a reviewed migration before schema sync continues"
	}
	return "Not a rename. Documents keep their old values under the old name, where the new config does not read them"
}

// confirm asks a yes-or-no question until it gets one of those answers. An
// empty line is not an answer: Enter pressed before the question appeared is
// still waiting in the terminal and must not move data.
func (renames *developmentRenames) confirm(ctx context.Context, question string) (bool, error) {
	for {
		answer, err := renames.ask(ctx, question)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(answer) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		fmt.Fprintf(renames.prompt, "Answer y to move the existing data to the new name, or n to leave it under the old one (%q is neither).\n", answer)
	}
}

// migrationName asks for the migration's name, offering a suggestion.
func (renames *developmentRenames) migrationName(ctx context.Context, suggested string) (string, error) {
	for {
		name, err := renames.ask(ctx, fmt.Sprintf("Migration name [%s]: ", suggested))
		if err != nil {
			return "", err
		}
		if name == "" {
			return suggested, nil
		}
		if migrationNamePattern.MatchString(name) {
			return name, nil
		}
		fmt.Fprintf(renames.prompt, "Name %q must be lowercase kebab-case, such as %s.\n", name, suggested)
	}
}

// ask reads one line, returning early when `ridu dev` is stopped.
func (renames *developmentRenames) ask(ctx context.Context, question string) (string, error) {
	fmt.Fprint(renames.prompt, question)
	type line struct {
		text string
		err  error
	}
	read := make(chan line, 1)
	go func() {
		text, err := renames.input.ReadString('\n')
		read <- line{text: text, err: err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case result := <-read:
		if result.err != nil && result.text == "" {
			return "", fmt.Errorf("read rename answer: %w", result.err)
		}
		return strings.TrimSpace(result.text), nil
	}
}

// migrate writes the rename as a migration and applies it to the development
// database, so development and production take the same path and the decision
// is made once. A database that ridu dev brought ahead of the committed
// history first gets a migration for those earlier changes, which it already
// has and baseline therefore records.
//
// Everything that can refuse the rename without touching stored content runs
// before the development server is stopped.
func (renames *developmentRenames) migrate(ctx context.Context, definition projectfile.File, previous, current schema.Manifest, accepted []schemadiff.RenameCandidate, name string) error {
	directory := definition.Absolute(definition.Migrations)
	detected := accepted
	var created []string
	// recorded says why the new files must stay once the database's ledger
	// refers to them.
	recorded := ""
	fail := func(step string, err error) error {
		return developmentRenameFailure(definition.Database, step, err, created, recorded)
	}

	history, err := migrationartifact.ReadAll(directory)
	if err != nil {
		return fail("read migration history", err)
	}
	target, err := renames.openTarget(ctx, definition.Database)
	if err != nil {
		return fail("open the development database", err)
	}
	defer target.close()
	if target.runner && mongoDBHistoryRequiresProjectDriver(history) {
		// Only the project binary holds the compiled transforms the runner
		// replays, so this process could write the migration but never apply it.
		return fail("plan rename", errors.New("this project's migrations run compiled data transforms, which ridu dev cannot replay; create the rename with ridu migrate create and apply it with ridu migrate up"))
	}

	create := func(name string, before *schema.Manifest, after schema.Manifest, candidates []schemadiff.RenameCandidate) error {
		path, err := renames.createMigration(ctx, definition.Database, directory, name, before, after, candidates)
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(definition.Root, path)
		created = append(created, filepath.ToSlash(relative))
		return nil
	}
	var base *schema.Manifest
	var head schema.Manifest
	headExists := len(history) != 0
	if headExists {
		if head, err = history[len(history)-1].Artifact.AfterManifest(); err != nil {
			return fail("read migration history", err)
		}
		base = &head
	}
	switch {
	case headExists && head.Equal(current):
		// An earlier attempt already wrote the migrations; only applying
		// remains. A migration that reaches this config some other way, such as
		// a reviewed removal, would not move the data the developer just asked
		// to keep.
		if err := developmentRenameRecorded(history[len(history)-1], accepted); err != nil {
			return fail("continue the earlier rename", err)
		}
	case !headExists || !head.Equal(previous):
		err := create("changes-before-"+name, base, previous, nil)
		switch {
		case err == nil:
			base = &previous
		case headExists && strings.Contains(err.Error(), "schema is current"):
			// Only presentation differs; the rename continues the head.
			accepted = matchingRenameCandidates(schemadiff.RenameCandidates(head, current), accepted)
			if len(accepted) == 0 {
				return fail("plan rename", errors.New("the rename no longer matches the committed migration history"))
			}
		default:
			return fail("plan a migration for the development changes no migration covers yet", err)
		}
		fallthrough
	default:
		if err := create(name, base, current, accepted); err != nil {
			return fail("plan rename migration", err)
		}
	}

	if target.runner {
		adopted, err := target.adopt(ctx, directory)
		if err != nil {
			return fail("record the migrations this database already has", err)
		}
		if len(adopted) != 0 {
			recorded = "the database now records " + strings.Join(adopted, " and ")
		}
		if err := target.refuseDestructivePending(ctx, directory, current); err != nil {
			return fail("apply rename migration", err)
		}
	}
	renames.stopRunningServer()
	if err := target.apply(ctx, directory, previous, current, contentRenames(detected)); err != nil {
		if target.mongodb != nil {
			// MongoDB records each finished step, so a failed run can leave the
			// ledger part way through the rename.
			recorded = "MongoDB may have recorded part of the rename"
		}
		return fail("apply rename", err)
	}
	if len(created) == 0 {
		renames.output.Info("Applied rename and preserved existing data")
	} else {
		renames.output.Info("Applied rename and preserved existing data", "migrations", strings.Join(created, ", "))
	}
	return nil
}

// developmentRenameFailure reports a failed rename with what it left behind
// and how to carry on. Files the database's ledger already refers to must
// stay, so deleting them is only offered while nothing was recorded.
func developmentRenameFailure(adapter projectfile.DatabaseAdapter, step string, err error, created []string, recorded string) error {
	switch {
	case recorded != "":
		return fmt.Errorf("%s: %w; %s, so keep the migration files and save again to retry, or %s", step, err, recorded, developmentRenameManualFinish(adapter))
	case len(created) != 0:
		return fmt.Errorf("%s: %w; created %s, so save again to retry, %s, or delete the new files to start over", step, err, strings.Join(created, " and "), developmentRenameManualFinish(adapter))
	}
	return fmt.Errorf("%s: %w", step, err)
}

// createMigration writes one migration continuing the directory's history and
// returns its path. candidates are the renames it preserves, if any.
func (renames *developmentRenames) createMigration(ctx context.Context, adapter projectfile.DatabaseAdapter, directory, name string, before *schema.Manifest, after schema.Manifest, candidates []schemadiff.RenameCandidate) (string, error) {
	switch adapter {
	case projectfile.DatabaseSQLite:
		if len(candidates) == 0 {
			created, err := sqlite.CreateArtifact(ctx, directory, name, after, renames.now(), false)
			return created.Path, err
		}
		created, err := sqlite.CreateArtifactWithRenames(ctx, directory, name, after, renames.now(), contentRenames(candidates))
		return created.Path, err
	case projectfile.DatabaseMongoDB:
		created, err := mongodb.CreateArtifactWithOptions(ctx, directory, name, after, renames.now(), mongodb.ArtifactOptions{Renames: contentRenames(candidates)})
		return created.Path, err
	}
	plannerVersion, err := migrationHeadPlannerVersion(directory)
	if err != nil {
		return "", err
	}
	artifact, err := buildPostgresArtifactWithDataTransforms(ctx, name, before, after, postgresRenames(candidates), false, plannerVersion, nil)
	if err != nil {
		return "", err
	}
	file, err := migrationartifact.Create(directory, name, artifact, renames.now())
	return file.Path, err
}

// developmentRenameRecorded checks that a migration records exactly the
// renames the developer accepted.
func developmentRenameRecorded(file migrationartifact.File, accepted []schemadiff.RenameCandidate) error {
	describe := func(rename migration.Rename) string {
		return fmt.Sprintf("%s.%s -> %s.%s", rename.CollectionBefore, rename.FieldBefore, rename.CollectionAfter, rename.FieldAfter)
	}
	recorded := make(map[string]bool)
	for _, phase := range file.Artifact.Phases {
		for _, step := range phase.Steps {
			if step.Kind != migration.StepRenameContent {
				continue
			}
			var payload migration.RenamePayload
			if err := json.Unmarshal(step.Payload, &payload); err != nil {
				return fmt.Errorf("decode migration %s: %w", file.Name, err)
			}
			recorded[describe(payload.Rename)] = true
		}
	}
	wanted := contentRenames(accepted)
	matches := len(recorded) == len(wanted)
	for _, rename := range wanted {
		matches = matches && recorded[describe(rename)]
	}
	if !matches {
		return fmt.Errorf("migration %s already reaches this config without recording these renames, so applying it would not move the data; answer n, or replace that migration", file.Name)
	}
	return nil
}

// developmentRenameTarget is the development database a rename is applied to.
type developmentRenameTarget struct {
	postgres *postgres.Store
	mongodb  *mongodb.Store
	sqlite   *sqlite.Store
	// runner is false only for a SQLite database without migration history.
	// ridu dev keeps synchronizing that database, so its content moves directly
	// and schema sync rebuilds the rest; applying the migration there would
	// hand the database to ridu migrate.
	runner bool
}

func (renames *developmentRenames) openTarget(ctx context.Context, adapter projectfile.DatabaseAdapter) (*developmentRenameTarget, error) {
	switch adapter {
	case projectfile.DatabaseSQLite:
		backend, err := sqlite.Open(ctx, renames.databasePath)
		if err != nil {
			return nil, err
		}
		managed, err := backend.HasMigrationHistory(ctx)
		if err != nil {
			_ = backend.Close()
			return nil, fmt.Errorf("inspect SQLite migration history: %w", err)
		}
		return &developmentRenameTarget{sqlite: backend, runner: managed}, nil
	case projectfile.DatabaseMongoDB:
		backend, err := openDevelopmentMongoDB(ctx, renames.databaseURL)
		if err != nil {
			return nil, err
		}
		return &developmentRenameTarget{mongodb: backend, runner: true}, nil
	}
	backend, err := openDevelopmentPostgres(ctx, renames.databaseURL)
	if err != nil {
		return nil, err
	}
	return &developmentRenameTarget{postgres: backend, runner: true}, nil
}

func (target *developmentRenameTarget) close() {
	switch {
	case target.sqlite != nil:
		_ = target.sqlite.Close()
	case target.mongodb != nil:
		_ = target.mongodb.Close()
	default:
		target.postgres.Close()
	}
}

// has reports whether the database still has baseline's schema. directory is
// the project's migrations directory, or empty when it has none.
func (target *developmentRenameTarget) has(ctx context.Context, directory string, baseline, current schema.Manifest) (bool, error) {
	switch {
	case target.sqlite != nil:
		// SQLite records the exact manifest it was brought to.
		return target.sqlite.Ready(ctx, baseline) == nil, nil
	case target.mongodb != nil:
		if err := target.mongodb.VerifyIndexes(ctx, baseline); err != nil {
			return false, nil
		}
		// MongoDB keeps no schema beyond its indexes, and renaming an
		// unindexed field leaves those as they were. Only its ledger shows
		// that the migrations reaching the new config have run.
		return !target.migratedTo(ctx, directory, current), nil
	}
	plan, err := target.postgres.Plan(ctx, baseline)
	return err == nil && len(plan) == 0, nil
}

// migratedTo reports whether the committed migrations end at current and the
// MongoDB ledger records every one of them as applied.
func (target *developmentRenameTarget) migratedTo(ctx context.Context, directory string, current schema.Manifest) bool {
	if directory == "" {
		return false
	}
	head, exists, err := migrationartifact.LatestManifest(directory)
	if err != nil || !exists || !head.Equal(current) {
		return false
	}
	statuses, err := target.mongodb.ArtifactStatus(ctx, directory)
	if err != nil || len(statuses) == 0 {
		return false
	}
	for _, status := range statuses {
		if !status.Applied {
			return false
		}
	}
	return true
}

// adopt records the migrations the database already has. It changes the
// ledger only, so it runs while the development server is still up.
func (target *developmentRenameTarget) adopt(ctx context.Context, directory string) ([]string, error) {
	switch {
	case target.sqlite != nil:
		// A managed SQLite database is already in step with its ledger.
		return nil, nil
	case target.mongodb != nil:
		return target.mongodb.AdoptArtifacts(ctx, directory)
	}
	return target.postgres.AdoptArtifacts(ctx, directory)
}

// refuseDestructivePending stops the prompt from applying a pending migration
// that removes data, such as a teammate's reviewed removal that this database
// has not run yet. The migrations this flow writes never carry that risk.
func (target *developmentRenameTarget) refuseDestructivePending(ctx context.Context, directory string, current schema.Manifest) error {
	applied := make(map[string]bool)
	switch {
	case target.sqlite != nil:
		statuses, err := target.sqlite.ArtifactStatus(ctx, directory, current)
		if err != nil {
			return err
		}
		for _, status := range statuses {
			applied[status.Name] = status.Applied
		}
	case target.mongodb != nil:
		statuses, err := target.mongodb.ArtifactStatus(ctx, directory)
		if err != nil {
			return err
		}
		for _, status := range statuses {
			applied[status.Name] = status.Applied
		}
	default:
		statuses, err := target.postgres.ArtifactStatus(ctx, directory)
		if err != nil {
			return err
		}
		for _, status := range statuses {
			applied[status.Name] = status.Applied
		}
	}
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		return err
	}
	for _, file := range files {
		if applied[file.Name] {
			continue
		}
		for _, risk := range file.Artifact.Risks {
			if risk.Level == migration.RiskDestructive {
				return fmt.Errorf("migration %s is pending and removes data (%s); apply it yourself with ridu migrate up", file.Name, risk.Code)
			}
		}
	}
	return nil
}

// apply moves the development database's content to the new names. renames
// are the ones between the schema the database has and the new config.
func (target *developmentRenameTarget) apply(ctx context.Context, directory string, previous, current schema.Manifest, renames []migration.Rename) error {
	switch {
	case target.sqlite != nil && !target.runner:
		return target.sqlite.RenameDevelopmentFields(ctx, previous, current, renames)
	case target.sqlite != nil:
		return target.sqlite.ApplyArtifacts(ctx, directory)
	case target.mongodb != nil:
		return target.mongodb.ApplyArtifactsWithOptions(ctx, directory, mongodb.RunnerOptions{AllowMaintenance: true})
	}
	return target.postgres.ApplyArtifactsWithOptions(ctx, directory, postgres.RunnerOptions{AllowMaintenance: true, AllowInsecureDatabase: true})
}

func (renames *developmentRenames) stopRunningServer() {
	if renames.stopServer == nil {
		return
	}
	renames.output.Info("Stopping the development server while the rename moves stored content")
	renames.stopServer()
}

// developmentRenameManualFinish says how to apply already written migrations
// by hand.
func developmentRenameManualFinish(adapter projectfile.DatabaseAdapter) string {
	if adapter == projectfile.DatabaseSQLite {
		return "finish by hand with ridu migrate baseline and ridu migrate up, after which ridu migrate manages this database"
	}
	return "finish by hand with ridu migrate baseline and ridu migrate up --allow-maintenance"
}

// matchingRenameCandidates keeps the candidates the developer already
// accepted, re-derived against another base manifest.
func matchingRenameCandidates(candidates, accepted []schemadiff.RenameCandidate) []schemadiff.RenameCandidate {
	wanted := make(map[string]bool, len(accepted))
	for _, candidate := range accepted {
		wanted[renameDescription(candidate)] = true
	}
	var matched []schemadiff.RenameCandidate
	for _, candidate := range candidates {
		if wanted[renameDescription(candidate)] {
			matched = append(matched, candidate)
		}
	}
	if len(matched) != len(accepted) {
		return nil
	}
	return matched
}

var (
	migrationNameSeparators = regexp.MustCompile(`[^a-z0-9]+`)
	// migrationNamePattern mirrors the name migrationartifact.Create accepts.
	migrationNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
)

// developmentRenameName suggests a migration name such as
// rename-posts-title-to-headline.
func developmentRenameName(accepted []schemadiff.RenameCandidate) string {
	candidate := accepted[0]
	words := []string{"rename", string(candidate.BeforeCollection.Slug)}
	if candidate.Kind == schemadiff.RenameCollection {
		words = append(words, "to", string(candidate.AfterCollection.Slug))
	} else {
		words = append(words, candidate.BeforeField.Path.String(), "to", candidate.AfterField.Path.String())
	}
	if len(accepted) > 1 {
		words = append(words, "and-more")
	}
	name := strings.Trim(migrationNameSeparators.ReplaceAllString(strings.ToLower(strings.Join(words, "-")), "-"), "-")
	if name == "" {
		return "rename"
	}
	return name
}
