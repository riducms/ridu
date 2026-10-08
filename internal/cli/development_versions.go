package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/riducms/ridu/internal/enableversions"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/internal/projectfile"
	"github.com/riducms/ridu/internal/schemadiff"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

// resolveVersions settles a collection or global that starts keeping versions
// in the saved config. While the development database stores nothing for any
// of them, schema sync enables versions and nothing is asked. Otherwise the
// developer chooses what the stored documents become, and the choice is
// written into a migration and applied, like an accepted rename. A rejected
// or cancelled choice keeps the accepted schema and every stored document,
// and the question comes up again on the next save.
func (renames *developmentRenames) resolveVersions(ctx context.Context, definition projectfile.File, current schema.Manifest, fresh func() bool) error {
	previous, exists, err := renames.baseline(ctx, definition)
	if err != nil {
		return fmt.Errorf("read accepted development schema: %w", err)
	}
	if !exists {
		return nil
	}
	enabled := migration.VersionsEnabled(previous.Snapshot(), current.Snapshot(), nil)
	if len(enabled) == 0 {
		return nil
	}
	target, err := renames.openTarget(ctx, definition.Database)
	if err != nil {
		return err
	}
	defer target.close()
	reports, err := target.reviewVersions(ctx, previous, current)
	if err != nil {
		return fmt.Errorf("count the documents of collections that start keeping versions: %w", err)
	}
	var stored []enableversions.Report
	for _, report := range reports {
		if report.Documents != 0 {
			stored = append(stored, report)
		}
	}
	if len(stored) == 0 {
		return nil
	}
	if len(schemadiff.RenameCandidates(previous, current)) != 0 {
		// One migration either moves renamed content or converts documents.
		return fmt.Errorf("enabling versions over stored documents cannot share a save with a possible rename; restore one of the changes and settle them in separate saves")
	}
	if renames.input == nil {
		return developmentRenameHeldError{enableversions.StoredDocumentsError(stored[0].Resource, stored[0].Documents)}
	}
	if definition.Migrations == "" {
		return fmt.Errorf("%w; ridu dev records that decision in a migration, and this project configures no migrations directory", enableversions.StoredDocumentsError(stored[0].Resource, stored[0].Documents))
	}
	choices := make(enableversions.Choices, len(reports))
	for _, report := range reports {
		if report.Documents == 0 {
			// Nothing here needs a decision, so none is made for the databases
			// the migration later reaches: ridu migrate up stops if one of them
			// stores documents.
			choices[report.Resource.ID] = migration.ExistingRequireEmpty
			renames.output.Info(fmt.Sprintf("%s stores no documents here; the migration requires it to be empty wherever it runs", capitalize(enableversions.Describe(report.Resource))))
			continue
		}
		existing, err := renames.chooseExistingDocuments(ctx, report)
		if err != nil {
			return err
		}
		choices[report.Resource.ID] = existing
	}
	name, err := renames.migrationName(ctx, developmentVersionsName(reports))
	if err != nil {
		return err
	}
	if fresh != nil && !fresh() {
		// The config changed while the question waited, so the answers describe
		// a change that may no longer exist.
		return errDevelopmentSourceChanged
	}
	return renames.migrate(ctx, definition, previous, current, developmentSettlement{existing: choices}, name)
}

// syncedVersions records require-empty for each resource that starts keeping
// versions between base and the development schema. Only schema sync enabled
// them, and it does so only while a resource stores nothing, which is what
// require-empty requires wherever the migration runs.
func syncedVersions(base *schema.Manifest, development schema.Manifest) enableversions.Choices {
	if base == nil {
		return nil
	}
	choices := make(enableversions.Choices)
	for _, resource := range migration.VersionsEnabled(base.Snapshot(), development.Snapshot(), nil) {
		choices[resource.ID] = migration.ExistingRequireEmpty
	}
	return choices
}

// chooseExistingDocuments asks what one resource's stored documents become.
// Like every development question, an empty line is not an answer.
func (renames *developmentRenames) chooseExistingDocuments(ctx context.Context, report enableversions.Report) (migration.ExistingDocuments, error) {
	resource := report.Resource
	drafts := resource.Versions != nil && resource.Versions.Drafts
	fmt.Fprintf(renames.prompt, "%s starts keeping versions and stores %s here. What do they become?\n", capitalize(enableversions.Describe(resource)), documentCount(report.Documents))
	fmt.Fprintln(renames.prompt, "  published  publish each one with a matching draft, so readers keep seeing it (recommended)")
	choices := "published or cancel"
	if drafts {
		fmt.Fprintln(renames.prompt, "  draft      keep each one as an unpublished draft that readers no longer see until it is published")
		choices = "published, draft or cancel"
	}
	fmt.Fprintln(renames.prompt, "  cancel     keep the current schema running and change nothing")
	for {
		answer, err := renames.ask(ctx, fmt.Sprintf("Choose %s: ", choices))
		if err != nil {
			return "", err
		}
		switch strings.ToLower(answer) {
		case "published", "p":
			return migration.ExistingPublished, nil
		case "draft", "d":
			if drafts {
				return migration.ExistingDraft, nil
			}
		case "cancel", "c":
			return "", fmt.Errorf("enabling versions cancelled; kept the current schema and all stored documents")
		}
		fmt.Fprintf(renames.prompt, "Enter %s.\n", choices)
	}
}

// developmentVersionsRecorded checks that a migration records exactly the
// choices the developer made.
func developmentVersionsRecorded(file migrationartifact.File, existing enableversions.Choices) error {
	recorded, err := enableversions.Recorded(file.Artifact)
	if err != nil {
		return err
	}
	matches := len(recorded) == len(existing)
	for id, choice := range existing {
		matches = matches && recorded[id] == choice
	}
	if !matches {
		return fmt.Errorf("migration %s already reaches this config without recording these choices about existing documents; cancel, or replace that migration", file.Name)
	}
	return nil
}

func (target *developmentRenameTarget) reviewVersions(ctx context.Context, before, after schema.Manifest) ([]enableversions.Report, error) {
	switch {
	case target.sqlite != nil:
		return target.sqlite.ReviewDevelopmentVersions(ctx, before, after)
	case target.mongodb != nil:
		return target.mongodb.ReviewDevelopmentVersions(ctx, before, after)
	default:
		return target.postgres.ReviewDevelopmentVersions(ctx, before, after)
	}
}

// developmentVersionsName suggests a migration name such as
// enable-versions-posts.
func developmentVersionsName(reports []enableversions.Report) string {
	words := []string{"enable", "versions", string(reports[0].Resource.Slug)}
	if len(reports) > 1 {
		words = append(words, "and-more")
	}
	name := strings.Trim(migrationNameSeparators.ReplaceAllString(strings.ToLower(strings.Join(words, "-")), "-"), "-")
	if !migrationNamePattern.MatchString(name) {
		return "enable-versions"
	}
	return name
}

func documentCount(documents int64) string {
	if documents == 1 {
		return "1 document"
	}
	return fmt.Sprintf("%d documents", documents)
}
