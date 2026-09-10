package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/riducms/ridu/internal/embedded"
	"github.com/riducms/ridu/internal/localization"
	populationwalk "github.com/riducms/ridu/internal/population"
	"github.com/riducms/ridu/internal/primitivefield"
	"github.com/riducms/ridu/internal/referenceindex"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	modernsqlite "modernc.org/sqlite"
)

type rowScanner interface{ Scan(...any) error }

const sqliteDocumentMatcherFunction = "ridu_document_matches"

// Residual ordering is reserved for genuinely repeated/non-indexable paths.
// Keep it finite so one authored sort cannot decode an unbounded collection in
// the application process.
const maxSQLiteResidualSortDocuments = 4096

var (
	sqliteMatcherSequence atomic.Int64
	sqliteMatchers        sync.Map
)

func init() {
	modernsqlite.MustRegisterScalarFunction(sqliteDocumentMatcherFunction, 7, sqliteDocumentMatcher)
}

func sqliteDocumentMatcher(_ *modernsqlite.FunctionContext, arguments []driver.Value) (driver.Value, error) {
	if len(arguments) != 7 {
		return nil, fmt.Errorf("SQLite document matcher requires seven arguments")
	}
	valuesJSON, valid := sqliteDriverString(arguments[0])
	if !valid {
		return nil, fmt.Errorf("SQLite document matcher values must be JSON text")
	}
	id, valid := sqliteDriverString(arguments[1])
	if !valid {
		return nil, fmt.Errorf("SQLite document matcher ID must be text")
	}
	createdAt, valid := sqliteDriverTime(arguments[2])
	if !valid {
		return nil, fmt.Errorf("SQLite document matcher createdAt must be an integer or RFC3339 timestamp")
	}
	updatedAt, valid := sqliteDriverTime(arguments[3])
	if !valid {
		return nil, fmt.Errorf("SQLite document matcher updatedAt must be an integer or RFC3339 timestamp")
	}
	status, valid := sqliteDriverString(arguments[4])
	if !valid {
		return nil, fmt.Errorf("SQLite document matcher status must be text")
	}
	revision, valid := sqliteDriverInt64(arguments[5])
	if !valid {
		return nil, fmt.Errorf("SQLite document matcher revision must be an integer")
	}
	token, valid := sqliteDriverInt64(arguments[6])
	if !valid {
		return nil, fmt.Errorf("SQLite document matcher token must be an integer")
	}
	registered, exists := sqliteMatchers.Load(token)
	if !exists {
		return nil, fmt.Errorf("SQLite document matcher token %d is unavailable", token)
	}
	request := registered.(store.Request)
	var values store.Values
	if err := json.Unmarshal([]byte(valuesJSON), &values); err != nil {
		return nil, fmt.Errorf("decode SQLite matcher values: %w", err)
	}
	document := store.Document{
		ID: id, CreatedAt: createdAt, UpdatedAt: updatedAt, Status: store.Status(status), Revision: int(revision),
		Values: currentValues(request.Collection.Fields, values),
	}
	if matchesRequest(document, request) {
		return int64(1), nil
	}
	return int64(0), nil
}

func sqliteDriverTime(value driver.Value) (time.Time, bool) {
	if encoded, valid := sqliteDriverInt64(value); valid {
		return decodeTime(encoded), true
	}
	text, valid := sqliteDriverString(value)
	if !valid {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	return parsed, err == nil
}

func sqliteDriverString(value driver.Value) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	case []byte:
		return string(typed), true
	default:
		return "", false
	}
}

func sqliteDriverInt64(value driver.Value) (int64, bool) {
	switch typed := value.(type) {
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	default:
		return 0, false
	}
}

func registerSQLiteMatcher(request store.Request) (int64, func()) {
	request.Filter = cloneQueryNode(request.Filter)
	request.Access = cloneQueryNode(request.Access)
	request.Locales = append([]schema.LocaleCode(nil), request.Locales...)
	request.LocaleChain = append([]schema.LocaleCode(nil), request.LocaleChain...)
	token := sqliteMatcherSequence.Add(1)
	sqliteMatchers.Store(token, request)
	return token, func() { sqliteMatchers.Delete(token) }
}

func cloneQueryNode(node *query.Node) *query.Node {
	if node == nil {
		return nil
	}
	cloned := *node
	if node.Comparison != nil {
		comparison := *node.Comparison
		cloned.Comparison = &comparison
	}
	if node.Children != nil {
		cloned.Children = make([]query.Node, len(node.Children))
		for index := range node.Children {
			child := cloneQueryNode(&node.Children[index])
			cloned.Children[index] = *child
		}
	}
	return &cloned
}

const indexedAuthIdentityQuery = `SELECT
  documents.id, documents.created_at, documents.updated_at, documents.deleted_at,
  documents.status, documents.revision, documents.values_json
FROM ridu_unique_values identity
JOIN ridu_documents documents
  ON documents.collection_id = identity.collection_id
 AND documents.id = identity.document_id
WHERE identity.collection_id = ?
  AND identity.index_key = ?
  AND identity.value_key = ?
  AND documents.deleted_at IS NULL`

func loadDocumentByIdentity(ctx context.Context, runner sqlRunner, collection schema.Collection, identity string) (store.Document, error) {
	if collection.Auth == nil || strings.TrimSpace(collection.Auth.IdentityField) == "" {
		return store.Document{}, store.ErrNotFound
	}
	var identityField *schema.Field
	for index := range collection.Fields {
		if collection.Fields[index].Name == collection.Auth.IdentityField {
			identityField = &collection.Fields[index]
			break
		}
	}
	if identityField == nil {
		return store.Document{}, fmt.Errorf("auth collection %q has no identity field", collection.Slug)
	}
	identity = store.CanonicalAuthIdentity(identity)
	if !identityField.Unique {
		// Resolved Ridu auth schemas always make the identity field unique. Keep
		// direct store fixtures with a hand-written incomplete schema usable,
		// without restoring the previous decode-every-document Go scan.
		return scanDocumentRecord(runner.QueryRowContext(ctx, `SELECT
  id, created_at, updated_at, deleted_at, status, revision, values_json
FROM ridu_documents
WHERE collection_id = ? AND deleted_at IS NULL
  AND json_extract(values_json, ?) = ?
LIMIT 1`, string(collection.ID), "$"+"."+collection.Auth.IdentityField, identity), collection)
	}
	valueKey, err := json.Marshal(identity)
	if err != nil {
		return store.Document{}, fmt.Errorf("encode SQLite auth identity: %w", err)
	}
	return scanDocumentRecord(runner.QueryRowContext(ctx, indexedAuthIdentityQuery,
		string(collection.ID), "field:"+string(identityField.ID), string(valueKey),
	), collection)
}

func scanDocumentRecord(row rowScanner, collection schema.Collection) (store.Document, error) {
	var id, status, valuesJSON string
	var createdAt, updatedAt int64
	var deletedAt sql.NullInt64
	var revision int
	if err := row.Scan(&id, &createdAt, &updatedAt, &deletedAt, &status, &revision, &valuesJSON); err != nil {
		return store.Document{}, translateError(err)
	}
	document := store.Document{ID: id, CreatedAt: decodeTime(createdAt), UpdatedAt: decodeTime(updatedAt), Revision: revision}
	if deletedAt.Valid {
		value := decodeTime(deletedAt.Int64)
		document.DeletedAt = &value
	}
	if collection.Versions != nil {
		document.Status = store.Status(status)
	} else {
		document.Revision = 0
	}
	var values store.Values
	if err := json.Unmarshal([]byte(valuesJSON), &values); err != nil {
		return store.Document{}, fmt.Errorf("decode SQLite document values: %w", err)
	}
	document.Values = currentValues(collection.Fields, values)
	return document, nil
}

func currentValues(fields []schema.Field, values store.Values) store.Values {
	result := make(store.Values)
	for _, field := range fields {
		if field.Category == schema.FieldCategoryPresentation {
			continue
		}
		if value, exists := values[field.Name]; exists {
			result[field.Name] = value
		}
	}
	return result
}

func (transaction *documentTransaction) Create(ctx context.Context, request store.CreateRequest) (store.Document, error) {
	leave, err := transaction.enter(ctx, true)
	if err != nil {
		return store.Document{}, err
	}
	defer leave()
	id := request.ID
	if id == "" {
		var err error
		id, err = newID(string(request.Collection.ID))
		if err != nil {
			return store.Document{}, err
		}
	}
	if err := store.ValidateDocumentID(id); err != nil {
		return store.Document{}, err
	}
	now := transaction.store.now().UTC()
	createdAt, updatedAt := request.CreatedAt.UTC(), request.UpdatedAt.UTC()
	if request.CreatedAt.IsZero() {
		createdAt = now
	}
	if request.UpdatedAt.IsZero() {
		updatedAt = createdAt
	}
	if err := validateSQLiteTimes("document timestamp", createdAt, updatedAt); err != nil {
		return store.Document{}, err
	}
	status := request.Status
	revision := 0
	if request.Collection.Versions != nil {
		if status == "" {
			status = store.StatusDraft
			if !request.Collection.Versions.Drafts {
				status = store.StatusPublished
			}
		}
		revision = 1
	}
	values := store.CloneValues(request.Values)
	canonicalizeSQLiteAuthIdentity(request.Collection, values)
	encoded, err := json.Marshal(values)
	if err != nil {
		return store.Document{}, fmt.Errorf("encode SQLite document values: %w", err)
	}
	_, err = transaction.connection.ExecContext(ctx, `INSERT INTO ridu_documents (
  collection_id, id, created_at, updated_at, deleted_at, status, revision, values_json
) VALUES (?, ?, ?, ?, NULL, ?, ?, ?)`, string(request.Collection.ID), id, encodeTime(createdAt), encodeTime(updatedAt), string(status), revision, string(encoded))
	if err != nil {
		return store.Document{}, translateError(err)
	}
	document := store.Document{ID: id, CreatedAt: createdAt, UpdatedAt: updatedAt, Status: status, Revision: revision, Values: values}
	if err := transaction.replaceUniqueValues(ctx, request.Collection, document); err != nil {
		return store.Document{}, err
	}
	if err := transaction.replaceDocumentReferences(ctx, request.Collection, document); err != nil {
		return store.Document{}, err
	}
	return store.CloneDocument(document), nil
}

func (transaction *documentTransaction) Find(ctx context.Context, request store.Request) (store.Document, error) {
	if err := primitivefield.ValidateRequest(request); err != nil {
		return store.Document{}, err
	}
	leave, err := transaction.enter(ctx, false)
	if err != nil {
		return store.Document{}, err
	}
	defer leave()
	if request.ID == "" {
		return store.Document{}, fmt.Errorf("document ID is required")
	}
	switch request.Lock {
	case store.LockNone, store.LockReference, store.LockMutation:
	default:
		return store.Document{}, fmt.Errorf("unsupported document lock mode %q", request.Lock)
	}
	document, err := loadRequestedDocument(ctx, transaction.connection, request)
	if err != nil {
		return store.Document{}, err
	}
	if !documentMatchesRequest(document, request) {
		return store.Document{}, store.ErrNotFound
	}
	return transaction.prepare(ctx, document, request)
}

func loadRequestedDocument(ctx context.Context, runner sqlRunner, request store.Request) (store.Document, error) {
	predicate := sqliteRequestPredicate(request)
	defer predicate.close()
	predicate.clause += " AND id = ?"
	predicate.arguments = append(predicate.arguments, request.ID)
	return scanDocumentRecord(runner.QueryRowContext(ctx, `SELECT
  id, created_at, updated_at, deleted_at, status, revision, values_json
FROM ridu_documents WHERE `+predicate.clause, predicate.arguments...), request.Collection)
}

func loadDocumentRecord(ctx context.Context, runner sqlRunner, collection schema.Collection, id string) (store.Document, error) {
	return scanDocumentRecord(runner.QueryRowContext(ctx, `SELECT
  id, created_at, updated_at, deleted_at, status, revision, values_json
FROM ridu_documents WHERE collection_id = ? AND id = ?`, string(collection.ID), id), collection)
}

func loadDocument(ctx context.Context, runner sqlRunner, collection schema.Collection, id string) (store.Document, error) {
	return loadDocumentRecord(ctx, runner, collection, id)
}

func (transaction *documentTransaction) List(ctx context.Context, request store.Request) (store.Page, error) {
	if err := primitivefield.ValidateRequest(request); err != nil {
		return store.Page{}, err
	}
	leave, err := transaction.enter(ctx, false)
	if err != nil {
		return store.Page{}, err
	}
	defer leave()
	predicate := sqliteRequestPredicate(request)
	defer predicate.close()
	order, ordered := sqliteSortClause(request)
	if predicate.exact && ordered {
		page, exact, err := transaction.listSQL(ctx, request, predicate, order)
		if err != nil {
			return store.Page{}, err
		}
		if exact {
			return page, nil
		}
	}
	return transaction.listInMemory(ctx, request)
}

func (transaction *documentTransaction) Distinct(ctx context.Context, request store.DistinctRequest) (store.DistinctPage, error) {
	if err := primitivefield.ValidateRequest(store.Request{Collection: request.Collection, Filter: request.Filter, Access: request.Access}); err != nil {
		return store.DistinctPage{}, err
	}
	leave, err := transaction.enter(ctx, false)
	if err != nil {
		return store.DistinctPage{}, err
	}
	defer leave()
	if err := store.ValidateDistinctRequest(request); err != nil {
		return store.DistinctPage{}, err
	}
	value, supported := sqliteValueAtPathForLocales(request.Collection, request.Field, request.LocaleChain)
	if !supported {
		return store.DistinctPage{}, fmt.Errorf("SQLite distinct field %q is not supported", request.Field.String())
	}
	documentRequest := store.Request{
		Collection: request.Collection, Filter: request.Filter, Access: request.Access,
		PublishedOnly: request.PublishedOnly, Deletion: request.Deletion,
		Locales: request.Locales, LocaleChain: request.LocaleChain,
	}
	predicate := sqliteRequestPredicate(documentRequest)
	defer predicate.close()
	distinctQuery := "SELECT DISTINCT " + value.valueSQL + " AS distinct_value FROM ridu_documents WHERE " + predicate.clause
	var total int
	if err := transaction.connection.QueryRowContext(ctx, "SELECT count(*) FROM ("+distinctQuery+")", predicate.arguments...).Scan(&total); err != nil {
		return store.DistinctPage{}, translateError(err)
	}
	page, limit, offset, _ := store.ListPageBounds(request.Page, request.Limit, total)
	arguments := append([]any(nil), predicate.arguments...)
	arguments = append(arguments, limit, offset)
	rows, err := transaction.connection.QueryContext(ctx, distinctQuery+" ORDER BY distinct_value ASC LIMIT ? OFFSET ?", arguments...)
	if err != nil {
		return store.DistinctPage{}, translateError(err)
	}
	defer rows.Close()
	values := make([]store.Value, 0, min(limit, total))
	for rows.Next() {
		switch value.kind {
		case sqliteStringValue:
			var raw sql.NullString
			if err := rows.Scan(&raw); err != nil {
				return store.DistinctPage{}, translateError(err)
			}
			if raw.Valid {
				values = append(values, store.String(raw.String))
			} else {
				values = append(values, store.Null())
			}
		case sqliteNumberValue:
			var raw sql.NullFloat64
			if err := rows.Scan(&raw); err != nil {
				return store.DistinctPage{}, translateError(err)
			}
			if raw.Valid {
				values = append(values, store.Number(raw.Float64))
			} else {
				values = append(values, store.Null())
			}
		case sqliteBooleanValue:
			var raw sql.NullBool
			if err := rows.Scan(&raw); err != nil {
				return store.DistinctPage{}, translateError(err)
			}
			if raw.Valid {
				values = append(values, store.Boolean(raw.Bool))
			} else {
				values = append(values, store.Null())
			}
		default:
			return store.DistinctPage{}, fmt.Errorf("SQLite distinct field %q has an unsupported value kind", request.Field.String())
		}
	}
	if err := rows.Err(); err != nil {
		return store.DistinctPage{}, translateError(err)
	}
	return store.DistinctPage{Values: values, Page: page, Limit: limit, Total: total}, nil
}

func (transaction *documentTransaction) listSQL(ctx context.Context, request store.Request, predicate sqlitePredicate, order string) (store.Page, bool, error) {
	var total int
	if err := transaction.connection.QueryRowContext(ctx,
		"SELECT count(*) FROM ridu_documents WHERE "+predicate.clause,
		predicate.arguments...,
	).Scan(&total); err != nil {
		return store.Page{}, false, translateError(err)
	}
	page, limit, offset, _ := store.ListPageBounds(request.Page, request.Limit, total)

	arguments := append([]any(nil), predicate.arguments...)
	arguments = append(arguments, limit, offset)
	rows, err := transaction.connection.QueryContext(ctx, `SELECT
  id, created_at, updated_at, deleted_at, status, revision, values_json
FROM ridu_documents WHERE `+predicate.clause+" ORDER BY "+order+" LIMIT ? OFFSET ?", arguments...)
	if err != nil {
		return store.Page{}, false, translateError(err)
	}
	defer rows.Close()
	documents := make([]store.Document, 0, min(limit, total))
	for rows.Next() {
		document, err := scanDocumentRecord(rows, request.Collection)
		if err != nil {
			return store.Page{}, false, err
		}
		// The in-memory matcher remains the semantic oracle. If an exact SQL
		// path ever diverges, fall back to the conservative path instead of
		// returning a miscounted or unauthorized page.
		if !documentMatchesRequest(document, request) {
			return store.Page{}, false, nil
		}
		documents = append(documents, document)
	}
	if err := rows.Err(); err != nil {
		return store.Page{}, false, translateError(err)
	}
	if len(request.Populate) != 0 && request.PopulationBudget == nil {
		request.PopulationBudget = store.NewPopulationBudget(store.MaxPopulationMaterializedDocuments)
	}
	for index := range documents {
		prepared, err := transaction.prepare(ctx, documents[index], request)
		if err != nil {
			return store.Page{}, false, err
		}
		documents[index] = prepared
	}
	return store.Page{Documents: documents, Page: page, Limit: limit, Total: total}, true, nil
}

func (transaction *documentTransaction) listInMemory(ctx context.Context, request store.Request) (store.Page, error) {
	if err := primitivefield.ValidateRequest(request); err != nil {
		return store.Page{}, err
	}
	documents, err := transaction.matchingDocuments(ctx, request)
	if err != nil {
		return store.Page{}, err
	}
	sorts := stableSort(request.Sort)
	sortRequest := request
	sortRequest.AllLocales = false
	sort.SliceStable(documents, func(left, right int) bool {
		leftDocument := localizedForRequest(documents[left], sortRequest)
		rightDocument := localizedForRequest(documents[right], sortRequest)
		return compareDocuments(leftDocument, rightDocument, sorts) < 0
	})
	page, limit, start, end := store.ListPageBounds(request.Page, request.Limit, len(documents))
	if len(request.Populate) != 0 && request.PopulationBudget == nil {
		request.PopulationBudget = store.NewPopulationBudget(store.MaxPopulationMaterializedDocuments)
	}
	selected := make([]store.Document, end-start)
	for index, document := range documents[start:end] {
		prepared, err := transaction.prepare(ctx, document, request)
		if err != nil {
			return store.Page{}, err
		}
		selected[index] = prepared
	}
	return store.Page{Documents: selected, Page: page, Limit: limit, Total: len(documents)}, nil
}

func (transaction *documentTransaction) matchingDocuments(ctx context.Context, request store.Request) ([]store.Document, error) {
	predicate := sqliteRequestPredicate(request)
	defer predicate.close()
	arguments := append(append([]any(nil), predicate.arguments...), maxSQLiteResidualSortDocuments+1)
	documents, err := loadDocumentRecordsWhere(ctx, transaction.connection, request.Collection, predicate.clause+" LIMIT ?", arguments...)
	if err != nil {
		return nil, err
	}
	if len(documents) > maxSQLiteResidualSortDocuments {
		return nil, fmt.Errorf("SQLite residual sort exceeds the %d-document materialization limit; declare and sort by a non-repeated indexable path", maxSQLiteResidualSortDocuments)
	}
	result := documents[:0]
	for _, document := range documents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if documentMatchesRequest(document, request) {
			result = append(result, document)
		}
	}
	return result, nil
}

func loadCollectionDocumentRecords(ctx context.Context, runner sqlRunner, collection schema.Collection) ([]store.Document, error) {
	return loadDocumentRecordsWhere(ctx, runner, collection, "collection_id = ?", string(collection.ID))
}

func loadDocumentRecordsWhere(ctx context.Context, runner sqlRunner, collection schema.Collection, predicate string, arguments ...any) ([]store.Document, error) {
	rows, err := runner.QueryContext(ctx, `SELECT
  id, created_at, updated_at, deleted_at, status, revision, values_json
FROM ridu_documents WHERE `+predicate, arguments...)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	var documents []store.Document
	for rows.Next() {
		document, err := scanDocumentRecord(rows, collection)
		if err != nil {
			return nil, err
		}
		documents = append(documents, document)
	}
	return documents, translateError(rows.Err())
}

type sqlitePredicate struct {
	clause    string
	arguments []any
	exact     bool
	release   func()
}

func (predicate *sqlitePredicate) close() {
	if predicate.release != nil {
		predicate.release()
		predicate.release = nil
	}
}

type sqliteDocumentValueKind uint8

const (
	sqliteStringValue sqliteDocumentValueKind = iota + 1
	sqliteNumberValue
	sqliteBooleanValue
	sqliteTimestampValue
)

type sqliteDocumentValue struct {
	valueSQL      string
	typeSQL       string
	kind          sqliteDocumentValueKind
	alwaysPresent bool
}

func sqliteRequestPredicate(request store.Request) sqlitePredicate {
	result := sqlitePredicate{
		clause: "collection_id = ?", arguments: []any{string(request.Collection.ID)}, exact: true,
	}
	appendExact := func(clause string, arguments ...any) {
		result.clause += " AND (" + clause + ")"
		result.arguments = append(result.arguments, arguments...)
	}
	switch request.Deletion {
	case store.DeletionAll:
	case store.DeletionTrash:
		appendExact("deleted_at IS NOT NULL")
	default:
		appendExact("deleted_at IS NULL")
	}
	if request.PublishedOnly && request.Collection.Versions != nil {
		appendExact("status = ?", string(store.StatusPublished))
	}
	if request.ExpectedRevision > 0 {
		appendExact("revision = ?", request.ExpectedRevision)
	}
	appendNode := func(node *query.Node, locales []schema.LocaleCode) {
		if node == nil {
			return
		}
		compiled, supported := compileSQLiteNodeForLocales(request.Collection, *node, locales)
		if supported {
			appendExact(compiled.clause, compiled.arguments...)
		}
		result.exact = result.exact && supported && compiled.exact
	}
	appendNode(request.Filter, request.LocaleChain)
	if request.Access != nil && request.AllLocales && len(request.Locales) != 0 {
		for _, locale := range request.Locales {
			appendNode(request.Access, []schema.LocaleCode{locale})
		}
	} else {
		appendNode(request.Access, request.LocaleChain)
	}
	if !result.exact && (request.Filter != nil || request.Access != nil) {
		token, release := registerSQLiteMatcher(request)
		appendExact(sqliteDocumentMatcherFunction+"(values_json, id, created_at, updated_at, status, revision, ?) = 1", token)
		result.release = release
		result.exact = true
	}
	return result
}

func compileSQLiteNode(collection schema.Collection, node query.Node) (sqlitePredicate, bool) {
	return compileSQLiteNodeForLocales(collection, node, nil)
}

func compileSQLiteNodeForLocales(collection schema.Collection, node query.Node, locales []schema.LocaleCode) (sqlitePredicate, bool) {
	switch node.Kind {
	case query.ExpressionComparison:
		if node.Comparison == nil {
			return sqlitePredicate{clause: "0", exact: true}, true
		}
		return compileSQLiteComparison(collection, *node.Comparison, locales)
	case query.ExpressionAnd:
		if len(node.Children) < 2 {
			return sqlitePredicate{clause: "0", exact: true}, true
		}
		result := sqlitePredicate{exact: true}
		for _, child := range node.Children {
			compiled, supported := compileSQLiteNodeForLocales(collection, child, locales)
			if !supported {
				result.exact = false
				continue
			}
			if result.clause != "" {
				result.clause += " AND "
			}
			result.clause += "(" + compiled.clause + ")"
			result.arguments = append(result.arguments, compiled.arguments...)
			result.exact = result.exact && compiled.exact
		}
		return result, result.clause != ""
	case query.ExpressionOr:
		if len(node.Children) < 2 {
			return sqlitePredicate{clause: "0", exact: true}, true
		}
		result := sqlitePredicate{exact: true}
		for _, child := range node.Children {
			compiled, supported := compileSQLiteNodeForLocales(collection, child, locales)
			if !supported || !compiled.exact {
				return sqlitePredicate{}, false
			}
			if result.clause != "" {
				result.clause += " OR "
			}
			result.clause += "(" + compiled.clause + ")"
			result.arguments = append(result.arguments, compiled.arguments...)
		}
		return result, true
	case query.ExpressionNot:
		if len(node.Children) != 1 {
			return sqlitePredicate{clause: "0", exact: true}, true
		}
		compiled, supported := compileSQLiteNodeForLocales(collection, node.Children[0], locales)
		if !supported || !compiled.exact {
			return sqlitePredicate{}, false
		}
		// A missing JSON path is false to Ridu's matcher. Collapse SQLite's
		// third truth value only at negation boundaries so ordinary equality
		// and range clauses remain recognizable to expression indexes.
		compiled.clause = "NOT (COALESCE((" + compiled.clause + "), 0))"
		return compiled, true
	default:
		return sqlitePredicate{clause: "0", exact: true}, true
	}
}

func compileSQLiteComparison(collection schema.Collection, comparison query.Comparison, locales []schema.LocaleCode) (sqlitePredicate, bool) {
	value, supported := sqliteValueAtPathForLocales(collection, comparison.Path, locales)
	if !supported {
		return sqlitePredicate{}, false
	}
	if comparison.Operator == query.OperatorContains || comparison.Operator == query.OperatorLike {
		// SQLite's built-in lower()/NOCASE rules are ASCII-only, while Ridu's
		// matcher uses Unicode case folding. Applying that SQL predicate could
		// exclude a real match, so these operations remain an in-memory fallback.
		return sqlitePredicate{}, false
	}
	if comparison.Operator == query.OperatorExists {
		want, _ := comparison.Value.BooleanValue()
		if value.alwaysPresent {
			if want {
				return sqlitePredicate{clause: "1", exact: true}, true
			}
			return sqlitePredicate{clause: "0", exact: true}, true
		}
		present := value.typeSQL + " IS NOT NULL AND " + value.typeSQL + " <> 'null'"
		if !want {
			present = value.typeSQL + " IS NULL OR " + value.typeSQL + " = 'null'"
		}
		return sqlitePredicate{clause: present, exact: true}, true
	}
	if comparison.Operator == query.OperatorIn {
		items := comparison.Value.Values()
		if len(items) == 0 {
			return sqlitePredicate{clause: "0", exact: true}, true
		}
		result := sqlitePredicate{exact: true}
		for _, item := range items {
			equal := sqliteEquality(value, item)
			if result.clause != "" {
				result.clause += " OR "
			}
			result.clause += "(" + equal.clause + ")"
			result.arguments = append(result.arguments, equal.arguments...)
		}
		return result, true
	}
	if comparison.Operator == query.OperatorEqual || comparison.Operator == query.OperatorNotEqual {
		if value.kind == sqliteTimestampValue && comparison.Value.Kind() != query.ValueNull {
			if _, valid := sqliteTimestampArgument(comparison.Value); !valid {
				return sqlitePredicate{clause: "0", exact: true}, true
			}
		}
		result := sqliteEquality(value, comparison.Value)
		if comparison.Operator == query.OperatorNotEqual {
			result.clause = "NOT (COALESCE((" + result.clause + "), 0))"
		}
		return result, true
	}

	operator := map[query.Operator]string{
		query.OperatorGreaterThan:      ">",
		query.OperatorGreaterThanEqual: ">=",
		query.OperatorLessThan:         "<",
		query.OperatorLessThanEqual:    "<=",
	}[comparison.Operator]
	if operator == "" {
		return sqlitePredicate{}, false
	}
	if value.kind == sqliteTimestampValue {
		expected, valid := sqliteTimestampArgument(comparison.Value)
		if !valid {
			return sqlitePredicate{clause: "0", exact: true}, true
		}
		return sqlitePredicate{clause: value.valueSQL + " " + operator + " ?", arguments: []any{expected}, exact: true}, true
	}
	switch comparison.Value.Kind() {
	case query.ValueString:
		if value.kind != sqliteStringValue {
			return sqlitePredicate{clause: "0", exact: true}, true
		}
		expected, _ := comparison.Value.StringValue()
		guard := ""
		if !value.alwaysPresent {
			guard = value.typeSQL + " = 'text' AND "
		}
		return sqlitePredicate{clause: guard + value.valueSQL + " " + operator + " ? COLLATE BINARY", arguments: []any{expected}, exact: true}, true
	case query.ValueNumber:
		if value.kind != sqliteNumberValue {
			return sqlitePredicate{clause: "0", exact: true}, true
		}
		expected, _ := comparison.Value.NumberValue()
		guard := ""
		if !value.alwaysPresent {
			guard = value.typeSQL + " IN ('integer', 'real') AND "
		}
		return sqlitePredicate{clause: guard + value.valueSQL + " " + operator + " ?", arguments: []any{expected}, exact: true}, true
	default:
		return sqlitePredicate{clause: "0", exact: true}, true
	}
}

func sqliteEquality(value sqliteDocumentValue, expected query.Value) sqlitePredicate {
	switch expected.Kind() {
	case query.ValueNull:
		if value.alwaysPresent {
			return sqlitePredicate{clause: "0", exact: true}
		}
		return sqlitePredicate{clause: value.typeSQL + " IS NULL OR " + value.typeSQL + " = 'null'", exact: true}
	case query.ValueString:
		if value.kind == sqliteTimestampValue {
			expectedTimestamp, valid := sqliteTimestampArgument(expected)
			if !valid {
				return sqlitePredicate{clause: "0", exact: true}
			}
			return sqlitePredicate{clause: value.valueSQL + " = ?", arguments: []any{expectedTimestamp}, exact: true}
		}
		if value.kind != sqliteStringValue {
			return sqlitePredicate{clause: "0", exact: true}
		}
		expectedText, _ := expected.StringValue()
		guard := ""
		if !value.alwaysPresent {
			guard = value.typeSQL + " = 'text' AND "
		}
		return sqlitePredicate{clause: guard + value.valueSQL + " = ? COLLATE BINARY", arguments: []any{expectedText}, exact: true}
	case query.ValueNumber:
		if value.kind != sqliteNumberValue {
			return sqlitePredicate{clause: "0", exact: true}
		}
		expectedNumber, _ := expected.NumberValue()
		guard := ""
		if !value.alwaysPresent {
			guard = value.typeSQL + " IN ('integer', 'real') AND "
		}
		return sqlitePredicate{clause: guard + value.valueSQL + " = ?", arguments: []any{expectedNumber}, exact: true}
	case query.ValueBoolean:
		if value.kind != sqliteBooleanValue {
			return sqlitePredicate{clause: "0", exact: true}
		}
		expectedBoolean, _ := expected.BooleanValue()
		wantType := "'false'"
		want := 0
		if expectedBoolean {
			wantType = "'true'"
			want = 1
		}
		if value.alwaysPresent {
			return sqlitePredicate{clause: value.valueSQL + " = ?", arguments: []any{want}, exact: true}
		}
		return sqlitePredicate{clause: value.typeSQL + " = " + wantType + " AND " + value.valueSQL + " = ?", arguments: []any{want}, exact: true}
	default:
		return sqlitePredicate{clause: "0", exact: true}
	}
}

func sqliteValueAtPath(collection schema.Collection, path query.Path) (sqliteDocumentValue, bool) {
	return sqliteValueAtPathForLocales(collection, path, nil)
}

func sqliteValueAtPathForLocales(collection schema.Collection, path query.Path, locales []schema.LocaleCode) (sqliteDocumentValue, bool) {
	segments := path.Segments()
	if len(segments) == 0 {
		return sqliteDocumentValue{}, false
	}
	if len(segments) == 1 {
		switch segments[0] {
		case "id":
			return sqliteDocumentValue{valueSQL: "id COLLATE BINARY", kind: sqliteStringValue, alwaysPresent: true}, true
		case "createdAt":
			return sqliteDocumentValue{valueSQL: "created_at", kind: sqliteTimestampValue, alwaysPresent: true}, true
		case "updatedAt":
			return sqliteDocumentValue{valueSQL: "updated_at", kind: sqliteTimestampValue, alwaysPresent: true}, true
		case "_status":
			if collection.Versions == nil {
				return sqliteDocumentValue{}, false
			}
			return sqliteDocumentValue{valueSQL: "status COLLATE BINARY", kind: sqliteStringValue, alwaysPresent: true}, true
		case "_revision":
			if collection.Versions == nil {
				return sqliteDocumentValue{}, false
			}
			return sqliteDocumentValue{valueSQL: "revision", kind: sqliteNumberValue, alwaysPresent: true}, true
		}
	}
	chain, supported := sqliteQueryableFieldChain(collection.Fields, segments)
	if !supported {
		return sqliteDocumentValue{}, false
	}
	valueSQL, typeSQL, kind, supported := sqliteDocumentPathExpression(chain, locales)
	return sqliteDocumentValue{valueSQL: valueSQL, typeSQL: typeSQL, kind: kind}, supported
}

func sqliteSortClause(request store.Request) (string, bool) {
	terms := stableSort(request.Sort)
	parts := make([]string, len(terms))
	for index, term := range terms {
		value, supported := sqliteValueAtPathForLocales(request.Collection, term.Path, request.LocaleChain)
		if !supported {
			return "", false
		}
		direction := "ASC"
		if term.Direction == query.Descending {
			direction = "DESC"
		}
		parts[index] = value.valueSQL + " " + direction
	}
	return strings.Join(parts, ", "), true
}

func sqliteTimestampArgument(value query.Value) (int64, bool) {
	text, ok := value.StringValue()
	if !ok {
		return 0, false
	}
	timestamp, err := time.Parse(time.RFC3339Nano, text)
	if err != nil || validateSQLiteTimes("query timestamp", timestamp) != nil {
		return 0, false
	}
	return encodeTime(timestamp), true
}

func (transaction *documentTransaction) ListWindow(ctx context.Context, request store.Request) (store.Window, error) {
	if err := primitivefield.ValidateRequest(request); err != nil {
		return store.Window{}, err
	}
	leave, err := transaction.enter(ctx, false)
	if err != nil {
		return store.Window{}, err
	}
	defer leave()
	statement, arguments, err := listWindowQuery(request)
	if err != nil {
		return store.Window{}, err
	}
	rows, err := transaction.connection.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return store.Window{}, translateError(err)
	}
	defer rows.Close()
	candidates := make([]store.Document, 0, request.Limit+1)
	for rows.Next() {
		document, err := scanDocumentRecord(rows, request.Collection)
		if err != nil {
			return store.Window{}, err
		}
		candidates = append(candidates, document)
	}
	if err := rows.Err(); err != nil {
		return store.Window{}, translateError(err)
	}
	hasMore := len(candidates) > request.Limit
	if hasMore {
		candidates = candidates[:request.Limit]
	}
	prepared := make([]store.Document, len(candidates))
	for index, document := range candidates {
		value, err := transaction.prepare(ctx, document, request)
		if err != nil {
			return store.Window{}, err
		}
		prepared[index] = value
	}
	return store.Window{Documents: prepared, HasMore: hasMore}, nil
}

func listWindowQuery(request store.Request) (string, []any, error) {
	if err := store.ValidateListWindowRequest(request); err != nil {
		return "", nil, err
	}
	value, supported := sqliteValueAtPathForLocales(request.Collection, request.IndexWindow.Path, request.LocaleChain)
	if !supported || value.kind != sqliteStringValue {
		return "", nil, fmt.Errorf("SQLite index window path %q is not a supported text index", request.IndexWindow.Path.String())
	}
	expression := value.valueSQL
	typeExpression := value.typeSQL
	predicates := []string{
		"collection_id = ?",
		typeExpression + " = 'text'",
		expression + " >= ?",
		expression + " < ?",
	}
	arguments := []any{string(request.Collection.ID), request.IndexWindow.LowerBound, request.IndexWindow.UpperBound}
	if request.Deletion != store.DeletionAll {
		predicates = append(predicates, "deleted_at IS NULL")
	}
	arguments = append(arguments, request.Limit+1)
	statement := `SELECT
  id, created_at, updated_at, deleted_at, status, revision, values_json
FROM ridu_documents WHERE ` + strings.Join(predicates, " AND ") +
		" ORDER BY " + expression + " ASC LIMIT ?"
	return statement, arguments, nil
}

func (transaction *documentTransaction) ResolveFilteredSelection(ctx context.Context, request store.FilteredSelectionRequest) (store.FilteredSelection, error) {
	leave, err := transaction.enter(ctx, false)
	if err != nil {
		return store.FilteredSelection{}, err
	}
	defer leave()
	if request.Limit < 1 {
		return store.FilteredSelection{}, fmt.Errorf("filtered selection limit must be positive")
	}
	documentRequest := store.Request{
		Collection: request.Collection, Filter: request.Filter, Access: request.Access,
		Deletion: request.Deletion, Locales: request.Locales, LocaleChain: request.LocaleChain, AllLocales: request.AllLocales,
	}
	predicate := sqliteRequestPredicate(documentRequest)
	defer predicate.close()
	statement := `SELECT
  id, created_at, updated_at, deleted_at, status, revision, values_json
FROM ridu_documents WHERE ` + predicate.clause + " ORDER BY id ASC"
	arguments := append([]any(nil), predicate.arguments...)
	if predicate.exact {
		statement += " LIMIT ?"
		arguments = append(arguments, request.Limit+1)
	}
	rows, err := transaction.connection.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return store.FilteredSelection{}, err
	}
	defer rows.Close()
	ids := make([]string, 0, request.Limit+1)
	for rows.Next() {
		document, err := scanDocumentRecord(rows, request.Collection)
		if err != nil {
			return store.FilteredSelection{}, err
		}
		if documentMatchesRequest(document, documentRequest) {
			ids = append(ids, document.ID)
			if len(ids) > request.Limit {
				break
			}
		}
	}
	if err := rows.Err(); err != nil {
		return store.FilteredSelection{}, translateError(err)
	}
	overflow := len(ids) > request.Limit
	if overflow {
		ids = ids[:request.Limit]
	}
	return store.FilteredSelection{IDs: ids, Overflow: overflow}, nil
}

func (transaction *documentTransaction) Update(ctx context.Context, request store.UpdateRequest) (store.Document, error) {
	if err := primitivefield.ValidateRequest(request.Request); err != nil {
		return store.Document{}, err
	}
	leave, err := transaction.enter(ctx, true)
	if err != nil {
		return store.Document{}, err
	}
	defer leave()
	if request.ID == "" {
		return store.Document{}, fmt.Errorf("document ID is required")
	}
	probe := request.Request
	probe.ExpectedRevision = 0
	document, err := loadRequestedDocument(ctx, transaction.connection, probe)
	if err != nil {
		return store.Document{}, err
	}
	if !documentMatchesRequest(document, probe) {
		return store.Document{}, store.ErrNotFound
	}
	if request.ExpectedRevision > 0 && document.Revision != request.ExpectedRevision {
		return store.Document{}, store.ErrConflict
	}
	if request.ReplaceValues {
		document.Values = store.CloneValues(request.Values)
	} else {
		document.Values = localization.MergeStoragePatch(request.Collection.Fields, document.Values, request.Values)
	}
	canonicalizeSQLiteAuthIdentity(request.Collection, document.Values)
	document.UpdatedAt = transaction.store.now().UTC()
	if request.Status != nil {
		document.Status = *request.Status
	}
	if request.Collection.Versions != nil {
		document.Revision++
	}
	if err := transaction.persistDocument(ctx, request.Collection, document); err != nil {
		return store.Document{}, err
	}
	return store.CloneDocument(document), nil
}

func (transaction *documentTransaction) persistDocument(ctx context.Context, collection schema.Collection, document store.Document) error {
	timestamps := []time.Time{document.UpdatedAt}
	if document.DeletedAt != nil {
		timestamps = append(timestamps, *document.DeletedAt)
	}
	if err := validateSQLiteTimes("document timestamp", timestamps...); err != nil {
		return err
	}
	encoded, err := json.Marshal(document.Values)
	if err != nil {
		return fmt.Errorf("encode SQLite document values: %w", err)
	}
	result, err := transaction.connection.ExecContext(ctx, `UPDATE ridu_documents SET
  updated_at = ?, deleted_at = ?, status = ?, revision = ?, values_json = ?
WHERE collection_id = ? AND id = ?`, encodeTime(document.UpdatedAt), optionalTime(document.DeletedAt), string(document.Status), document.Revision, string(encoded), string(collection.ID), document.ID)
	if err != nil {
		return translateError(err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return store.ErrNotFound
	}
	if err := transaction.replaceUniqueValues(ctx, collection, document); err != nil {
		return err
	}
	return transaction.replaceDocumentReferences(ctx, collection, document)
}

func optionalTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return encodeTime(*value)
}

func canonicalizeSQLiteAuthIdentity(collection schema.Collection, values store.Values) {
	if collection.Auth == nil || values == nil {
		return
	}
	identity, exists := values[collection.Auth.IdentityField]
	if !exists {
		return
	}
	text, valid := identity.StringValue()
	if valid {
		values[collection.Auth.IdentityField] = store.String(store.CanonicalAuthIdentity(text))
	}
}

func (transaction *documentTransaction) Trash(ctx context.Context, request store.Request) (store.Document, error) {
	if err := primitivefield.ValidateRequest(request); err != nil {
		return store.Document{}, err
	}
	leave, err := transaction.enter(ctx, true)
	if err != nil {
		return store.Document{}, err
	}
	defer leave()
	request.Deletion = store.DeletionActive
	document, err := transaction.mutableDocument(ctx, request)
	if err != nil {
		return store.Document{}, err
	}
	now := transaction.store.now().UTC()
	document.DeletedAt = &now
	document.UpdatedAt = now
	if err := transaction.persistDocument(ctx, request.Collection, document); err != nil {
		return store.Document{}, err
	}
	return document, nil
}

func (transaction *documentTransaction) Restore(ctx context.Context, request store.Request) (store.Document, error) {
	if err := primitivefield.ValidateRequest(request); err != nil {
		return store.Document{}, err
	}
	leave, err := transaction.enter(ctx, true)
	if err != nil {
		return store.Document{}, err
	}
	defer leave()
	request.Deletion = store.DeletionTrash
	document, err := transaction.mutableDocument(ctx, request)
	if err != nil {
		return store.Document{}, err
	}
	document.DeletedAt = nil
	document.UpdatedAt = transaction.store.now().UTC()
	if err := transaction.persistDocument(ctx, request.Collection, document); err != nil {
		return store.Document{}, err
	}
	return document, nil
}

func (transaction *documentTransaction) mutableDocument(ctx context.Context, request store.Request) (store.Document, error) {
	if err := primitivefield.ValidateRequest(request); err != nil {
		return store.Document{}, err
	}
	if request.ID == "" {
		return store.Document{}, fmt.Errorf("document ID is required")
	}
	probe := request
	probe.ExpectedRevision = 0
	document, err := loadRequestedDocument(ctx, transaction.connection, probe)
	if err != nil {
		return store.Document{}, err
	}
	if !documentMatchesRequest(document, probe) {
		return store.Document{}, store.ErrNotFound
	}
	if request.ExpectedRevision > 0 && document.Revision != request.ExpectedRevision {
		return store.Document{}, store.ErrConflict
	}
	return document, nil
}

func (transaction *documentTransaction) Delete(ctx context.Context, request store.Request) (store.Document, error) {
	if err := primitivefield.ValidateRequest(request); err != nil {
		return store.Document{}, err
	}
	leave, err := transaction.enter(ctx, true)
	if err != nil {
		return store.Document{}, err
	}
	defer leave()
	document, err := transaction.mutableDocument(ctx, request)
	if err != nil {
		return store.Document{}, err
	}
	if _, err := transaction.connection.ExecContext(ctx, `DELETE FROM ridu_document_references
WHERE owner_collection_id = ? AND owner_document_id = ?`, string(request.Collection.ID), request.ID); err != nil {
		return store.Document{}, translateError(err)
	}
	result, err := transaction.connection.ExecContext(ctx, `DELETE FROM ridu_documents WHERE collection_id = ? AND id = ?`, string(request.Collection.ID), request.ID)
	if err != nil {
		return store.Document{}, translateError(err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return store.Document{}, store.ErrNotFound
	}
	return document, nil
}

func (transaction *documentTransaction) replaceUniqueValues(ctx context.Context, collection schema.Collection, document store.Document) error {
	if _, err := transaction.connection.ExecContext(ctx, `DELETE FROM ridu_unique_values WHERE collection_id = ? AND document_id = ?`, string(collection.ID), document.ID); err != nil {
		return translateError(err)
	}
	if document.DeletedAt != nil {
		return nil
	}
	encoded, err := json.Marshal(document.Values)
	if err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &raw); err != nil {
		return err
	}
	return insertRawUniqueValues(ctx, transaction.connection, collection, document.ID, raw)
}

func documentMatchesRequest(document store.Document, request store.Request) bool {
	if !matchesDeletion(document, request.Deletion) {
		return false
	}
	if request.PublishedOnly && request.Collection.Versions != nil && document.Status != store.StatusPublished {
		return false
	}
	if request.ExpectedRevision > 0 && document.Revision != request.ExpectedRevision {
		return false
	}
	return matchesRequest(document, request)
}

func localizedForRequest(document store.Document, request store.Request) store.Document {
	selection := localization.Selection{
		Configured: append([]schema.LocaleCode(nil), request.Locales...),
		Chain:      append([]schema.LocaleCode(nil), request.LocaleChain...),
		All:        request.AllLocales,
	}
	if len(selection.Chain) != 0 {
		selection.Locale = selection.Chain[0]
	}
	return localization.ProjectDocument(document, request.Collection.Fields, selection)
}

func matchesDeletion(document store.Document, mode store.DeletionMode) bool {
	switch mode {
	case store.DeletionAll:
		return true
	case store.DeletionTrash:
		return document.DeletedAt != nil
	default:
		return document.DeletedAt == nil
	}
}

func matchesRequest(document store.Document, request store.Request) bool {
	filterRequest := request
	filterRequest.AllLocales = false
	if !matchesAll(document, filterRequest, request.Filter) {
		return false
	}
	if request.Access == nil {
		return true
	}
	if !request.AllLocales || len(request.Locales) == 0 {
		return matchesAll(document, request, request.Access)
	}
	for _, locale := range request.Locales {
		localeRequest := request
		localeRequest.AllLocales = false
		localeRequest.LocaleChain = []schema.LocaleCode{locale}
		if !matchesAll(document, localeRequest, request.Access) {
			return false
		}
	}
	return true
}

func matchesAll(document store.Document, request store.Request, nodes ...*query.Node) bool {
	cache := make(map[string][]store.Value)
	values := func(path query.Path) []store.Value {
		if values, found := cache[path.String()]; found {
			return values
		}
		values := queryDocumentValues(document, request, path)
		cache[path.String()] = values
		return values
	}
	for _, node := range nodes {
		if node != nil && !matchesPrimitiveQuery(document, request.Collection.Fields, *node, values) {
			return false
		}
	}
	return true
}

// queryDocumentValues resolves schema paths against canonical stored values.
// Runtime objects must not reinterpret a block discriminator as an ordinary
// child name: a public variant path could otherwise reach a private variant.
func queryDocumentValues(document store.Document, request store.Request, path query.Path) []store.Value {
	segments := path.Segments()
	if len(segments) == 1 && (segments[0] == "id" || segments[0] == "_status" || segments[0] == "_revision") {
		return documentValues(document, segments)
	}
	if len(segments) == 0 {
		return nil
	}
	root, present := document.Values[segments[0]]
	if !present {
		return nil
	}
	var values []store.Value
	locales := populationwalk.LocaleSelection{All: request.AllLocales, Chain: request.LocaleChain}
	visit := func(prefix query.Path, remainder []string) {
		populationwalk.VisitAtPath(request.Collection.Fields, store.Values{segments[0]: root}, prefix, locales, func(_ schema.Field, value store.Value) {
			if len(remainder) == 0 {
				values = append(values, value)
			} else {
				values = append(values, valuesBelow(value, remainder)...)
			}
		})
	}
	if _, found := populationwalk.FieldAtPath(request.Collection.Fields, path); found {
		visit(path, nil)
		return values
	}
	// Opaque JSON/plugins support data keys without authored child
	// fields. Structured fields and embedded plugin trees require canonical
	// schema paths instead of accepting their serialized wire shape.
	for length := len(segments) - 1; length > 0; length-- {
		prefix, err := query.NewPath(segments[:length]...)
		if err != nil {
			continue
		}
		if field, found := populationwalk.FieldAtPath(request.Collection.Fields, prefix); found && (field.Type == schema.FieldTypeJSON || field.Type == schema.FieldTypePlugin && !embedded.HasFields(field)) {
			visit(prefix, segments[length:])
			return values
		}
	}
	return nil
}

func matches(document store.Document, node query.Node) bool {
	return matchesWithValues(document, node, func(path query.Path) []store.Value {
		return documentValues(document, path.Segments())
	})
}

func matchesWithValues(document store.Document, node query.Node, atPath func(query.Path) []store.Value) bool {
	switch node.Kind {
	case query.ExpressionComparison:
		if node.Comparison == nil {
			return false
		}
		if timestamp, system := sqliteDocumentTimestamp(document, node.Comparison.Path); system {
			return sqliteMatchesTimestamp(timestamp, node.Comparison.Operator, node.Comparison.Value)
		}
		values := atPath(node.Comparison.Path)
		if node.Comparison.Operator == query.OperatorExists {
			want, _ := node.Comparison.Value.BooleanValue()
			present := false
			for _, value := range values {
				present = present || value.Kind() != store.ValueNull
			}
			return present == want
		}
		if len(values) == 0 {
			return compareValue(store.Value{}, false, node.Comparison.Operator, node.Comparison.Value)
		}
		if node.Comparison.Operator == query.OperatorNotEqual {
			for _, value := range values {
				if compareValue(value, true, query.OperatorEqual, node.Comparison.Value) {
					return false
				}
			}
			return true
		}
		for _, value := range values {
			if compareValue(value, true, node.Comparison.Operator, node.Comparison.Value) {
				return true
			}
		}
		return false
	case query.ExpressionAnd:
		if len(node.Children) < 2 {
			return false
		}
		for _, child := range node.Children {
			if !matchesWithValues(document, child, atPath) {
				return false
			}
		}
		return true
	case query.ExpressionOr:
		if len(node.Children) < 2 {
			return false
		}
		for _, child := range node.Children {
			if matchesWithValues(document, child, atPath) {
				return true
			}
		}
		return false
	case query.ExpressionNot:
		return len(node.Children) == 1 && !matchesWithValues(document, node.Children[0], atPath)
	default:
		return false
	}
}

func sqliteDocumentTimestamp(document store.Document, path query.Path) (time.Time, bool) {
	switch path.String() {
	case "createdAt":
		return document.CreatedAt, true
	case "updatedAt":
		return document.UpdatedAt, true
	default:
		return time.Time{}, false
	}
}

func sqliteMatchesTimestamp(actual time.Time, operator query.Operator, expected query.Value) bool {
	switch operator {
	case query.OperatorExists:
		want, _ := expected.BooleanValue()
		return want
	case query.OperatorIn:
		for _, item := range expected.Values() {
			if sqliteMatchesTimestamp(actual, query.OperatorEqual, item) {
				return true
			}
		}
		return false
	}
	if expected.Kind() == query.ValueNull {
		return operator == query.OperatorNotEqual
	}
	text, ok := expected.StringValue()
	if !ok {
		return false
	}
	want, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return false
	}
	comparison := sqliteCompareTimes(actual, want)
	switch operator {
	case query.OperatorEqual:
		return comparison == 0
	case query.OperatorNotEqual:
		return comparison != 0
	case query.OperatorGreaterThan:
		return comparison > 0
	case query.OperatorGreaterThanEqual:
		return comparison >= 0
	case query.OperatorLessThan:
		return comparison < 0
	case query.OperatorLessThanEqual:
		return comparison <= 0
	default:
		return false
	}
}

func sqliteCompareTimes(left, right time.Time) int {
	if left.Before(right) {
		return -1
	}
	if left.After(right) {
		return 1
	}
	return 0
}

func documentValues(document store.Document, segments []string) []store.Value {
	if len(segments) == 1 && segments[0] == "id" {
		return []store.Value{store.String(document.ID)}
	}
	if len(segments) == 1 && segments[0] == "_status" && document.Status != "" {
		return []store.Value{store.String(string(document.Status))}
	}
	if len(segments) == 1 && segments[0] == "_revision" && document.Status != "" {
		return []store.Value{store.Number(float64(document.Revision))}
	}
	return valuesAtSegments(document.Values, segments)
}

func valuesAtSegments(values store.Values, segments []string) []store.Value {
	if len(segments) == 0 {
		return nil
	}
	value, exists := values[segments[0]]
	if !exists {
		return nil
	}
	if len(segments) == 1 {
		return []store.Value{value}
	}
	return valuesBelow(value, segments[1:])
}

func valuesBelow(value store.Value, segments []string) []store.Value {
	if len(segments) == 0 {
		return []store.Value{value}
	}
	if value.Kind() == store.ValueObject {
		if blockType, exists := value.Lookup("blockType"); exists {
			kind, _ := blockType.StringValue()
			if kind == segments[0] {
				segments = segments[1:]
				if len(segments) == 0 {
					return nil
				}
			}
		}
		child, exists := value.Lookup(segments[0])
		if !exists {
			return nil
		}
		return valuesBelow(child, segments[1:])
	}
	if value.Kind() == store.ValueList {
		var result []store.Value
		for item := range value.Elements() {
			result = append(result, valuesBelow(item, segments)...)
		}
		return result
	}
	return nil
}

func documentValue(document store.Document, segments []string) (store.Value, bool) {
	if len(segments) == 1 && segments[0] == "id" {
		return store.String(document.ID), true
	}
	if len(segments) == 1 && segments[0] == "_status" && document.Status != "" {
		return store.String(string(document.Status)), true
	}
	if len(segments) == 1 && segments[0] == "_revision" && document.Status != "" {
		return store.Number(float64(document.Revision)), true
	}
	if len(segments) == 0 {
		return store.Value{}, false
	}
	value, exists := document.Values[segments[0]]
	for _, segment := range segments[1:] {
		if !exists {
			return store.Value{}, false
		}
		value, exists = value.Lookup(segment)
	}
	return value, exists
}

func compareValue(actual store.Value, exists bool, operator query.Operator, expected query.Value) bool {
	if operator == query.OperatorExists {
		want, _ := expected.BooleanValue()
		return (exists && actual.Kind() != store.ValueNull) == want
	}
	if operator == query.OperatorIn {
		for _, item := range expected.Values() {
			if compareValue(actual, exists, query.OperatorEqual, item) {
				return true
			}
		}
		return false
	}
	if operator == query.OperatorContains || operator == query.OperatorLike {
		expectedText, expectedOK := expected.StringValue()
		if !exists || !expectedOK {
			return false
		}
		if actual.Kind() == store.ValueList {
			if operator == query.OperatorLike {
				return false
			}
			for value := range actual.Elements() {
				text, valid := value.StringValue()
				if valid && text == expectedText {
					return true
				}
			}
			return false
		}
		actualText, valid := actual.StringValue()
		if !valid {
			return false
		}
		actualText, expectedText = strings.ToLower(actualText), strings.ToLower(expectedText)
		if operator == query.OperatorContains {
			return strings.Contains(actualText, expectedText)
		}
		for _, word := range strings.Fields(expectedText) {
			if !strings.Contains(actualText, word) {
				return false
			}
		}
		return true
	}
	if operator == query.OperatorGreaterThan || operator == query.OperatorGreaterThanEqual || operator == query.OperatorLessThan || operator == query.OperatorLessThanEqual {
		comparison, comparable := compareOrdered(actual, expected)
		if !exists || !comparable {
			return false
		}
		switch operator {
		case query.OperatorGreaterThan:
			return comparison > 0
		case query.OperatorGreaterThanEqual:
			return comparison >= 0
		case query.OperatorLessThan:
			return comparison < 0
		default:
			return comparison <= 0
		}
	}
	equal := false
	switch expected.Kind() {
	case query.ValueNull:
		equal = !exists || actual.Kind() == store.ValueNull
	case query.ValueString:
		expectedText, _ := expected.StringValue()
		actualText, valid := actual.StringValue()
		equal = exists && valid && actualText == expectedText
	case query.ValueNumber:
		expectedNumber, _ := expected.NumberValue()
		actualNumber, valid := actual.NumberValue()
		equal = exists && valid && actualNumber == expectedNumber
	case query.ValueBoolean:
		expectedBoolean, _ := expected.BooleanValue()
		actualBoolean, valid := actual.BooleanValue()
		equal = exists && valid && actualBoolean == expectedBoolean
	}
	if operator == query.OperatorNotEqual {
		return !equal
	}
	return equal
}

func compareOrdered(actual store.Value, expected query.Value) (int, bool) {
	switch expected.Kind() {
	case query.ValueString:
		left, valid := actual.StringValue()
		right, _ := expected.StringValue()
		if !valid {
			return 0, false
		}
		return strings.Compare(left, right), true
	case query.ValueNumber:
		left, valid := actual.NumberValue()
		right, _ := expected.NumberValue()
		if !valid {
			return 0, false
		}
		if left < right {
			return -1, true
		}
		if left > right {
			return 1, true
		}
		return 0, true
	default:
		return 0, false
	}
}

func stableSort(sorts []query.Sort) []query.Sort {
	result := append([]query.Sort(nil), sorts...)
	for _, term := range result {
		if term.Path.String() == "id" {
			return result
		}
	}
	id, _ := query.NewPath("id")
	return append(result, query.Sort{Path: id, Direction: query.Ascending})
}

func compareDocuments(left, right store.Document, sorts []query.Sort) int {
	for _, term := range sorts {
		if leftTime, timestamp := sqliteDocumentTimestamp(left, term.Path); timestamp {
			rightTime, _ := sqliteDocumentTimestamp(right, term.Path)
			comparison := sqliteCompareTimes(leftTime, rightTime)
			if comparison != 0 {
				if term.Direction == query.Descending {
					return -comparison
				}
				return comparison
			}
			continue
		}
		leftValue, leftExists := documentValue(left, term.Path.Segments())
		rightValue, rightExists := documentValue(right, term.Path.Segments())
		comparison := compareStoreValues(leftValue, leftExists, rightValue, rightExists)
		if comparison != 0 {
			if term.Direction == query.Descending {
				return -comparison
			}
			return comparison
		}
	}
	return 0
}

func compareStoreValues(left store.Value, leftExists bool, right store.Value, rightExists bool) int {
	if !leftExists || left.Kind() == store.ValueNull {
		if !rightExists || right.Kind() == store.ValueNull {
			return 0
		}
		return -1
	}
	if !rightExists || right.Kind() == store.ValueNull {
		return 1
	}
	if leftText, ok := left.StringValue(); ok {
		rightText, valid := right.StringValue()
		if !valid {
			return strings.Compare(string(left.Kind()), string(right.Kind()))
		}
		return strings.Compare(leftText, rightText)
	}
	if leftNumber, ok := left.NumberValue(); ok {
		rightNumber, valid := right.NumberValue()
		if !valid {
			return strings.Compare(string(left.Kind()), string(right.Kind()))
		}
		if leftNumber < rightNumber {
			return -1
		}
		if leftNumber > rightNumber {
			return 1
		}
		return 0
	}
	if leftBool, ok := left.BooleanValue(); ok {
		rightBool, valid := right.BooleanValue()
		if !valid {
			return strings.Compare(string(left.Kind()), string(right.Kind()))
		}
		if leftBool == rightBool {
			return 0
		}
		if !leftBool {
			return -1
		}
		return 1
	}
	return strings.Compare(string(left.Kind()), string(right.Kind()))
}

func (transaction *documentTransaction) prepare(ctx context.Context, document store.Document, request store.Request) (store.Document, error) {
	document = store.CloneDocument(document)
	populationBudget := request.PopulationBudget
	if len(request.Populate) != 0 && populationBudget == nil {
		populationBudget = store.NewPopulationBudget(store.MaxPopulationMaterializedDocuments)
		request.PopulationBudget = populationBudget
	}
	for _, population := range request.Populate {
		field, found := populationwalk.FieldAtPath(request.Collection.Fields, population.Path)
		relationship := populationwalk.RelationshipDetails(field)
		if !found || relationship == nil {
			return store.Document{}, fmt.Errorf("population field %q is not a relationship", population.Path.String())
		}
		var populationError error
		mapped, _ := populationwalk.MapAtPath(request.Collection.Fields, document.Values, population.Path, populationwalk.LocaleSelection{
			All: request.AllLocales, Chain: request.LocaleChain,
		}, func(_ schema.Field, value store.Value) store.Value {
			if populationError != nil {
				return value
			}
			return populateValue(value, relationship, func(collectionID schema.StableID, id string) (store.Document, bool) {
				targetSchema, exists := request.Collections[collectionID]
				if !exists {
					populationError = fmt.Errorf("relationship target %q is unavailable", collectionID)
					return store.Document{}, false
				}
				targetRequest := store.Request{
					Collection: targetSchema, ID: id, Access: request.PopulationAccess[collectionID],
					PublishedOnly: request.PublishedOnly,
					Locales:       request.Locales, LocaleChain: request.LocaleChain, AllLocales: request.AllLocales,
				}
				target, err := loadRequestedDocument(ctx, transaction.connection, targetRequest)
				found := err == nil
				if err != nil && !errors.Is(err, store.ErrNotFound) {
					populationError = err
					return store.Document{}, false
				}
				if found && !documentMatchesRequest(target, targetRequest) {
					found = false
				}
				if found && population.Depth > 1 {
					target, err = transaction.prepare(ctx, target, store.Request{
						Collection: targetSchema, Collections: request.Collections,
						Populate: populationwalk.DepthPopulations(targetSchema, population.Depth-1), PopulationAccess: request.PopulationAccess,
						PopulationBudget: populationBudget, PublishedOnly: request.PublishedOnly,
						Locales: request.Locales, LocaleChain: request.LocaleChain, AllLocales: request.AllLocales,
					})
					if err != nil {
						populationError = err
						return store.Document{}, false
					}
				}
				if found {
					target = localizedForRequest(target, store.Request{Collection: targetSchema, Locales: request.Locales, LocaleChain: request.LocaleChain, AllLocales: request.AllLocales})
					target = projectDocument(target, population.Select)
					if err := populationBudget.ConsumeDocument(target); err != nil {
						populationError = err
						return store.Document{}, false
					}
				}
				return target, found
			})
		})
		if populationError != nil {
			return store.Document{}, populationError
		}
		document.Values = mapped
	}
	return projectDocument(document, request.Select), nil
}

func populateValue(value store.Value, relationship *schema.RelationshipField, lookup func(schema.StableID, string) (store.Document, bool)) store.Value {
	if relationship.HasMany {
		items, valid := value.CopyList()
		if !valid {
			return value
		}
		for index, item := range items {
			items[index] = populateReference(item, relationship, lookup)
		}
		return store.List(items...)
	}
	return populateReference(value, relationship, lookup)
}

func populateReference(value store.Value, relationship *schema.RelationshipField, lookup func(schema.StableID, string) (store.Document, bool)) store.Value {
	if !relationship.Polymorphic {
		id, valid := value.StringValue()
		if !valid {
			return value
		}
		if document, found := lookup(relationship.CollectionID, id); found {
			return store.Populated(document)
		}
		return value
	}
	object, valid := value.CopyObject()
	if !valid {
		return value
	}
	slug, _ := object["relationTo"].StringValue()
	id, _ := object["id"].StringValue()
	for _, target := range relationship.Targets {
		if string(target.CollectionSlug) == slug {
			if document, found := lookup(target.CollectionID, id); found {
				object["id"] = store.Populated(document)
			}
			break
		}
	}
	return store.Object(object)
}

func projectDocument(document store.Document, selection []query.Path) store.Document {
	if selection == nil {
		return store.CloneDocument(document)
	}
	values := make(store.Values)
	for _, path := range selection {
		segments := path.Segments()
		if len(segments) == 1 {
			if value, exists := document.Values[segments[0]]; exists {
				values[segments[0]] = value
			}
		}
	}
	document.Values = values
	return store.CloneDocument(document)
}

func (transaction *documentTransaction) replaceDocumentReferences(ctx context.Context, collection schema.Collection, document store.Document) error {
	owner := store.DocumentReference{CollectionID: collection.ID, DocumentID: document.ID}
	if err := transaction.deleteDocumentReferences(ctx, owner); err != nil {
		return err
	}
	entries, referenceErr := referenceindex.Collect(collection, document)
	if referenceErr != nil {
		return referenceErr
	}
	for _, entry := range entries {
		_, err := transaction.connection.ExecContext(ctx, `INSERT INTO ridu_document_references (
  owner_collection_id, owner_document_id, field_id,
  target_collection_id, target_document_id, locale, occurrence
) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			string(entry.Owner.CollectionID), entry.Owner.DocumentID, string(entry.FieldID),
			string(entry.Target.CollectionID), entry.Target.DocumentID, string(entry.Locale), entry.Occurrence,
		)
		if err != nil {
			return translateError(err)
		}
	}
	return nil
}

func (transaction *documentTransaction) deleteDocumentReferences(ctx context.Context, owner store.DocumentReference) error {
	_, err := transaction.connection.ExecContext(ctx, `DELETE FROM ridu_document_references
WHERE owner_collection_id = ? AND owner_document_id = ?`, string(owner.CollectionID), owner.DocumentID)
	return translateError(err)
}

func (transaction *documentTransaction) ApplyReferenceDelete(ctx context.Context, request store.ReferenceDeleteRequest) error {
	leave, err := transaction.enter(ctx, true)
	if err != nil {
		return err
	}
	defer leave()
	if request.Target.CollectionID == "" || request.Target.DocumentID == "" {
		return fmt.Errorf("reference delete requires a target collection and document ID")
	}
	ignored := make(map[store.DocumentReference]struct{}, len(request.IgnoreOwners))
	for _, owner := range request.IgnoreOwners {
		if owner == request.Target {
			ignored[owner] = struct{}{}
			continue
		}
		collection, exists := request.Collections[owner.CollectionID]
		if !exists {
			return fmt.Errorf("ignored reference owner collection %q is unavailable", owner.CollectionID)
		}
		if _, err := loadDocumentRecord(ctx, transaction.connection, collection, owner.DocumentID); errors.Is(err, store.ErrNotFound) {
			ignored[owner] = struct{}{}
		} else if err != nil {
			return err
		}
	}
	rows, err := transaction.connection.QueryContext(ctx, `SELECT
  owner_collection_id, owner_document_id, field_id, locale, occurrence
FROM ridu_document_references
WHERE target_collection_id = ? AND target_document_id = ?
ORDER BY owner_collection_id, owner_document_id, field_id, locale, occurrence`, string(request.Target.CollectionID), request.Target.DocumentID)
	if err != nil {
		return translateError(err)
	}
	var entries []referenceindex.Entry
	for rows.Next() {
		entry := referenceindex.Entry{Target: request.Target}
		if err := rows.Scan(&entry.Owner.CollectionID, &entry.Owner.DocumentID, &entry.FieldID, &entry.Locale, &entry.Occurrence); err != nil {
			rows.Close()
			return translateError(err)
		}
		if _, skip := ignored[entry.Owner]; !skip {
			entries = append(entries, entry)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return translateError(err)
	}
	rows.Close()

	constraints := make(map[store.ReferenceConstraint]struct{})
	for _, entry := range entries {
		collection, exists := request.Collections[entry.Owner.CollectionID]
		if !exists {
			return fmt.Errorf("reference owner collection %q is unavailable", entry.Owner.CollectionID)
		}
		field, _, exists := referenceindex.FindReferenceField(collection, entry.FieldID)
		if !exists {
			return fmt.Errorf("reference owner field %q is unavailable", entry.FieldID)
		}
		action := schema.ReferenceDeleteNullify
		if field.Relationship != nil {
			action = field.Relationship.OnDelete
		} else if field.Upload != nil {
			action = field.Upload.OnDelete
		}
		switch action {
		case schema.ReferenceDeleteNullify:
		case schema.ReferenceDeleteRestrict:
			constraints[store.ReferenceConstraint{OwnerCollectionID: entry.Owner.CollectionID, FieldID: entry.FieldID}] = struct{}{}
		default:
			return fmt.Errorf("reference owner field %q has unsupported delete action %q", entry.FieldID, action)
		}
	}
	if len(constraints) != 0 {
		ordered := make([]store.ReferenceConstraint, 0, len(constraints))
		for constraint := range constraints {
			ordered = append(ordered, constraint)
		}
		sort.Slice(ordered, func(left, right int) bool {
			if ordered[left].OwnerCollectionID != ordered[right].OwnerCollectionID {
				return ordered[left].OwnerCollectionID < ordered[right].OwnerCollectionID
			}
			return ordered[left].FieldID < ordered[right].FieldID
		})
		return &store.DeleteRestrictedError{Constraints: ordered}
	}

	owners := make(map[store.DocumentReference]struct{}, len(entries))
	for _, entry := range entries {
		owners[entry.Owner] = struct{}{}
	}
	orderedOwners := make([]store.DocumentReference, 0, len(owners))
	for owner := range owners {
		orderedOwners = append(orderedOwners, owner)
	}
	sort.Slice(orderedOwners, func(left, right int) bool {
		if orderedOwners[left].CollectionID != orderedOwners[right].CollectionID {
			return orderedOwners[left].CollectionID < orderedOwners[right].CollectionID
		}
		return orderedOwners[left].DocumentID < orderedOwners[right].DocumentID
	})
	for _, owner := range orderedOwners {
		collection := request.Collections[owner.CollectionID]
		document, err := loadDocumentRecord(ctx, transaction.connection, collection, owner.DocumentID)
		if errors.Is(err, store.ErrNotFound) {
			if err := transaction.deleteDocumentReferences(ctx, owner); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		values, changed, referenceErr := referenceindex.NullifyTarget(collection, document.Values, request.Target)
		if referenceErr != nil {
			return referenceErr
		}
		if !changed {
			return fmt.Errorf("reference index for owner collection %q is inconsistent with current values", owner.CollectionID)
		}
		document.Values = values
		if err := transaction.persistDocument(ctx, collection, document); err != nil {
			return err
		}
	}
	return nil
}

func (transaction *documentTransaction) DeleteDocumentState(ctx context.Context, reference store.DocumentReference) error {
	leave, err := transaction.enter(ctx, true)
	if err != nil {
		return err
	}
	defer leave()
	if reference.CollectionID == "" || reference.DocumentID == "" {
		return fmt.Errorf("document state cleanup requires collection and document IDs")
	}
	statements := []string{
		`DELETE FROM ridu_versions WHERE collection_id = ? AND document_id = ?`,
		`DELETE FROM ridu_tasks WHERE (target_collection_id = ? AND target_document_id = ?) OR (requested_by_collection_id = ? AND requested_by_document_id = ?)`,
		`DELETE FROM ridu_document_locks WHERE (collection_id = ? AND document_id = ?) OR (owner_collection_id = ? AND owner_id = ?)`,
		`DELETE FROM ridu_auth_tokens WHERE collection_id = ? AND user_id = ?`,
		`DELETE FROM ridu_auth_sessions WHERE collection_id = ? AND user_id = ?`,
		`DELETE FROM ridu_auth_api_keys WHERE collection_id = ? AND user_id = ?`,
		`DELETE FROM ridu_auth_credentials WHERE collection_id = ? AND user_id = ?`,
		`DELETE FROM ridu_preferences WHERE collection_id = ? AND user_id = ?`,
	}
	for index, statement := range statements {
		arguments := []any{string(reference.CollectionID), reference.DocumentID}
		if index == 1 || index == 2 {
			arguments = append(arguments, string(reference.CollectionID), reference.DocumentID)
		}
		if _, err := transaction.connection.ExecContext(ctx, statement, arguments...); err != nil {
			return translateError(err)
		}
	}
	return nil
}

func (transaction *documentTransaction) SaveVersion(ctx context.Context, collection schema.Collection, document store.Document, maximum int) (store.Version, error) {
	leave, err := transaction.enter(ctx, true)
	if err != nil {
		return store.Version{}, err
	}
	defer leave()
	now := transaction.store.now().UTC()
	if err := validateSQLiteTimes("version timestamp", now); err != nil {
		return store.Version{}, err
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		return store.Version{}, fmt.Errorf("encode version snapshot: %w", err)
	}
	version := store.Version{
		ID: fmt.Sprintf("%s:%d", document.ID, document.Revision), DocumentID: document.ID,
		Revision: document.Revision, Status: document.Status, Snapshot: store.CloneDocument(document), CreatedAt: now,
	}
	var createdAt int64
	err = transaction.connection.QueryRowContext(ctx, `INSERT INTO ridu_versions (
  id, collection_id, document_id, revision, status, snapshot_json, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(collection_id, document_id, revision) DO UPDATE SET
  status = excluded.status,
  snapshot_json = excluded.snapshot_json
RETURNING created_at`, version.ID, string(collection.ID), document.ID, document.Revision, string(document.Status), string(encoded), encodeTime(now)).Scan(&createdAt)
	if err != nil {
		return store.Version{}, translateError(err)
	}
	version.CreatedAt = decodeTime(createdAt)
	if maximum > 0 {
		_, err := transaction.connection.ExecContext(ctx, `DELETE FROM ridu_versions
WHERE collection_id = ? AND document_id = ? AND revision NOT IN (
  SELECT revision FROM ridu_versions WHERE collection_id = ? AND document_id = ?
  ORDER BY revision DESC LIMIT ?
)`, string(collection.ID), document.ID, string(collection.ID), document.ID, maximum)
		if err != nil {
			return store.Version{}, translateError(err)
		}
	}
	return version, nil
}

func (transaction *documentTransaction) ListVersions(ctx context.Context, request store.VersionRequest) ([]store.Version, error) {
	if err := primitivefield.ValidateNode(request.Collection.Fields, request.Access); err != nil {
		return nil, err
	}
	leave, err := transaction.enter(ctx, false)
	if err != nil {
		return nil, err
	}
	defer leave()
	statement := `SELECT
  id, revision, status, snapshot_json, created_at
FROM ridu_versions WHERE collection_id = ? AND document_id = ?
`
	arguments := []any{string(request.Collection.ID), request.DocumentID}
	var release func()
	if request.Access != nil {
		token, matcherRelease := registerSQLiteMatcher(store.Request{
			Collection: request.Collection, Access: request.Access,
			Locales: request.Locales, LocaleChain: request.LocaleChain, AllLocales: request.AllLocales,
		})
		release = matcherRelease
		statement += " AND " + sqliteDocumentMatcherFunction + `(json_extract(snapshot_json, '$.Values'), json_extract(snapshot_json, '$.ID'), json_extract(snapshot_json, '$.CreatedAt'), json_extract(snapshot_json, '$.UpdatedAt'), json_extract(snapshot_json, '$.Status'), json_extract(snapshot_json, '$.Revision'), ?) = 1`
		arguments = append(arguments, token)
	}
	if release != nil {
		defer release()
	}
	statement += " ORDER BY revision DESC"
	rows, err := transaction.connection.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, translateError(err)
	}
	defer rows.Close()
	var versions []store.Version
	for rows.Next() {
		var version store.Version
		var encoded string
		var createdAt int64
		version.DocumentID = request.DocumentID
		if err := rows.Scan(&version.ID, &version.Revision, &version.Status, &encoded, &createdAt); err != nil {
			return nil, translateError(err)
		}
		if err := json.Unmarshal([]byte(encoded), &version.Snapshot); err != nil {
			return nil, fmt.Errorf("decode version snapshot: %w", err)
		}
		version.Snapshot.Values = currentValues(request.Collection.Fields, version.Snapshot.Values)
		version.CreatedAt = decodeTime(createdAt)
		if matchesRequest(version.Snapshot, store.Request{
			Collection: request.Collection, Access: request.Access, Locales: request.Locales,
			LocaleChain: request.LocaleChain, AllLocales: request.AllLocales,
		}) {
			versions = append(versions, version)
		}
	}
	return versions, translateError(rows.Err())
}

func (transaction *documentTransaction) FindVersion(ctx context.Context, collection schema.Collection, documentID string, revision int) (store.Version, error) {
	leave, err := transaction.enter(ctx, false)
	if err != nil {
		return store.Version{}, err
	}
	defer leave()
	var version store.Version
	var encoded string
	var createdAt int64
	version.DocumentID, version.Revision = documentID, revision
	err = transaction.connection.QueryRowContext(ctx, `SELECT
  id, status, snapshot_json, created_at
FROM ridu_versions WHERE collection_id = ? AND document_id = ? AND revision = ?`, string(collection.ID), documentID, revision).Scan(&version.ID, &version.Status, &encoded, &createdAt)
	if err != nil {
		return store.Version{}, translateError(err)
	}
	if err := json.Unmarshal([]byte(encoded), &version.Snapshot); err != nil {
		return store.Version{}, fmt.Errorf("decode version snapshot: %w", err)
	}
	version.Snapshot.Values = currentValues(collection.Fields, version.Snapshot.Values)
	version.CreatedAt = decodeTime(createdAt)
	return version, nil
}

var _ store.WindowTransaction = (*documentTransaction)(nil)
var _ store.DistinctTransaction = (*documentTransaction)(nil)
var _ store.VersionTransaction = (*documentTransaction)(nil)

func matchesPrimitiveQuery(document store.Document, fields []schema.Field, node query.Node, values func(query.Path) []store.Value) bool {
	if node.Kind == query.ExpressionComparison && node.Comparison != nil && node.Comparison.Operator == query.OperatorIn {
		if field, ok := populationwalk.FieldAtPath(fields, node.Comparison.Path); ok && primitivefield.IsList(field) {
			for _, value := range values(node.Comparison.Path) {
				if primitivefield.Membership(value, node.Comparison.Value) {
					return true
				}
			}
			return false
		}
	}
	switch node.Kind {
	case query.ExpressionAnd:
		for _, child := range node.Children {
			if !matchesPrimitiveQuery(document, fields, child, values) {
				return false
			}
		}
		return len(node.Children) >= 2
	case query.ExpressionOr:
		for _, child := range node.Children {
			if matchesPrimitiveQuery(document, fields, child, values) {
				return true
			}
		}
		return false
	case query.ExpressionNot:
		return len(node.Children) == 1 && !matchesPrimitiveQuery(document, fields, node.Children[0], values)
	default:
		return matchesWithValues(document, node, values)
	}
}
