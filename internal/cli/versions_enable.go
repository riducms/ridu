package cli

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/riducms/ridu/internal/enableversions"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

// migrateCreateInput is the one reader every ridu migrate create question
// reads from, so answers piped for one question are not buffered away from
// the next.
func migrateCreateInput(options Options) io.Reader {
	if options.Stdin == nil {
		return nil
	}
	return bufio.NewReader(options.Stdin)
}

// chooseExistingDocuments decides what becomes of the stored documents of each
// resource that starts keeping versions between the latest migration and
// current. flag answers for every such resource; otherwise each is asked
// about. Without either the planner reports which choice is missing.
func chooseExistingDocuments(previous schema.Manifest, previousExists bool, current schema.Manifest, flag string, input io.Reader, output io.Writer) (map[schema.StableID]migration.ExistingDocuments, error) {
	var enabled []schema.Collection
	if previousExists {
		enabled = migration.VersionsEnabled(previous.Snapshot(), current.Snapshot(), nil)
	}
	if flag != "" {
		existing, err := migration.ParseExistingDocuments(flag)
		if err != nil {
			return nil, fmt.Errorf("--versions-existing: %w", err)
		}
		if len(enabled) == 0 {
			return nil, fmt.Errorf("--versions-existing was given, but no collection or global starts keeping versions in this migration")
		}
		choices := make(map[schema.StableID]migration.ExistingDocuments, len(enabled))
		for _, resource := range enabled {
			if err := enableversions.Validate(resource, existing); err != nil {
				return nil, err
			}
			choices[resource.ID] = existing
		}
		return choices, nil
	}
	if len(enabled) == 0 || input == nil {
		return nil, nil
	}
	reader := bufio.NewReader(input)
	choices := make(map[schema.StableID]migration.ExistingDocuments, len(enabled))
	for _, resource := range enabled {
		fmt.Fprintf(output, "%s starts keeping versions. What do the documents it already stores become?\n", capitalize(enableversions.Describe(resource)))
		fmt.Fprint(output, existingDocumentsOptions(resource))
		for {
			fmt.Fprintf(output, "Choose %s [published]: ", existingDocumentsChoices(resource))
			answer, err := reader.ReadString('\n')
			if err != nil && answer == "" {
				return nil, fmt.Errorf("read what existing documents become: %w; pass --versions-existing=published, draft or require-empty", err)
			}
			answer = strings.ToLower(strings.TrimSpace(answer))
			if answer == "" {
				answer = string(migration.ExistingPublished)
			}
			existing, err := migration.ParseExistingDocuments(answer)
			if err == nil {
				err = enableversions.Validate(resource, existing)
			}
			if err != nil {
				fmt.Fprintf(output, "%v.\n", err)
				continue
			}
			choices[resource.ID] = existing
			break
		}
	}
	return choices, nil
}

// existingDocumentsOptions explains each choice resource can take.
func existingDocumentsOptions(resource schema.Collection) string {
	lines := "  published      publish each one with a matching draft, so readers keep seeing it (recommended)\n"
	if resource.Versions != nil && resource.Versions.Drafts {
		lines += "  draft          keep each one as an unpublished draft that readers no longer see until it is published\n"
	}
	return lines + "  require-empty  make ridu migrate up stop while it stores any document\n"
}

func existingDocumentsChoices(resource schema.Collection) string {
	if resource.Versions != nil && resource.Versions.Drafts {
		return "published, draft or require-empty"
	}
	return "published or require-empty"
}

func capitalize(text string) string {
	if text == "" {
		return text
	}
	return strings.ToUpper(text[:1]) + text[1:]
}
