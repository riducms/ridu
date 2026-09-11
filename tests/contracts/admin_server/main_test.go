package main

import (
	"context"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/riducms/ridu"
	localstorage "github.com/riducms/ridu/adapters/storage/local"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestPostgresFixtureURLScopesConnectionsToOwnedSchema(t *testing.T) {
	configured, err := postgresFixtureURL(
		"postgres://ridu:ridu@localhost:5432/ridu?sslmode=disable",
		"ridu_admin_fixture_contract",
	)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(configured)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Query().Get("search_path"); got != "ridu_admin_fixture_contract" {
		t.Fatalf("search_path = %q", got)
	}
	if got := parsed.Query().Get("sslmode"); got != "disable" {
		t.Fatalf("sslmode = %q", got)
	}
}

func TestPostgresFixtureSchemaRejectsNonFixtureTargets(t *testing.T) {
	for _, schemaName := range []string{"public", "ridu_admin_fixture_", "ridu_admin_fixture_BAD"} {
		t.Run(schemaName, func(t *testing.T) {
			t.Setenv("RIDU_POSTGRES_FIXTURE_SCHEMA", schemaName)
			if _, err := postgresFixtureSchema(); err == nil {
				t.Fatalf("postgresFixtureSchema accepted %q", schemaName)
			}
		})
	}
}

func TestPostgresFixtureSchemaGeneratesUniqueReservedNames(t *testing.T) {
	t.Setenv("RIDU_POSTGRES_FIXTURE_SCHEMA", "")
	first, err := postgresFixtureSchema()
	if err != nil {
		t.Fatal(err)
	}
	second, err := postgresFixtureSchema()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("generated duplicate fixture schema %q", first)
	}
	if !postgresFixtureSchemaPattern.MatchString(first) || !postgresFixtureSchemaPattern.MatchString(second) {
		t.Fatalf("generated schemas are outside the reserved namespace: %q %q", first, second)
	}
}

func TestResetFixtureUploadsClearsOnlyOwnedRoot(t *testing.T) {
	root := t.TempDir()
	sibling := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "admin-fixture", "objects"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "admin-fixture", "objects", "stale.png"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	siblingFile := filepath.Join(sibling, "keep.txt")
	if err := os.WriteFile(siblingFile, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := resetFixtureUploads(root); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("fixture upload root still contains %d entries", len(entries))
	}
	if content, err := os.ReadFile(siblingFile); err != nil || string(content) != "keep" {
		t.Fatalf("reset changed storage outside its owned root: content=%q err=%v", content, err)
	}
}

func TestResetSQLiteFixtureRemovesOnlyOwnedDatabaseFiles(t *testing.T) {
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "admin.sqlite")
	for _, path := range []string{databasePath, databasePath + "-wal", databasePath + "-shm"} {
		if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sibling := filepath.Join(directory, "keep.txt")
	if err := os.WriteFile(sibling, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := resetSQLiteFixture(databasePath); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{databasePath, databasePath + "-wal", databasePath + "-shm"} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("SQLite fixture file %s still exists: %v", filepath.Base(path), err)
		}
	}
	if content, err := os.ReadFile(sibling); err != nil || string(content) != "keep" {
		t.Fatalf("reset changed sibling file: content=%q err=%v", content, err)
	}
}

func TestResetRouteRequiresLoopbackListener(t *testing.T) {
	for _, address := range []string{"127.0.0.1:18081", "[::1]:18081", "localhost:18081"} {
		if err := requireLoopbackListener(address); err != nil {
			t.Errorf("requireLoopbackListener(%q): %v", address, err)
		}
	}
	for _, address := range []string{"0.0.0.0:18081", "[::]:18081", "192.0.2.1:18081", ":18081"} {
		if err := requireLoopbackListener(address); err == nil {
			t.Errorf("requireLoopbackListener(%q) succeeded", address)
		}
	}
}

func TestResetTokenRequiresExactExplicitValue(t *testing.T) {
	if resetTokenMatches("", "") || resetTokenMatches("token", "") || resetTokenMatches("wrong", "token") {
		t.Fatal("reset token accepted a disabled or mismatched value")
	}
	if !resetTokenMatches("ridu-playwright-reset-v1", "ridu-playwright-reset-v1") {
		t.Fatal("reset token rejected an exact value")
	}
}

func TestFixtureListenersFailWhenEitherPortIsOccupied(t *testing.T) {
	t.Run("admin", func(t *testing.T) {
		occupied, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer occupied.Close()
		adminListener, previewListener, err := listenFixtureServers(occupied.Addr().String(), "127.0.0.1:0")
		if adminListener != nil || previewListener != nil {
			t.Fatal("occupied admin address returned a listener")
		}
		if err == nil || !strings.Contains(err.Error(), "listen for admin fixture") {
			t.Fatalf("occupied admin listener error = %v", err)
		}
	})
	t.Run("preview", func(t *testing.T) {
		reservedAdmin, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		adminAddress := reservedAdmin.Addr().String()
		if err := reservedAdmin.Close(); err != nil {
			t.Fatal(err)
		}
		occupied, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer occupied.Close()
		adminListener, previewListener, err := listenFixtureServers(adminAddress, occupied.Addr().String())
		if adminListener != nil || previewListener != nil {
			t.Fatal("occupied preview address returned a listener")
		}
		if err == nil || !strings.Contains(err.Error(), "listen for preview fixture") {
			t.Fatalf("occupied preview listener error = %v", err)
		}
		rebound, err := net.Listen("tcp", adminAddress)
		if err != nil {
			t.Fatalf("admin listener leaked after preview bind failure: %v", err)
		}
		_ = rebound.Close()
	})
}

func TestPostgresFixtureOwnershipLockRejectsConcurrentProcess(t *testing.T) {
	databaseURL := os.Getenv("RIDU_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("RIDU_POSTGRES_URL is required for the PostgreSQL ownership-lock contract")
	}
	t.Setenv("RIDU_POSTGRES_FIXTURE_SCHEMA", "")
	schemaName, err := postgresFixtureSchema()
	if err != nil {
		t.Fatal(err)
	}
	release, err := acquirePostgresFixtureLock(context.Background(), databaseURL, schemaName)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if secondRelease, err := acquirePostgresFixtureLock(context.Background(), databaseURL, schemaName); err == nil {
		secondRelease()
		t.Fatal("second process acquired the same PostgreSQL fixture schema lock")
	} else if !strings.Contains(err.Error(), "already owned") {
		t.Fatalf("second ownership error = %v", err)
	}
}

func TestFixtureResolvesEveryImplementedAdminFieldFamily(t *testing.T) {
	uploadStorage, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := ridu.Resolve(fixtureConfig(uploadStorage))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := manifest.Snapshot()
	if len(snapshot.Collections) != 27 {
		t.Fatalf("collections = %d, want 27", len(snapshot.Collections))
	}
	if len(snapshot.Globals) != 2 || snapshot.Globals[0].Slug != "site-settings" || !snapshot.Globals[0].Capabilities.Global || !snapshot.Globals[0].Capabilities.Versions || snapshot.Globals[1].Slug != "validation-settings" {
		t.Fatalf("globals = %#v", snapshot.Globals)
	}

	wantedCollections := map[schema.CollectionSlug]bool{
		"primitive-products": false,
		"unified-articles":   false,
		"issue-targets":      false,
		"dynamic-defaults":   false,
		"live-validation":    false,
		"users":              false, "media": false, "categories": false, "folders": false, "posts": false,
		"pages": false, "events": false, "editorial-notes": false, "redirects": false,
		"payload-only-capabilities": false, "forms": false, "form-submissions": false, "outlines": false,
		"block-articles": false,
		"block-pages":    false,
		"block-names":    false,
		"inline-pages":   false, "inline-articles": false,
		"reference-pages": false, "reference-articles": false,
		"inline-block-labels": false, "reference-block-labels": false,
	}
	fieldTypes := make(map[schema.FieldType]bool)
	var inspectFields func([]schema.Field)
	inspectFields = func(fields []schema.Field) {
		for _, candidate := range fields {
			fieldTypes[candidate.Type] = true
			inspectFields(schema.ChildFields(candidate))
		}
	}
	for _, collection := range snapshot.Collections {
		if _, expected := wantedCollections[collection.Slug]; !expected {
			t.Fatalf("unexpected collection %q", collection.Slug)
		}
		wantedCollections[collection.Slug] = true
		inspectFields(collection.Fields)
	}
	var trashCollections []schema.CollectionSlug
	for _, collection := range snapshot.Collections {
		if collection.Capabilities.Trash {
			trashCollections = append(trashCollections, collection.Slug)
		}
	}
	if !slices.Equal(trashCollections, []schema.CollectionSlug{"media", "posts"}) {
		t.Fatalf("trash collections = %v", trashCollections)
	}
	for slug, found := range wantedCollections {
		if !found {
			t.Errorf("collection %q is missing", slug)
		}
	}
	var categoryJoin, postFilter, polymorphicFilter *schema.Field
	for index := range snapshot.Collections {
		collection := &snapshot.Collections[index]
		for fieldIndex := range collection.Fields {
			candidate := &collection.Fields[fieldIndex]
			if collection.Slug == "categories" && candidate.Name == "posts" {
				categoryJoin = candidate
			}
			if collection.Slug == "posts" && candidate.Name == "sameCategoryPosts" {
				postFilter = candidate
			}
			if collection.Slug == "posts" && candidate.Name == "relatedContent" {
				polymorphicFilter = candidate
			}
		}
	}
	if categoryJoin == nil || categoryJoin.Join == nil || categoryJoin.Join.On.String() != "category" {
		t.Fatalf("category inverse join = %#v", categoryJoin)
	}
	if !slices.Equal(categoryJoin.Join.DefaultColumns, []string{"title", "status", "author", "updatedAt"}) || categoryJoin.Join.DefaultSort != "title" || categoryJoin.Join.AllowCreate == nil || !*categoryJoin.Join.AllowCreate {
		t.Fatalf("category join admin metadata = %#v", categoryJoin.Join)
	}
	if postFilter == nil || postFilter.Relationship == nil || len(postFilter.Relationship.OptionFilters) != 2 || postFilter.Relationship.OptionFilters[0].SourcePath.String() != "category" || postFilter.Relationship.OptionFilters[0].TargetPath.String() != "category" {
		t.Fatalf("post relationship option filter = %#v", postFilter)
	}
	if postFilter.Relationship.OptionFilters[1].TargetPath.String() != "seo.title" || postFilter.Relationship.OptionFilters[1].Operator != "equals" {
		t.Fatalf("post nested relationship filters = %#v", postFilter.Relationship.OptionFilters)
	}
	if polymorphicFilter == nil || polymorphicFilter.Relationship == nil || len(polymorphicFilter.Relationship.OptionFilters) != 2 || polymorphicFilter.Relationship.OptionFilters[1].CollectionSlug != "pages" || polymorphicFilter.Relationship.OptionFilters[1].TargetPath.String() != "navigation.showInHeader" {
		t.Fatalf("polymorphic relationship filters = %#v", polymorphicFilter)
	}
	for _, fieldType := range []schema.FieldType{
		schema.FieldTypeText, schema.FieldTypeCode, schema.FieldTypeTextarea, schema.FieldTypeEmail, schema.FieldTypeDate,
		schema.FieldTypeNumber, schema.FieldTypeCheckbox, schema.FieldTypeJSON, schema.FieldTypeSelect,
		schema.FieldTypeTextList, schema.FieldTypeNumberList,
		schema.FieldTypeRadio, schema.FieldTypePoint, schema.FieldTypeUI, schema.FieldTypeJoin, schema.FieldTypeVirtual,
		schema.FieldTypeRelationship, schema.FieldTypeUpload, schema.FieldTypeGroup, schema.FieldTypeArray,
		schema.FieldTypeBlocks, schema.FieldTypePlugin,
	} {
		if !fieldTypes[fieldType] {
			t.Errorf("field type %q is not represented", fieldType)
		}
	}
	var showcase schema.Collection
	for _, collection := range snapshot.Collections {
		if collection.Slug == "payload-only-capabilities" {
			showcase = collection
			break
		}
	}
	showcaseByName := make(map[string]schema.Field, len(showcase.Fields))
	var team, curriculumNodes, questionParts schema.Field
	for _, candidate := range showcase.Fields {
		showcaseByName[candidate.Name] = candidate
		switch candidate.Name {
		case "team":
			team = candidate
		case "curriculumNodes":
			curriculumNodes = candidate
		case "questionParts":
			questionParts = candidate
		}
	}
	if team.Nested == nil || team.Nested.MinRows != 1 || team.Nested.MaxRows != 3 || team.Nested.RowLabel != "displayName" {
		t.Fatalf("showcase team metadata = %#v", team.Nested)
	}
	for name, candidate := range map[string]schema.Field{
		"curriculumNodes": curriculumNodes,
		"questionParts":   questionParts,
	} {
		if candidate.Nested == nil || candidate.Nested.RowLabelComponent == nil || candidate.Nested.RowLabelComponent.Reference != "app:compositeRowLabel" {
			t.Fatalf("showcase %s row-label component metadata = %#v", name, candidate.Nested)
		}
	}
	if got := string(curriculumNodes.Nested.RowLabelComponent.Config); !strings.Contains(got, `"kind":"typedOrder"`) {
		t.Fatalf("curriculum row-label config = %s", got)
	}
	if got := string(questionParts.Nested.RowLabelComponent.Config); !strings.Contains(got, `"kind":"keyLabel"`) {
		t.Fatalf("question-parts row-label config = %s", got)
	}
	internalName := showcaseByName["internalName"]
	implementationNotes := showcaseByName["implementationNotes"]
	if internalName.Admin.Collapsible == nil || !internalName.Admin.Collapsible.InitiallyCollapsed || implementationNotes.Admin.Collapsible == nil {
		t.Fatalf("showcase collapsible metadata missing: %#v %#v", internalName.Admin, implementationNotes.Admin)
	}
	title := showcaseByName["title"]
	priority := showcaseByName["priority"]
	location := showcaseByName["location"]
	fieldSummary := showcaseByName["fieldSummary"]
	audiences := showcaseByName["audiences"]
	if title.Admin.Placeholder != "Name this field showcase" || title.Admin.PlaceholderTranslations["fr"] == "" || !priority.Admin.Sidebar || !location.Admin.Sidebar || !internalName.Admin.Hidden || !fieldSummary.Admin.Sidebar || audiences.Select == nil || !audiences.Select.HasMany {
		t.Fatalf("showcase admin field metadata missing: title=%#v priority=%#v location=%#v hidden=%#v summary=%#v audiences=%#v", title.Admin, priority.Admin, location.Admin, internalName.Admin, fieldSummary.Admin, audiences.Select)
	}
}

func TestFixtureSeedsPayloadStyleAccessHooksAndVersions(t *testing.T) {
	ctx := context.Background()
	uploadStorage, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	application, err := ridu.New(fixtureConfig(uploadStorage), teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	seed, err := seedFixture(ctx, application)
	if err != nil {
		t.Fatal(err)
	}

	title, _ := seed.DraftPost.Values["title"].StringValue()
	lastEditedBy, _ := seed.DraftPost.Values["lastEditedBy"].StringValue()
	if title != "Relationship field notes" || lastEditedBy != seed.Contributor.ID {
		t.Fatalf("hook output title=%q lastEditedBy=%q", title, lastEditedBy)
	}
	if seed.PublishedPost.Status != store.StatusPublished || seed.PublishedPost.Revision < 2 {
		t.Fatalf("published post status=%q revision=%d", seed.PublishedPost.Status, seed.PublishedPost.Revision)
	}
	categories, err := application.Local().List(ctx, "categories", ridu.ListOptions{Actor: &seed.Editor})
	if err != nil {
		t.Fatal(err)
	}
	var news *store.Document
	for index := range categories.Documents {
		name, _ := categories.Documents[index].Values["name"].StringValue()
		if name == "News" {
			news = &categories.Documents[index]
		}
	}
	if news == nil {
		t.Fatal("seeded News category is missing")
	}
	displayLabel, _ := news.Values["displayLabel"].StringValue()
	joinedPosts, joined := news.Values["posts"].CopyList()
	if displayLabel != "Category · News" || !joined || len(joinedPosts) != 1 {
		t.Fatalf("computed category output label=%q posts=%#v", displayLabel, joinedPosts)
	}

	assertTotal(t, application, "posts", nil, 1)
	assertTotal(t, application, "posts", &seed.Editor, 2)
	assertTotal(t, application, "posts", &seed.Contributor, 2)
	assertTotal(t, application, "editorial-notes", &seed.Contributor, 1)

	adminNotes, err := application.Local().List(ctx, "editorial-notes", ridu.ListOptions{Actor: &seed.Administrator})
	if err != nil {
		t.Fatal(err)
	}
	editorNotes, err := application.Local().List(ctx, "editorial-notes", ridu.ListOptions{Actor: &seed.Editor})
	if err != nil {
		t.Fatal(err)
	}
	if !pageContainsField(adminNotes, "confidentialDetails") {
		t.Fatal("administrator response redacted confidentialDetails")
	}
	if pageContainsField(editorNotes, "confidentialDetails") {
		t.Fatal("editor response leaked confidentialDetails")
	}

	_, err = application.Local().Update(ctx, "posts", seed.DraftPost.ID, store.Values{
		"status": store.String("published"),
	}, ridu.MutationOptions{Actor: &seed.Contributor})
	assertOperationCode(t, err, "field_access_denied")
	_, err = application.Local().Update(ctx, "users", seed.Contributor.ID, store.Values{
		"role": store.String(roleAdministrator),
	}, ridu.MutationOptions{Actor: &seed.Contributor})
	assertOperationCode(t, err, "field_access_denied")
	if _, err := application.Local().Update(ctx, "users", seed.Contributor.ID, store.Values{
		"name": store.String("Demo Author Updated"), "role": store.String(roleContributor),
	}, ridu.MutationOptions{Actor: &seed.Editor}); err != nil {
		t.Fatalf("editor resubmitting an unchanged protected role: %v", err)
	}
}

func assertTotal(t *testing.T, application *ridu.App, collection string, actor *store.Document, expected int) {
	t.Helper()
	page, err := application.Local().List(context.Background(), collection, ridu.ListOptions{Actor: actor})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != expected {
		t.Fatalf("%s total = %d, want %d", collection, page.Total, expected)
	}
}

func pageContainsField(page store.Page, field string) bool {
	for _, document := range page.Documents {
		if _, exists := document.Values[field]; exists {
			return true
		}
	}
	return false
}

func assertOperationCode(t *testing.T, err error, code string) {
	t.Helper()
	var operationError *ridu.OperationError
	if !errors.As(err, &operationError) || operationError.Code != code {
		t.Fatalf("operation error = %v, want code %q", err, code)
	}
}
