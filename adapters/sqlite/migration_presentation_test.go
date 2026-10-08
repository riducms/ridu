package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/migrationartifact"
	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestSQLitePresentationMigrationPreservesVersionedData(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	endpoint := ridu.Endpoint{Method: "GET", Path: "/example", Summary: "Example", Handler: func(ridu.EndpointContext) {}}
	config := ridu.Config{Name: "Audit", Endpoints: []ridu.Endpoint{endpoint}, Collections: []ridu.Collection{
		{Slug: "authors", Versions: true, Fields: field.Fields{field.Text("name"), field.Join("posts", "posts", "author").DefaultColumns("title")}},
		{Slug: "posts", Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true}, Fields: field.Fields{field.Text("title").Required().Unique().Index().Localized(), field.Relationship("author", "authors"), field.Select("tone", "light", "dark"), field.Radio("layout", "compact", "large")}, Endpoints: []ridu.Endpoint{endpoint}},
	}, Globals: []ridu.Global{{Slug: "settings", Versions: true, Fields: field.Fields{field.Text("site")}, Endpoints: []ridu.Endpoint{endpoint}}}}
	config.Admin = ridu.AdminConfig{Localization: ridu.AdminLocalizationConfig{
		DefaultLanguage: "en", Languages: []ridu.AdminLanguage{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}},
		DefaultTimeZone: "UTC", TimeZones: []ridu.AdminTimeZone{{ID: "Europe/London", Label: "London"}, {ID: "UTC", Label: "UTC"}},
	}}
	config.Localization = ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "ar", Label: "Arabic"}}}
	resolve := func() schema.Manifest {
		t.Helper()
		m, err := ridu.Resolve(config)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	initial := resolve()
	if _, err := CreateArtifact(ctx, directory, "initial", initial, time.Unix(1, 0), ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	backend, err := Open(ctx, filepath.Join(t.TempDir(), "content.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = backend.Close() })
	// Keep TEMP write guards on the connection used by the migration runner.
	backend.db.SetMaxOpenConns(1)
	backend.db.SetMaxIdleConns(1)
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	author, err := application.Local().Create(ctx, "authors", store.Values{"name": store.String("Ada")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	document, err := application.Local().Create(ctx, "posts", store.Values{"title": store.String("Draft"), "author": store.String(string(author.ID)), "tone": store.String("light"), "layout": store.String("compact")}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	document, err = application.Local().PublishChanges(ctx, "posts", document.ID, store.Values{"title": store.String("Published")}, ridu.MutationOptions{ExpectedRevision: document.Revision})
	if err != nil {
		t.Fatal(err)
	}
	originalDocument, err := application.Local().Find(ctx, "posts", document.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	originalVersions, err := application.Local().Versions(ctx, "posts", document.ID, ridu.FindOptions{})
	if err != nil || len(originalVersions) < 2 {
		t.Fatalf("retained revisions = %#v, %v", originalVersions, err)
	}
	tables := []string{"ridu_documents", "ridu_versions", "ridu_unique_values", "ridu_document_references"}
	originalRows := sqlitePresentationRows(t, backend, tables)
	for _, table := range tables {
		if originalRows[table] == "null" {
			t.Fatalf("empty test table %s", table)
		}
		for _, operation := range []string{"INSERT", "UPDATE", "DELETE"} {
			statement := fmt.Sprintf(`CREATE TEMP TRIGGER guard_%s_%s BEFORE %s ON %s BEGIN SELECT RAISE(ABORT, 'presentation migration touched data'); END`, table, operation, operation, table)
			if _, err := backend.db.ExecContext(ctx, statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	var originalSchemaVersion int
	if err := backend.db.QueryRowContext(ctx, `PRAGMA schema_version`).Scan(&originalSchemaVersion); err != nil {
		t.Fatal(err)
	}
	// None of these shape stored data, so none needs a migration.
	changes := []struct {
		name  string
		apply func()
	}{
		{"application-name", func() { config.Name = "My publication" }},
		{"field-label", func() {
			config.Collections[1].Fields[0] = field.Text("title").Required().Unique().Index().Localized().Label("Headline").Admin(field.Admin{Description: "Public headline"})
		}},
		{"collection-admin", func() {
			config.Collections[1].Admin.Hidden = true
			config.Collections[1].Admin.Group = "Editorial"
		}},
		{"versioned-select-choice-order", func() {
			config.Collections[1].Fields[2] = field.Select("tone", "dark", "light")
		}},
		{"versioned-radio-choice-order", func() {
			config.Collections[1].Fields[3] = field.Radio("layout", "large", "compact")
		}},
		{"admin-language-label", func() { config.Admin.Localization.Languages[0].Label = "English (UK)" }},
		{"admin-language-translations", func() {
			config.Admin.Localization.Languages[0].LabelTranslations = map[string]string{"en": "UK English"}
		}},
		{"editor-timezone-label", func() { config.Admin.Localization.TimeZones[0].Label = "London time" }},
		{"editor-timezone-translations", func() { config.Admin.Localization.TimeZones[0].LabelTranslations = map[string]string{"en": "UK time"} }},
		{"admin-language-default", func() { config.Admin.Localization.DefaultLanguage = "fr" }},
		{"editor-timezone-default", func() { config.Admin.Localization.DefaultTimeZone = "Europe/London" }},
		{"admin-language-order", func() {
			settings := &config.Admin.Localization
			settings.Languages[0], settings.Languages[1] = settings.Languages[1], settings.Languages[0]
		}},
		{"editor-timezone-order", func() {
			settings := &config.Admin.Localization
			settings.TimeZones[0], settings.TimeZones[1] = settings.TimeZones[1], settings.TimeZones[0]
		}},
		{"content-locale-label", func() { config.Localization.Locales[0].Label = "English (UK)" }},
		{"content-locale-direction", func() { config.Localization.Locales[1].RTL = true }},
		{"versioned-join-columns", func() {
			config.Collections[0].Fields[1] = field.Join("posts", "posts", "author").DefaultColumns("title", "author")
		}},
		{"versioned-join-create-button", func() {
			config.Collections[0].Fields[1] = field.Join("posts", "posts", "author").DefaultColumns("title", "author").AllowCreate(false)
		}},
		{"application-endpoint-summary", func() { config.Endpoints[0].Summary = "Application description" }},
		{"application-endpoint-path", func() { config.Endpoints[0].Path = "/renamed" }},
		{"collection-labels", func() { config.Collections[1].Labels = ridu.CollectionLabels{Singular: "Article", Plural: "Articles"} }},
		{"admin-loader", func() {
			type stats struct {
				Count int `json:"count"`
			}
			config.Admin.Loaders = []ridu.AdminLoaderDefinition{ridu.NewAdminLoader("stats", func(ridu.AdminLoadContext, struct{}) (stats, error) {
				return stats{}, nil
			})}
		}},
		{"collection-endpoint-summary", func() { config.Collections[1].Endpoints[0].Summary = "Collection description" }},
		{"global-endpoint-summary", func() { config.Globals[0].Endpoints[0].Summary = "Global description" }},
	}
	for _, change := range changes {
		change.apply()
		current := resolve()
		if _, err := CreateArtifact(ctx, directory, change.name, current, time.Unix(2, 0), ArtifactOptions{}); !errors.Is(err, migrationartifact.ErrOnlyPresentationChanges) {
			t.Fatalf("create %s = %v", change.name, err)
		}
		// The database already has everything this change describes.
		if _, err := backend.ArtifactStatus(ctx, directory, current); err != nil {
			t.Fatalf("%s needs a migration: %v", change.name, err)
		}
		if err := backend.Ready(ctx, current); err != nil {
			t.Fatalf("%s is not ready without a migration: %v", change.name, err)
		}
		sqlitePresentationReady(t, backend, directory, current)
		application, err = ridu.New(config, backend)
		if err != nil {
			t.Fatal(err)
		}
		gotDocument, err := application.Local().Find(ctx, "posts", document.ID, ridu.FindOptions{})
		if err != nil || !reflect.DeepEqual(originalDocument, gotDocument) {
			t.Fatalf("%s changed the document: %#v, %v", change.name, gotDocument, err)
		}
		gotVersions, err := application.Local().Versions(ctx, "posts", document.ID, ridu.FindOptions{})
		if err != nil || !reflect.DeepEqual(originalVersions, gotVersions) {
			t.Fatalf("%s changed revisions: %#v, %v", change.name, gotVersions, err)
		}
	}
	if got := sqlitePresentationRows(t, backend, tables); !reflect.DeepEqual(originalRows, got) {
		t.Fatalf("stored bytes changed:\nbefore: %v\nafter: %v", originalRows, got)
	}
	var schemaVersion int
	if err := backend.db.QueryRowContext(ctx, `PRAGMA schema_version`).Scan(&schemaVersion); err != nil {
		t.Fatal(err)
	}
	if schemaVersion != originalSchemaVersion {
		t.Fatalf("physical schema mutated: %d -> %d", originalSchemaVersion, schemaVersion)
	}
	for _, table := range tables {
		for _, operation := range []string{"INSERT", "UPDATE", "DELETE"} {
			if _, err := backend.db.ExecContext(ctx, fmt.Sprintf(`DROP TRIGGER guard_%s_%s`, table, operation)); err != nil {
				t.Fatal(err)
			}
		}
	}
	config.Collections[1].Fields = append(config.Collections[1].Fields, field.Text("summary").Index().Unique())
	additive := resolve()
	_, err = CreateArtifact(ctx, directory, "add-summary", additive, time.Unix(int64(len(changes)+2), 0), ArtifactOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	sqlitePresentationReady(t, backend, directory, additive)
	if got := sqlitePresentationRows(t, backend, tables[:2]); got["ridu_documents"] != originalRows["ridu_documents"] || got["ridu_versions"] != originalRows["ridu_versions"] {
		t.Fatal("additive migration rewrote documents or revisions")
	}
	application, err = ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := application.Local().PublishChanges(ctx, "posts", document.ID, store.Values{"summary": store.String("New field works")}, ridu.MutationOptions{ExpectedRevision: document.Revision})
	if err != nil {
		t.Fatal(err)
	}
	var indexed int
	if err := backend.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE name = ?`, documentFieldIndexName("posts", "summary", nil)).Scan(&indexed); err != nil || indexed != 1 {
		t.Fatalf("additive index = %d, %v", indexed, err)
	}
	restored, err := application.Local().RestoreAsDraft(ctx, "posts", document.ID, 1, ridu.MutationOptions{ExpectedRevision: updated.Revision})
	if err != nil {
		t.Fatalf("restore revision retained before presentation changes: %v", err)
	}
	if title, _ := restored.Values["title"].StringValue(); title != "Draft" {
		t.Fatalf("restored title = %q", title)
	}
}

func sqlitePresentationReady(t *testing.T, backend *Store, directory string, manifest schema.Manifest) {
	t.Helper()
	files, err := migrationartifact.ReadAll(directory)
	if err != nil {
		t.Fatal(err)
	}
	identities := make([]migration.ArtifactIdentity, len(files))
	for i, file := range files {
		identities[i] = migration.ArtifactIdentity{Name: file.Name, Digest: file.Digest}
	}
	fingerprint, err := migration.DigestArtifactHistory(identities, files[len(files)-1].Artifact.ToDigest, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.ReadyWithMigrationHistory(context.Background(), manifest, fingerprint); err != nil {
		t.Fatalf("production readiness: %v", err)
	}
}

func sqlitePresentationRows(t *testing.T, backend *Store, tables []string) map[string]string {
	t.Helper()
	result := make(map[string]string, len(tables))
	for _, table := range tables {
		rows, err := backend.db.QueryContext(context.Background(), `SELECT * FROM `+table+` ORDER BY rowid`)
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		var records [][]any
		for rows.Next() {
			record := make([]any, len(columns))
			destinations := make([]any, len(columns))
			for i := range record {
				destinations[i] = &record[i]
			}
			if err := rows.Scan(destinations...); err != nil {
				t.Fatal(err)
			}
			records = append(records, record)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		_ = rows.Close()
		encoded, err := json.Marshal(records)
		if err != nil {
			t.Fatal(err)
		}
		result[table] = string(encoded)
	}
	return result
}

func TestSQLitePresentationMigrationNestedAndEmbeddedMetadata(t *testing.T) {
	block := field.Block{Slug: "hero", Fields: field.Fields{field.Text("heading").Required()}}
	manifest, err := ridu.Resolve(ridu.Config{Name: "Presentation", Plugins: []ridu.Plugin{richtext.New()},
		Collections: []ridu.Collection{{Slug: "pages", Versions: true, Fields: field.Fields{field.Array("items", field.Fields{field.Text("title")}), field.Select("tone", "light", "dark"), field.Blocks("layout", block), richtext.Field("body", richtext.Config{Blocks: []field.Block{block}})}}},
		Globals:     []ridu.Global{{Slug: "settings", Versions: true, Fields: field.Fields{field.Group("branding", field.Fields{field.Text("title")})}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	before := manifest.Snapshot()
	after := manifest.Snapshot()
	after.Application.Name = "Renamed"
	after.Collections[0].Labels.Singular = "Page"
	after.Collections[0].Admin.Description = "Website pages"
	after.Collections[0].Fields[0].Nested.RowLabel = "title"
	after.Collections[0].Fields[0].Nested.ResolvedFields()[0].Admin.Label = "Item title"
	after.Collections[0].Fields[1].Select.Options[0].Label = "Light theme"
	// The blocks field and the rich text field place one hero definition.
	hero := &after.Blocks[0]
	if hero.Slug != "hero" || len(after.Blocks) != 1 {
		t.Fatalf("block definitions = %#v", after.Blocks)
	}
	hero.Labels = schema.BlockLabels{Singular: "Banner", Plural: "Banners"}
	hero.Fields[0].Admin.Description = "Banner heading"
	hero.Fields[0].Admin.Label = "Inline heading"
	after.Globals[0].Fields[0].Nested.ResolvedFields()[0].Admin.Placeholder = "Site title"
	frozen := schema.NewManifest(after)
	after = frozen.Snapshot()
	if placed := after.Collections[0].Fields[3].Plugin.EmbeddedTrees[0].Cases[0].ResolvedTypes()[0]; placed.Labels.Singular != "Banner" || placed.ResolvedFields()[0].Admin.Label != "Inline heading" {
		t.Fatalf("embedded placement = %#v", placed)
	}
	if err := validateSQLiteAdditiveTransition(before, after); err != nil {
		t.Fatal(err)
	}
	if sqlitePlacements(t, before) != sqlitePlacements(t, manifest.Snapshot()) || !schema.NewManifest(before).Equal(manifest) ||
		sqlitePlacements(t, after) != sqlitePlacements(t, frozen.Snapshot()) || !schema.NewManifest(after).Equal(frozen) {
		t.Fatal("presentation comparison mutated input")
	}
	directory := t.TempDir()
	if _, err := CreateArtifact(context.Background(), directory, "initial", manifest, time.Unix(1, 0), ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateArtifact(context.Background(), directory, "presentation", frozen, time.Unix(2, 0), ArtifactOptions{}); !errors.Is(err, migrationartifact.ErrOnlyPresentationChanges) {
		t.Fatalf("presentation migration = %v", err)
	}
}

// sqlitePlacements renders the snapshot's resources with every lazily derived
// block placement view, so a mutation check also covers placements that a
// canonical encoding, which records only definitions, does not show.
func sqlitePlacements(t *testing.T, snapshot schema.Snapshot) string {
	t.Helper()
	var rendered strings.Builder
	write := func(value any) {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		rendered.Write(append(encoded, '\n'))
	}
	var fields func([]schema.Field)
	blocks := func(types []schema.BlockType) {
		for _, block := range types {
			write(block)
			fields(block.ResolvedFields())
		}
	}
	fields = func(list []schema.Field) {
		for _, candidate := range list {
			write(candidate)
			if candidate.Nested != nil {
				fields(candidate.Nested.ResolvedFields())
			}
			if candidate.Blocks != nil {
				blocks(candidate.Blocks.ResolvedTypes())
			}
			if candidate.Plugin != nil {
				for _, tree := range candidate.Plugin.EmbeddedTrees {
					for _, c := range tree.Cases {
						blocks(c.ResolvedTypes())
					}
				}
			}
		}
	}
	for _, resource := range append(append([]schema.Collection(nil), snapshot.Collections...), snapshot.Globals...) {
		fields(resource.Fields)
	}
	return rendered.String()
}

func TestSQLitePresentationMigrationStillRejectsStorageChanges(t *testing.T) {
	before, err := ridu.Resolve(ridu.Config{Name: "Audit", Collections: []ridu.Collection{{Slug: "posts", Versions: true, Fields: field.Fields{field.Text("title").Required().Unique().Index()}}}})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*schema.Snapshot){
		"remove collection": func(s *schema.Snapshot) { s.Collections = nil },
		"remove field":      func(s *schema.Snapshot) { s.Collections[0].Fields = nil },
		"rename field": func(s *schema.Snapshot) {
			s.Collections[0].Fields[0].Name = "headline"
			s.Collections[0].Fields[0].Path, _ = query.NewPath("headline")
		},
		"change field type": func(s *schema.Snapshot) { s.Collections[0].Fields[0].Type = schema.FieldTypeTextarea },
		"remove uniqueness": func(s *schema.Snapshot) { s.Collections[0].Fields[0].Unique = false },
		"remove index":      func(s *schema.Snapshot) { s.Collections[0].Fields[0].Index = false },
		"change default":    func(s *schema.Snapshot) { value := "Default"; s.Collections[0].Fields[0].Default = &value },
		"change drafts":     func(s *schema.Snapshot) { s.Collections[0].Versions.Drafts = true },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			snapshot := before.Snapshot()
			snapshot.Application.Name = "My publication"
			snapshot.Collections[0].Fields[0].Admin.Label = "Headline"
			mutate(&snapshot)
			after := schema.NewManifest(snapshot)
			for _, allow := range []bool{false, true} {
				if _, err := planArtifact(context.Background(), "unsupported", &before, after, allow); err == nil {
					t.Fatalf("unsupported transition accepted (allow-destructive=%v)", allow)
				}
			}
		})
	}
}

func TestSQLitePresentationPreservesJoinOperations(t *testing.T) {
	config := ridu.Config{Name: "Operational boundaries", Collections: []ridu.Collection{
		{Slug: "authors", Versions: true, Fields: field.Fields{field.Text("name"), field.Join("posts", "posts", "author")}},
		{Slug: "posts", Fields: field.Fields{field.Text("title"), field.Relationship("author", "authors")}},
	}}
	before, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*schema.Snapshot){
		"join limit": func(s *schema.Snapshot) { s.Collections[0].Fields[1].Join.Limit++ },
		"join sort":  func(s *schema.Snapshot) { s.Collections[0].Fields[1].Join.DefaultSort = "-title" },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			snapshot := before.Snapshot()
			snapshot.Collections[0].Fields[1].Join.DefaultColumns = []string{"title"}
			change(&snapshot)
			after := schema.NewManifest(snapshot)
			for _, allow := range []bool{false, true} {
				if _, err := planArtifact(context.Background(), "operational", &before, after, allow); err == nil {
					t.Fatalf("operational transition accepted (allow-destructive=%v)", allow)
				}
			}
		})
	}
}

func TestSQLitePresentationOptionOrderPreservesValueContract(t *testing.T) {
	before, err := ridu.Resolve(ridu.Config{Name: "Choices", Collections: []ridu.Collection{{Slug: "posts", Versions: true, Fields: field.Fields{field.Select("tone", "light", "dark"), field.Radio("layout", "compact", "large"), field.MultiSelect("tones", "light", "dark").Default("light", "dark")}}}})
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func([]schema.Field){
		"remove select value": func(fields []schema.Field) { fields[0].Select.Options = fields[0].Select.Options[:1] },
		"add select value": func(fields []schema.Field) {
			fields[0].Select.Options = append(fields[0].Select.Options, schema.SelectOption{Value: "sepia", Label: "Sepia"})
		},
		"replace select value": func(fields []schema.Field) { fields[0].Select.Options[0].Value = "sepia" },
		"select default":       func(fields []schema.Field) { value := "dark"; fields[0].Default = &value },
		"select cardinality":   func(fields []schema.Field) { fields[0].Select.HasMany = true },
		"remove radio value":   func(fields []schema.Field) { fields[1].Select.Options = fields[1].Select.Options[:1] },
		"add radio value": func(fields []schema.Field) {
			fields[1].Select.Options = append(fields[1].Select.Options, schema.SelectOption{Value: "wide", Label: "Wide"})
		},
		"replace radio value": func(fields []schema.Field) { fields[1].Select.Options[0].Value = "wide" },
		"radio default":       func(fields []schema.Field) { value := "large"; fields[1].Default = &value },
		"ordered defaults":    func(fields []schema.Field) { fields[2].Select.DefaultValues = []string{"dark", "light"} },
	}
	descriptor := migration.DataTransformDescriptor{Name: "reviewed", Checksum: migration.DataTransformChecksum([]byte("reviewed-v1"))}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			snapshot := before.Snapshot()
			for _, candidate := range snapshot.Collections[0].Fields {
				candidate.Select.Options[0], candidate.Select.Options[1] = candidate.Select.Options[1], candidate.Select.Options[0]
			}
			mutate(snapshot.Collections[0].Fields)
			after := schema.NewManifest(snapshot)
			for _, allow := range []bool{false, true} {
				for _, transforms := range [][]migration.DataTransformDescriptor{nil, {descriptor}} {
					_, err := planArtifact(context.Background(), "unsupported", &before, after, allow, transforms...)
					if err == nil {
						t.Fatalf("value contract change accepted (allow-destructive=%v, transforms=%v)", allow, transforms)
					}
					if len(transforms) != 0 && !strings.Contains(err.Error(), "versioned collection") {
						t.Fatalf("expected retained-snapshot safety rejection: %v", err)
					}
				}
			}
		})
	}
}

func TestSQLitePresentationScalarEditorMetadata(t *testing.T) {
	resolve := func(fields ...field.Node) schema.Manifest {
		t.Helper()
		m, err := ridu.Resolve(ridu.Config{Name: "Editor", Collections: []ridu.Collection{{Slug: "posts", Versions: true, Fields: fields}}})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	before := resolve(field.Number("amount"), field.Code("source"), field.Date("date"))
	directory := t.TempDir()
	if _, err := CreateArtifact(context.Background(), directory, "initial", before, time.Unix(1, 0), ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	for i, fields := range []field.Fields{
		{field.Number("amount").Step(1), field.Code("source").Admin(field.Admin{CodeLanguage: "go"}), field.Date("date")},
		{field.Number("amount").Step(0.5), field.Code("source").Admin(field.Admin{CodeLanguage: "javascript"}), field.Date("date")},
		{field.Number("amount"), field.Code("source"), field.Date("date")},
	} {
		after := resolve(fields...)
		if _, err := CreateArtifact(context.Background(), directory, fmt.Sprintf("editor-%d", i), after, time.Unix(int64(i+2), 0), ArtifactOptions{}); !errors.Is(err, migrationartifact.ErrSchemaCurrent) {
			t.Fatalf("editor metadata migration = %v", err)
		}
		before = after
	}
	for name, fields := range map[string]field.Fields{
		"number bounds":   {field.Number("amount").Step(2).Min(1), field.Code("source"), field.Date("date")},
		"code length":     {field.Number("amount"), field.Code("source").MinLength(10).Admin(field.Admin{CodeLanguage: "go"}), field.Date("date")},
		"date wire shape": {field.Number("amount"), field.Code("source"), field.Date("date").Format(field.TimeOnly)},
	} {
		t.Run(name, func(t *testing.T) {
			after := resolve(fields...)
			for _, allow := range []bool{false, true} {
				if _, err := planArtifact(context.Background(), "validation", &before, after, allow); err == nil {
					t.Fatalf("validation change accepted (allow-destructive=%v)", allow)
				}
			}
		})
	}
}

func TestSQLitePresentationPreservesOperationalContentLocaleSettings(t *testing.T) {
	config := ridu.Config{Name: "Locales",
		Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}},
		Admin: ridu.AdminConfig{Localization: ridu.AdminLocalizationConfig{
			DefaultLanguage: "en", Languages: []ridu.AdminLanguage{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}},
			DefaultTimeZone: "Europe/London", TimeZones: []ridu.AdminTimeZone{{ID: "Europe/London", Label: "London"}, {ID: "UTC", Label: "UTC"}},
		}},
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}},
	}
	before, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	// Content locales key stored values; the admin interface's languages and
	// timezones, changed beside them below, do not.
	cases := map[string]func(*schema.Snapshot){
		"locale code":     func(s *schema.Snapshot) { s.Application.Localization.Locales[1].Code = "de" },
		"remove locale":   func(s *schema.Snapshot) { s.Application.Localization.Locales = s.Application.Localization.Locales[:1] },
		"default locale":  func(s *schema.Snapshot) { s.Application.Localization.DefaultLocale = "fr" },
		"fallback policy": func(s *schema.Snapshot) { s.Application.Localization.Fallback = false },
		"fallback chain": func(s *schema.Snapshot) {
			s.Application.Localization.Locales[1].FallbackLocales = []schema.LocaleCode{"en"}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			after := before.Snapshot()
			after.Application.Name = "New name"
			after.Application.AdminLocalization.DefaultLanguage = "fr"
			after.Application.AdminLocalization.DefaultTimeZone = "UTC"
			settings := after.Application.AdminLocalization
			settings.Languages[0], settings.Languages[1] = settings.Languages[1], settings.Languages[0]
			settings.TimeZones[0], settings.TimeZones[1] = settings.TimeZones[1], settings.TimeZones[0]
			after.Application.Localization.Locales[1].RTL = true
			settings.Languages = append(settings.Languages, schema.AdminLanguage{Code: "de", Label: "German"})
			settings.TimeZones = settings.TimeZones[:1]
			mutate(&after)
			for _, validate := range []func(schema.Snapshot, schema.Snapshot) error{validateSQLiteAdditiveTransition, validateSQLiteTransformedTransition} {
				if err := validate(before.Snapshot(), after); err == nil {
					t.Fatal("operational locale change was ignored")
				}
			}
		})
	}
}

func TestSQLitePresentationRejectsUnconfiguredInterfaceDefaults(t *testing.T) {
	before, err := ridu.Resolve(ridu.Config{Name: "Defaults", Collections: []ridu.Collection{{Slug: "posts", Fields: field.Fields{field.Text("title")}}}, Admin: ridu.AdminConfig{Localization: ridu.AdminLocalizationConfig{
		DefaultLanguage: "en", Languages: []ridu.AdminLanguage{{Code: "en", Label: "English"}},
		DefaultTimeZone: "UTC", TimeZones: []ridu.AdminTimeZone{{ID: "UTC", Label: "UTC"}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"language", "timezone"} {
		t.Run(name, func(t *testing.T) {
			snapshot := before.Snapshot()
			if name == "language" {
				snapshot.Application.AdminLocalization.DefaultLanguage = "fr"
			} else {
				snapshot.Application.AdminLocalization.DefaultTimeZone = "Europe/London"
			}
			if _, err := planArtifact(context.Background(), "invalid-default", &before, schema.NewManifest(snapshot), false); err == nil || !strings.Contains(err.Error(), "not configured") {
				t.Fatalf("unconfigured interface default accepted: %v", err)
			}
		})
	}
}

func TestSQLitePresentationWithUnversionedBackfill(t *testing.T) {
	for _, cosmetic := range []string{"application-name", "versioned-label"} {
		t.Run(cosmetic, func(t *testing.T) {
			ctx := context.Background()
			config := ridu.Config{Name: "Audit", Collections: []ridu.Collection{
				{Slug: "posts", Versions: true, Fields: field.Fields{field.Text("title")}},
				{Slug: "notes", Fields: field.Fields{field.Text("body")}},
			}}
			before, err := ridu.Resolve(config)
			if err != nil {
				t.Fatal(err)
			}
			directory := t.TempDir()
			if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), ArtifactOptions{}); err != nil {
				t.Fatal(err)
			}
			backend := newSQLiteMigrationStore(t)
			if err := backend.ApplyArtifacts(ctx, directory); err != nil {
				t.Fatal(err)
			}
			app, err := ridu.New(config, backend)
			if err != nil {
				t.Fatal(err)
			}
			post, err := app.Local().Create(ctx, "posts", store.Values{"title": store.String("Original")}, ridu.MutationOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := app.Local().PublishChanges(ctx, "posts", post.ID, store.Values{"title": store.String("Edited")}, ridu.MutationOptions{ExpectedRevision: post.Revision}); err != nil {
				t.Fatal(err)
			}
			note, err := app.Local().Create(ctx, "notes", store.Values{}, ridu.MutationOptions{})
			if err != nil {
				t.Fatal(err)
			}
			original := sqlitePresentationRows(t, backend, []string{"ridu_documents", "ridu_versions"})
			config.Collections[1].Fields[0] = field.Text("body").Required()
			if cosmetic == "application-name" {
				config.Name = "Publication"
			} else {
				config.Collections[0].Fields[0] = field.Text("title").Label("Headline")
			}
			after, err := ridu.Resolve(config)
			if err != nil {
				t.Fatal(err)
			}
			descriptor := migration.DataTransformDescriptor{Name: "backfill-notes", Checksum: migration.DataTransformChecksum([]byte("backfill-notes-v1"))}
			// Requiring a field is additive: the artifact audits stored values
			// after its transforms instead of asking for destructive approval.
			if _, err := CreateArtifact(ctx, directory, "backfill", after, time.Unix(2, 0), ArtifactOptions{DataTransforms: []migration.DataTransformDescriptor{descriptor}}); err != nil {
				t.Fatal(err)
			}
			if kinds := sqliteLatestStepKinds(t, directory); len(kinds) != 3 || kinds[0] != migration.StepDataTransform || kinds[1] != migration.StepAuditRequiredValues {
				t.Fatalf("backfill steps = %v, want the transform before the required-value audit", kinds)
			}
			if err := backend.ApplyArtifacts(ctx, directory, migration.DataTransform{DataTransformDescriptor: descriptor,
				Up:   func(context.Context, migration.DataTransaction) error { return nil },
				Down: func(context.Context, migration.DataTransaction) error { return nil },
			}); err == nil || !strings.Contains(err.Error(), "RIDU_REQUIRED_VALUES_MISSING") || !strings.Contains(err.Error(), "notes.body in 1 document") {
				t.Fatalf("a transform that writes nothing passed the audit: %v", err)
			}
			// Raw manifest shapes stay authoritative at runtime; comparison projection
			// must never admit callback mutations to this versioned collection.
			attemptVersionedMutation := true
			transform := migration.DataTransform{DataTransformDescriptor: descriptor,
				Up: func(ctx context.Context, tx migration.DataTransaction) error {
					notes := after.Snapshot().Collections[1]
					page, err := tx.List(ctx, store.Request{Collection: notes})
					if err != nil {
						return err
					}
					for _, document := range page.Documents {
						if _, err := tx.Update(ctx, migration.UpdateRequest{Request: store.Request{Collection: notes, ID: document.ID}, Values: store.Values{"body": store.String("Backfilled")}}); err != nil {
							return err
						}
					}
					if attemptVersionedMutation {
						_, err := tx.Update(ctx, migration.UpdateRequest{Request: store.Request{Collection: after.Snapshot().Collections[0], ID: post.ID}, Values: store.Values{"title": store.String("Forbidden")}})
						return err
					}
					return nil
				},
				Down: func(context.Context, migration.DataTransaction) error { return nil },
			}
			if err := backend.ApplyArtifacts(ctx, directory, transform); err == nil || !strings.Contains(err.Error(), "cannot mutate versioned resource") {
				t.Fatalf("versioned callback guard = %v", err)
			}
			if got := sqlitePresentationRows(t, backend, []string{"ridu_documents", "ridu_versions"}); !reflect.DeepEqual(original, got) {
				t.Fatal("rejected callback did not roll back")
			}
			attemptVersionedMutation = false
			if err := VerifyArtifacts(ctx, directory, transform); err != nil {
				t.Fatalf("shadow replay: %v", err)
			}
			if err := backend.ApplyArtifacts(ctx, directory, transform); err != nil {
				t.Fatal(err)
			}
			sqlitePresentationReady(t, backend, directory, after)
			if got := sqlitePresentationRows(t, backend, []string{"ridu_versions"}); got["ridu_versions"] != original["ridu_versions"] {
				t.Fatal("backfill changed retained revisions")
			}
			updatedApp, err := ridu.New(config, backend)
			if err != nil {
				t.Fatal(err)
			}
			stored, err := updatedApp.Local().Find(ctx, "notes", note.ID, ridu.FindOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if body, _ := stored.Values["body"].StringValue(); body != "Backfilled" {
				t.Fatalf("backfill body = %q", body)
			}
			stored, err = updatedApp.Local().Find(ctx, "posts", post.ID, ridu.FindOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if title, _ := stored.Values["title"].StringValue(); title != "Edited" || stored.Revision != 2 {
				t.Fatalf("versioned document changed: %#v", stored)
			}
		})
	}
}

func TestSQLitePresentationRegisteredBlockLabels(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{Name: "Registered labels", Blocks: []field.Block{{Slug: "hero", Fields: field.Fields{field.Text("heading")}}}, Collections: []ridu.Collection{{Slug: "pages", Fields: field.Fields{field.Blocks("layout").References("hero")}}}})
	if err != nil {
		t.Fatal(err)
	}
	before, after := manifest.Snapshot(), manifest.Snapshot()
	after.Blocks[0].Labels = schema.BlockLabels{Singular: "Banner", Plural: "Banners", SingularTranslations: map[string]string{"fr": "Bannière"}, PluralTranslations: map[string]string{"fr": "Bannières"}}
	if err := validateSQLiteAdditiveTransition(before, after); err != nil {
		t.Fatal(err)
	}
	changed := manifest.Snapshot()
	maximum := 3
	changed.Blocks[0].Fields[0].Text = &schema.TextField{MaxLength: &maximum}
	if err := validateSQLiteAdditiveTransition(before, changed); err == nil {
		t.Fatal("comparison skipped changed referenced child constraints")
	}
	if after.Blocks[0].Labels.Plural != "Banners" || after.Blocks[0].Labels.PluralTranslations["fr"] != "Bannières" {
		t.Fatal("comparison mutated labels")
	}
}
