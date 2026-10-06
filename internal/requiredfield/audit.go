package requiredfield

import (
	"fmt"
	"sort"
	"strings"

	"github.com/riducms/ridu/internal/blockgraph"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// maxSamples bounds the document IDs an error names for one finding.
const maxSamples = 5

// Audit counts the stored documents in which each requirement has no value.
// Adapters feed it every stored row that must be complete; a document stored
// in several rows, such as a working and a published row, counts once.
type Audit struct {
	requirements []Requirement
	byResource   map[schema.StableID]auditedResource
	findings     map[findingKey]*finding
}

// auditedResource is one resource's fields and the requirements its stored
// documents must meet, each with a reusable inspection.
type auditedResource struct {
	fields       []schema.Field
	requirements []int
	inspections  []*inspection
}

type findingKey struct {
	requirement int
	locale      schema.LocaleCode
	everyLocale bool
}

type finding struct {
	documents map[string]struct{}
	samples   []string
}

// NewAudit prepares an audit of requirements.
func NewAudit(requirements []Requirement) *Audit {
	audit := &Audit{requirements: requirements, byResource: make(map[schema.StableID]auditedResource), findings: make(map[findingKey]*finding)}
	for _, group := range groups(requirements) {
		audited := auditedResource{fields: group.Resource.Fields, requirements: group.indexes}
		for _, index := range group.indexes {
			audited.inspections = append(audited.inspections, &inspection{requirement: requirements[index], walkers: make(map[string]*blockgraph.Walker)})
		}
		audit.byResource[group.Resource.ID] = audited
	}
	return audit
}

// Inspect records the requirements of resource that one stored row of
// document leaves without a value. Values hold at least every requirement
// root, in the after schema's stored shape.
func (audit *Audit) Inspect(resource schema.StableID, document string, values store.Values) {
	audited := audit.byResource[resource]
	lookup := func(name string) (store.Value, bool) {
		value, exists := values[name]
		return value, exists
	}
	for position, index := range audited.requirements {
		inspection := audited.inspections[position]
		inspection.report = func(locale schema.LocaleCode, everyLocale bool) {
			key := findingKey{requirement: index, locale: locale, everyLocale: everyLocale}
			current := audit.findings[key]
			if current == nil {
				current = &finding{documents: make(map[string]struct{})}
				audit.findings[key] = current
			}
			if _, counted := current.documents[document]; counted {
				return
			}
			current.documents[document] = struct{}{}
			if len(current.samples) < maxSamples {
				current.samples = append(current.samples, document)
			}
		}
		inspection.object(audited.fields, lookup, inspection.requirement.Steps, "", true)
	}
}

// Err returns nil when every inspected document has every required value.
// Inspected names what the adapter read; development selects the remedy for
// development synchronization instead of an immutable migration.
func (audit *Audit) Err(inspected string, development bool) error {
	if len(audit.findings) == 0 {
		return nil
	}
	keys := make([]findingKey, 0, len(audit.findings))
	for key := range audit.findings {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool {
		a, b := keys[left], keys[right]
		if a.requirement != b.requirement {
			return a.requirement < b.requirement
		}
		if a.everyLocale != b.everyLocale {
			return a.everyLocale
		}
		return a.locale < b.locale
	})
	failure := &MissingValuesError{Inspected: inspected, Development: development}
	for _, key := range keys {
		current := audit.findings[key]
		failure.Findings = append(failure.Findings, Finding{
			Address: audit.requirements[key.requirement].Address(), Locale: key.locale, EveryLocale: key.everyLocale,
			Documents: len(current.documents), Samples: append([]string(nil), current.samples...),
		})
	}
	return failure
}

// Finding is one newly required field, in one locale when it is localized,
// that stored documents leave without a value.
type Finding struct {
	Address string
	// Locale is set when a translation exists but is empty, or when a
	// localized container's translation lacks the field.
	Locale schema.LocaleCode
	// EveryLocale is set when a localized field has no translation at all.
	EveryLocale bool
	Documents   int
	// Samples are up to five of those documents' IDs.
	Samples []string
}

// MissingValuesError refuses a schema change that would require a field that
// stored documents leave without a value.
type MissingValuesError struct {
	Findings    []Finding
	Inspected   string
	Development bool
}

func (failure *MissingValuesError) Error() string {
	var builder strings.Builder
	builder.WriteString(Code)
	builder.WriteString(": stored documents have no value for fields that become required: ")
	for index, finding := range failure.Findings {
		if index > 0 {
			builder.WriteString("; ")
		}
		builder.WriteString(finding.Address)
		switch {
		case finding.EveryLocale:
			builder.WriteString(" (no translation in any locale)")
		case finding.Locale != "":
			fmt.Fprintf(&builder, " (locale %s)", finding.Locale)
		}
		noun := "documents"
		if finding.Documents == 1 {
			noun = "document"
		}
		fmt.Fprintf(&builder, " in %d %s, for example %s", finding.Documents, noun, strings.Join(finding.Samples, ", "))
	}
	builder.WriteString(". ")
	if failure.Inspected != "" {
		builder.WriteString("The audit read ")
		builder.WriteString(failure.Inspected)
		builder.WriteString(". ")
	}
	if failure.Development {
		builder.WriteString("Kept the current schema and every stored value. Keep these fields optional until the listed documents have values, then make them required; or keep them optional.")
	} else {
		builder.WriteString("Write the missing values before the fields become required: create the migration with a compiled data transform that backfills them (ridu migrate create <name> --transform <transform>); a versioned resource needs the backfill in an earlier data-only migration, because a transform cannot change its fields. Or keep the fields optional.")
	}
	return builder.String()
}
