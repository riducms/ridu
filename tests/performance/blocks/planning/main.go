// Command planning measures how migration planning scales on the block-heavy benchmark schemas.
// For each scenario it applies a schema change that touches block definitions, then measures the
// offline `ridu migrate create` path and the `ridu dev` schema synchronization path on
// PostgreSQL, SQLite and MongoDB. Each measurement runs in a fresh child process, so its peak RSS
// covers only reading the manifests and planning or synchronizing one change.
//
//	planning -specs <directory> -out <directory> [-postgres URL] [-mongodb URL]
//
// The spec directory holds the `<scenario>-references.json` specs that ../run.ts writes; the
// README shows how to write only them. Development synchronization uses disposable PostgreSQL
// schemas and MongoDB databases that it creates and drops itself; it never touches another schema
// or database. RIDU_PLANNING_PROFILE=<directory> writes each step's CPU and allocation profiles.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/mongodb"
	"github.com/riducms/ridu/adapters/postgres"
	"github.com/riducms/ridu/adapters/sqlite"
	"github.com/riducms/ridu/internal/fieldchange"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/schemadiff"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/tests/performance/blocks/blockspec"
)

// Changes are the schema edits measured on every scenario.
var changes = []string{"add-field", "rename-field", "require-nested", "add-block"}

// job is one measured step, passed to the child process as JSON.
type job struct {
	Scenario   string `json:"scenario"`
	Change     string `json:"change"`
	Adapter    string `json:"adapter"`
	Mode       string `json:"mode"`
	Before     string `json:"before"`
	After      string `json:"after"`
	Migrations string `json:"migrations"`
	Database   string `json:"database,omitempty"`
}

// result is one measurement.
type result struct {
	job
	Accepted       bool    `json:"accepted"`
	Error          string  `json:"error,omitempty"`
	Milliseconds   float64 `json:"ms"`
	AllocBytes     uint64  `json:"allocBytes"`
	Allocations    uint64  `json:"allocations"`
	PeakRSSBytes   int64   `json:"peakRSSBytes"`
	ManifestRSS    int64   `json:"manifestRSSBytes"`
	RenameCount    int     `json:"renames"`
	ArtifactBytes  int64   `json:"artifactBytes,omitempty"`
	ArtifactSteps  int     `json:"artifactSteps,omitempty"`
	ManifestBytes  int     `json:"manifestBytes"`
	Placements     int     `json:"changedPlacements"`
	ChangeDescribe string  `json:"changeDescription"`
}

func main() {
	if len(os.Args) == 3 && os.Args[1] == "measure" {
		if err := measure(os.Args[2]); err != nil {
			log.Fatal(err)
		}
		return
	}
	specs := flag.String("specs", "", "directory of <scenario>-references.json specs")
	out := flag.String("out", "", "output directory")
	postgresURL := flag.String("postgres", "", "PostgreSQL URL for development synchronization")
	mongoURL := flag.String("mongodb", "", "MongoDB replica-set URL for development synchronization")
	scenarios := flag.String("scenarios", "A,B-dense,C-over", "scenarios")
	selected := flag.String("changes", strings.Join(changes, ","), "changes")
	adapters := flag.String("adapters", "postgres,sqlite,mongodb", "adapters")
	modes := flag.String("modes", "create,dev", "modes")
	flag.Parse()
	if *specs == "" || *out == "" {
		log.Fatal("usage: planning -specs <directory> -out <directory> [-postgres URL] [-mongodb URL]")
	}
	if err := runAll(context.Background(), *specs, *out, *postgresURL, *mongoURL, split(*scenarios), split(*selected), split(*adapters), split(*modes)); err != nil {
		log.Fatal(err)
	}
}

func split(value string) []string {
	var values []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			values = append(values, item)
		}
	}
	return values
}

func runAll(ctx context.Context, specs, out, postgresURL, mongoURL string, scenarios, selected, adapters, modes []string) error {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	var results []result
	for _, scenario := range scenarios {
		spec, err := blockspec.Read(filepath.Join(specs, scenario+"-references.json"))
		if err != nil {
			return err
		}
		before, err := resolve(spec)
		if err != nil {
			return fmt.Errorf("%s: %w", scenario, err)
		}
		for _, change := range selected {
			changed, description, placements, err := applyChange(spec, change)
			if err != nil {
				return fmt.Errorf("%s %s: %w", scenario, change, err)
			}
			after, err := resolve(changed)
			if err != nil {
				return fmt.Errorf("%s %s: %w", scenario, change, err)
			}
			directory := filepath.Join(out, scenario, change)
			if err := os.MkdirAll(directory, 0o755); err != nil {
				return err
			}
			beforePath, afterPath := filepath.Join(directory, "before.json"), filepath.Join(directory, "after.json")
			if err := writeManifest(beforePath, before); err != nil {
				return err
			}
			if err := writeManifest(afterPath, after); err != nil {
				return err
			}
			for _, adapter := range adapters {
				for _, mode := range modes {
					item := job{Scenario: scenario, Change: change, Adapter: adapter, Mode: mode, Before: beforePath, After: afterPath}
					item.Migrations = filepath.Join(directory, adapter+"-"+mode+"-migrations")
					measured, err := runJob(ctx, item, before, postgresURL, mongoURL)
					if err != nil {
						return fmt.Errorf("%s %s %s %s: %w", scenario, change, adapter, mode, err)
					}
					if measured == nil {
						continue
					}
					measured.ChangeDescribe, measured.Placements = description, placements
					results = append(results, *measured)
					fmt.Printf("%-7s %-15s %-8s %-6s %9.1f ms %8.1f MB alloc %9d allocs %7.1f MB peak RSS %5d renames %s\n",
						scenario, change, adapter, mode, measured.Milliseconds, float64(measured.AllocBytes)/1e6,
						measured.Allocations, float64(measured.PeakRSSBytes)/1e6, measured.RenameCount, measured.Error)
				}
			}
		}
	}
	encoded, err := json.MarshalIndent(results, "", "\t")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, "results.json"), append(encoded, '\n'), 0o644)
}

func resolve(spec blockspec.Spec) (schema.Manifest, error) {
	config, err := blockspec.Config(spec, nil)
	if err != nil {
		return schema.Manifest{}, err
	}
	return ridu.Resolve(config)
}

func writeManifest(path string, manifest schema.Manifest) error {
	encoded, err := manifest.Bytes()
	if err != nil {
		return err
	}
	return os.WriteFile(path, encoded, 0o644)
}

func readManifest(path string) (schema.Manifest, int, error) {
	encoded, err := os.ReadFile(path)
	if err != nil {
		return schema.Manifest{}, 0, err
	}
	manifest, err := schema.Parse(encoded)
	return manifest, len(encoded), err
}

// runJob prepares the before state, measures one step in a child process and removes the
// disposable database it created.
func runJob(ctx context.Context, item job, before schema.Manifest, postgresURL, mongoURL string) (*result, error) {
	if err := os.RemoveAll(item.Migrations); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(item.Migrations, 0o755); err != nil {
		return nil, err
	}
	cleanup := func() error { return nil }
	switch item.Mode {
	case "create":
		if err := createInitial(ctx, item.Adapter, item.Migrations, before); err != nil {
			return nil, fmt.Errorf("initial artifact: %w", err)
		}
	case "dev":
		var err error
		switch item.Adapter {
		case "postgres":
			if postgresURL == "" {
				return nil, nil
			}
			item.Database, cleanup, err = postgresSchema(ctx, postgresURL)
		case "mongodb":
			if mongoURL == "" {
				return nil, nil
			}
			item.Database, cleanup, err = mongoDatabase(ctx, mongoURL)
		case "sqlite":
			item.Database = filepath.Join(item.Migrations, "development.sqlite")
		}
		if err != nil {
			return nil, err
		}
		if err := syncDevelopment(ctx, item.Adapter, item.Database, before); err != nil {
			_ = cleanup()
			return nil, fmt.Errorf("synchronize the before schema: %w", err)
		}
	default:
		return nil, fmt.Errorf("unknown mode %q", item.Mode)
	}
	defer func() {
		if err := cleanup(); err != nil {
			log.Printf("cleanup: %v", err)
		}
	}()
	encoded, err := json.Marshal(item)
	if err != nil {
		return nil, err
	}
	command := exec.CommandContext(ctx, os.Args[0], "measure", string(encoded))
	command.Stderr = os.Stderr
	output, err := command.Output()
	if err != nil {
		return nil, err
	}
	var measured result
	if err := json.Unmarshal(output, &measured); err != nil {
		return nil, fmt.Errorf("decode measurement %q: %w", output, err)
	}
	return &measured, nil
}

func createInitial(ctx context.Context, adapter, directory string, manifest schema.Manifest) error {
	now := time.Unix(1, 0)
	switch adapter {
	case "postgres":
		_, err := postgres.CreateArtifact(ctx, directory, "initial", manifest, now, nil, false)
		return err
	case "sqlite":
		_, err := sqlite.CreateArtifact(ctx, directory, "initial", manifest, now, false)
		return err
	case "mongodb":
		_, err := mongodb.CreateArtifact(ctx, directory, "initial", manifest, now, mongodb.ArtifactOptions{})
		return err
	}
	return fmt.Errorf("unknown adapter %q", adapter)
}

func randomName() string {
	var bytes [6]byte
	_, _ = rand.Read(bytes[:])
	return "bpl_" + hex.EncodeToString(bytes[:])
}

func postgresSchema(ctx context.Context, base string) (string, func() error, error) {
	name := randomName()
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		return "", nil, err
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{name}.Sanitize()); err != nil {
		return "", nil, err
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return "", nil, err
	}
	parameters := parsed.Query()
	parameters.Set("search_path", name)
	parsed.RawQuery = parameters.Encode()
	cleanup := func() error {
		connection, err := pgx.Connect(context.Background(), base)
		if err != nil {
			return err
		}
		defer connection.Close(context.Background())
		_, err = connection.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{name}.Sanitize()+" CASCADE")
		return err
	}
	return parsed.String(), cleanup, nil
}

func mongoDatabase(_ context.Context, base string) (string, func() error, error) {
	name := randomName()
	parsed, err := url.Parse(base)
	if err != nil {
		return "", nil, err
	}
	parsed.Path = "/" + name
	databaseURL := parsed.String()
	cleanup := func() error {
		client, err := mongo.Connect(options.Client().ApplyURI(databaseURL))
		if err != nil {
			return err
		}
		defer client.Disconnect(context.Background())
		return client.Database(name).Drop(context.Background())
	}
	return databaseURL, cleanup, nil
}

func syncDevelopment(ctx context.Context, adapter, database string, manifest schema.Manifest) error {
	switch adapter {
	case "postgres":
		backend, err := postgres.OpenWithConfig(ctx, postgres.PoolConfig{DatabaseURL: database, AllowInsecureTransport: true})
		if err != nil {
			return err
		}
		defer backend.Close()
		return backend.SyncDevelopmentSchema(ctx, manifest)
	case "mongodb":
		backend, err := mongodb.OpenWithConfig(ctx, mongodb.Config{DatabaseURL: database, AllowInsecureTransport: true})
		if err != nil {
			return err
		}
		defer backend.Close()
		return backend.SyncDevelopmentSchema(ctx, manifest)
	case "sqlite":
		backend, err := sqlite.Open(ctx, database)
		if err != nil {
			return err
		}
		defer backend.Close()
		if err := backend.Migrate(ctx, manifest); err != nil {
			return err
		}
		return backend.Ready(ctx, manifest)
	}
	return fmt.Errorf("unknown adapter %q", adapter)
}

// measure runs in the child process.
func measure(encoded string) error {
	var item job
	if err := json.Unmarshal([]byte(encoded), &item); err != nil {
		return err
	}
	ctx := context.Background()
	after, size, err := readManifest(item.After)
	if err != nil {
		return err
	}
	measured := result{job: item, ManifestBytes: size}
	runtime.GC()
	measured.ManifestRSS = peakRSS()
	stopProfile, err := startProfile(item)
	if err != nil {
		return err
	}
	var start runtime.MemStats
	runtime.ReadMemStats(&start)
	started := time.Now()
	var renames int
	switch item.Mode {
	case "create":
		renames, err = createChange(ctx, item, after)
	default:
		renames, err = developmentChange(ctx, item, after)
	}
	measured.Milliseconds = float64(time.Since(started).Microseconds()) / 1000
	if profileError := stopProfile(); profileError != nil {
		return profileError
	}
	var finished runtime.MemStats
	runtime.ReadMemStats(&finished)
	measured.AllocBytes = finished.TotalAlloc - start.TotalAlloc
	measured.Allocations = finished.Mallocs - start.Mallocs
	measured.PeakRSSBytes = peakRSS()
	measured.RenameCount = renames
	measured.Accepted = err == nil
	if err != nil {
		measured.Error = err.Error()
	}
	if files, readError := migrationartifact.ReadAll(item.Migrations); readError == nil && len(files) != 0 {
		last := files[len(files)-1]
		if info, statError := os.Stat(last.Path); statError == nil && len(files) > 1 {
			measured.ArtifactBytes = info.Size()
			for _, phase := range last.Artifact.Phases {
				measured.ArtifactSteps += len(phase.Steps)
			}
		}
	}
	return json.NewEncoder(os.Stdout).Encode(measured)
}

// startProfile writes CPU and allocation profiles of the measured step to
// $RIDU_PLANNING_PROFILE/<scenario>-<change>-<adapter>-<mode>.{cpu,allocs}.pprof when set.
func startProfile(item job) (func() error, error) {
	directory := os.Getenv("RIDU_PLANNING_PROFILE")
	if directory == "" {
		return func() error { return nil }, nil
	}
	base := filepath.Join(directory, strings.Join([]string{item.Scenario, item.Change, item.Adapter, item.Mode}, "-"))
	cpu, err := os.Create(base + ".cpu.pprof")
	if err != nil {
		return nil, err
	}
	if err := pprof.StartCPUProfile(cpu); err != nil {
		return nil, err
	}
	return func() error {
		pprof.StopCPUProfile()
		if err := cpu.Close(); err != nil {
			return err
		}
		allocs, err := os.Create(base + ".allocs.pprof")
		if err != nil {
			return err
		}
		if err := pprof.Lookup("allocs").WriteTo(allocs, 0); err != nil {
			return err
		}
		return allocs.Close()
	}, nil
}

func peakRSS() int64 {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return 0
	}
	if runtime.GOOS == "darwin" {
		return usage.Maxrss
	}
	return usage.Maxrss * 1024
}

// createChange is `ridu migrate create --accept-renames` without the project handshake.
func createChange(ctx context.Context, item job, after schema.Manifest) (int, error) {
	previous, exists, err := migrationartifact.LatestManifest(item.Migrations)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, errors.New("missing initial artifact")
	}
	candidates := schemadiff.RenameCandidates(previous, after)
	now := time.Unix(2, 0)
	switch item.Adapter {
	case "postgres":
		_, err = postgres.CreateArtifact(ctx, item.Migrations, "change", after, now, postgresRenames(candidates), false)
	case "sqlite":
		var fields []schemadiff.RenameCandidate
		for _, candidate := range candidates {
			if candidate.Kind != schemadiff.RenameCollection {
				fields = append(fields, candidate)
			}
		}
		candidates = fields
		if len(candidates) != 0 {
			_, err = sqlite.CreateArtifactWithRenames(ctx, item.Migrations, "change", after, now, contentRenames(candidates))
		} else {
			_, err = sqlite.CreateArtifact(ctx, item.Migrations, "change", after, now, false)
		}
	case "mongodb":
		_, err = mongodb.CreateArtifact(ctx, item.Migrations, "change", after, now, mongodb.ArtifactOptions{Renames: contentRenames(candidates)})
	}
	return len(candidates), err
}

// developmentChange is one `ridu dev` reload with every rename accepted: the field-kind
// review, rename settlement through a migration, and schema synchronization.
func developmentChange(ctx context.Context, item job, after schema.Manifest) (int, error) {
	switch item.Adapter {
	case "postgres":
		backend, err := postgres.OpenWithConfig(ctx, postgres.PoolConfig{DatabaseURL: item.Database, AllowInsecureTransport: true})
		if err != nil {
			return 0, err
		}
		defer backend.Close()
		previous, renames, err := reviewDevelopment(ctx, after, backend.DevelopmentManifest)
		if err != nil || len(renames) == 0 {
			if err != nil {
				return 0, err
			}
			return 0, backend.SyncDevelopmentSchema(ctx, after)
		}
		if err := backend.VerifySchema(ctx, previous); err != nil {
			return len(renames), err
		}
		if err := createDevelopmentRename(ctx, item, previous, after, renames); err != nil {
			return len(renames), err
		}
		if _, err := backend.AdoptArtifacts(ctx, item.Migrations); err != nil {
			return len(renames), err
		}
		if err := backend.ApplyArtifactsWithOptions(ctx, item.Migrations, postgres.RunnerOptions{AllowMaintenance: true, AllowInsecureDatabase: true}); err != nil {
			return len(renames), err
		}
		return len(renames), backend.SyncDevelopmentSchema(ctx, after)
	case "mongodb":
		backend, err := mongodb.OpenWithConfig(ctx, mongodb.Config{DatabaseURL: item.Database, AllowInsecureTransport: true})
		if err != nil {
			return 0, err
		}
		defer backend.Close()
		previous, renames, err := reviewDevelopment(ctx, after, backend.DevelopmentManifest)
		if err != nil || len(renames) == 0 {
			if err != nil {
				return 0, err
			}
			return 0, backend.SyncDevelopmentSchema(ctx, after)
		}
		if err := backend.VerifyIndexes(ctx, previous); err != nil {
			return len(renames), err
		}
		if err := createDevelopmentRename(ctx, item, previous, after, renames); err != nil {
			return len(renames), err
		}
		if _, err := backend.AdoptArtifacts(ctx, item.Migrations); err != nil {
			return len(renames), err
		}
		if err := backend.ApplyArtifactsWithOptions(ctx, item.Migrations, mongodb.RunnerOptions{AllowMaintenance: true}); err != nil {
			return len(renames), err
		}
		return len(renames), backend.SyncDevelopmentSchema(ctx, after)
	case "sqlite":
		backend, err := sqlite.Open(ctx, item.Database)
		if err != nil {
			return 0, err
		}
		defer backend.Close()
		previous, renames, err := reviewDevelopment(ctx, after, backend.DevelopmentManifest)
		if err != nil {
			return 0, err
		}
		if len(renames) != 0 {
			if err := backend.Ready(ctx, previous); err != nil {
				return len(renames), err
			}
			// A SQLite database without migration history moves the content directly.
			if err := backend.RenameDevelopmentFields(ctx, previous, after, contentRenames(renames)); err != nil {
				return len(renames), err
			}
		}
		if err := backend.Migrate(ctx, after); err != nil {
			return len(renames), err
		}
		return len(renames), backend.Ready(ctx, after)
	}
	return 0, fmt.Errorf("unknown adapter %q", item.Adapter)
}

// reviewDevelopment reads the accepted development schema, runs the field-kind review and
// detects renames, as `ridu dev` does before generation.
func reviewDevelopment(ctx context.Context, after schema.Manifest, read func(context.Context) (schema.Manifest, bool, error)) (schema.Manifest, []schemadiff.RenameCandidate, error) {
	baseline := func() (schema.Manifest, error) {
		manifest, exists, err := read(ctx)
		if err != nil {
			return schema.Manifest{}, err
		}
		if !exists {
			return schema.Manifest{}, errors.New("missing development schema record")
		}
		return schemadiff.CompactManifest(manifest)
	}
	previous, err := baseline()
	if err != nil {
		return schema.Manifest{}, nil, err
	}
	if changes := fieldchange.Detect(previous.Snapshot(), after.Snapshot()); len(changes) != 0 {
		return schema.Manifest{}, nil, fmt.Errorf("unexpected field-kind changes: %d", len(changes))
	}
	if previous, err = baseline(); err != nil {
		return schema.Manifest{}, nil, err
	}
	return previous, schemadiff.RenameCandidates(previous, after), nil
}

// createDevelopmentRename writes the migration for the development changes that preceded the
// rename and the rename migration itself, as `ridu dev` does for a database without history.
func createDevelopmentRename(ctx context.Context, item job, previous, after schema.Manifest, renames []schemadiff.RenameCandidate) error {
	create := func(name string, before *schema.Manifest, target schema.Manifest, candidates []schemadiff.RenameCandidate, now time.Time) error {
		switch item.Adapter {
		case "mongodb":
			_, err := mongodb.CreateArtifact(ctx, item.Migrations, name, target, now, mongodb.ArtifactOptions{Renames: contentRenames(candidates)})
			return err
		default:
			artifact, err := postgres.BuildArtifact(ctx, name, before, target, postgresRenames(candidates), false)
			if err != nil {
				return err
			}
			_, err = migrationartifact.Create(item.Migrations, name, artifact, now)
			return err
		}
	}
	if err := create("changes-before-rename", nil, previous, nil, time.Unix(1, 0)); err != nil {
		return err
	}
	return create("rename", &previous, after, renames, time.Unix(2, 0))
}

func postgresRenames(candidates []schemadiff.RenameCandidate) []postgres.Rename {
	renamed := make([]postgres.Rename, len(candidates))
	for index, candidate := range candidates {
		renamed[index] = postgres.Rename{
			Kind:             postgres.RenameKind(candidate.Kind),
			BeforeCollection: candidate.BeforeCollection,
			AfterCollection:  candidate.AfterCollection,
			Block:            candidate.Block,
			BeforeField:      candidate.BeforeField,
			AfterField:       candidate.AfterField,
		}
		for _, pair := range candidate.Fields {
			renamed[index].Fields = append(renamed[index].Fields, postgres.FieldRename{Before: pair.Before, After: pair.After})
		}
	}
	return renamed
}

func contentRenames(candidates []schemadiff.RenameCandidate) []migration.Rename {
	renamed := make([]migration.Rename, len(candidates))
	for index, candidate := range candidates {
		renamed[index] = migration.Rename{CollectionBefore: candidate.BeforeCollection.Slug, CollectionAfter: candidate.AfterCollection.Slug, Block: candidate.Block}
		if candidate.Kind != schemadiff.RenameCollection {
			renamed[index].FieldBefore = candidate.BeforeField.Path.String()
			renamed[index].FieldAfter = candidate.AfterField.Path.String()
			continue
		}
		for _, pair := range candidate.Fields {
			renamed[index].Fields = append(renamed[index].Fields, migration.FieldRename{Before: pair.Before.Path.String(), After: pair.After.Path.String()})
		}
	}
	return renamed
}

// applyChange edits a copy of the spec. It reports the change and how many block placements of
// the changed definition the scenario has.
func applyChange(spec blockspec.Spec, change string) (blockspec.Spec, string, int, error) {
	changed := cloneSpec(spec)
	counts := placementCounts(spec)
	switch change {
	case "add-field":
		for index := range changed.Blocks {
			changed.Blocks[index].Fields = append(changed.Blocks[index].Fields, blockspec.Field{Type: "text", Name: "planningNote"})
		}
		total := 0
		for _, count := range counts {
			total += count
		}
		return changed, "text planningNote added to every block", total, nil
	case "rename-field", "require-nested":
		index, field := mostPlaced(changed, counts, func(item blockspec.Field) bool {
			return item.Type == "text" && !item.Required
		})
		if index < 0 {
			return blockspec.Spec{}, "", 0, errors.New("no block has an optional text field")
		}
		target := &changed.Blocks[index].Fields[field]
		slug := changed.Blocks[index].Slug
		if change == "rename-field" {
			description := fmt.Sprintf("%s.%s renamed to %sRenamed", slug, target.Name, target.Name)
			target.Name += "Renamed"
			return changed, description, counts[slug], nil
		}
		target.Required = true
		return changed, fmt.Sprintf("%s.%s made required", slug, target.Name), counts[slug], nil
	case "add-block":
		index, field := mostPlaced(changed, counts, func(item blockspec.Field) bool { return item.Type == "blocks" })
		if index < 0 {
			return blockspec.Spec{}, "", 0, errors.New("no block contains blocks")
		}
		extra := blockspec.Block{Slug: "planning-extra", Fields: []blockspec.Field{
			{Type: "text", Name: "label"},
			{Type: "relationship", Name: "link", RelationTo: "media"},
		}}
		changed.Blocks = append(changed.Blocks, extra)
		container := &changed.Blocks[index].Fields[field]
		container.Blocks = append(container.Blocks, extra.Slug)
		slug := changed.Blocks[index].Slug
		return changed, fmt.Sprintf("block planning-extra added to %s.%s", slug, container.Name), counts[slug], nil
	}
	return blockspec.Spec{}, "", 0, fmt.Errorf("unknown change %q", change)
}

func cloneSpec(spec blockspec.Spec) blockspec.Spec {
	encoded, _ := json.Marshal(spec)
	var clone blockspec.Spec
	_ = json.Unmarshal(encoded, &clone)
	return clone
}

// mostPlaced selects the most placed block with a direct field matching accept.
func mostPlaced(spec blockspec.Spec, counts map[string]int, accept func(blockspec.Field) bool) (int, int) {
	best, bestField := -1, -1
	for index, block := range spec.Blocks {
		for field, item := range block.Fields {
			if !accept(item) {
				continue
			}
			if best < 0 || counts[block.Slug] > counts[spec.Blocks[best].Slug] {
				best, bestField = index, field
			}
			break
		}
	}
	return best, bestField
}

// placementCounts counts the block placements of every definition: the paths from a resource
// field to the block.
func placementCounts(spec blockspec.Spec) map[string]int {
	definitions := make(map[string]blockspec.Block, len(spec.Blocks))
	for _, block := range spec.Blocks {
		definitions[block.Slug] = block
	}
	counts := make(map[string]int)
	memo := make(map[string]map[string]int)
	var below func(fields []blockspec.Field) map[string]int
	var within func(slug string) map[string]int
	within = func(slug string) map[string]int {
		if cached, ok := memo[slug]; ok {
			return cached
		}
		result := map[string]int{slug: 1}
		for key, value := range below(definitions[slug].Fields) {
			result[key] += value
		}
		memo[slug] = result
		return result
	}
	below = func(fields []blockspec.Field) map[string]int {
		result := map[string]int{}
		for _, item := range fields {
			for key, value := range below(item.Fields) {
				result[key] += value
			}
			for _, slug := range item.Blocks {
				for key, value := range within(slug) {
					result[key] += value
				}
			}
		}
		return result
	}
	for _, resources := range [][]blockspec.Resource{spec.Collections, spec.Globals} {
		for _, resource := range resources {
			for key, value := range below(resource.Fields) {
				counts[key] += value
			}
		}
	}
	return counts
}
