package content

import (
	"fmt"

	"github.com/riducms/ridu/store"
)

func Example_values() {
	values := store.Values{
		"title":    store.String("Welcome"),
		"featured": store.Boolean(false),
		"summary":  store.Null(),
		"tags":     store.List(),
	}

	title, ok := values["title"].StringValue()
	fmt.Println("title:", title, ok)
	// The bool checks the type, not whether the value is nonzero.
	featured, ok := values["featured"].BooleanValue()
	fmt.Println("featured:", featured, ok)
	_, ok = values["title"].NumberValue()
	fmt.Println("title is a number:", ok)

	// Map membership is a separate check from the value's type.
	_, exists := values["missing"]
	fmt.Println("missing key exists:", exists)
	summary := values["summary"]
	fmt.Println("summary is null:", summary.Kind() == store.ValueNull)
	tags := values["tags"]
	fmt.Println("tags:", tags.Len(), tags.Kind() == store.ValueList)

	// Output:
	// title: Welcome true
	// featured: false true
	// title is a number: false
	// missing key exists: false
	// summary is null: true
	// tags: 0 true
}
