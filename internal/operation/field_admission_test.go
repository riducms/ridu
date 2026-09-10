package operation

import (
	"testing"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

func TestBoundReadPoliciesProtectNestedQueryPaths(t *testing.T) {
	parentPath, _ := query.NewPath("meta")
	childPath, _ := query.NewPath("meta", "secret")
	child := schema.Field{ID: "meta-secret", Name: "secret", Path: childPath, Type: schema.FieldTypeText, QueryRestricted: true}
	parent := schema.Field{ID: "meta", Name: "meta", Path: parentPath, Type: schema.FieldTypeGroup, Nested: &schema.NestedField{Fields: []schema.Field{child}}}
	nested := Collection{Schema: schema.Collection{ID: "nested", Slug: "nested", Fields: []schema.Field{parent}}, Bindings: []FieldBinding{{ID: "resolved-secret", Field: child, Access: FieldRules{Read: func(Context) (bool, error) { return false, nil }}}}}
	if err := authorizeQuery(nested, nil, []query.Sort{{Path: parentPath, Direction: query.Ascending}}); err == nil {
		t.Fatal("parent container sort bypassed attached descendant policy")
	}
	if err := authorizeQuery(nested, query.Equal(childPath, query.String("probe")), nil); err == nil {
		t.Fatal("nested attached policy did not protect canonical field query")
	}
}
