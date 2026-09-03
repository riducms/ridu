package project

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

// ManifestResolver delays config resolution until a valid machine request has
// been checked, keeping ordinary command parsing free of config side effects.
type ManifestResolver func() (schema.Manifest, error)

// GenerationResolver resolves the same canonical manifest plus deterministic
// artifacts that require executable plugin configuration.
type GenerationResolver func([]ArtifactRequest) (schema.Manifest, []Artifact, error)

type artifactRequestFlags []ArtifactRequest

func (requests *artifactRequestFlags) String() string {
	identities := make([]string, len(*requests))
	for index, request := range *requests {
		identities[index] = request.Plugin + "/" + request.Name
	}
	return strings.Join(identities, ",")
}

func (requests *artifactRequestFlags) Set(value string) error {
	plugin, name, found := strings.Cut(value, "/")
	if !found || strings.Contains(name, "/") || !schema.IsValidCollectionSlug(plugin) || !schema.IsValidCollectionSlug(name) {
		return fmt.Errorf("artifact %q must use <plugin>/<artifact>", value)
	}
	for _, existing := range *requests {
		if existing.Plugin == plugin && existing.Name == name {
			return fmt.Errorf("artifact %q was requested more than once", value)
		}
	}
	*requests = append(*requests, ArtifactRequest{Plugin: plugin, Name: name})
	return nil
}

// Run handles application-side project commands. Migration execution is
// available only when the application explicitly registers a compiled driver;
// manifest and generation commands still never open a database.
func Run(args []string, stdout, stderr io.Writer, frameworkVersion string, resolveManifest ManifestResolver, resolveGeneration GenerationResolver, migrationDriver migration.ProjectDriver) error {
	if len(args) == 0 {
		return fmt.Errorf("the Ridu server runtime is not implemented yet; use the global CLI to run generate")
	}
	if args[0] != Command {
		return fmt.Errorf("unknown project command %q", args[0])
	}
	if len(args) < 2 || (args[1] != "manifest" && args[1] != "generate" && args[1] != "migrate") {
		return fmt.Errorf("unknown %s command; expected manifest, generate, or migrate", Command)
	}

	operation := args[1]
	privateDatabaseURL := os.Getenv(MigrationDatabaseURLEnvironment)
	if err := os.Unsetenv(MigrationDatabaseURLEnvironment); err != nil {
		return fmt.Errorf("clear private project migration database selection")
	}
	flags := flag.NewFlagSet(Command+" "+operation, flag.ContinueOnError)
	flags.SetOutput(stderr)
	protocolVersion := flags.Int("protocol-version", 0, "requesting CLI protocol version")
	requestedFramework := flags.String("framework-version", "", "requesting CLI framework version")
	var requestedArtifacts artifactRequestFlags
	flags.Var(&requestedArtifacts, "artifact", "requested plugin artifact identity")
	migrationAction := flags.String("action", "", "project migration action")
	databasePath := flags.String("database-path", "", "selected SQLite database path")
	migrationDirectory := flags.String("directory", "", "committed migration artifact directory")
	allowInsecureDatabase := flags.Bool("allow-insecure-database", false, "admit explicitly selected plaintext database transport")
	allowMaintenance := flags.Bool("allow-maintenance", false, "admit traffic-sensitive migration steps")
	allowUnbounded := flags.Bool("allow-unbounded", false, "admit explicitly unbounded migration waits")
	lockWait := flags.Duration("lock-wait", 0, "maximum adapter migration-lock wait")
	operationTimeout := flags.Duration("operation-timeout", 0, "maximum adapter migration operation duration")
	lockTimeout := flags.Duration("lock-timeout", 0, "maximum PostgreSQL lock wait per phase")
	statementTimeout := flags.Duration("statement-timeout", 0, "maximum PostgreSQL transactional statement duration")
	batchTimeout := flags.Duration("batch-timeout", 0, "maximum PostgreSQL checkpoint batch duration")
	idleTransactionTimeout := flags.Duration("idle-transaction-timeout", 0, "maximum PostgreSQL idle transaction duration")
	stopAfterPhase := flags.String("stop-after-phase", "", "stop after a committed PostgreSQL phase")
	stopAfterStep := flags.String("stop-after-step", "", "stop after a committed PostgreSQL step")
	if err := flags.Parse(args[2:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("%s %s does not accept positional arguments", Command, operation)
	}
	if operation == "manifest" && len(requestedArtifacts) != 0 {
		return fmt.Errorf("%s manifest does not accept artifact requests", Command)
	}
	if operation != "migrate" && (*migrationAction != "" || *databasePath != "" || *migrationDirectory != "" ||
		*allowInsecureDatabase || *allowMaintenance || *allowUnbounded || *lockWait != 0 || *operationTimeout != 0 ||
		*lockTimeout != 0 || *statementTimeout != 0 || *batchTimeout != 0 || *idleTransactionTimeout != 0 ||
		*stopAfterPhase != "" || *stopAfterStep != "") {
		return fmt.Errorf("%s %s does not accept migration execution options", Command, operation)
	}
	if operation == "migrate" && len(requestedArtifacts) != 0 {
		return fmt.Errorf("%s migrate does not accept artifact requests", Command)
	}
	if *protocolVersion != ProtocolVersion {
		return fmt.Errorf("CLI protocol version %d is incompatible with project protocol version %d", *protocolVersion, ProtocolVersion)
	}
	if *requestedFramework != frameworkVersion {
		return fmt.Errorf("CLI framework version %q is incompatible with project framework version %q; install matching Ridu versions", *requestedFramework, frameworkVersion)
	}
	if migrationDriver != nil {
		if err := migrationDriver.Validate(); err != nil {
			return fmt.Errorf("validate project migration driver: %w", err)
		}
	}

	var manifest schema.Manifest
	var artifacts []Artifact
	var err error
	if operation == "migrate" {
		if migrationDriver == nil {
			return fmt.Errorf("project migration driver is unavailable; register one with ridu.WithProjectMigrations")
		}
		request := migration.ProjectRequest{
			Action: migration.ProjectAction(*migrationAction), DatabasePath: *databasePath, DatabaseURL: privateDatabaseURL, Directory: *migrationDirectory,
			AllowInsecureDatabase: *allowInsecureDatabase, AllowMaintenance: *allowMaintenance, AllowUnbounded: *allowUnbounded,
			LockWait: *lockWait, OperationTimeout: *operationTimeout,
			LockTimeout: *lockTimeout, StatementTimeout: *statementTimeout, BatchTimeout: *batchTimeout,
			IdleTransactionTimeout: *idleTransactionTimeout, StopAfterPhase: *stopAfterPhase, StopAfterStep: *stopAfterStep,
		}
		if err := request.Validate(); err != nil {
			return err
		}
		manifest, err = resolveManifest()
		if err != nil {
			return err
		}
		if err := migrationDriver.RunProjectMigration(context.Background(), request, manifest); err != nil {
			return fmt.Errorf("run project migration: %w", err)
		}
	} else if operation == "generate" {
		if resolveGeneration == nil {
			return fmt.Errorf("project generation resolver is unavailable")
		}
		manifest, artifacts, err = resolveGeneration(requestedArtifacts)
	} else {
		manifest, err = resolveManifest()
	}
	if err != nil {
		return fmt.Errorf("resolve project %s: %w", operation, err)
	}
	manifestJSON, err := manifest.MarshalJSON()
	if err != nil {
		return fmt.Errorf("encode project manifest: %w", err)
	}
	response, err := EncodeResponse(Response{
		ProtocolVersion:  ProtocolVersion,
		FrameworkVersion: frameworkVersion,
		ManifestVersion:  uint32(schema.CurrentVersion),
		Manifest:         manifestJSON,
		Artifacts:        artifacts,
		DataTransforms:   projectDataTransformDescriptors(migrationDriver),
	})
	if err != nil {
		return err
	}
	if _, err := stdout.Write(response); err != nil {
		return fmt.Errorf("write project response: %w", err)
	}
	return nil
}

func projectDataTransformDescriptors(driver migration.ProjectDriver) []migration.DataTransformDescriptor {
	if driver == nil {
		return nil
	}
	return driver.DataTransforms()
}
