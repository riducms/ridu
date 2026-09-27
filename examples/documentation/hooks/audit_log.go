package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/store"
)

var AuditLog = ridu.Collection{
	Slug: "audit-log",
	Fields: field.Fields{
		field.Text("document").Required(),
		field.Text("operation").Required(),
	},
}

func writeAuditEntry(ctx ridu.HookContext) error {
	if ctx.Document == nil {
		return nil
	}
	// Passing ctx.Context saves both documents in one transaction.
	_, err := ctx.Local.Create(
		ctx.Context,
		"audit-log",
		store.Values{
			"document":  store.String(ctx.Document.ID),
			"operation": store.String(string(ctx.Operation)),
		},
		// Local API calls do not inherit the user or locale.
		ridu.MutationOptions{
			Actor:           ctx.Actor,
			ActorCollection: ctx.ActorCollection,
			Locale:          ctx.Locale,
		},
	)
	// Returning the error also rolls back the original save.
	return err
}
