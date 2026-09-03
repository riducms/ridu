// Package adoption compile-checks the public field, adapter, and plugin surfaces used throughout
// the adoption documentation. It is intentionally database-free and safe in the default test gate.
package adoption

import (
	"encoding/json"

	"github.com/riducms/ridu/adapters/mongodb"
	"github.com/riducms/ridu/adapters/postgres"
	"github.com/riducms/ridu/adapters/sqlite"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/plugins/formbuilder"
	graphqlplugin "github.com/riducms/ridu/plugins/graphql"
	"github.com/riducms/ridu/plugins/mcp"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/plugins/seo"
)

// Adapter constructors and migration drivers stay referenced here so documentation cannot drift
// to a renamed or removed public setup surface without failing to compile.
var (
	PostgresOpen              = postgres.Open
	PostgresProjectMigrations = postgres.ProjectMigrations
	SQLiteOpen                = sqlite.Open
	SQLiteProjectMigrations   = sqlite.ProjectMigrations
	MongoDBOpen               = mongodb.Open
	MongoDBProjectMigrations  = mongodb.ProjectMigrations
)

// Paired plugin constructors used by the public installation guides.
var (
	RichTextNew    = richtext.New
	SEONew         = seo.New
	FormBuilderNew = formbuilder.New
	GraphQLNew     = graphqlplugin.New
	MCPNew         = mcp.New
)

// Fields exercises every built-in field constructor with its smallest valid public shape.
func Fields() []field.Definition {
	return []field.Definition{
		field.Text("title"),
		field.Textarea("summary"),
		field.Email("email"),
		field.Code("source"),
		field.Date("publishedAt"),
		field.Number("priority"),
		field.Checkbox("featured"),
		field.JSON("metadata"),
		field.Select("status", field.OneOf("draft", "published")),
		field.Radio("tone", field.OneOf("neutral", "urgent")),
		field.Point("location"),
		field.Relationship("author", field.To("users")),
		field.Upload("cover", field.To("media")),
		field.Group("seo", field.Fields(field.Text("title"))),
		field.Array("links", field.Fields(field.Text("label"))),
		field.Blocks("layout", field.BlockTypes(
			field.BlockType("copy", "Copy", field.Textarea("body")),
		)),
		field.Tabs(field.NamedTab("settings", "Settings", field.Text("theme"))),
		field.Row(field.Text("firstName"), field.Text("lastName")),
		field.Collapsible("advanced", true, field.Text("internalName")),
		field.Join("related", "posts", "category"),
		field.Slug("slug", "title"),
		field.Virtual("displayLabel", field.ValueString),
		field.UI("fieldHelp"),
		field.Plugin("color", "example/color", json.RawMessage(`{"format":"hex"}`)),
	}
}
