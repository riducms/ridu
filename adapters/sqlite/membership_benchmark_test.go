package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"sync"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// BenchmarkSQLiteMembershipFilter measures the two reads behind a filtered
// admin list on an on-disk database: the first page (limit 10, no total) and
// the count, which the engine reads as a one-row counted page. Each document
// lists three of 100 tags and two of 1,000 authors, so "tagged X" matches about
// 3% of documents and "written by Y" about 0.2%.
//
//	go test ./adapters/sqlite -run '^$' -bench SQLiteMembershipFilter -benchtime 5x
func BenchmarkSQLiteMembershipFilter(b *testing.B) {
	for _, documents := range []int{10_000, 100_000} {
		b.Run(fmt.Sprintf("docs=%d", documents), func(b *testing.B) {
			fixture := newSQLiteMembershipBenchmark(b, documents)
			for _, filter := range []struct {
				name       string
				expression query.Expression
				matches    int
			}{
				{"relationship", query.In("authors", sqliteBenchmarkAuthor(7)), fixture.authorMatches},
				{"textlist", query.In("tags", sqliteBenchmarkTag(7)), fixture.tagMatches},
			} {
				node := filter.expression.Node()
				list := store.Request{Collection: fixture.posts, Filter: &node, Page: 1, Limit: 10, SkipTotal: true}
				count := store.Request{Collection: fixture.posts, Filter: &node, Page: 1, Limit: 1}
				b.Run("field="+filter.name+"/op=list", func(b *testing.B) {
					fixture.measure(b, list, func(page store.Page) bool {
						return len(page.Documents) == 10 && page.HasNextPage
					})
				})
				b.Run("field="+filter.name+"/op=count", func(b *testing.B) {
					fixture.measure(b, count, func(page store.Page) bool {
						return page.Total != nil && *page.Total == filter.matches
					})
				})
			}
		})
	}
}

type sqliteMembershipBenchmark struct {
	backend                   *Store
	posts                     schema.Collection
	tagMatches, authorMatches int
}

func sqliteBenchmarkTag(index int) string    { return fmt.Sprintf("tag-%03d", index) }
func sqliteBenchmarkAuthor(index int) string { return fmt.Sprintf("person-%04d", index) }

func newSQLiteMembershipBenchmark(b *testing.B, documents int) *sqliteMembershipBenchmark {
	b.Helper()
	ctx := context.Background()
	manifest, err := ridu.Resolve(ridu.Config{Name: "SQLite membership benchmark", Collections: []ridu.Collection{
		{Slug: "people", Fields: field.Fields{field.Text("name")}},
		{Slug: "posts", Fields: field.Fields{field.Text("title"), field.TextList("tags"), field.Relationships("authors", "people")}},
	}})
	if err != nil {
		b.Fatal(err)
	}
	fixture := &sqliteMembershipBenchmark{}
	for _, collection := range manifest.Snapshot().Collections {
		if collection.Slug == "posts" {
			fixture.posts = collection
		}
	}
	fixture.backend, err = Open(ctx, filepath.Join(b.TempDir(), "membership.sqlite"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = fixture.backend.Close() })
	if err := fixture.backend.Migrate(ctx, manifest); err != nil {
		b.Fatal(err)
	}
	random := rand.New(rand.NewPCG(1, uint64(documents)))
	now := encodeTime(time.Date(2026, time.January, 2, 12, 0, 0, 0, time.UTC))
	if err := fixture.backend.withImmediate(ctx, func(connection *sql.Conn) error {
		statement, err := connection.PrepareContext(ctx, `INSERT INTO ridu_documents (
  collection_id, id, created_at, updated_at, deleted_at, status, revision, values_json
) VALUES (?, ?, ?, ?, NULL, '', 0, ?)`)
		if err != nil {
			return err
		}
		defer statement.Close()
		for index := range documents {
			tags := random.Perm(100)[:3]
			authors := random.Perm(1000)[:2]
			values := store.Values{
				"title":   store.String(fmt.Sprintf("Post %06d", index)),
				"tags":    store.List(store.String(sqliteBenchmarkTag(tags[0])), store.String(sqliteBenchmarkTag(tags[1])), store.String(sqliteBenchmarkTag(tags[2]))),
				"authors": store.List(store.String(sqliteBenchmarkAuthor(authors[0])), store.String(sqliteBenchmarkAuthor(authors[1]))),
			}
			for _, tag := range tags {
				if tag == 7 {
					fixture.tagMatches++
				}
			}
			for _, author := range authors {
				if author == 7 {
					fixture.authorMatches++
				}
			}
			encoded, err := values.MarshalJSON()
			if err != nil {
				return err
			}
			if _, err := statement.ExecContext(ctx, string(fixture.posts.ID), fmt.Sprintf("post-%06d", index), now, now, string(encoded)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		b.Fatal(err)
	}
	if _, err := fixture.backend.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		b.Fatal(err)
	}
	return fixture
}

// measure runs request b.N times and reports the peak Go heap in use while
// it ran, which includes values the residual matcher decodes from each row.
func (fixture *sqliteMembershipBenchmark) measure(b *testing.B, request store.Request, valid func(store.Page) bool) {
	ctx := context.Background()
	read := func() store.Page {
		transaction, err := fixture.backend.BeginSnapshot(ctx)
		if err != nil {
			b.Fatal(err)
		}
		page, err := transaction.List(ctx, request)
		if rollbackErr := transaction.Rollback(ctx); err == nil {
			err = rollbackErr
		}
		if err != nil {
			b.Fatal(err)
		}
		return page
	}
	if page := read(); !valid(page) {
		b.Fatalf("unexpected page: %d documents, total %v, next %t", len(page.Documents), page.Total, page.HasNextPage)
	}
	runtime.GC()
	samples := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
	var peak uint64
	sample := func() {
		metrics.Read(samples)
		peak = max(peak, samples[0].Value.Uint64())
	}
	sample()
	done := make(chan struct{})
	var sampler sync.WaitGroup
	sampler.Go(func() {
		ticker := time.NewTicker(200 * time.Microsecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				sample()
			}
		}
	})
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		read()
	}
	b.StopTimer()
	close(done)
	sampler.Wait()
	sample()
	b.ReportMetric(float64(b.Elapsed().Microseconds())/1000/float64(b.N), "ms/op")
	b.ReportMetric(float64(peak)/(1<<20), "peak-heap-MB")
}
