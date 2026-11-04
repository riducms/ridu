package oauth

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
)

// Only the plugin's own trusted code, with System, reads or writes clients
// and the secrets on connections; access rules refuse everyone else.
func deny(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Deny(), nil }

var private = field.Access{
	Create: func(operation.Context) (bool, error) { return false, nil },
	Read:   func(operation.Context) (bool, error) { return false, nil },
	Update: func(operation.Context) (bool, error) { return false, nil },
}

var hidden = field.Admin{Hidden: true}

func clientsCollection() ridu.Collection {
	return ridu.Collection{
		Slug:   ClientsCollection,
		Labels: ridu.CollectionLabels{Singular: "OAuth client", Plural: "OAuth clients"},
		Admin:  ridu.CollectionAdmin{Hidden: true, UseAsTitle: "name"},
		Access: ridu.CollectionAccess{Create: deny, Read: deny, Update: deny, Delete: deny},
		Fields: field.Fields{
			field.Text("name").Required().MaxLength(100),
			field.TextList("redirectUris").Required().MaxRows(10),
		},
	}
}

// connectionsCollection lists who connected which client, acting as whom. A
// person sees the connections they approved or that act as them, and
// deleting one revokes it.
func (plugin *Plugin) connectionsCollection() ridu.Collection {
	users := plugin.config.Users
	manage := plugin.config.ManageConnections
	// An approval whose code was never exchanged isn't a connected app yet;
	// lastUsedAt is set when it first gets tokens.
	connected := query.Exists("lastUsedAt", true)
	own := func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
		if ctx.Actor == nil || ctx.ActorCollection != users {
			return ridu.Deny(), nil
		}
		if manage != nil {
			all, err := manage(ctx)
			if err != nil || all {
				return ridu.Where(connected), err
			}
		}
		return ridu.Where(query.And(connected, query.Or(query.Equal("user", ctx.Actor.ID), query.Equal("approvedBy", ctx.Actor.ID)))), nil
	}
	readOnly := field.Admin{ReadOnly: true}
	return ridu.Collection{
		Slug:   ConnectionsCollection,
		Labels: ridu.CollectionLabels{Singular: "Connected app", Plural: "Connected apps"},
		Admin: ridu.CollectionAdmin{
			UseAsTitle: "clientName", DefaultColumns: []string{"clientName", "user", "lastUsedAt", "createdAt"},
			Description: "Apps such as MCP clients connected by OAuth. Delete a connection to revoke it: its tokens stop working at once.",
		},
		Access: ridu.CollectionAccess{Create: deny, Read: own, Update: deny, Delete: own},
		Fields: field.Fields{
			field.Text("clientName").Required().Label("App").Admin(readOnly),
			// Removing a user, or the client, removes its connections.
			field.Relationship("user", users).Required().Index().OnDelete(field.ReferenceDeleteCascade).Label("Acts as").Admin(readOnly),
			field.Relationship("approvedBy", users).Required().Index().OnDelete(field.ReferenceDeleteCascade).Label("Approved by").Admin(readOnly),
			field.Date("lastUsedAt").Format(field.DateTime).Label("Last connected").Admin(readOnly),
			field.Relationship("client", ClientsCollection).Required().Index().OnDelete(field.ReferenceDeleteCascade).Access(private).Admin(hidden),
			field.Text("redirectUri").Required().Access(private).Admin(hidden),
			field.Text("codeHash").Index().Access(private).Admin(hidden),
			field.Text("codeChallenge").Access(private).Admin(hidden),
			field.Date("codeExpiresAt").Format(field.DateTime).Access(private).Admin(hidden),
			field.Text("accessTokenHash").Index().Access(private).Admin(hidden),
			field.Date("accessExpiresAt").Format(field.DateTime).Access(private).Admin(hidden),
			field.Text("refreshTokenHash").Index().Access(private).Admin(hidden),
			field.Date("refreshExpiresAt").Format(field.DateTime).Access(private).Admin(hidden),
			// generation counts token grants. A code exchange or refresh claims
			// the next value with Local.Increment, so of two concurrent uses of
			// one code or refresh token only one wins.
			field.Number("generation").Access(private).Admin(hidden),
		},
	}
}
