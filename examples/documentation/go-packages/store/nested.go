package content

import (
	"fmt"

	"github.com/riducms/ridu/store"
)

func RenameFirstLink(
	links store.Value,
	label string,
) (store.Value, error) {
	first, ok := links.ListItem(0)
	if !ok {
		return store.Value{}, fmt.Errorf("links must be a nonempty list")
	}
	// Only this row is copied. Its _key and other fields are retained.
	row, ok := first.WithMembers(store.Values{"label": store.String(label)})
	if !ok {
		return store.Value{}, fmt.Errorf("first link must be an object")
	}
	updated, _ := links.WithListItem(0, row)
	return updated, nil
}
