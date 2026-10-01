package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/riducms/ridu/internal/fieldchange"
	"github.com/riducms/ridu/internal/projectfile"
	"github.com/riducms/ridu/internal/schemadiff"
	"github.com/riducms/ridu/schema"
)

// resolveFieldKinds runs before generation and rename settlement, so rejecting
// a candidate keeps the accepted executable and its generated contracts intact.
func (renames *developmentRenames) resolveFieldKinds(ctx context.Context, definition projectfile.File, current schema.Manifest, syncSchema bool, fresh func() bool) error {
	previous, exists, err := renames.baseline(ctx, definition)
	if err != nil {
		return fmt.Errorf("read accepted development schema: %w", err)
	}
	if !exists {
		return nil
	}
	changes := fieldchange.Detect(previous.Snapshot(), current.Snapshot())
	if len(changes) == 0 {
		return nil
	}
	if !syncSchema {
		return fmt.Errorf("RIDU_FIELD_KIND_CHANGE_REQUIRES_TRANSFORM: --no-sync cannot accept a field-kind change; restore the old schema or review the change with ordinary ridu dev or ridu migrate")
	}
	target, err := renames.openTarget(ctx, definition.Database)
	if err != nil {
		return err
	}
	defer target.close()
	reports, err := target.reviewFieldKinds(ctx, previous, current)
	if err != nil {
		return fmt.Errorf("inspect stored field values and version snapshots: %w", err)
	}
	affected := false
	for _, report := range reports {
		if report.Documents == 0 && report.Snapshots == 0 {
			continue
		}
		affected = true
		renames.output.Warn(fmt.Sprintf("%s changes from %s to %s; %d current documents and %d version snapshots contain stored values", fieldchange.Description(report), report.Before, report.After, report.Documents, report.Snapshots), nil)
	}
	if !affected {
		if fresh != nil && !fresh() {
			return errDevelopmentSourceChanged
		}
		// The old server could write between this inspection and publication.
		// Drain it before generation, then verify emptiness again while no old
		// schema writer remains. A raced value rejects rather than certifies.
		drained := renames.stopRunningServer()
		latest, err := target.reviewFieldKinds(ctx, previous, current)
		if err != nil {
			return renames.resumeAfterFieldKindRejection(fmt.Errorf("recheck stored field values after stopping the old server: %w", err), drained)
		}
		if err := fieldchange.RequireEmpty(latest); err != nil {
			return renames.resumeAfterFieldKindRejection(fmt.Errorf("%w; stored values appeared while the old development server drained; schema and content remain unchanged; save again to review them", err), drained)
		}
		renames.fieldKindDrained = renames.fieldKindDrained || drained
		return nil
	}
	alternative := fieldKindAlternative(changes)
	if renames.input == nil {
		return fmt.Errorf("RIDU_FIELD_KIND_CHANGE_REQUIRES_TRANSFORM: stored values of the changed fields keep the current schema running; run ridu dev in a terminal to review clearing them, or restore the previous field kind. %s", alternative)
	}
	clearReason := fieldchange.ValidateClear(changes)
	if clearReason == nil {
		managed, err := target.hasFieldKindHistory(ctx)
		if err != nil {
			return fmt.Errorf("inspect development migration history: %w", err)
		}
		if managed {
			clearReason = fmt.Errorf("this database has immutable migration history, so clearing must use a reviewed migration")
		}
	}
	if clearReason == nil && len(schemadiff.RenameCandidates(previous, current)) != 0 {
		clearReason = fmt.Errorf("field-kind recovery cannot share a save with renames; settle the changes in separate saves")
	}
	if clearReason != nil {
		return fmt.Errorf("RIDU_FIELD_KIND_CHANGE_REQUIRES_TRANSFORM: clearing is unavailable: %w; kept the current schema and all stored values; restore the previous field kind. %s", clearReason, alternative)
	}
	fmt.Fprintln(renames.prompt, alternative)
	for {
		answer, err := renames.ask(ctx, "Choose clear (permanently removes these field values from current documents AND all version snapshots) or cancel (keeps the current schema running): ")
		if err != nil {
			return err
		}
		if fresh != nil && !fresh() {
			return errDevelopmentSourceChanged
		}
		switch strings.ToLower(answer) {
		case "cancel", "c", "":
			return fmt.Errorf("field-kind change cancelled; kept the current schema and all stored values")
		case "clear":
			confirmed, err := renames.confirmFieldKindClear(ctx)
			if err != nil {
				return err
			}
			if !confirmed {
				return fmt.Errorf("field-kind change cancelled; kept the current schema and all stored values")
			}
			if fresh != nil && !fresh() {
				return errDevelopmentSourceChanged
			}
			drained := renames.stopRunningServer()
			if err := target.clearFieldKinds(ctx, previous, current, reports); err != nil {
				return renames.resumeAfterFieldKindRejection(fmt.Errorf("clear stored field values: %w; schema and content remain unchanged", err), drained)
			}
			renames.fieldKindDrained = false
			renames.output.Info("Cleared changed field values from current documents and version snapshots")
			return nil
		default:
			fmt.Fprintln(renames.prompt, "Enter clear or cancel.")
		}
	}
}

// fieldKindAlternative says how to keep stored values instead of clearing
// them. Generic transforms cannot rewrite retained version snapshots.
func fieldKindAlternative(changes []fieldchange.Change) string {
	line := "To keep the values, restore the previous field kind, then add a field with the new kind and copy the values into it in a reviewed migration with a compiled data transform (ridu migrate create --transform <name>, then ridu migrate up)"
	var versioned []string
	for _, resource := range fieldchange.AffectedResources(changes) {
		if resource.Versions != nil || resource.Capabilities.Versions {
			versioned = append(versioned, string(resource.Slug))
		}
	}
	if len(versioned) != 0 {
		line += "; data transforms cannot yet change fields of versioned collections such as " + strings.Join(versioned, ", ")
	}
	return line + "."
}

// resumeAfterRejectedReload restarts the accepted server when only the
// field-kind review drained it and nothing has changed stored content since.
func (renames *developmentRenames) resumeAfterRejectedReload(rejection error) error {
	if renames == nil || !renames.fieldKindDrained {
		return rejection
	}
	return renames.resumeAfterFieldKindRejection(fmt.Errorf("%w; the accepted schema record and stored content remain unchanged", rejection), true)
}

func (renames *developmentRenames) resumeAfterFieldKindRejection(rejection error, drained bool) error {
	drained = drained || renames.fieldKindDrained
	renames.fieldKindDrained = false
	if !drained || renames.resumeServer == nil {
		return rejection
	}
	if err := renames.resumeServer(); err != nil {
		return fmt.Errorf("%w; could not restart the accepted development server: %v", rejection, err)
	}
	renames.output.Info("Restarted the accepted development server without changing its schema")
	return rejection
}

func (renames *developmentRenames) confirmFieldKindClear(ctx context.Context) (bool, error) {
	for {
		answer, err := renames.ask(ctx, "Permanently clear the field values from the displayed current documents and version snapshots? [y/n] ")
		if err != nil {
			return false, err
		}
		switch strings.ToLower(answer) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Fprintln(renames.prompt, "Answer y to permanently remove these field values from current documents and every version snapshot, or n to cancel.")
		}
	}
}

func (target *developmentRenameTarget) reviewFieldKinds(ctx context.Context, before, after schema.Manifest) ([]fieldchange.Report, error) {
	switch {
	case target.sqlite != nil:
		return target.sqlite.ReviewDevelopmentFieldKinds(ctx, before, after)
	case target.mongodb != nil:
		return target.mongodb.ReviewDevelopmentFieldKinds(ctx, before, after)
	default:
		return target.postgres.ReviewDevelopmentFieldKinds(ctx, before, after)
	}
}

func (target *developmentRenameTarget) clearFieldKinds(ctx context.Context, before, after schema.Manifest, reports []fieldchange.Report) error {
	switch {
	case target.sqlite != nil:
		return target.sqlite.ClearDevelopmentFieldKinds(ctx, before, after, reports)
	case target.mongodb != nil:
		return target.mongodb.ClearDevelopmentFieldKinds(ctx, before, after, reports)
	default:
		return target.postgres.ClearDevelopmentFieldKinds(ctx, before, after, reports)
	}
}

func (target *developmentRenameTarget) hasFieldKindHistory(ctx context.Context) (bool, error) {
	switch {
	case target.sqlite != nil:
		return target.sqlite.HasMigrationHistory(ctx)
	case target.mongodb != nil:
		return target.mongodb.HasMigrationHistory(ctx)
	default:
		return target.postgres.HasMigrationHistory(ctx)
	}
}
