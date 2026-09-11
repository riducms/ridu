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
	// Reuse the transaction so the post and audit entry save together.
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
	// An audit failure must also fail the post's save.
	return err
}
