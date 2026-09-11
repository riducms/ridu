package postgres_test

import (
	"context"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

func TestPostgresReferenceOptionFiltersAreAtomicWithTargetReadAccess(t *testing.T) {
	ctx := context.Background()
	visiblePath, err := query.NewPath("visible")
	if err != nil {
		t.Fatal(err)
	}
	config := ridu.Config{
		Name: "PostgreSQL reference option filters",
		Collections: []ridu.Collection{
			{
				Slug: "people", Fields: field.Fields{field.Text("label"), field.Number("score"), field.Checkbox("visible").Required()},
				Access: ridu.CollectionAccess{Read: func(ridu.AccessContext) (ridu.AccessDecision, error) {
					return ridu.Where(query.Equal(visiblePath, query.Boolean(true))), nil
				}},
			},
			{
				Slug: "entries", Fields: field.Fields{field.Text("exactLabel"), field.Text("notLabel"), field.Text("likeNeedle"), field.Text("containsNeedle"), field.Number("gtThreshold"), field.Number("gteThreshold"), field.Number("ltThreshold"), field.Number("lteThreshold"), field.Text("lexicalThreshold"), field.Relationship("equalsRef", "people").FilterOptionRules(field.OptionFilter("label", field.FilterEquals, "exactLabel")), field.Relationship("notEqualRef", "people").FilterOptionRules(field.OptionFilter("label", field.FilterNotEquals, "notLabel")), field.Relationship("likeRef", "people").FilterOptionRules(field.OptionFilter("label", field.FilterLike, "likeNeedle")), field.Relationship("containsRef", "people").FilterOptionRules(field.OptionFilter("label", field.FilterContains, "containsNeedle")), field.Relationship("greaterThanRef", "people").FilterOptionRules(field.OptionFilter("score", field.FilterGreaterThan, "gtThreshold")), field.Relationship("greaterThanEqualRef", "people").FilterOptionRules(field.OptionFilter("score", field.FilterGreaterThanEqual, "gteThreshold")), field.Relationship("lessThanRef", "people").FilterOptionRules(field.OptionFilter("score", field.FilterLessThan, "ltThreshold")), field.Relationship("lessThanEqualRef", "people").FilterOptionRules(field.OptionFilter("score", field.FilterLessThanEqual, "lteThreshold")), field.Relationship("orderedTextRef", "people").FilterOptionRules(field.OptionFilter("label", field.FilterGreaterThan, "lexicalThreshold"))},
			},
		},
	}
	backend, manifest := integrationBackend(t, ctx, config)
	applyInitialArtifact(t, ctx, backend, manifest)
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	const matchingLabel = "Ångström Alpha_100% Café 東京"
	matching, err := application.Local().Create(ctx, "people", store.Values{
		"label": store.String(matchingLabel), "score": store.Number(10), "visible": store.Boolean(true),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	wildcardTrap, err := application.Local().Create(ctx, "people", store.Values{
		"label": store.String("Ångström AlphaX100Z Café 東京"), "score": store.Number(10), "visible": store.Boolean(true),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	hidden, err := application.Local().Create(ctx, "people", store.Values{
		"label": store.String(matchingLabel), "score": store.Number(10), "visible": store.Boolean(false),
	}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}

	base := store.Values{
		"exactLabel": store.String(matchingLabel), "notLabel": store.String("Other"),
		"likeNeedle": store.String("ALPHA_100% 東京"), "containsNeedle": store.String("100% Café"),
		"gtThreshold": store.Number(9), "gteThreshold": store.Number(10), "ltThreshold": store.Number(11), "lteThreshold": store.Number(10), "lexicalThreshold": store.String("Z"),
		"equalsRef": store.String(matching.ID), "notEqualRef": store.String(matching.ID),
		"likeRef": store.String(matching.ID), "containsRef": store.String(matching.ID),
		"greaterThanRef": store.String(matching.ID), "greaterThanEqualRef": store.String(matching.ID),
		"lessThanRef": store.String(matching.ID), "lessThanEqualRef": store.String(matching.ID),
		"orderedTextRef": store.String(matching.ID),
	}
	entry, err := application.Local().Create(ctx, "entries", base, ridu.MutationOptions{})
	if err != nil {
		t.Fatalf("valid PostgreSQL option-filter operators: %v", err)
	}

	for _, test := range []struct {
		name    string
		field   string
		value   store.Value
		refPath string
	}{
		{name: "equals", field: "exactLabel", value: store.String("Different"), refPath: "equalsRef"},
		{name: "not equals", field: "notLabel", value: store.String(matchingLabel), refPath: "notEqualRef"},
		{name: "like", field: "likeNeedle", value: store.String("ALPHA_100% missing"), refPath: "likeRef"},
		{name: "contains", field: "containsNeedle", value: store.String("missing"), refPath: "containsRef"},
		{name: "greater than", field: "gtThreshold", value: store.Number(10), refPath: "greaterThanRef"},
		{name: "greater than equal", field: "gteThreshold", value: store.Number(11), refPath: "greaterThanEqualRef"},
		{name: "less than", field: "ltThreshold", value: store.Number(10), refPath: "lessThanRef"},
		{name: "less than equal", field: "lteThreshold", value: store.Number(9), refPath: "lessThanEqualRef"},
		{name: "ordered Unicode text", field: "lexicalThreshold", value: store.String("🧭"), refPath: "orderedTextRef"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := application.Local().Update(ctx, "entries", entry.ID, store.Values{test.field: test.value}, ridu.MutationOptions{}); !hasRelationshipIssue(err, test.refPath) {
				t.Fatalf("PostgreSQL %s-filtered source-only update error = %v", test.name, err)
			}
		})
	}

	// PostgreSQL ILIKE treats '%' and '_' as wildcards unless they are escaped.
	// The admin/test-store semantics treat picker values literally, so this
	// trap must stay unavailable even though the rest of the text is similar.
	if _, err := application.Local().Create(ctx, "entries", store.Values{
		"likeNeedle": store.String("ALPHA_100% 東京"), "likeRef": store.String(wildcardTrap.ID),
	}, ridu.MutationOptions{}); !hasRelationshipIssue(err, "likeRef") {
		t.Fatalf("PostgreSQL like wildcard escaping error = %v", err)
	}

	// Target Read remains a separate atomic predicate and is deliberately
	// indistinguishable from a target rejected by an option filter.
	if _, err := application.Local().Create(ctx, "entries", store.Values{
		"exactLabel": store.String(matchingLabel), "equalsRef": store.String(hidden.ID),
	}, ridu.MutationOptions{}); !hasRelationshipIssue(err, "equalsRef") {
		t.Fatalf("PostgreSQL read-filtered relationship error = %v", err)
	}
}
