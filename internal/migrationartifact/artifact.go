// Package migrationartifact owns atomic, immutable migration artifact files.
package migrationartifact

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

// Extension distinguishes Ridu artifacts from arbitrary JSON.
const Extension = ".ridu.json"

var (
	namePattern         = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	filePattern         = regexp.MustCompile(`^[0-9]{14}\.[0-9]{9}_([a-z][a-z0-9-]*)` + regexp.QuoteMeta(Extension) + `$`)
	artifactLikePattern = regexp.MustCompile(`^[0-9]{14}(?:\.[0-9]{9})?_[a-z][a-z0-9-]*(?:\.[a-z0-9-]+)+$`)
)

// File contains one parsed immutable migration and its filesystem identity.
type File struct {
	Path     string
	Name     string
	Digest   string
	Artifact migration.Artifact
}

// Create atomically creates one migration artifact and never overwrites an
// existing filename.
func Create(directory, name string, artifact migration.Artifact, now time.Time) (File, error) {
	if !namePattern.MatchString(name) || artifact.Name != name {
		return File{}, fmt.Errorf("migration name %q must match the artifact and be lowercase kebab-case", name)
	}
	if artifact.Version != migration.ArtifactVersion {
		return File{}, fmt.Errorf("unsupported migration artifact version %d; this build creates version %d", artifact.Version, migration.ArtifactVersion)
	}
	if err := artifact.Validate(); err != nil {
		return File{}, err
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return File{}, err
	}
	createLock, err := acquireCreateLock(directory)
	if err != nil {
		return File{}, err
	}
	defer createLock.Close()
	existing, err := ReadAll(directory)
	if err != nil {
		return File{}, err
	}
	if len(existing) == 0 {
		if artifact.Before != nil || artifact.FromDigest != "" || artifact.PreviousArtifactDigest != "" {
			return File{}, fmt.Errorf("first migration must start from an empty schema")
		}
	} else {
		previous := existing[len(existing)-1]
		if artifact.FromDigest != previous.Artifact.ToDigest {
			return File{}, fmt.Errorf("migration does not continue the latest committed manifest")
		}
		if artifact.PreviousArtifactDigest != "" && artifact.PreviousArtifactDigest != previous.Digest {
			return File{}, fmt.Errorf("migration does not continue the latest committed artifact")
		}
		artifact.PreviousArtifactDigest = previous.Digest
	}
	if err := artifact.Validate(); err != nil {
		return File{}, err
	}
	base := now.UTC().Format("20060102150405.000000000") + "_" + name + Extension
	if len(existing) != 0 && base <= existing[len(existing)-1].Name {
		return File{}, fmt.Errorf("migration filename %s would not sort after %s; correct the system clock and retry", base, existing[len(existing)-1].Name)
	}
	path := filepath.Join(directory, base)
	encoded, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return File{}, err
	}
	digest, err := artifact.Digest()
	if err != nil {
		return File{}, err
	}
	candidate := File{Name: base, Digest: digest, Artifact: artifact}
	if err := validatePluginMigrationHistory(append(append([]File(nil), existing...), candidate)); err != nil {
		return File{}, err
	}
	encoded = append(encoded, '\n')
	if err := publishArtifact(directory, path, encoded); err != nil {
		if os.IsExist(err) {
			return File{}, fmt.Errorf("migration artifact %s already exists; choose another name or retry", base)
		}
		return File{}, err
	}
	return File{Path: path, Name: base, Digest: digest, Artifact: artifact}, nil
}

// acquireCreateLock serializes the history-head check and immutable file link
// across processes. The lock lives in ignored .ridu state beneath the artifact
// directory, so it is shared by every process without becoming committed history.
func acquireCreateLock(directory string) (*flock.Flock, error) {
	lockDirectory := filepath.Join(directory, ".ridu")
	if err := os.MkdirAll(lockDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("create migration artifact creation-lock directory: %w", err)
	}
	creationLock := flock.New(filepath.Join(lockDirectory, "create.lock"), flock.SetPermissions(0o600))
	locked, err := creationLock.TryLock()
	if err != nil {
		_ = creationLock.Close()
		return nil, fmt.Errorf("acquire migration artifact creation lock: %w", err)
	}
	if !locked {
		_ = creationLock.Close()
		return nil, fmt.Errorf("migration artifact directory is busy; retry artifact creation")
	}
	return creationLock, nil
}

// publishArtifact writes and syncs a complete same-directory temporary file,
// then installs it with a hard link. Link creation is one atomic no-replace
// operation: readers see either no final artifact or the complete file, and a
// concurrent creator cannot replace an existing immutable artifact.
func publishArtifact(directory, path string, encoded []byte) error {
	temporary, err := os.CreateTemp(directory, ".ridu-artifact-*.tmp")
	if err != nil {
		return fmt.Errorf("create migration artifact temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		if temporaryPath != "" {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o644); err != nil {
		return fmt.Errorf("set migration artifact permissions: %w", err)
	}
	if _, err := temporary.Write(encoded); err != nil {
		return fmt.Errorf("write migration artifact temporary file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync migration artifact temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close migration artifact temporary file: %w", err)
	}
	if err := os.Link(temporaryPath, path); err != nil {
		return err
	}
	if err := os.Remove(temporaryPath); err != nil {
		return fmt.Errorf("remove published migration artifact temporary file: %w", err)
	}
	temporaryPath = ""
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open migration artifact directory for sync: %w", err)
	}
	if err := directoryHandle.Sync(); err != nil {
		_ = directoryHandle.Close()
		return fmt.Errorf("sync migration artifact directory: %w", err)
	}
	if err := directoryHandle.Close(); err != nil {
		return fmt.Errorf("close migration artifact directory: %w", err)
	}
	return nil
}

// ReadAll parses, validates, and returns artifacts in lexical execution order.
func ReadAll(directory string) ([]File, error) {
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []File
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), Extension) {
			if artifactLikePattern.MatchString(entry.Name()) {
				return nil, fmt.Errorf("unsupported migration artifact filename %s; the migrations directory accepts only %s files", entry.Name(), Extension)
			}
			continue
		}
		matches := filePattern.FindStringSubmatch(entry.Name())
		if len(matches) != 2 {
			return nil, fmt.Errorf("migration artifact filename %s is invalid", entry.Name())
		}
		path := filepath.Join(directory, entry.Name())
		encoded, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		artifact, err := migration.DecodeArtifact(encoded)
		if err != nil {
			return nil, fmt.Errorf("parse migration artifact %s: %w", entry.Name(), err)
		}
		if artifact.Name != matches[1] {
			return nil, fmt.Errorf("migration artifact %s contains name %q", entry.Name(), artifact.Name)
		}
		digest, err := artifact.Digest()
		if err != nil {
			return nil, err
		}
		files = append(files, File{Path: path, Name: entry.Name(), Digest: digest, Artifact: artifact})
	}
	sort.Slice(files, func(left, right int) bool { return files[left].Name < files[right].Name })
	if len(files) != 0 && (files[0].Artifact.Before != nil || files[0].Artifact.FromDigest != "" || files[0].Artifact.PreviousArtifactDigest != "") {
		return nil, fmt.Errorf("first migration artifact %s must start from an empty schema", files[0].Name)
	}
	for index := 1; index < len(files); index++ {
		previous, current := files[index-1].Artifact, files[index].Artifact
		if current.PreviousArtifactDigest != files[index-1].Digest {
			return nil, fmt.Errorf("migration history predecessor differs between %s and %s", files[index-1].Name, files[index].Name)
		}
		if current.FromDigest != previous.ToDigest {
			return nil, fmt.Errorf("migration history is discontinuous between %s and %s", files[index-1].Name, files[index].Name)
		}
	}
	if err := validatePluginMigrationHistory(files); err != nil {
		return nil, err
	}
	return files, nil
}

func validatePluginMigrationHistory(files []File) error {
	seen := make(map[string]string)
	for _, file := range files {
		adapter, supported := pluginAdapterForPlanner(file.Artifact.Planner.Name)
		if !supported {
			continue
		}
		for _, plugin := range file.Artifact.After.Plugins {
			contribution, exists := plugin.DatabaseContribution(adapter)
			if !exists {
				continue
			}
			for _, pluginMigration := range contribution.Migrations {
				identity := string(adapter) + ":" + plugin.Key + ":" + fmt.Sprint(pluginMigration.Version)
				checksum := migration.PluginStepChecksum(adapter, plugin.Key, pluginMigration.Version, "up", pluginMigration.UpSQL) + ":" +
					migration.PluginStepChecksum(adapter, plugin.Key, pluginMigration.Version, "down", pluginMigration.DownSQL) + ":" + pluginMigration.Name
				if previous, exists := seen[identity]; exists && previous != checksum {
					return fmt.Errorf("plugin %s %s migration %d changed after publication in %s", plugin.Key, adapter, pluginMigration.Version, file.Name)
				}
				seen[identity] = checksum
			}
		}
	}
	return nil
}

func pluginAdapterForPlanner(planner string) (schema.PluginDatabaseAdapter, bool) {
	switch planner {
	case "atlas":
		return schema.PluginDatabaseAdapterPostgres, true
	case "ridu-sqlite":
		return schema.PluginDatabaseAdapterSQLite, true
	default:
		return "", false
	}
}

// LatestManifest returns the desired schema committed by the newest artifact.
func LatestManifest(directory string) (schema.Manifest, bool, error) {
	files, err := ReadAll(directory)
	if err != nil || len(files) == 0 {
		return schema.Manifest{}, false, err
	}
	manifest, err := files[len(files)-1].Artifact.AfterManifest()
	return manifest, err == nil, err
}

// RequireCurrentHistory proves that a deployable executable manifest is the
// exact head of a non-empty, immutable artifact history. An empty history is a
// valid input only to migration creation; release checks and database commands
// must fail closed until the initial artifact has been created and committed.
func RequireCurrentHistory(directory string, executable schema.Manifest) ([]File, error) {
	files, err := ReadAll(directory)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("migration artifact history is empty\ncreate the initial migration with `ridu migrate create --name initial`, review and commit the generated file, then rerun this command")
	}
	digest, err := migration.DigestManifest(executable)
	if err != nil {
		return nil, fmt.Errorf("digest executable manifest: %w", err)
	}
	latest := files[len(files)-1]
	if digest != latest.Artifact.ToDigest {
		return nil, fmt.Errorf("executable manifest digest %s does not match latest migration artifact %s digest %s; create and commit the missing migration", digest, latest.Name, latest.Artifact.ToDigest)
	}
	return files, nil
}

// RequireCurrentHistoryForPlanner additionally proves that every immutable
// artifact belongs to the planner family selected by the project database.
// Adapter runners retain ownership of planner-version and exact-plan checks.
func RequireCurrentHistoryForPlanner(directory string, executable schema.Manifest, plannerName string) ([]File, error) {
	files, err := RequireCurrentHistory(directory, executable)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(plannerName) == "" {
		return nil, fmt.Errorf("migration planner name is required")
	}
	for _, file := range files {
		if file.Artifact.Planner.Name != plannerName {
			return nil, fmt.Errorf("migration artifact %s uses planner %q instead of selected database planner %q", file.Name, file.Artifact.Planner.Name, plannerName)
		}
	}
	return files, nil
}
