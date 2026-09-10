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
	row, ok := first.CopyObject()
	if !ok {
		return store.Value{}, fmt.Errorf("first link must be an object")
	}

	// Only this row needs a mutable copy. Preserve its _key and other fields.
	row["label"] = store.String(label)
	updated, _ := links.WithListItem(0, store.Object(row))
	return updated, nil
}
