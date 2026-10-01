package migrationartifact

import (
	"errors"
	"fmt"
	"strings"

	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

// ErrNoRecordedHistory refuses a replacement for a database without a ledger.
var ErrNoRecordedHistory = errors.New("no recorded history to replace; use ridu migrate baseline")

// ErrReplacementPartlyApplied refuses a replacement while recorded work is
// incomplete: only the original artifacts can finish it.
var ErrReplacementPartlyApplied = errors.New("a migration is partly applied; finish it with the original history before replacing the baseline")

// ReplacementHistories reads the replacement and the evidence for recorded
// history. A checksum alone cannot establish whether a deleted artifact ran data
// steps, so divergent histories must provide the original artifacts.
func ReplacementHistories(directory string, options migration.BaselineReplacementOptions) ([]File, []File, error) {
	files, err := ReadAll(directory)
	if err != nil {
		return nil, nil, err
	}
	if len(files) == 0 {
		return nil, nil, fmt.Errorf("replacement migration history is empty; create and commit an initial migration first")
	}
	previous := directory
	if options.PreviousDirectory != "" {
		previous = options.PreviousDirectory
	}
	old, err := ReadAll(previous)
	if err != nil {
		return nil, nil, fmt.Errorf("read previous history: %w", err)
	}
	return files, old, nil
}

// CheckReplacement refuses to forget data/semantic work and returns how many
// replacement artifacts the ledger can record. Recorded data/semantic artifacts
// must survive unchanged at the same history position. The first replacement
// artifact with such a step that the database has not recorded unchanged ends
// the recordable prefix: it and every later artifact stay pending for
// ridu migrate up.
func CheckReplacement(files, previous []File, applied int, blockingStep func(File) string) (int, error) {
	if applied == 0 {
		return 0, ErrNoRecordedHistory
	}
	if applied > len(previous) {
		return 0, PreviousHistoryError(fmt.Errorf("previous history contains fewer artifacts than the database records"))
	}
	recorded := func(index int) bool {
		return index < applied && index < len(files) && files[index].Name == previous[index].Name && files[index].Digest == previous[index].Digest
	}
	end := len(files)
	for index, file := range files {
		if blockingStep(file) != "" && !recorded(index) {
			end = index
			break
		}
	}
	for index, old := range previous[:applied] {
		if kind := blockingStep(old); kind != "" && (index >= end || !recorded(index)) {
			return 0, fmt.Errorf("cannot replace recorded migration %s: its %s step would be discarded; retain this artifact unchanged or restore the complete database recovery point", old.Name, kind)
		}
	}
	if end == 0 {
		return 0, fmt.Errorf("cannot baseline replacement migration %s: its %s step has not run on this database; begin the replacement history with schema-only migrations and move that step into a later migration for ridu migrate up", files[0].Name, blockingStep(files[0]))
	}
	return end, nil
}

// RemovedResources lists the collections and globals in the recorded head
// that the replacement head no longer has. A real migration retires their
// framework-owned state (versions, references, uniqueness); a ledger
// replacement cannot.
func RemovedResources(old, replacement schema.Snapshot) []schema.StableID {
	present := make(map[schema.StableID]bool, len(replacement.Collections)+len(replacement.Globals))
	for _, collection := range replacement.Collections {
		present[collection.ID] = true
	}
	for _, global := range replacement.Globals {
		present[global.ID] = true
	}
	var removed []schema.StableID
	for _, collection := range old.Collections {
		if !present[collection.ID] {
			removed = append(removed, collection.ID)
		}
	}
	for _, global := range old.Globals {
		if !present[global.ID] {
			removed = append(removed, global.ID)
		}
	}
	return removed
}

// RequireRetainedResources refuses a replacement that would claim the removal
// of a collection or global ran without retiring its framework-owned state.
func RequireRetainedResources(old, replacement schema.Snapshot) error {
	removed := RemovedResources(old, replacement)
	if len(removed) == 0 {
		return nil
	}
	names := make([]string, len(removed))
	for index, id := range removed {
		names[index] = string(id)
	}
	return fmt.Errorf("cannot replace recorded history: the replacement removes %s, whose stored documents, versions, and references only a migration retires; recreate this development database from the replacement history with ridu migrate up, or keep the recorded history and remove them with ridu migrate create and ridu migrate up", strings.Join(names, ", "))
}

// ReplacementSchemaDriftError explains a database whose physical schema does
// not match the replacement head the ledger would record.
func ReplacementSchemaDriftError(err error) error {
	return fmt.Errorf("replacement schema differs from the database; bring the database to the replacement's schema first (ridu dev for PostgreSQL or MongoDB, ridu migrate up of the previous history for SQLite), or keep the recorded history and create/apply a migration: %w", err)
}

// PreviousHistoryError explains how to recover the evidence needed to rewrite
// a divergent ledger safely without direct database edits.
func PreviousHistoryError(err error) error {
	return fmt.Errorf("cannot verify recorded history: %w; pass --previous-history <directory> containing the original immutable artifacts (recover them from Git or a backup)", err)
}

// SameRecordedHistory identifies a replacement which already exactly matches.
func SameRecordedHistory(files, previous []File, applied int) bool {
	if len(files) != applied {
		return false
	}
	for index, file := range files {
		if file.Name != previous[index].Name || file.Digest != previous[index].Digest {
			return false
		}
	}
	return true
}
