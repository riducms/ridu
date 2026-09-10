package content

import (
	"fmt"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

func Example_inspectSchema() {
	config := ridu.Config{
		Name: "Blog",
		Collections: []ridu.Collection{{
			Slug:   "posts",
			Fields: field.Fields{field.Text("title").Required()},
		}},
	}
	// Resolve checks the Go config without opening a database.
	manifest, err := ridu.Resolve(config)
	if err != nil {
		panic(err)
	}

	// Snapshot gives us a copy of the resolved definitions.
	snapshot := manifest.Snapshot()
	posts := snapshot.Collections[0]
	title := posts.Fields[0]
	fmt.Println(posts.Slug, title.Path.String(), title.ID)
	fmt.Println("Required:", title.Required)

	// Changing the copy does not change the application's schema.
	snapshot.Collections[0].Fields[0].Required = false
	fmt.Println("Still required:",
		manifest.Snapshot().Collections[0].Fields[0].Required)

	// Output:
	// posts title posts-title
	// Required: true
	// Still required: true
}
