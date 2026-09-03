package mongodb

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func TestMongoClientConfigurationRequiresNamedDatabaseAndSecureTransport(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		want   string
	}{
		{name: "missing URL", config: Config{}, want: "URL is required"},
		{name: "unsupported scheme", config: Config{DatabaseURL: "https://db.example/ridu"}, want: "invalid MongoDB connection"},
		{name: "missing database", config: Config{DatabaseURL: "mongodb://db.example/?tls=true"}, want: "must select a database"},
		{name: "unsafe database", config: Config{DatabaseURL: "mongodb://db.example/a.b?tls=true"}, want: "letters, numbers"},
		{name: "plaintext", config: Config{DatabaseURL: "mongodb://db.example/ridu"}, want: "must verify TLS"},
		{name: "invalid TLS", config: Config{DatabaseURL: "mongodb://db.example/ridu?tls=true&tlsInsecure=true"}, want: "must verify TLS"},
		{name: "negative timeout", config: Config{DatabaseURL: "mongodb://db.example/ridu?tls=true", ConnectTimeout: -time.Second}, want: "cannot be negative"},
		{name: "inverted pool", config: Config{DatabaseURL: "mongodb://db.example/ridu?tls=true", MinPoolSize: 3, MaxPoolSize: 2}, want: "exceeds maximum"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := normalizedClientOptions(test.config); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("configuration error = %v, want containing %q", err, test.want)
			}
		})
	}
	configured, database, err := normalizedClientOptions(Config{DatabaseURL: "mongodb://localhost/ridu_local", AllowInsecureTransport: true})
	if err != nil {
		t.Fatal(err)
	}
	if database != "ridu_local" || configured.MaxPoolSize == nil || *configured.MaxPoolSize != defaultMaxPoolSize || configured.AppName == nil || *configured.AppName != "ridu" {
		t.Fatalf("normalized config = database %q, pool %#v, app %#v", database, configured.MaxPoolSize, configured.AppName)
	}
	database, err = databaseNameFromURL("mongodb+srv://cluster.example/ridu_srv")
	if err != nil {
		t.Fatal(err)
	}
	if database != "ridu_srv" {
		t.Fatalf("SRV database = %q, want ridu_srv", database)
	}
}

func TestMongoConfigurationErrorsDoNotExposeCredentials(t *testing.T) {
	secret := "dont-print-this"
	_, _, err := normalizedClientOptions(Config{DatabaseURL: "mongodb://user:" + secret + "@host/%zz"})
	if err == nil {
		t.Fatal("invalid credential-bearing URL was accepted")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "user") {
		t.Fatalf("configuration error exposed credentials: %v", err)
	}
}

func TestMongoConnectionEntryPointsRejectNilContexts(t *testing.T) {
	if _, err := OpenWithConfig(nil, Config{}); err == nil || !strings.Contains(err.Error(), "context is required") {
		t.Fatalf("open error = %v, want required context", err)
	}
	if err := (&Store{}).Ping(nil); err == nil || !strings.Contains(err.Error(), "context is required") {
		t.Fatalf("ping error = %v, want required context", err)
	}
}

func TestMongoOpenPreservesPreCanceledContext(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := OpenWithConfig(canceled, Config{DatabaseURL: "mongodb://127.0.0.1/ridu", AllowInsecureTransport: true}); !errors.Is(err, context.Canceled) {
		t.Fatalf("open error = %v, want context.Canceled", err)
	}
}

func TestMongoTopologyRequiresWritableReplicaSetPrimaryAndSessions(t *testing.T) {
	sessions := int64(30)
	valid := mongoTopology{
		SetName: "ridu-rs0", IsWritablePrimary: true,
		LogicalSessionTimeoutMinutes: &sessions,
	}
	if err := validateMongoTopology(valid); err != nil {
		t.Fatalf("valid topology rejected: %v", err)
	}
	for _, test := range []struct {
		name     string
		topology mongoTopology
		want     string
	}{
		{name: "standalone", topology: mongoTopology{IsWritablePrimary: true, LogicalSessionTimeoutMinutes: &sessions}, want: "writable replica-set primary"},
		{name: "mongos", topology: mongoTopology{Message: "isdbgrid", IsWritablePrimary: true, LogicalSessionTimeoutMinutes: &sessions}, want: "sharded clusters"},
		{name: "secondary", topology: mongoTopology{SetName: "ridu-rs0", LogicalSessionTimeoutMinutes: &sessions}, want: "writable replica-set primary"},
		{name: "read only", topology: mongoTopology{SetName: "ridu-rs0", IsWritablePrimary: true, ReadOnly: true, LogicalSessionTimeoutMinutes: &sessions}, want: "writable replica-set primary"},
		{name: "sessions disabled", topology: mongoTopology{SetName: "ridu-rs0", IsWritablePrimary: true}, want: "logical sessions"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateMongoTopology(test.topology); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("topology error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestMongoCloseIsSafeForPartialInitialization(t *testing.T) {
	var missing *Store
	if err := missing.Close(); err != nil {
		t.Fatalf("nil close: %v", err)
	}
	partial := &Store{}
	if err := partial.Close(); err != nil {
		t.Fatalf("partial close: %v", err)
	}
	if err := partial.Close(); err != nil {
		t.Fatalf("repeated partial close: %v", err)
	}
}

func TestMongoErrorTranslationUsesStableStoreErrors(t *testing.T) {
	ctx := context.Background()
	if err := translateMongoError(ctx, mongo.ErrNoDocuments); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("no documents = %v, want ErrNotFound", err)
	}
	for name, conflict := range map[string]error{
		"write conflict":   mongo.CommandError{Code: 112, Message: "physical-name and authored-value"},
		"duplicate insert": mongo.CommandError{Code: 11000, Message: "E11000 physical-name index: secret-index dup key: authored-value"},
		"duplicate update": mongo.WriteException{WriteErrors: mongo.WriteErrors{{
			Code: 11001, Message: "E11001 physical-name index: secret-index dup key: authored-value",
		}}},
	} {
		t.Run(name, func(t *testing.T) {
			assertMongoRedactedConflict(t, translateMongoError(ctx, conflict), "physical-name", "secret-index", "authored-value")
		})
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := translateMongoError(canceled, errors.New("driver details")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation = %v, want context.Canceled", err)
	}
	deadline, cancelDeadline := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelDeadline()
	if err := translateMongoError(deadline, errors.New("driver details")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline = %v, want context.DeadlineExceeded", err)
	}
	wrappedCancellation := errors.Join(errors.New("private driver cancellation"), context.Canceled)
	if err := translateMongoError(ctx, wrappedCancellation); !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "private") {
		t.Fatalf("wrapped cancellation = %v, want redacted context.Canceled", err)
	}
	serverTimeout := mongo.CommandError{Code: 50, Message: "private server timeout"}
	if err := translateMongoError(ctx, serverTimeout); !errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "private") {
		t.Fatalf("server timeout = %v, want redacted context.DeadlineExceeded", err)
	}
}

func TestMongoAuthMetadataAcceptsOptionalUniqueIdentity(t *testing.T) {
	identityPath, err := query.ParsePath("email")
	if err != nil {
		t.Fatal(err)
	}
	collection := schema.Collection{
		ID: "optional-auth-users", Slug: "users",
		Capabilities: schema.Capabilities{Auth: true},
		Auth:         &schema.AuthSettings{IdentityField: "email"},
		Fields: []schema.Field{{
			ID: "optional-auth-users-email", Name: "email", Path: identityPath,
			Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Unique: true,
		}},
	}
	identity, err := validateMongoAuthCollectionMetadata(collection)
	if err != nil || identity.Name != "email" {
		t.Fatalf("optional unique auth identity = %#v, %v", identity, err)
	}
}

func assertMongoRedactedConflict(t testing.TB, err error, forbidden ...string) {
	t.Helper()
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("MongoDB conflict = %v, want ErrConflict", err)
	}
	if err.Error() != store.ErrConflict.Error() {
		t.Fatalf("MongoDB conflict text = %q, want %q", err.Error(), store.ErrConflict.Error())
	}
	var serverError mongo.ServerError
	if errors.As(err, &serverError) {
		t.Fatalf("MongoDB conflict exposes raw server error %T: %v", serverError, serverError)
	}
	for _, secret := range forbidden {
		if secret != "" && strings.Contains(err.Error(), secret) {
			t.Fatalf("MongoDB conflict exposes %q: %v", secret, err)
		}
	}
}

func TestMongoTransactionCompletionStatesFailDeterministically(t *testing.T) {
	for _, state := range []transactionState{transactionOpen, transactionCommitted, transactionRolledBack, transactionCommitUnknown} {
		t.Run(state.String(), func(t *testing.T) {
			transaction := &documentTransaction{state: state}
			if err := transaction.Commit(t.Context()); err == nil || !strings.Contains(err.Error(), state.String()) {
				t.Fatalf("commit error = %v, want state %q", err, state)
			}
			if err := transaction.Rollback(t.Context()); err == nil || !strings.Contains(err.Error(), state.String()) {
				t.Fatalf("rollback error = %v, want state %q", err, state)
			}
		})
	}
}

func TestMongoBoundedSchemaEnvelopeAcceptsImplementedAndRejectsUnimplementedCapabilities(t *testing.T) {
	base := mongoScalarCollection(false)
	if err := validateCollectionEnvelope(base); err != nil {
		t.Fatalf("direct scalar collection rejected: %v", err)
	}
	versioned := base
	versioned.Capabilities.Versions = true
	versioned.Versions = &schema.VersionSettings{Drafts: true}
	if err := validateCollectionEnvelope(versioned); err != nil {
		t.Fatalf("coherent version envelope rejected: %v", err)
	}
	locking := base
	locking.Capabilities.Locking = true
	locking.DocumentLock = &schema.DocumentLockSettings{DurationSeconds: 300}
	if err := validateCollectionEnvelope(locking); err != nil {
		t.Fatalf("coherent document-lock envelope rejected: %v", err)
	}
	inconsistentLocking := base
	inconsistentLocking.Capabilities.Locking = true
	if err := validateCollectionEnvelope(inconsistentLocking); err == nil || !strings.Contains(err.Error(), "inconsistent document-lock capability") {
		t.Fatalf("inconsistent document-lock envelope error = %v", err)
	}
	inconsistentVersion := base
	inconsistentVersion.Capabilities.Versions = true
	if err := validateCollectionEnvelope(inconsistentVersion); err == nil || !strings.Contains(err.Error(), "inconsistent version capability") {
		t.Fatalf("inconsistent version envelope error = %v", err)
	}
	indexed := base
	indexed.Fields = append([]schema.Field(nil), base.Fields...)
	indexed.Fields[0].Unique = true
	if err := validateCollectionEnvelope(indexed); err != nil {
		t.Fatalf("declared unique index envelope rejected: %v", err)
	}
	localized := base
	localized.Fields = append([]schema.Field(nil), base.Fields...)
	localized.Fields[0].Localized = true
	if err := validateCollectionEnvelope(localized); err != nil {
		t.Fatalf("localized scalar envelope rejected: %v", err)
	}
	localized.Fields[0].Index = true
	if err := validateCollectionEnvelope(localized); err != nil {
		t.Fatalf("localized indexed scalar envelope rejected: %v", err)
	}
	global := base
	global.Capabilities.Global = true
	if err := validateCollectionEnvelope(global); err != nil {
		t.Fatalf("global resource envelope rejected: %v", err)
	}
	path, _ := query.NewPath("title")
	if err := validateRequestEnvelope(store.Request{Collection: base, Populate: []query.Population{{Path: path}}}); err == nil || !strings.Contains(err.Error(), "population") {
		t.Fatalf("population envelope error = %v", err)
	}
}

func TestMongoRequestEnvelopeDoesNotLetNonFindCallsIgnoreMutationLocks(t *testing.T) {
	request := store.Request{Collection: mongoScalarCollection(false), Lock: store.LockMutation}
	if err := validateRequestEnvelope(request); err == nil || !strings.Contains(err.Error(), "lock mode") {
		t.Fatalf("shared request envelope accepted a mutation lock: %v", err)
	}
}

func TestMongoFilteredSelectionRejectsUnboundedLimitsBeforeEnteringTransaction(t *testing.T) {
	transaction := &documentTransaction{}
	for _, limit := range []int{0, store.MaxListWindowDocuments + 1, math.MaxInt} {
		_, err := transaction.ResolveFilteredSelection(t.Context(), store.FilteredSelectionRequest{Limit: limit})
		if err == nil || !strings.Contains(err.Error(), "between 1 and") {
			t.Fatalf("limit %d error = %v, want bounded-limit error", limit, err)
		}
	}
}

func TestMongoRequiredValuesStayReadableAcrossDirectWrites(t *testing.T) {
	collection := mongoScalarCollection(false)
	collection.Fields = append([]schema.Field(nil), collection.Fields...)
	collection.Fields[0].Required = true
	if err := validateCompleteValues(collection, store.Values{"rank": store.Number(1)}); err == nil || !strings.Contains(err.Error(), "required field") {
		t.Fatalf("incomplete create values error = %v", err)
	}
	if err := validatePatchValues(collection, store.Values{"title": store.Null()}); err == nil || !strings.Contains(err.Error(), "cannot clear") {
		t.Fatalf("required-field patch error = %v", err)
	}
	if err := validatePatchValues(collection, store.Values{"rank": store.Number(2)}); err != nil {
		t.Fatalf("unrelated patch rejected: %v", err)
	}
}

func TestMongoNestedGroupEnvelopeSupportsPortableObjects(t *testing.T) {
	if err := validateCollectionEnvelope(mongoGroupCollection()); err != nil {
		t.Fatalf("nested scalar group envelope rejected: %v", err)
	}
	indexed := mongoGroupCollection()
	indexed.Fields[1].Nested.Fields[0].Index = true
	if err := validateCollectionEnvelope(indexed); err != nil {
		t.Fatalf("nested non-unique index rejected: %v", err)
	}
	localized := mongoGroupCollection()
	localized.Fields[1].Nested.Fields[0].Localized = true
	if err := validateCollectionEnvelope(localized); err != nil {
		t.Fatalf("localized scalar group child rejected: %v", err)
	}
	repeated := mongoGroupCollection()
	repeatedChild := &repeated.Fields[1].Nested.Fields[0]
	repeatedChild.Type = schema.FieldTypeArray
	repeatedChild.Category = schema.FieldCategoryNested
	repeatedChild.Text = nil
	repeatedChild.Nested = &schema.NestedField{}
	if err := validateCollectionEnvelope(repeated); err != nil {
		t.Fatalf("nested repeated group child rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*schema.Collection)
		want   string
	}{
		{
			name: "unique nested child",
			mutate: func(collection *schema.Collection) {
				collection.Fields[1].Nested.Fields[0].Unique = true
			},
			want: "cannot enforce unique nested field \"seo.headline\"",
		},
		{
			name: "relationship child",
			mutate: func(collection *schema.Collection) {
				child := &collection.Fields[1].Nested.Fields[0]
				child.Type = schema.FieldTypeRelationship
				child.Text = nil
				child.Relationship = &schema.RelationshipField{CollectionID: "users", CollectionSlug: "users"}
			},
			want: "relationship field \"seo.headline\" does not have a relationship contract",
		},
		{
			name: "noncanonical child path",
			mutate: func(collection *schema.Collection) {
				collection.Fields[1].Nested.Fields[0].Path, _ = query.NewPath("headline")
			},
			want: "canonical resolved path \"seo.headline\"",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			collection := mongoGroupCollection()
			test.mutate(&collection)
			if err := validateCollectionEnvelope(collection); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("group envelope error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestMongoNestedGroupValuesAreRecursivelySchemaChecked(t *testing.T) {
	collection := mongoGroupCollection()
	valid := store.Values{
		"title": store.String("nested"),
		"seo": store.Object(store.Values{
			"headline": store.String("safe"),
			"rank":     store.Number(1),
			"details": store.Object(store.Values{
				"summary": store.String("deep"),
			}),
		}),
	}
	if err := validateCompleteValues(collection, valid); err != nil {
		t.Fatalf("complete nested values rejected: %v", err)
	}

	tests := []struct {
		name   string
		values store.Values
		patch  bool
		want   string
	}{
		{name: "missing required group", values: store.Values{"title": store.String("missing")}, want: "required field \"seo\""},
		{name: "group list", values: store.Values{"seo": store.List(store.String("unsafe"))}, want: "value \"seo\" does not match field type \"group\""},
		{name: "unknown child", values: store.Values{"seo": store.Object(store.Values{"headline": store.String("safe"), "removed": store.String("leak")})}, want: "value \"seo.removed\" is not a stored field"},
		{name: "missing required child", values: store.Values{"seo": store.Object(store.Values{"rank": store.Number(1)})}, want: "required field \"seo.headline\""},
		{name: "missing deep required child", values: store.Values{"seo": store.Object(store.Values{"headline": store.String("safe"), "details": store.Object(store.Values{})})}, want: "required field \"seo.details.summary\""},
		{name: "patch cannot clear required group", values: store.Values{"seo": store.Null()}, patch: true, want: "cannot clear required field \"seo\""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var err error
			if test.patch {
				err = validatePatchValues(collection, test.values)
			} else {
				err = validateCompleteValues(collection, test.values)
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("nested value error = %v, want containing %q", err, test.want)
			}
		})
	}
	if err := validatePatchValues(collection, store.Values{"title": store.String("unrelated")}); err != nil {
		t.Fatalf("unrelated root patch rejected: %v", err)
	}
	if err := validatePatchValues(collection, store.Values{"seo": store.Object(store.Values{"headline": store.String("changed")})}); err != nil {
		t.Fatalf("partial supplied group patch rejected: %v", err)
	}
	if err := validatePatchValues(collection, store.Values{"seo": store.Object(store.Values{"rank": store.Number(2)})}); err != nil {
		t.Fatalf("sibling-only supplied group patch rejected: %v", err)
	}
}

func TestMongoRepeatedFieldEnvelopeIsBounded(t *testing.T) {
	if err := validateCollectionEnvelope(mongoRepeatedCollection()); err != nil {
		t.Fatalf("bounded repeated envelope rejected: %v", err)
	}
	portable := []struct {
		name   string
		mutate func(*schema.Collection)
	}{
		{name: "localized root", mutate: func(collection *schema.Collection) { collection.Fields[2].Localized = true }},
		{name: "localized row child", mutate: func(collection *schema.Collection) { collection.Fields[2].Nested.Fields[0].Localized = true }},
		{
			name: "relationship row child",
			mutate: func(collection *schema.Collection) {
				child := &collection.Fields[2].Nested.Fields[0]
				child.Type, child.Category, child.Text = schema.FieldTypeRelationship, schema.FieldCategoryRelationship, nil
				child.Relationship = &schema.RelationshipField{CollectionID: "users", CollectionSlug: "users", OnDelete: schema.ReferenceDeleteRestrict}
			},
		},
	}
	for _, test := range portable {
		t.Run(test.name, func(t *testing.T) {
			collection := mongoRepeatedCollection()
			test.mutate(&collection)
			if err := validateCollectionEnvelope(collection); err != nil {
				t.Fatalf("portable repeated envelope error = %v", err)
			}
		})
	}
	tests := []struct {
		name   string
		mutate func(*schema.Collection)
		want   string
	}{
		{name: "indexed root", mutate: func(collection *schema.Collection) { collection.Fields[2].Index = true }, want: "indexed or unique repeated"},
		{name: "indexed row child", mutate: func(collection *schema.Collection) { collection.Fields[2].Nested.Fields[0].Index = true }, want: "indexes on field"},
		{
			name: "block discriminator child",
			mutate: func(collection *schema.Collection) {
				child := collection.Fields[3].Blocks.Types[0].Fields[0]
				child.Name = "blockType"
				child.Path, _ = query.NewPath("layout", "hero", "blockType")
				collection.Fields[3].Blocks.Types[0].Fields[0] = child
			},
			want: "reserved discriminator",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			collection := mongoRepeatedCollection()
			test.mutate(&collection)
			if err := validateCollectionEnvelope(collection); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("repeated envelope error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestMongoRepeatedValuesRequireCanonicalWholeRoots(t *testing.T) {
	collection := mongoRepeatedCollection()
	valid := mongoRepeatedValues()
	if err := validateCompleteValues(collection, valid); err != nil {
		t.Fatalf("complete repeated values rejected: %v", err)
	}
	if err := validatePatchValues(collection, store.Values{
		"rows": valid["rows"], "layout": valid["layout"], "tags": valid["tags"],
	}); err != nil {
		t.Fatalf("whole-root repeated patch rejected: %v", err)
	}

	tests := []struct {
		name   string
		values store.Values
		patch  bool
		want   string
	}{
		{name: "select scalar", values: store.Values{"tags": store.String("alpha")}, want: "does not match field type"},
		{name: "unknown select", values: store.Values{"tags": store.List(store.String("missing"))}, want: "unknown select choice"},
		{name: "duplicate select", values: store.Values{"tags": store.List(store.String("alpha"), store.String("alpha"))}, want: "duplicates select choice"},
		{name: "array scalar", values: store.Values{"rows": store.String("unsafe")}, want: "does not match field type"},
		{name: "array row scalar", values: store.Values{"rows": store.List(store.String("unsafe"))}, want: "must be an object"},
		{name: "unknown row field", values: store.Values{"rows": store.List(store.Object(store.Values{"kind": store.String("a"), "label": store.String("b"), "removed": store.String("unsafe")}))}, want: "not a stored field"},
		{name: "invalid row key", values: store.Values{"rows": store.List(store.Object(store.Values{"_key": store.String(" "), "kind": store.String("a"), "label": store.String("b")}))}, want: "non-empty string"},
		{name: "duplicate row key", values: store.Values{"rows": store.List(
			store.Object(store.Values{"_key": store.String("same"), "kind": store.String("a"), "label": store.String("b")}),
			store.Object(store.Values{"_key": store.String("same"), "kind": store.String("c"), "label": store.String("d")}),
		)}, want: "duplicates row"},
		{name: "partial row replacement", patch: true, values: store.Values{"rows": store.List(store.Object(store.Values{"kind": store.String("a")}))}, want: "missing required field"},
		{name: "block without discriminator", values: store.Values{"layout": store.List(store.Object(store.Values{"heading": store.String("unsafe")}))}, want: "invalid blockType"},
		{name: "unknown block discriminator", values: store.Values{"layout": store.List(store.Object(store.Values{"blockType": store.String("missing")}))}, want: "invalid blockType"},
		{name: "wrong block fields", values: store.Values{"layout": store.List(store.Object(store.Values{"blockType": store.String("quote"), "tone": store.String("unsafe"), "heading": store.String("quote")}))}, want: "not a stored field"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var err error
			if test.patch {
				err = validatePatchValues(collection, test.values)
			} else {
				err = validateValues(collection, test.values)
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("repeated value error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func mongoScalarCollection(trash bool) schema.Collection {
	title, _ := query.NewPath("title")
	rank, _ := query.NewPath("rank")
	return schema.Collection{
		ID: "posts", Slug: "posts", Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"},
		Capabilities: schema.Capabilities{Trash: trash},
		Fields: []schema.Field{
			{ID: "posts-title", Name: "title", Path: title, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Text: &schema.TextField{}},
			{ID: "posts-rank", Name: "rank", Path: rank, Type: schema.FieldTypeNumber, Category: schema.FieldCategoryScalar, Number: &schema.NumberField{}},
		},
	}
}

func mongoGroupCollection() schema.Collection {
	title, _ := query.NewPath("title")
	seo, _ := query.NewPath("seo")
	headline, _ := query.NewPath("seo", "headline")
	rank, _ := query.NewPath("seo", "rank")
	details, _ := query.NewPath("seo", "details")
	summary, _ := query.NewPath("seo", "details", "summary")
	return schema.Collection{
		ID: "posts", Slug: "posts", Labels: schema.CollectionLabels{Singular: "Post", Plural: "Posts"},
		Fields: []schema.Field{
			{ID: "posts-title", Name: "title", Path: title, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Text: &schema.TextField{}},
			{
				ID: "posts-seo", Name: "seo", Path: seo, Type: schema.FieldTypeGroup, Category: schema.FieldCategoryNested, Required: true,
				Nested: &schema.NestedField{Fields: []schema.Field{
					{ID: "posts-seo-headline", Name: "headline", Path: headline, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Required: true, Text: &schema.TextField{}},
					{ID: "posts-seo-rank", Name: "rank", Path: rank, Type: schema.FieldTypeNumber, Category: schema.FieldCategoryScalar, Number: &schema.NumberField{}},
					{
						ID: "posts-seo-details", Name: "details", Path: details, Type: schema.FieldTypeGroup, Category: schema.FieldCategoryNested,
						Nested: &schema.NestedField{Fields: []schema.Field{
							{ID: "posts-seo-details-summary", Name: "summary", Path: summary, Type: schema.FieldTypeText, Category: schema.FieldCategoryScalar, Required: true, Text: &schema.TextField{}},
						}},
					},
				}},
			},
		},
	}
}

func mongoRepeatedCollection() schema.Collection {
	path := func(segments ...string) query.Path {
		value, _ := query.NewPath(segments...)
		return value
	}
	text := func(id schema.StableID, name string, fieldPath query.Path, required bool) schema.Field {
		return schema.Field{
			ID: id, Name: name, Path: fieldPath, Type: schema.FieldTypeText,
			Category: schema.FieldCategoryScalar, Required: required, Text: &schema.TextField{},
		}
	}
	return schema.Collection{
		ID: "repeated-posts", Slug: "repeated-posts",
		Labels: schema.CollectionLabels{Singular: "Repeated post", Plural: "Repeated posts"},
		Fields: []schema.Field{
			text("repeated-posts-title", "title", path("title"), false),
			{
				ID: "repeated-posts-tags", Name: "tags", Path: path("tags"),
				Type: schema.FieldTypeSelect, Category: schema.FieldCategoryScalar,
				Select: &schema.SelectField{HasMany: true, Choices: []schema.SelectChoice{
					{Value: "alpha", Label: "Alpha"}, {Value: "beta", Label: "Beta"},
				}},
			},
			{
				ID: "repeated-posts-rows", Name: "rows", Path: path("rows"),
				Type: schema.FieldTypeArray, Category: schema.FieldCategoryNested,
				Nested: &schema.NestedField{Fields: []schema.Field{
					text("repeated-posts-rows-kind", "kind", path("rows", "kind"), true),
					text("repeated-posts-rows-label", "label", path("rows", "label"), true),
					{
						ID: "repeated-posts-rows-state", Name: "state", Path: path("rows", "state"),
						Type: schema.FieldTypeSelect, Category: schema.FieldCategoryScalar,
						Select: &schema.SelectField{Choices: []schema.SelectChoice{
							{Value: "draft", Label: "Draft"}, {Value: "published", Label: "Published"},
						}},
					},
					{
						ID: "repeated-posts-rows-tone", Name: "tone", Path: path("rows", "tone"),
						Type: schema.FieldTypeRadio, Category: schema.FieldCategoryScalar,
						Select: &schema.SelectField{Choices: []schema.SelectChoice{
							{Value: "quiet", Label: "Quiet"}, {Value: "loud", Label: "Loud"},
						}},
					},
					{
						ID: "repeated-posts-rows-details", Name: "details", Path: path("rows", "details"),
						Type: schema.FieldTypeGroup, Category: schema.FieldCategoryNested,
						Nested: &schema.NestedField{Fields: []schema.Field{
							text("repeated-posts-rows-details-note", "note", path("rows", "details", "note"), false),
						}},
					},
				}},
			},
			{
				ID: "repeated-posts-layout", Name: "layout", Path: path("layout"),
				Type: schema.FieldTypeBlocks, Category: schema.FieldCategoryNested,
				Blocks: &schema.BlocksField{Types: []schema.BlockType{
					{Key: "hero", Label: "Hero", Fields: []schema.Field{
						text("repeated-posts-layout-hero-heading", "heading", path("layout", "hero", "heading"), true),
						text("repeated-posts-layout-hero-tone", "tone", path("layout", "hero", "tone"), false),
					}},
					{Key: "quote", Label: "Quote", Fields: []schema.Field{
						text("repeated-posts-layout-quote-heading", "heading", path("layout", "quote", "heading"), true),
					}},
				}},
			},
		},
	}
}

func mongoRepeatedValues() store.Values {
	return store.Values{
		"title": store.String("repeated"),
		"tags":  store.List(store.String("alpha"), store.String("beta")),
		"rows": store.List(
			store.Object(store.Values{
				"_key": store.String("row-1"), "kind": store.String("primary"), "label": store.String("First"),
				"details": store.Object(store.Values{"note": store.String("nested")}),
			}),
			store.Object(store.Values{"_key": store.String("row-2"), "kind": store.String("secondary"), "label": store.String("Second")}),
		),
		"layout": store.List(
			store.Object(store.Values{"_key": store.String("hero-1"), "blockType": store.String("hero"), "heading": store.String("Welcome"), "tone": store.String("bright")}),
			store.Object(store.Values{"_key": store.String("quote-1"), "blockType": store.String("quote"), "heading": store.String("Evidence")}),
		),
	}
}
