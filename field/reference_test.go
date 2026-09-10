package field_test

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/riducms/ridu/field"
)

func TestReferencesValidatePathsWithoutResolvingTargets(t *testing.T) {
	for _, reference := range []field.Reference{field.Sibling("kind"), field.Root("tenant"), field.Sibling("settings.kind"), field.Root("seo.title"), field.Sibling("unknownButValid")} {
		if err := reference.Err(); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"", "owner..kind", "owner.", "rows[0]", "..", "_rowKey", "hyphen-name", "9invalid"} {
		if err := field.Sibling(path).Err(); err == nil {
			t.Errorf("accepted structurally invalid reference %q", path)
		}
	}
	if err := (field.Reference{}).Err(); err == nil {
		t.Fatal("unspecified reference unexpectedly binds")
	}
}

func TestConditionSerializationPreservesReferencesAndTypedValues(t *testing.T) {
	condition := field.All(
		field.Equal(field.Sibling("settings.kind"), "link"),
		field.Not(field.NotEqual(field.Root("published"), true)),
		field.OneOf(field.Root("rating"), 1, 2, 3),
	)
	encoded, err := json.Marshal(condition)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"kind":"all","conditions":[{"kind":"predicate","reference":{"scope":"sibling","path":"settings.kind"},"operator":"equals","values":["link"]},{"kind":"not","conditions":[{"kind":"predicate","reference":{"scope":"root","path":"published"},"operator":"notEquals","values":[true]}]},{"kind":"predicate","reference":{"scope":"root","path":"rating"},"operator":"oneOf","values":[1,2,3]}]}`
	if string(encoded) != want {
		t.Fatalf("condition serialization = %s; want %s", encoded, want)
	}
	if encoded, err := json.Marshal(field.Condition{}); err != nil || string(encoded) != "null" {
		t.Fatalf("unspecified condition = %s, %v", encoded, err)
	}
}

func TestConditionsRejectNonFiniteOperands(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		condition := field.Equal(field.Root("rating"), value)
		if err := condition.Err(); err == nil {
			t.Errorf("accepted non-finite operand %v", value)
		}
		if _, err := json.Marshal(condition); err == nil {
			t.Errorf("serialized non-finite operand %v", value)
		}
	}
}
