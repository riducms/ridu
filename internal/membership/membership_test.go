package membership

import (
	"math"
	"strings"
	"testing"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

var (
	tags      = schema.Field{Name: "tags", Type: schema.FieldTypeSelect, Select: &schema.SelectField{HasMany: true}}
	sizes     = schema.Field{Name: "sizes", Type: schema.FieldTypeNumberList}
	authors   = schema.Field{Name: "authors", Type: schema.FieldTypeRelationship, Relationship: &schema.RelationshipField{HasMany: true, CollectionSlug: "people"}}
	subjects  = schema.Field{Name: "subjects", Type: schema.FieldTypeRelationship, Relationship: &schema.RelationshipField{HasMany: true, Polymorphic: true, Targets: []schema.RelationshipTarget{{CollectionSlug: "posts"}, {CollectionSlug: "media"}}}}
	subject   = schema.Field{Name: "subject", Type: schema.FieldTypeRelationship, Relationship: &schema.RelationshipField{Polymorphic: true, Targets: []schema.RelationshipTarget{{CollectionSlug: "posts"}}}}
	author    = schema.Field{Name: "author", Type: schema.FieldTypeRelationship, Relationship: &schema.RelationshipField{CollectionSlug: "people"}}
	gallery   = schema.Field{Name: "gallery", Type: schema.FieldTypeUpload, Upload: &schema.UploadField{HasMany: true}}
	title     = schema.Field{Name: "title", Type: schema.FieldTypeText}
	metadata  = schema.Field{Name: "metadata", Type: schema.FieldTypeJSON}
	allFields = []schema.Field{tags, sizes, authors, subjects, subject, author, gallery, title, metadata}
)

func TestKindOfClassifiesSetValuedFields(t *testing.T) {
	for _, test := range []struct {
		field schema.Field
		want  Kind
	}{
		{tags, Strings}, {sizes, Numbers}, {authors, Strings}, {gallery, Strings},
		{subjects, References}, {subject, References},
		{author, None}, {title, None}, {metadata, None},
		{schema.Field{Type: schema.FieldTypeSelect, Select: &schema.SelectField{}}, None},
	} {
		if got := KindOf(test.field); got != test.want {
			t.Errorf("KindOf(%s) = %d, want %d", test.field.Name, got, test.want)
		}
	}
}

func TestValidateComparisonAllowsOnlyMembership(t *testing.T) {
	reference := query.Reference("posts", "p1")
	allowed := []query.Expression{
		query.In("tags", "news"), query.Not(query.In("tags", "news")), query.Exists("tags", false),
		query.Equal("tags", query.Null()), query.NotEqual("authors", query.Null()),
		query.In("authors", "a1", "a2"), query.In("gallery", "m1"), query.In("sizes", 0, -1.5),
		query.In("subjects", reference), query.In("subject", reference), query.Exists("subject", true),
		query.Equal("author", "a1"), query.Contains("title", "launch"), query.Contains("metadata", "plan"),
	}
	for _, expression := range allowed {
		node := expression.Node()
		if err := ValidateNode(allFields, &node); err != nil {
			t.Errorf("%v rejected: %v", node, err)
		}
	}
	rejected := map[string]query.Expression{
		"select equals":           query.Equal("tags", "news"),
		"select contains":         query.Contains("tags", "news"),
		"relationship equals":     query.Equal("authors", "a1"),
		"relationship not equals": query.NotEqual("authors", "a1"),
		"relationship contains":   query.Contains("authors", "a1"),
		"relationship like":       query.Like("authors", "a1"),
		"upload range":            query.GreaterThan("gallery", "a"),
		"relationship null item":  query.In("authors", query.Null()),
		"relationship number":     query.In("authors", 1),
		"polymorphic id":          query.In("subjects", "p1"),
		"polymorphic equals":      query.Equal("subject", reference),
		"polymorphic target":      query.In("subjects", query.Reference("people", "p1")),
		"polymorphic empty id":    query.In("subjects", query.Reference("posts", "")),
		"reference on relation":   query.In("author", reference),
		"reference on text":       query.Equal("title", reference),
		"reference on id":         query.In("id", reference),
		"reference in JSON value": query.Equal("metadata.owner", reference),
		"nested inside not":       query.Not(query.Equal("tags", "news")),
	}
	for name, expression := range rejected {
		node := expression.Node()
		if err := ValidateNode(allFields, &node); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestNumberListMembershipRequiresFiniteNumbers(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		// The plain helper panics on these; a typed Value reaches validation.
		node := query.In("sizes", query.Number(value)).Node()
		if err := ValidateComparison(sizes, *node.Comparison); err == nil || !strings.Contains(err.Error(), "finite numbers") {
			t.Fatalf("error=%v", err)
		}
	}
}

func TestValidateRequestRejectsMembershipSorts(t *testing.T) {
	collection := schema.Collection{Fields: allFields}
	for _, name := range []string{"tags", "authors", "subject", "sizes"} {
		sort, _ := query.NewSort(query.Field(name), query.Ascending)
		if err := ValidateRequest(store.Request{Collection: collection, Sort: []query.Sort{sort}}); err == nil || !strings.Contains(err.Error(), "cannot be sorted") {
			t.Errorf("sort %s error = %v", name, err)
		}
	}
	access := query.Equal("authors", "a1").Node()
	if err := ValidateRequest(store.Request{Collection: collection, Access: &access}); err == nil {
		t.Fatal("an access predicate outside the contract was accepted")
	}
}

func TestMatchesComparesItemsExactly(t *testing.T) {
	post := store.Object(store.Values{"relationTo": store.String("posts"), "id": store.String("p1")})
	media := store.Object(store.Values{"relationTo": store.String("media"), "id": store.String("p1")})
	for _, test := range []struct {
		name       string
		value      store.Value
		candidates query.Value
		want       bool
	}{
		{"string item", store.List(store.String("sale"), store.String("new")), query.List(query.String("new")), true},
		{"no substring", store.List(store.String("wholesale")), query.List(query.String("sale")), false},
		{"case", store.List(store.String("Sale")), query.List(query.String("sale")), false},
		{"number", store.List(store.Number(8.5)), query.List(query.Number(8.5)), true},
		{"empty", store.List(), query.List(query.String("")), false},
		{"reference list", store.List(media, post), query.List(query.Reference("posts", "p1")), true},
		{"same id elsewhere", store.List(media), query.List(query.Reference("posts", "p1")), false},
		{"singular reference", post, query.List(query.Reference("media", "p1"), query.Reference("posts", "p1")), true},
		{"scalar is no set", store.String("sale"), query.List(query.String("sale")), false},
	} {
		if got := Matches(test.value, test.candidates); got != test.want {
			t.Errorf("%s = %v, want %v", test.name, got, test.want)
		}
	}
}
