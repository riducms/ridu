package content

import (
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/store"
)

// Write hooks receive the stored value: the reviewer's document ID.
func clearSelfReview(
	ctx operation.Context,
	value operation.Value[operation.ID],
) (operation.Change[operation.ID], error) {
	id, present := value.Get()
	if !present || ctx.Actor.ID == "" || id != ctx.Actor.ID {
		return operation.Keep[operation.ID](), nil
	}
	// Authors cannot review their own work; save it unreviewed.
	return operation.Replace(operation.Empty[operation.ID]()), nil
}

// Read hooks receive the response value, which may be the populated reviewer.
func showReviewerName(
	_ operation.Context,
	value operation.Value[operation.ReferenceOutput],
) (operation.Change[operation.ReferenceOutput], error) {
	reference, present := value.Get()
	if !present {
		return operation.Keep[operation.ReferenceOutput](), nil
	}
	reviewer, populated := reference.Document()
	if !populated {
		// An unpopulated response holds only the ID; leave it alone.
		return operation.Keep[operation.ReferenceOutput](), nil
	}
	// Return the reference with only the reviewer's name.
	return operation.Replace(operation.Present(operation.Populated(store.Document{
		ID:     reviewer.ID,
		Values: store.Values{"name": reviewer.Values["name"]},
	}))), nil
}

var Reviewer = field.Relationship("reviewer", "users").
	Hooks(field.Hooks[operation.ID]{
		BeforeChange: []field.Transform[operation.ID]{clearSelfReview},
	}).
	AfterRead(showReviewerName)
