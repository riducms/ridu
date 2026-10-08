package postgres

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/store/conformance"
)

type explainedPlan struct {
	NodeType   string          `json:"Node Type"`
	Relation   string          `json:"Relation Name"`
	Index      string          `json:"Index Name"`
	SharedHit  int             `json:"Shared Hit Blocks"`
	SharedRead int             `json:"Shared Read Blocks"`
	Plans      []explainedPlan `json:"Plans"`
}

func (plan explainedPlan) walk(visit func(explainedPlan)) {
	visit(plan)
	for _, child := range plan.Plans {
		child.walk(visit)
	}
}

func explainStatement(t *testing.T, backend *Store, statement string, arguments ...any) explainedPlan {
	t.Helper()
	var encoded []byte
	if err := backend.pool.QueryRow(t.Context(), "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+statement, arguments...).Scan(&encoded); err != nil {
		t.Fatal(err)
	}
	var plans []struct {
		Plan explainedPlan `json:"Plan"`
	}
	if err := json.Unmarshal(encoded, &plans); err != nil || len(plans) != 1 {
		t.Fatalf("decode plan %s: %v", encoded, err)
	}
	return plans[0].Plan
}

// Published lists are a high-volume public path. At a few thousand live rows
// the page must come from an index in key order, stop after its limit, and
// read neither the working table nor every live row.
func TestPostgresPublishedListPlansUseLiveIndexes(t *testing.T) {
	ctx := t.Context()
	backend, collection := publishedReadFixture(t, field.Fields{
		field.Text("title"),
		field.Select("status").Options(field.Option{Value: "draft", Label: "Draft"}, field.Option{Value: "published", Label: "Published"}),
		field.Number("rank").Index(),
	}, ridu.LocalizationConfig{})
	working, live := quote(collectionTable(collection.ID)), quote(publishedCollectionTable(collection.ID))
	column := func(name string) string { return quote(fieldColumn(fieldNamed(collection.Fields, name).ID)) }
	columns := "id, created_at, updated_at, deleted_at, _status, _revision, " + column("title") + ", " + column("status") + ", " + column("rank")
	for _, statement := range []string{
		`INSERT INTO ` + working + ` (id, _status, _revision, ` + column("title") + `, ` + column("status") + `, ` + column("rank") + `)
SELECT 'post-' || lpad(g::text, 5, '0'), 'published', 1, 'Post ' || g, 'published', g FROM generate_series(1, 5000) AS g`,
		`INSERT INTO ` + live + ` (` + columns + `) SELECT ` + columns + ` FROM ` + working,
		`ANALYZE ` + working + `, ` + live,
	} {
		if _, err := backend.pool.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	access := query.Equal("status", "published").Node()
	selected := []query.Path{publishedReadPath(t, "title"), publishedReadPath(t, "status")}
	assertIndexedPage := func(name string, request store.Request, indexes ...string) {
		t.Helper()
		count, page, arguments, _, err := listStatements(request)
		if err != nil {
			t.Fatal(err)
		}
		plan := explainStatement(t, backend, page, append(arguments, 10, 0)...)
		used := false
		plan.walk(func(node explainedPlan) {
			switch {
			case node.NodeType == "Seq Scan" || node.NodeType == "Sort" || node.NodeType == "Hash Join" || node.NodeType == "Merge Join":
				t.Fatalf("%s page uses %s on %s", name, node.NodeType, node.Relation)
			case request.PublishedOnly && node.Relation == collectionTable(collection.ID):
				t.Fatalf("%s live page reads the working table", name)
			case slices.Contains(indexes, node.Index):
				used = true
			}
		})
		if !used || plan.SharedHit+plan.SharedRead > 120 {
			t.Fatalf("%s page used index %t with %d buffers", name, used, plan.SharedHit+plan.SharedRead)
		}
		if !request.PublishedOnly {
			return
		}
		plan = explainStatement(t, backend, count, arguments...)
		plan.walk(func(node explainedPlan) {
			if node.Relation == collectionTable(collection.ID) || node.NodeType == "Nested Loop" || node.NodeType == "Hash Join" {
				t.Fatalf("%s live count joins the working table: %s on %s", name, node.NodeType, node.Relation)
			}
		})
	}
	livePrimary := publishedCollectionTable(collection.ID) + "_pkey"
	assertIndexedPage("published by ID", store.Request{Collection: collection, PublishedOnly: true, Access: &access, Select: selected, Page: 1, Limit: 10}, livePrimary)
	rankIndex := ""
	workingTable := collectionAtlasTable(collection, collection.ID, atlasIdentityMap{}, nil)
	for _, index := range publishedCollectionAtlasTable(collection, collection.ID, atlasIdentityMap{}, nil, workingTable).Indexes {
		if !index.Unique {
			rankIndex = index.Name
		}
	}
	assertIndexedPage("published by rank", store.Request{Collection: collection, PublishedOnly: true, Access: &access, Select: selected, Sort: []query.Sort{query.Desc("rank")}, Page: 1, Limit: 10}, rankIndex)
	// A working read carries the live revision through one primary-key probe
	// per returned row.
	assertIndexedPage("working with live state", store.Request{Collection: collection, Select: selected, Page: 1, Limit: 10}, collectionTable(collection.ID)+"_pkey", livePrimary)
}

func TestPostgresLiveRowsFollowWorkingRowLifecycle(t *testing.T) {
	ctx := t.Context()
	backend, collection := publishedReadFixture(t, field.Fields{field.Text("title"), field.Relationship("parent", "posts")}, ridu.LocalizationConfig{})
	live := quote(publishedCollectionTable(collection.ID))
	write, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer write.Rollback(ctx)
	published, err := write.Create(ctx, store.CreateRequest{Collection: collection, ID: "a", Status: store.StatusPublished, Values: store.Values{"title": store.String("Live")}})
	if err != nil || published.PublishedRevision != 1 {
		t.Fatalf("create published = %#v, %v", published, err)
	}
	pending, err := conformance.LockedUpdate(ctx, write, store.UpdateRequest{Request: store.Request{Collection: collection, ID: "a", ExpectedRevision: 1}, Intent: store.WriteIntentSaveDraft,
		Values: store.Values{"title": store.String("Pending"), "parent": store.String("a")}})
	if err != nil || pending.PublishedRevision != 1 || !pending.HasDraftChanges || pending.Revision != 2 {
		t.Fatalf("save draft = %#v, %v", pending, err)
	}
	if _, err := write.Create(ctx, store.CreateRequest{Collection: collection, ID: "b", Status: store.StatusDraft, Values: store.Values{"title": store.String("Draft")}}); err != nil {
		t.Fatal(err)
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	read, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	page, err := read.List(ctx, store.Request{Collection: collection, Page: 1, Limit: 10})
	if err != nil || len(page.Documents) != 2 {
		t.Fatalf("working list = %#v, %v", page, err)
	}
	if a, b := page.Documents[0], page.Documents[1]; a.PublishedRevision != 1 || !a.HasDraftChanges || b.PublishedRevision != 0 || b.HasDraftChanges {
		t.Fatalf("working list published state = %#v", page.Documents)
	}
	locked, err := read.Find(ctx, store.Request{Collection: collection, ID: "a", Lock: store.LockMutation})
	if err != nil || locked.PublishedRevision != 1 || !locked.HasDraftChanges {
		t.Fatalf("locked working read = %#v, %v", locked, err)
	}
	livePage, err := read.List(ctx, store.Request{Collection: collection, PublishedOnly: true, Page: 1, Limit: 10})
	if err != nil || len(livePage.Documents) != 1 || postgresTitle(livePage.Documents[0]) != "Live" || livePage.Documents[0].PublishedRevision != 0 {
		t.Fatalf("live list = %#v, %v", livePage, err)
	}
	if err := read.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	liveDeletion := func() (bool, bool) {
		t.Helper()
		var deleted *time.Time
		err := backend.pool.QueryRow(ctx, `SELECT deleted_at FROM `+live+` WHERE id = 'a'`).Scan(&deleted)
		if err != nil {
			return false, false
		}
		return true, deleted != nil
	}
	write, err = backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	trashed, err := write.Trash(ctx, store.Request{Collection: collection, ID: "a"})
	if err != nil || trashed.DeletedAt == nil || trashed.PublishedRevision != 1 || !trashed.HasDraftChanges {
		t.Fatalf("trash = %#v, %v", trashed, err)
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if exists, deleted := liveDeletion(); !exists || !deleted {
		t.Fatalf("trash did not mirror into the live row: exists %t, deleted %t", exists, deleted)
	}
	write, err = backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := write.Restore(ctx, store.Request{Collection: collection, ID: "a"})
	if err != nil || restored.DeletedAt != nil || restored.PublishedRevision != 1 || !restored.HasDraftChanges {
		t.Fatalf("restore = %#v, %v", restored, err)
	}
	republished, err := conformance.LockedUpdate(ctx, write, store.UpdateRequest{Request: store.Request{Collection: collection, ID: "a", ExpectedRevision: restored.Revision}, Intent: store.WriteIntentPublish, Values: store.Values{}})
	if err != nil || republished.PublishedRevision != republished.Revision || republished.HasDraftChanges {
		t.Fatalf("publish = %#v, %v", republished, err)
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if exists, deleted := liveDeletion(); !exists || deleted {
		t.Fatalf("restore did not mirror into the live row: exists %t, deleted %t", exists, deleted)
	}
	var references int
	if err := backend.pool.QueryRow(ctx, `SELECT count(*) FROM ridu_document_references WHERE owner_document_id = 'a' AND published_head`).Scan(&references); err != nil || references != 1 {
		t.Fatalf("live reference index = %d, %v", references, err)
	}
	write, err = backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conformance.LockedUpdate(ctx, write, store.UpdateRequest{Request: store.Request{Collection: collection, ID: "a"}, Intent: store.WriteIntentSaveDraft, Values: store.Values{"parent": store.Null()}}); err != nil {
		t.Fatal(err)
	}
	if _, err := write.Delete(ctx, store.Request{Collection: collection, ID: "a", Deletion: store.DeletionAll}); err != nil {
		t.Fatal(err)
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if exists, _ := liveDeletion(); exists {
		t.Fatal("deleting the working row left its live row")
	}
	if err := backend.pool.QueryRow(ctx, `SELECT count(*) FROM ridu_document_references WHERE owner_document_id = 'a'`).Scan(&references); err != nil || references != 0 {
		t.Fatalf("deleted document references = %d, %v", references, err)
	}
}

func TestPostgresUploadReferencesIncludeLiveRows(t *testing.T) {
	ctx := t.Context()
	backend := migrationArtifactTestBackend(t)
	manifest := atlasUploadReferenceManifest()
	directory := t.TempDir()
	if _, err := CreateArtifact(ctx, directory, "initial", manifest, time.Unix(1, 0), ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	media := manifest.Snapshot().Collections[0]
	sizes := func(key string) store.Value {
		return store.Object(store.Values{"thumbnail": store.Object(store.Values{"objectKey": store.String(key)})})
	}
	write, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer write.Rollback(ctx)
	created, err := write.Create(ctx, store.CreateRequest{Collection: media, ID: "image", Status: store.StatusPublished,
		Values: store.Values{"objectKey": store.String("live-original"), "sizes": sizes("live-thumbnail")}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conformance.LockedUpdate(ctx, write, store.UpdateRequest{Request: store.Request{Collection: media, ID: created.ID, ExpectedRevision: created.Revision}, Intent: store.WriteIntentSaveDraft,
		Values: store.Values{"objectKey": store.String("draft-original"), "sizes": sizes("draft-thumbnail")}}); err != nil {
		t.Fatal(err)
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	read, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer read.Rollback(ctx)
	referenced, err := read.(*documentTransaction).ReferencedUploadObjects(ctx, store.UploadReferenceRequest{
		Collections: []schema.Collection{media},
		ObjectKeys:  []string{"draft-original", "draft-thumbnail", "live-original", "live-thumbnail", "unreferenced"},
	})
	if want := []string{"draft-original", "draft-thumbnail", "live-original", "live-thumbnail"}; err != nil || !slices.Equal(referenced, want) {
		t.Fatalf("referenced upload objects = %v, %v; want %v", referenced, err, want)
	}
}

func TestPostgresRetiringVersionedCollectionDropsBothTables(t *testing.T) {
	ctx := t.Context()
	backend := migrationArtifactTestBackend(t)
	versions := &schema.VersionSettings{Drafts: true, MaxPerDocument: 10}
	resource := func(id schema.StableID) schema.Collection {
		return schema.Collection{ID: id, Slug: schema.CollectionSlug(id), Capabilities: schema.Capabilities{Versions: true}, Versions: versions,
			Labels: schema.CollectionLabels{Singular: string(id), Plural: string(id)}, Fields: []schema.Field{atlasTextField(id+"-title", "title")}}
	}
	manifest := func(ids ...schema.StableID) schema.Manifest {
		snapshot := schema.Snapshot{Version: schema.CurrentVersion, Application: schema.Application{Name: "Live retirement"}, Plugins: []schema.Plugin{}}
		for _, id := range ids {
			snapshot.Collections = append(snapshot.Collections, resource(id))
		}
		return schema.NewManifest(snapshot)
	}
	before, after := manifest("people", "posts"), manifest("posts")
	directory := t.TempDir()
	if _, err := CreateArtifact(ctx, directory, "initial", before, time.Unix(1, 0), ArtifactOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifacts(ctx, directory); err != nil {
		t.Fatal(err)
	}
	write, err := backend.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer write.Rollback(ctx)
	if _, err := write.Create(ctx, store.CreateRequest{Collection: before.Snapshot().Collections[0], ID: "ada", Status: store.StatusPublished, Values: store.Values{"title": store.String("Ada")}}); err != nil {
		t.Fatal(err)
	}
	if err := write.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateArtifact(ctx, directory, "retire-people", after, time.Unix(2, 0), ArtifactOptions{AllowDestructive: true}); err != nil {
		t.Fatal(err)
	}
	if err := backend.ApplyArtifactsWithOptions(ctx, directory, RunnerOptions{AllowMaintenance: true}); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{collectionTable("people"), publishedCollectionTable("people")} {
		var exists bool
		if err := backend.pool.QueryRow(ctx, `SELECT to_regclass(quote_ident($1)) IS NOT NULL`, table).Scan(&exists); err != nil || exists {
			t.Fatalf("retired table %s exists %t, %v", table, exists, err)
		}
	}
	if err := backend.Ready(ctx, after); err != nil {
		t.Fatal(err)
	}
}
