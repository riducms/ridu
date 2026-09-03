package mongodb

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/internal/migrationartifact"
)

// TestGeneratedMongoDBProductionDeployment proves one release-shaped Linux
// deployment path rather than a platform or topology matrix. The test is
// intentionally opt-in: it publishes the current checkout as a synthetic
// release, builds two generated projects, and owns a three-member authenticated
// TLS replica set plus disposable databases and upload roots for the duration.
func TestGeneratedMongoDBProductionDeployment(t *testing.T) {
	if os.Getenv(mongoDBProductionDeploymentTestEnvironment) != "true" {
		t.Skip("set RIDU_MONGODB_PRODUCTION_DEPLOYMENT_TEST=true to run the generated-project MongoDB deployment proof")
	}
	harness := newMongoDBProductionDeploymentHarness(t)

	t.Run("starter continuity migration and recovery", func(t *testing.T) {
		database := harness.provisionDatabase(t, "starter")
		project := harness.newProject(t, "starter", database)
		proveGeneratedMongoDBStarterDeployment(t, harness, project)
	})

	t.Run("blank release lifecycle", func(t *testing.T) {
		database := harness.provisionDatabase(t, "blank")
		project := harness.newProject(t, "blank", database)
		proveGeneratedMongoDBBlankDeployment(t, harness, project)
	})
}

func proveGeneratedMongoDBStarterDeployment(
	t *testing.T,
	harness *mongoDBProductionDeploymentHarness,
	project mongoDBProductionDeploymentProject,
) {
	initialBinary, initialArtifact := qualifyGeneratedMongoDBInitialRelease(t, harness, project)
	if countMongoMigrationArtifactSteps(initialArtifact.Artifact) < 2 {
		t.Fatalf("generated MongoDB starter initial artifact has no representative physical plan: %#v", initialArtifact.Artifact.Phases)
	}

	first := harness.startServer(t, project, initialBinary, project.database.containerURL, "starter-a")
	mongoDBProductionDeploymentWaitReady(t, first, 90*time.Second)
	mongoDBProductionDeploymentAssertEmbeddedAdmin(t, first.baseURL())
	data := mongoDBProductionDeploymentBootstrapStarter(t, first.baseURL())
	mongoDBProductionDeploymentAssertStarter(t, first.baseURL(), data, false)
	browserCanary := harness.runAdminBrowserCanary(t, first, data)
	harness.runSDKCanary(t, project, first.baseURL(), data)

	// Build a genuinely different code-only release while the first process is
	// serving. Its manifest and exact migration history must remain unchanged,
	// so the replacement can be admitted before the old process drains.
	serverPath := filepath.Join(project.root, "cmd", "server", "main.go")
	mongoDBProductionReplaceFile(t, serverPath,
		`ApplicationName:        "ridu-server",`,
		`ApplicationName:        "ridu-server-code-only-rollout",`,
	)
	if output, err := harness.run(30*time.Second, project.root, nil, "gofmt", "-w", "cmd/server/main.go"); err != nil {
		t.Fatalf("format generated MongoDB code-only release: %v\n%s", err, output)
	}
	codeOnlyPoisonURL := "mongodb://offline-release-user:offline-release-secret@127.0.0.1:1/offline"
	runGeneratedMongoDBOfflineCommand(t, harness, project, codeOnlyPoisonURL, "generate", "--check")
	replacementBinary := harness.buildLinuxProject(t, project, "dist/mongodb-starter-code-only-linux")
	runGeneratedMongoDBOfflineCommand(t, harness, project, codeOnlyPoisonURL, "check")
	codeOnlyArtifacts, err := migrationartifact.ReadAll(filepath.Join(project.root, "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if len(codeOnlyArtifacts) != 1 || codeOnlyArtifacts[0].Name != initialArtifact.Name || codeOnlyArtifacts[0].Digest != initialArtifact.Digest {
		t.Fatalf("code-only release changed MongoDB migration history: %#v", codeOnlyArtifacts)
	}
	initialBytes, err := os.ReadFile(filepath.Join(project.root, filepath.FromSlash(initialBinary)))
	if err != nil {
		t.Fatal(err)
	}
	replacementBytes, err := os.ReadFile(filepath.Join(project.root, filepath.FromSlash(replacementBinary)))
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(initialBytes) == sha256.Sum256(replacementBytes) {
		t.Fatal("generated MongoDB code-only replacement is byte-identical to the initial release")
	}

	second := harness.startServer(t, project, replacementBinary, project.database.containerURL, "starter-b")
	mongoDBProductionDeploymentWaitReady(t, second, 90*time.Second)
	mongoDBProductionDeploymentAssertStarter(t, second.baseURL(), data, false)

	admin := harness.fixture.authenticatedAdminClient(t, harness.fixture.seedURL(harness.fixture.caPath, true))
	oldPrimary := waitForMongoDBProductionPrimary(t, admin, "", 30*time.Second)
	stepDownMongoDBProductionPrimary(t, harness.fixture, oldPrimary)
	newPrimary := waitForMongoDBProductionPrimary(t, admin, oldPrimary, 60*time.Second)
	disconnectMongoDBProductionClient(t, admin)
	if newPrimary == oldPrimary {
		t.Fatalf("MongoDB deployment election retained primary %q", oldPrimary)
	}
	mongoDBProductionDeploymentAssertStarter(t, first.baseURL(), data, false)
	mongoDBProductionDeploymentAssertStarter(t, second.baseURL(), data, false)

	stopStarted := time.Now()
	if err := first.stop(12 * time.Second); err != nil {
		t.Fatalf("drain old same-manifest MongoDB deployment: %v\n%s", err, mongoDBProductionRedacted(first.logs(), project.database.secrets()...))
	}
	if elapsed := time.Since(stopStarted); elapsed > 12*time.Second {
		t.Fatalf("old same-manifest MongoDB deployment drained in %s", elapsed)
	}
	mongoDBProductionDeploymentWaitUnavailable(t, first.baseURL()+"/readyz", 8*time.Second)
	mongoDBProductionDeploymentAssertStarter(t, second.baseURL(), data, false)

	// A manifest-changing release uses a coordinated drain before generation,
	// migration, and startup. Thirty-two separate indexes make the interruption
	// boundary observable without a test-only runner hook.
	stopStarted = time.Now()
	if err := second.stop(12 * time.Second); err != nil {
		t.Fatalf("drain final old-manifest MongoDB deployment: %v\n%s", err, mongoDBProductionRedacted(second.logs(), project.database.secrets()...))
	}
	if elapsed := time.Since(stopStarted); elapsed > 12*time.Second {
		t.Fatalf("final old-manifest MongoDB deployment drained in %s", elapsed)
	}
	mongoDBProductionDeploymentWaitUnavailable(t, second.baseURL()+"/readyz", 8*time.Second)

	mongoDBProductionDeploymentAddIndexedFields(t, project, 32)
	if output, err := harness.run(30*time.Second, project.root, nil, "gofmt", "-w", "content/posts.go"); err != nil {
		t.Fatalf("format generated MongoDB schema change: %v\n%s", err, output)
	}
	poisonURL := "mongodb://offline-schema-user:offline-schema-secret@127.0.0.1:1/offline"
	runGeneratedMongoDBOfflineCommand(t, harness, project, poisonURL, "generate")
	runGeneratedMongoDBOfflineCommand(t, harness, project, poisonURL, "generate", "--check")
	runGeneratedMongoDBOfflineCommand(t, harness, project, poisonURL, "migrate", "create", "--name", "indexed-rollout")
	artifacts, err := migrationartifact.ReadAll(filepath.Join(project.root, "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 2 {
		t.Fatalf("generated MongoDB starter history has %d artifacts after rollout, want 2", len(artifacts))
	}
	rolloutArtifact := artifacts[1]
	if steps := countMongoMigrationArtifactSteps(rolloutArtifact.Artifact); steps < 20 {
		t.Fatalf("generated MongoDB rollout has only %d steps; interruption would not be observable", steps)
	}
	rolloutBinary := harness.buildLinuxProject(t, project, "dist/mongodb-starter-rollout-linux")
	runGeneratedMongoDBOfflineCommand(t, harness, project, poisonURL, "check")

	// Cutover begins only after every old-manifest writer has drained. Replay
	// immutable history in a privileged shadow database, prove that verification
	// did not mutate the application ledger, and then capture one matched
	// database/upload recovery point while the application remains stopped.
	verifyURL := harness.fixture.seedURL(harness.fixture.caPath, true)
	assertGeneratedMongoDBVerifyRequiresOperationalAuthority(t, harness, project)
	verifyOutput := runGeneratedMongoDBVerifiedCommand(t, harness, project, verifyURL, harness.fixture.adminUser, harness.fixture.adminPassword, 2*time.Minute, "migrate", "verify")
	assertGeneratedMongoDBVerifySucceeded(t, verifyOutput)
	statusOutput := runGeneratedMongoDBLiveCommand(t, harness, project, project.database.hostURL, 45*time.Second, "migrate", "status", "--json")
	statuses := parseMongoDBProductionDeploymentStatuses(t, statusOutput)
	assertMongoDBProductionDeploymentStatus(t, statuses, rolloutArtifact.Name, false, false)
	scopeSentinel := harness.seedOutOfScopeDatabase(t, project.database)
	databaseArchive := harness.backupDatabase(t, project.database, scopeSentinel.database)
	uploadBackup := filepath.Join(t.TempDir(), "upload-backup")
	if err := mongoDBProductionDeploymentCopyTree(project.uploadRoot, uploadBackup); err != nil {
		t.Fatalf("backup generated MongoDB upload objects before migration: %v", err)
	}
	harness.mutateOutOfScopeDatabase(t, project.database, scopeSentinel)
	assertGeneratedMongoDBServerFailsBeforeMigration(t, harness, project, rolloutBinary, "rollout-before-up")

	harness.interruptMigrationAfterProgress(t, project, rolloutArtifact)
	statusOutput = runGeneratedMongoDBLiveCommand(t, harness, project, project.database.hostURL, 45*time.Second, "migrate", "status", "--json")
	statuses = parseMongoDBProductionDeploymentStatuses(t, statusOutput)
	assertMongoDBProductionDeploymentStatus(t, statuses, rolloutArtifact.Name, false, true)

	// The crashed owner retains a bounded lease. The real CLI waits for expiry,
	// recognizes already-completed physical steps, and resumes the same history.
	runGeneratedMongoDBLiveCommand(
		t, harness, project, project.database.hostURL, 3*time.Minute,
		"migrate", "up", "--advisory-lock-wait", "90s",
	)
	statusOutput = runGeneratedMongoDBLiveCommand(t, harness, project, project.database.hostURL, 45*time.Second, "migrate", "status", "--json")
	statuses = parseMongoDBProductionDeploymentStatuses(t, statusOutput)
	assertMongoDBProductionDeploymentStatus(t, statuses, rolloutArtifact.Name, true, false)

	rollout := harness.startServer(t, project, rolloutBinary, project.database.containerURL, "starter-rollout")
	mongoDBProductionDeploymentWaitReady(t, rollout, 90*time.Second)
	mongoDBProductionDeploymentAssertStarter(t, rollout.baseURL(), data, false)
	mongoDBProductionDeploymentAssertAdminCanary(t, rollout.baseURL(), data, browserCanary)
	harness.runSDKCanary(t, project, rollout.baseURL(), data)
	rolloutBrowserCanary := harness.runAdminBrowserCanary(t, rollout, data)
	mongoDBProductionDeploymentAssertAdminCanary(t, rollout.baseURL(), data, rolloutBrowserCanary)
	mongoDBProductionDeploymentUpdateSummary(t, rollout.baseURL(), data)
	mongoDBProductionDeploymentAssertStarter(t, rollout.baseURL(), data, true)
	data = mongoDBProductionDeploymentCaptureRestoreVersion(t, rollout.baseURL(), data)
	if err := rollout.stop(12 * time.Second); err != nil {
		t.Fatalf("drain schema-changing MongoDB deployment before recovery drill: %v\n%s", err, mongoDBProductionRedacted(rollout.logs(), project.database.secrets()...))
	}

	// Roll back to the matched pre-migration recovery point. The scoped restore
	// must recover its application user and uploads without touching a separate
	// database changed after the snapshot. The rollout is pending again, so the
	// new binary must fail closed until the exact same v2 history is reapplied.
	displacedUploads := project.uploadRoot + ".before-restore"
	if err := os.Rename(project.uploadRoot, displacedUploads); err != nil {
		t.Fatalf("displace generated MongoDB upload root before restore: %v", err)
	}
	harness.dropScopedDatabaseUser(t, project.database)
	harness.dropDatabase(t, project.database)
	harness.restoreDatabase(t, project.database, databaseArchive, scopeSentinel.database)
	harness.assertOutOfScopeDatabase(t, project.database, scopeSentinel)
	harness.assertScopedDatabaseUserRestored(t, project.database)
	if err := mongoDBProductionDeploymentCopyTree(uploadBackup, project.uploadRoot); err != nil {
		t.Fatalf("restore generated MongoDB upload objects: %v", err)
	}
	statusOutput = runGeneratedMongoDBLiveCommand(t, harness, project, project.database.hostURL, 45*time.Second, "migrate", "status", "--json")
	statuses = parseMongoDBProductionDeploymentStatuses(t, statusOutput)
	assertMongoDBProductionDeploymentStatus(t, statuses, rolloutArtifact.Name, false, false)
	assertGeneratedMongoDBServerFailsBeforeMigration(t, harness, project, rolloutBinary, "rollout-after-restore-before-up")
	runGeneratedMongoDBLiveCommand(
		t, harness, project, project.database.hostURL, 2*time.Minute,
		"migrate", "up", "--advisory-lock-wait", "45s",
	)
	statusOutput = runGeneratedMongoDBLiveCommand(t, harness, project, project.database.hostURL, 45*time.Second, "migrate", "status", "--json")
	statuses = parseMongoDBProductionDeploymentStatuses(t, statusOutput)
	assertMongoDBProductionDeploymentStatus(t, statuses, rolloutArtifact.Name, true, false)

	restored := harness.startServer(t, project, rolloutBinary, project.database.containerURL, "starter-restored")
	mongoDBProductionDeploymentWaitReady(t, restored, 90*time.Second)
	mongoDBProductionDeploymentAssertStarter(t, restored.baseURL(), data, false)
	mongoDBProductionDeploymentAssertAdminCanary(t, restored.baseURL(), data, browserCanary)
	harness.runSDKCanary(t, project, restored.baseURL(), data)
	restoredBrowserCanary := harness.runAdminBrowserCanary(t, restored, data)
	mongoDBProductionDeploymentAssertAdminCanary(t, restored.baseURL(), data, restoredBrowserCanary)
	mongoDBProductionDeploymentUpdateSummary(t, restored.baseURL(), data)
	mongoDBProductionDeploymentAssertStarter(t, restored.baseURL(), data, true)
	mongoDBProductionDeploymentRestoreCapturedVersion(t, restored.baseURL(), data)
	mongoDBProductionDeploymentAssertStarter(t, restored.baseURL(), data, false)
	mongoDBProductionDeploymentUpdateSummary(t, restored.baseURL(), data)
	mongoDBProductionDeploymentAssertStarter(t, restored.baseURL(), data, true)
	if err := restored.stop(12 * time.Second); err != nil {
		t.Fatalf("stop restored MongoDB deployment: %v\n%s", err, mongoDBProductionRedacted(restored.logs(), project.database.secrets()...))
	}
}

func proveGeneratedMongoDBBlankDeployment(
	t *testing.T,
	harness *mongoDBProductionDeploymentHarness,
	project mongoDBProductionDeploymentProject,
) {
	binary, _ := qualifyGeneratedMongoDBInitialRelease(t, harness, project)
	server := harness.startServer(t, project, binary, project.database.containerURL, "blank")
	mongoDBProductionDeploymentWaitReady(t, server, 90*time.Second)
	mongoDBProductionDeploymentAssertEmbeddedAdmin(t, server.baseURL())
	mongoDBProductionDeploymentAssertBlank(t, server.baseURL())
	if err := server.stop(12 * time.Second); err != nil {
		t.Fatalf("stop generated MongoDB blank deployment: %v\n%s", err, mongoDBProductionRedacted(server.logs(), project.database.secrets()...))
	}

	wrongPassword := mongoDBProductionRandomSecret(t, 24)
	wrongURL := mongoDBProductionURL(
		harness.fixture.hosts(), project.database.name, project.database.username, wrongPassword,
		filepath.Join(mongoDBProductionContainerFixtureRoot, "ca.pem"), true, false, true,
	)
	failed := harness.startServer(t, project, binary, wrongURL, "blank-wrong-credentials")
	output, exitErr, observationErr := failed.expectedFailure(25 * time.Second)
	wrongSecrets := project.database.secrets(wrongURL, wrongPassword, project.database.username)
	if observationErr != nil {
		t.Fatalf("generated MongoDB blank deployment did not exit after invalid credentials: %v\n%s", observationErr, mongoDBProductionRedacted(output, wrongSecrets...))
	}
	if exitErr == nil {
		t.Fatalf("generated MongoDB blank deployment accepted invalid credentials:\n%s", mongoDBProductionRedacted(output, wrongSecrets...))
	}
	mongoDBProductionAssertSecretsAbsent(t, output, wrongSecrets...)
	if !strings.Contains(strings.ToLower(output), "document store") && !strings.Contains(strings.ToLower(output), "mongodb") {
		t.Fatalf("generated MongoDB credential failure was not actionable: %v\n%s", exitErr, mongoDBProductionRedacted(output, wrongSecrets...))
	}
}

func qualifyGeneratedMongoDBInitialRelease(
	t *testing.T,
	harness *mongoDBProductionDeploymentHarness,
	project mongoDBProductionDeploymentProject,
) (string, migrationartifact.File) {
	t.Helper()
	poisonURL := "mongodb://offline-release-user:offline-release-secret@127.0.0.1:1/offline"
	runGeneratedMongoDBOfflineCommand(t, harness, project, poisonURL, "generate")
	runGeneratedMongoDBOfflineCommand(t, harness, project, poisonURL, "generate", "--check")
	runGeneratedMongoDBOfflineCommand(t, harness, project, poisonURL, "migrate", "create", "--name", "initial")
	artifacts, err := migrationartifact.ReadAll(filepath.Join(project.root, "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("generated MongoDB %s initial history has %d artifacts, want 1", project.template, len(artifacts))
	}
	binary := harness.buildLinuxProject(t, project, "dist/mongodb-"+project.template+"-initial-linux")
	// build installs the generated frontend's release-shaped dependencies, so
	// check now exercises the complete generated project without a separate
	// package-manager shortcut.
	runGeneratedMongoDBOfflineCommand(t, harness, project, poisonURL, "check")
	assertGeneratedMongoDBServerFailsBeforeMigration(t, harness, project, binary, project.template+"-before-up")

	statusOutput := runGeneratedMongoDBLiveCommand(t, harness, project, project.database.hostURL, 45*time.Second, "migrate", "status", "--json")
	statuses := parseMongoDBProductionDeploymentStatuses(t, statusOutput)
	assertMongoDBProductionDeploymentStatus(t, statuses, artifacts[0].Name, false, false)
	verifyURL := harness.fixture.seedURL(harness.fixture.caPath, true)
	runGeneratedMongoDBVerifiedCommand(t, harness, project, verifyURL, harness.fixture.adminUser, harness.fixture.adminPassword, 2*time.Minute, "migrate", "verify")
	runGeneratedMongoDBLiveCommand(t, harness, project, project.database.hostURL, 2*time.Minute, "migrate", "up")
	statusOutput = runGeneratedMongoDBLiveCommand(t, harness, project, project.database.hostURL, 45*time.Second, "migrate", "status", "--json")
	statuses = parseMongoDBProductionDeploymentStatuses(t, statusOutput)
	assertMongoDBProductionDeploymentStatus(t, statuses, artifacts[0].Name, true, false)
	return binary, artifacts[0]
}

func runGeneratedMongoDBOfflineCommand(
	t *testing.T,
	harness *mongoDBProductionDeploymentHarness,
	project mongoDBProductionDeploymentProject,
	poisonURL string,
	arguments ...string,
) string {
	t.Helper()
	output, err := harness.run(8*time.Minute, project.root, map[string]string{"DATABASE_URL": poisonURL}, harness.cliBinary, arguments...)
	secrets := []string{poisonURL, "offline-release-user", "offline-release-secret", "offline-schema-user", "offline-schema-secret"}
	if err != nil {
		t.Fatalf("offline ridu %s for generated MongoDB %s project: %v\n%s", strings.Join(arguments, " "), project.template, err, mongoDBProductionRedacted(output, secrets...))
	}
	mongoDBProductionAssertSecretsAbsent(t, output, secrets...)
	return output
}

func runGeneratedMongoDBLiveCommand(
	t *testing.T,
	harness *mongoDBProductionDeploymentHarness,
	project mongoDBProductionDeploymentProject,
	databaseURL string,
	timeout time.Duration,
	arguments ...string,
) string {
	t.Helper()
	return runGeneratedMongoDBVerifiedCommand(t, harness, project, databaseURL, project.database.username, project.database.password, timeout, arguments...)
}

func runGeneratedMongoDBVerifiedCommand(
	t *testing.T,
	harness *mongoDBProductionDeploymentHarness,
	project mongoDBProductionDeploymentProject,
	databaseURL string,
	username string,
	password string,
	timeout time.Duration,
	arguments ...string,
) string {
	t.Helper()
	output, err := harness.run(timeout, project.root, map[string]string{"DATABASE_URL": databaseURL}, harness.cliBinary, arguments...)
	secrets := project.database.secrets(databaseURL, username, password)
	if err != nil {
		t.Fatalf("ridu %s for generated MongoDB %s project: %v\n%s", strings.Join(arguments, " "), project.template, err, mongoDBProductionRedacted(output, secrets...))
	}
	mongoDBProductionAssertSecretsAbsent(t, output, secrets...)
	return output
}

func assertGeneratedMongoDBVerifyRequiresOperationalAuthority(
	t *testing.T,
	harness *mongoDBProductionDeploymentHarness,
	project mongoDBProductionDeploymentProject,
) {
	t.Helper()
	output, err := harness.run(
		2*time.Minute,
		project.root,
		map[string]string{"DATABASE_URL": project.database.hostURL},
		harness.cliBinary,
		"migrate", "verify",
	)
	secrets := project.database.secrets()
	mongoDBProductionAssertSecretsAbsent(t, output, secrets...)
	if err == nil {
		t.Fatal("generated MongoDB application role unexpectedly verified history in an operational shadow database")
	}
	lower := strings.ToLower(output)
	authorityDenied := strings.Contains(lower, "not authorized") ||
		strings.Contains(lower, "unauthorized") ||
		strings.Contains(lower, "authorization") ||
		strings.Contains(lower, "server code 13")
	if !authorityDenied || !strings.Contains(lower, "shadow") {
		t.Fatalf(
			"generated MongoDB application-role verification did not fail at the shadow-database authority boundary: %v\n%s",
			err,
			mongoDBProductionRedacted(output, secrets...),
		)
	}
}

func assertGeneratedMongoDBVerifySucceeded(t *testing.T, output string) {
	t.Helper()
	const (
		artifactReplay = "Migration history replays cleanly in an isolated shadow database."
		projectReplay  = "Migration history and compiled data transforms replay cleanly in an isolated MongoDB database."
	)
	if !strings.Contains(output, artifactReplay) && !strings.Contains(output, projectReplay) {
		t.Fatalf("generated MongoDB privileged verification did not report a successful isolated replay: %s", output)
	}
}

func assertGeneratedMongoDBServerFailsBeforeMigration(
	t *testing.T,
	harness *mongoDBProductionDeploymentHarness,
	project mongoDBProductionDeploymentProject,
	binary string,
	label string,
) {
	t.Helper()
	server := harness.startServer(t, project, binary, project.database.containerURL, label)
	output, exitErr, observationErr := server.expectedFailure(25 * time.Second)
	secrets := project.database.secrets()
	if observationErr != nil {
		t.Fatalf("generated MongoDB deployment did not exit after readiness rejection: %v\n%s", observationErr, mongoDBProductionRedacted(output, secrets...))
	}
	if exitErr == nil {
		t.Fatalf("generated MongoDB deployment started before exact migrations were applied:\n%s", mongoDBProductionRedacted(output, project.database.secrets()...))
	}
	mongoDBProductionAssertSecretsAbsent(t, output, secrets...)
	lower := strings.ToLower(output)
	if !strings.Contains(lower, "migration") || (!strings.Contains(lower, "empty") && !strings.Contains(lower, "current") && !strings.Contains(lower, "history") && !strings.Contains(lower, "ledger head")) {
		t.Fatalf("generated MongoDB pre-migration startup did not fail on readiness (%v):\n%s", exitErr, mongoDBProductionRedacted(output, secrets...))
	}
	mongoDBProductionDeploymentWaitUnavailable(t, server.baseURL()+"/readyz", 3*time.Second)
}

func parseMongoDBProductionDeploymentStatuses(t *testing.T, output string) []MigrationStatus {
	t.Helper()
	var statuses []MigrationStatus
	if err := json.Unmarshal([]byte(output), &statuses); err != nil {
		t.Fatalf("decode generated MongoDB migration status: %v\n%s", err, output)
	}
	return statuses
}

func assertMongoDBProductionDeploymentStatus(
	t *testing.T,
	statuses []MigrationStatus,
	name string,
	wantApplied bool,
	wantProgress bool,
) {
	t.Helper()
	for _, status := range statuses {
		if status.Name != name {
			continue
		}
		if status.Applied != wantApplied {
			t.Fatalf("MongoDB deployment migration %s applied = %t, want %t: %#v", name, status.Applied, wantApplied, status)
		}
		progress := false
		for _, phase := range status.Phases {
			for _, step := range phase.Steps {
				progress = progress || step.State == mongoMigrationStepRunning || step.State == mongoMigrationStepComplete
				if wantApplied && step.State != mongoMigrationStepComplete {
					t.Fatalf("applied MongoDB deployment migration %s retained non-complete step state: %#v", name, status)
				}
			}
		}
		if !wantApplied && progress != wantProgress {
			t.Fatalf("MongoDB deployment migration %s durable progress = %t, want %t: %#v", name, progress, wantProgress, status)
		}
		return
	}
	t.Fatalf("MongoDB deployment status omitted migration %s: %#v", name, statuses)
}

func mongoDBProductionDeploymentAssertEmbeddedAdmin(t *testing.T, baseURL string) {
	t.Helper()
	client := mongoDBProductionDeploymentHTTPClient(t)
	encoded := mongoDBProductionDeploymentRequest(t, client, http.MethodGet, baseURL+"/admin/", nil, "", http.StatusOK, nil)
	if !strings.Contains(strings.ToLower(string(encoded)), "<!doctype html") {
		t.Fatalf("generated MongoDB production binary did not embed the built admin: %s", encoded)
	}
}

func (data mongoDBProductionDeploymentData) String() string {
	return fmt.Sprintf("user=%s post=%s media=%s object=%s", data.userID, data.postID, data.mediaID, data.objectKey)
}

func TestMongoDBProductionDeploymentWaitDistinguishesTimeoutFromProcessExit(t *testing.T) {
	timedOut := &mongoDBProductionDeploymentServer{name: "still-running", done: make(chan struct{})}
	if err := timedOut.wait(time.Millisecond); !errors.Is(err, errMongoDBProductionDeploymentExitTimeout) {
		t.Fatalf("deployment wait timeout = %v, want classified timeout", err)
	}

	exitErr := errors.New("exit status 1")
	exited := &mongoDBProductionDeploymentServer{name: "exited", done: make(chan struct{}), waitErr: exitErr}
	close(exited.done)
	if err := exited.wait(time.Second); !errors.Is(err, exitErr) || errors.Is(err, errMongoDBProductionDeploymentExitTimeout) {
		t.Fatalf("deployment process exit = %v, want %v without timeout classification", err, exitErr)
	}
}

func TestMongoDBProductionDeploymentCredentialIdentityDoesNotOverlapNamespace(t *testing.T) {
	database, username := mongoDBProductionDeploymentDatabaseIdentity("starter", "0123456789ab")
	if strings.Contains(database, username) {
		t.Fatalf("MongoDB deployment username %q overlaps printed database namespace %q", username, database)
	}
}

func TestMongoDBProductionDeploymentImageRequiresExactDigest(t *testing.T) {
	composePath := filepath.Join("testdata", "production", "compose.yaml")
	mongoDBProductionDeploymentImage(t, composePath)

	tests := []struct {
		name    string
		compose string
		valid   bool
	}{
		{
			name:    "qualified image",
			compose: "x-member: &member\n  image: " + mongoDBProductionFixtureImage + "\n",
			valid:   true,
		},
		{
			name:    "tag only",
			compose: "x-member: &member\n  image: mongo:8.2.9-noble\n",
		},
		{
			name:    "wrong digest",
			compose: "x-member: &member\n  image: mongo:8.2.9-noble@sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff\n",
		},
		{
			name:    "mixed override",
			compose: "x-member: &member\n  image: " + mongoDBProductionFixtureImage + "\nservices:\n  mongodb-3:\n    image: mongo:latest\n",
		},
		{
			name:    "missing image",
			compose: "services: {}\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			image, err := mongoDBProductionDeploymentImageFromCompose(test.compose)
			if test.valid {
				if err != nil {
					t.Fatalf("qualified production image was rejected: %v", err)
				}
				if image != mongoDBProductionFixtureImage {
					t.Fatalf("qualified production image = %q, want %q", image, mongoDBProductionFixtureImage)
				}
				return
			}
			if err == nil {
				t.Fatalf("unqualified production image %q was accepted", image)
			}
		})
	}
}
