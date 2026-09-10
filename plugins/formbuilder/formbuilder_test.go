package formbuilder_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	ridu "github.com/riducms/ridu"
	localstorage "github.com/riducms/ridu/adapters/storage/local"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/plugins/formbuilder"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

func TestPluginAddsFormCollectionsAndAdminPair(t *testing.T) {
	manifest, err := ridu.Resolve(ridu.Config{
		Name: "Forms",
		Plugins: []ridu.Plugin{formbuilder.New(formbuilder.Config{
			EnabledFields:         []formbuilder.FieldType{formbuilder.FieldText, formbuilder.FieldRadio, formbuilder.FieldDate, formbuilder.FieldUpload, formbuilder.FieldPayment},
			UploadCollections:     []schema.CollectionSlug{"media"},
			RedirectRelationships: []schema.CollectionSlug{"pages"},
			PaymentProcessors:     []field.Option{{Value: "stripe", Label: "Stripe"}},
			HandlePayment: func(formbuilder.PaymentContext) (store.Value, error) {
				return store.Null(), nil
			},
		})},
		Collections: []ridu.Collection{
			{Slug: "media", Upload: true, Fields: field.Fields{
				field.Text("alt"),
			}},
			{Slug: "pages", Fields: field.Fields{
				field.Text("title"),
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := manifest.Snapshot()
	if len(snapshot.Plugins) != 1 || snapshot.Plugins[0].Admin == nil || snapshot.Plugins[0].Admin.Package != "@riducms/plugin-form-builder/admin" || snapshot.Plugins[0].Admin.PairingVersion != 1 {
		t.Fatalf("form builder descriptor = %#v", snapshot.Plugins)
	}
	if len(snapshot.Collections) != 4 {
		t.Fatalf("collections = %d, want authored collections plus forms and submissions", len(snapshot.Collections))
	}
	forms := snapshot.Collections[2]
	if forms.Slug != "forms" || forms.Admin.UseAsTitle != "title" || len(forms.Fields) != 7 {
		t.Fatalf("forms collection = %#v", forms)
	}
	if forms.Fields[1].Blocks == nil || len(forms.Fields[1].Blocks.ResolvedTypes()) != 5 {
		t.Fatalf("form field blocks = %#v", forms.Fields[1].Blocks)
	}
	submissions := snapshot.Collections[3]
	if submissions.Slug != "form-submissions" || len(submissions.Fields) != 4 || submissions.Fields[0].Relationship == nil || submissions.Fields[0].Relationship.CollectionSlug != "forms" {
		t.Fatalf("submissions collection = %#v", submissions)
	}
}

func TestPluginRejectsInvalidTargetsAndOptInDependencies(t *testing.T) {
	_, err := ridu.Resolve(ridu.Config{
		Name: "Forms",
		Plugins: []ridu.Plugin{formbuilder.New(formbuilder.Config{
			EnabledFields:         []formbuilder.FieldType{formbuilder.FieldUpload, formbuilder.FieldPayment},
			UploadCollections:     []schema.CollectionSlug{"files", "missing"},
			RedirectRelationships: []schema.CollectionSlug{"missing"},
		})},
		Collections: []ridu.Collection{{Slug: "files"}},
	})
	var validation *schema.ValidationError
	if !errors.As(err, &validation) || !strings.Contains(err.Error(), "invalid_form_upload_collection") || !strings.Contains(err.Error(), "unknown_form_redirect_collection") || !strings.Contains(err.Error(), "missing_form_payment_processor") {
		t.Fatalf("validation error = %v", err)
	}
}

func TestFieldsOverrideCannotBypassReservedPaymentDependencies(t *testing.T) {
	_, err := ridu.Resolve(ridu.Config{
		Name: "Forms",
		Plugins: []ridu.Plugin{formbuilder.New(formbuilder.Config{Fields: func(defaults []field.Block) ([]field.Block, error) {
			return append(defaults, field.Block{Slug: "payment", Fields: field.Fields{
				field.Text("name"),
			}}), nil
		}})},
	})
	var validation *schema.ValidationError
	if !errors.As(err, &validation) || !strings.Contains(err.Error(), "missing_form_payment_processor") || !strings.Contains(err.Error(), "missing_form_payment_handler") {
		t.Fatalf("effective payment validation error = %v", err)
	}
}

func TestFieldsOverrideCanAddAProjectOwnedFormBlock(t *testing.T) {
	application := newApplication(t, formbuilder.Config{
		Fields: func(defaults []field.Block) ([]field.Block, error) {
			return append(defaults, field.Block{Slug: "rating", Fields: field.Fields{
				field.Text("name").Required(),
				field.Text("label"),
				field.Checkbox("required"),
			}}), nil
		},
	})
	form := createForm(t, application, store.Values{
		"title":               store.String("Review"),
		"confirmationType":    store.String("message"),
		"confirmationMessage": store.String("Thanks"),
		"fields": store.List(store.Object(store.Values{
			"_key": store.String("rating"), "blockType": store.String("rating"), "name": store.String("rating"), "required": store.Boolean(true),
		})),
	})
	if _, err := application.Local().Create(t.Context(), "form-submissions", store.Values{
		"form":           store.String(form.ID),
		"submissionData": store.List(submissionValue("rating", store.Number(5))),
	}, nil); err != nil {
		t.Fatalf("custom form block submission: %v", err)
	}
}

func TestSubmissionValidationUsesSelectedForm(t *testing.T) {
	application := newApplication(t, formbuilder.Config{})
	form := createForm(t, application, store.Values{
		"title":               store.String("Contact"),
		"confirmationType":    store.String("message"),
		"confirmationMessage": store.String("Thanks"),
		"fields": store.List(
			formField("name", formbuilder.FieldText, true),
			formField("email", formbuilder.FieldEmail, true),
			store.Object(store.Values{
				"_key": store.String("choice"), "blockType": store.String(string(formbuilder.FieldSelect)), "name": store.String("topic"),
				"options": store.List(store.Object(store.Values{"_key": store.String("sales"), "label": store.String("Sales"), "value": store.String("sales")})),
			}),
		),
	})

	created, err := application.Local().Create(t.Context(), "form-submissions", store.Values{
		"form": store.String(form.ID),
		"submissionData": store.List(
			submissionValue("name", store.String("Ada")),
			submissionValue("email", store.String("ada@example.test")),
			submissionValue("topic", store.String("sales")),
		),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" {
		t.Fatal("submission ID is empty")
	}

	_, err = application.Local().Create(t.Context(), "form-submissions", store.Values{
		"form": store.String(form.ID),
		"submissionData": store.List(
			submissionValue("email", store.String("not-an-email")),
			submissionValue("topic", store.String("support")),
			submissionValue("unknown", store.String("value")),
		),
	}, nil)
	var operationError *ridu.OperationError
	if !errors.As(err, &operationError) || operationError.Code != "validation" || operationError.Status != 422 {
		t.Fatalf("invalid submission error = %#v / %v", operationError, err)
	}
	wantCodes := []string{"required", "invalid_email", "invalid_option", "unknown_form_field"}
	for _, code := range wantCodes {
		if !hasIssueCode(operationError.Issues, code) {
			t.Errorf("issues %#v do not contain %q", operationError.Issues, code)
		}
	}
	if _, err := application.Local().Find(t.Context(), "form-submissions", created.ID, nil); err == nil {
		t.Fatal("anonymous submission read unexpectedly succeeded")
	}
}

func TestFormDefinitionValidationUsesCompleteCandidateOnPatchUpdate(t *testing.T) {
	application := newApplication(t, formbuilder.Config{})
	form := createForm(t, application, store.Values{
		"title":               store.String("Contact"),
		"confirmationType":    store.String("message"),
		"confirmationMessage": store.String("Thanks"),
		"fields":              store.List(formField("name", formbuilder.FieldText, true)),
	})

	if _, err := application.Local().Update(t.Context(), "forms", form.ID, store.Values{"title": store.String("Contact us")}, formManager()); err != nil {
		t.Fatalf("partial update rejected existing confirmation and fields: %v", err)
	}
	_, err := application.Local().Update(t.Context(), "forms", form.ID, store.Values{"confirmationMessage": store.String("")}, formManager())
	var operationError *ridu.OperationError
	if !errors.As(err, &operationError) || operationError.Code != "validation" || !hasIssueCode(operationError.Issues, "required") {
		t.Fatalf("invalid partial update error = %#v / %v", operationError, err)
	}
}

func TestEmailsRunAfterCommitWithEscapedPlaceholders(t *testing.T) {
	var delivered []formbuilder.Email
	application := newApplication(t, formbuilder.Config{
		SendEmail: func(_ context.Context, email formbuilder.Email) error {
			delivered = append(delivered, email)
			return nil
		},
	})
	form := createForm(t, application, store.Values{
		"title":               store.String("Contact"),
		"confirmationType":    store.String("message"),
		"confirmationMessage": store.String("Thanks"),
		"fields":              store.List(formField("name", formbuilder.FieldText, true), formField("email", formbuilder.FieldEmail, true)),
		"emails": store.List(store.Object(store.Values{
			"_key": store.String("notification"), "emailTo": store.String("team@example.test"), "emailFrom": store.String("website@example.test"),
			"replyTo": store.String("{{email}}"), "subject": store.String("New lead: {{name}}"), "message": store.String("<p>{{name}}</p>{{*:table}}"),
		})),
	})
	publicForm, err := application.Local().Find(t.Context(), "forms", form.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, exposed := publicForm.Values["emails"]; exposed {
		t.Fatal("anonymous form read exposed email delivery configuration")
	}
	created, err := application.Local().Create(t.Context(), "form-submissions", store.Values{
		"form":           store.String(form.ID),
		"submissionData": store.List(submissionValue("name", store.String("<Ada>")), submissionValue("email", store.String("ada@example.test"))),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(delivered) != 1 || delivered[0].ReplyTo != "ada@example.test" || delivered[0].Subject != "New lead: <Ada>" || !strings.Contains(delivered[0].HTML, "&lt;Ada&gt;") || !strings.Contains(delivered[0].HTML, created.ID) {
		t.Fatalf("delivered emails = %#v", delivered)
	}
}

func TestPaymentTotalAndCallbackArePersisted(t *testing.T) {
	application := newApplication(t, formbuilder.Config{
		EnabledFields:     []formbuilder.FieldType{formbuilder.FieldNumber, formbuilder.FieldPayment},
		PaymentProcessors: []field.Option{{Value: "test", Label: "Test"}},
		HandlePayment: func(context formbuilder.PaymentContext) (store.Value, error) {
			return store.Object(store.Values{"processor": store.String("test"), "total": store.Number(context.Total)}), nil
		},
	})
	form := createForm(t, application, store.Values{
		"title":               store.String("Donation"),
		"confirmationType":    store.String("message"),
		"confirmationMessage": store.String("Thanks"),
		"fields": store.List(
			formField("quantity", formbuilder.FieldNumber, true),
			store.Object(store.Values{
				"_key": store.String("payment"), "blockType": store.String(string(formbuilder.FieldPayment)), "name": store.String("amount"), "basePrice": store.Number(10), "paymentProcessor": store.String("test"),
				"priceConditions": store.List(store.Object(store.Values{
					"_key": store.String("condition"), "fieldToUse": store.String("quantity"), "condition": store.String("hasValue"), "operator": store.String("multiply"), "valueType": store.String("valueOfField"), "valueForOperator": store.String("quantity"),
				})),
			}),
		),
	})
	created, err := application.Local().Create(t.Context(), "form-submissions", store.Values{
		"form":           store.String(form.ID),
		"submissionData": store.List(submissionValue("quantity", store.Number(3)), submissionValue("amount", store.Number(30))),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	payment := created.Values["payment"]
	if payment.Kind() != store.ValueObject {
		t.Fatalf("payment = %#v", created.Values["payment"])
	}
	total, _ := payment.Get("total").NumberValue()
	if total != 30 {
		t.Fatalf("payment total = %v, want 30", total)
	}
}

func TestMessageBlocksNeedNoFieldIdentity(t *testing.T) {
	application := newApplication(t, formbuilder.Config{})
	createForm(t, application, store.Values{
		"title":               store.String("Information"),
		"confirmationType":    store.String("message"),
		"confirmationMessage": store.String("Thanks"),
		"fields": store.List(
			store.Object(store.Values{"_key": store.String("intro"), "blockType": store.String("message"), "message": store.String("Welcome")}),
			store.Object(store.Values{"_key": store.String("details"), "blockType": store.String("message")}),
		),
	})
}

func TestCollectionOverridesCanTightenSubmissionCreation(t *testing.T) {
	application := newApplication(t, formbuilder.Config{
		Submissions: func(collection ridu.Collection) (ridu.Collection, error) {
			collection.Access.Create = func(ridu.AccessContext) (ridu.AccessDecision, error) { return ridu.Deny(), nil }
			return collection, nil
		},
	})
	form := createForm(t, application, store.Values{
		"title": store.String("Private"), "confirmationType": store.String("message"), "confirmationMessage": store.String("Thanks"),
	})
	_, err := application.Local().Create(t.Context(), "form-submissions", store.Values{"form": store.String(form.ID)}, nil)
	var operationError *ridu.OperationError
	if !errors.As(err, &operationError) || operationError.Code != "access_denied" {
		t.Fatalf("submission create override error = %#v / %v", operationError, err)
	}
}

func TestFormManagementRequiresAuthenticationByDefault(t *testing.T) {
	application := newApplication(t, formbuilder.Config{})
	_, err := application.Local().Create(t.Context(), "forms", store.Values{
		"title": store.String("Anonymous"), "confirmationType": store.String("message"), "confirmationMessage": store.String("Thanks"),
	}, nil)
	var operationError *ridu.OperationError
	if !errors.As(err, &operationError) || operationError.Code != "access_denied" {
		t.Fatalf("anonymous form create error = %#v / %v", operationError, err)
	}
}

func TestFormManagementRequiresExactAdminCollection(t *testing.T) {
	application, err := ridu.New(ridu.Config{
		Name: "Forms", Admin: ridu.AdminConfig{User: "users"},
		Collections: []ridu.Collection{{Slug: "users", Auth: true, Fields: field.Fields{
			field.Email("email").Required().Unique(),
		}}},
		Plugins: []ridu.Plugin{formbuilder.New(formbuilder.Config{})},
	}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	values := store.Values{"title": store.String("Private"), "confirmationType": store.String("message"), "confirmationMessage": store.String("Thanks")}
	actor := formManager()
	if _, err := application.Local().Create(t.Context(), "forms", values, actor); err == nil {
		t.Fatal("actor without an auth collection unexpectedly managed forms")
	}
	if _, err := application.Local().CreateWithOptions(t.Context(), "forms", values, ridu.MutationOptions{Actor: actor, ActorCollection: "users"}); err != nil {
		t.Fatalf("configured admin actor create: %v", err)
	}
}

func TestUploadSubmissionsUseGeneratedRelationshipShapes(t *testing.T) {
	for _, test := range []struct {
		name        string
		collections []schema.CollectionSlug
	}{
		{name: "single target IDs", collections: []schema.CollectionSlug{"media"}},
		{name: "multiple target polymorphic IDs", collections: []schema.CollectionSlug{"media", "files"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			backend, err := localstorage.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			collections := make([]ridu.Collection, len(test.collections))
			for index, slug := range test.collections {
				collections[index] = ridu.Collection{Slug: slug, Upload: true, UploadConfig: ridu.UploadConfig{MimeTypes: []string{"text/plain"}}, Fields: field.Fields{
					field.Text("alt"),
				}}
			}
			application, err := ridu.New(ridu.Config{
				Name: "Upload forms", Collections: collections,
				Storage: backend, StorageNamespace: "form-builder-test",
				Plugins: []ridu.Plugin{formbuilder.New(formbuilder.Config{EnabledFields: []formbuilder.FieldType{formbuilder.FieldUpload}, UploadCollections: test.collections})},
			}, teststore.New())
			if err != nil {
				t.Fatal(err)
			}
			asset, err := application.Upload(t.Context(), "media", ridu.UploadInput{
				Filename: "receipt.txt", Reader: bytes.NewBufferString("receipt"), Data: store.Values{"alt": store.String("Receipt")}, Actor: formManager(),
			})
			if err != nil {
				t.Fatal(err)
			}
			reference := store.String(asset.ID)
			if len(test.collections) > 1 {
				reference = store.Object(store.Values{"relationTo": store.String("media"), "id": store.String(asset.ID)})
			}
			form := createForm(t, application, store.Values{
				"title": store.String("Receipt"), "confirmationType": store.String("message"), "confirmationMessage": store.String("Thanks"),
				"fields": store.List(store.Object(store.Values{
					"_key": store.String("receipt"), "blockType": store.String("upload"), "name": store.String("receipt"),
					"uploadCollection": store.String("media"), "required": store.Boolean(true), "maxFileSize": store.Number(256),
					"mimeTypes": store.List(store.Object(store.Values{"_key": store.String("text"), "mimeType": store.String("text/plain")})),
				})),
			})
			if _, err := application.Local().Create(t.Context(), "form-submissions", store.Values{
				"form": store.String(form.ID),
				"submissionUploads": store.List(store.Object(store.Values{
					"_key": store.String("receipt"), "field": store.String("receipt"), "value": store.List(reference),
				})),
			}, nil); err != nil {
				t.Fatalf("create submission: %v", err)
			}
		})
	}
}

func TestOptionalPaymentIsNotProcessedAndMultiplePaymentsAreRejected(t *testing.T) {
	called := 0
	application := newApplication(t, formbuilder.Config{
		EnabledFields: []formbuilder.FieldType{formbuilder.FieldPayment}, PaymentProcessors: []field.Option{{Value: "test", Label: "Test"}},
		HandlePayment: func(formbuilder.PaymentContext) (store.Value, error) { called++; return store.Null(), nil },
	})
	optional := store.Object(store.Values{
		"_key": store.String("optional"), "blockType": store.String("payment"), "name": store.String("amount"),
		"basePrice": store.Number(10), "paymentProcessor": store.String("test"),
	})
	form := createForm(t, application, store.Values{
		"title": store.String("Donation"), "confirmationType": store.String("message"), "confirmationMessage": store.String("Thanks"), "fields": store.List(optional),
	})
	if _, err := application.Local().Create(t.Context(), "form-submissions", store.Values{"form": store.String(form.ID)}, nil); err != nil {
		t.Fatal(err)
	}
	if called != 0 {
		t.Fatalf("optional omitted payment callback calls = %d, want 0", called)
	}
	_, err := application.Local().Update(t.Context(), "forms", form.ID, store.Values{"fields": store.List(optional, store.Object(store.Values{
		"_key": store.String("second"), "blockType": store.String("payment"), "name": store.String("second"),
		"basePrice": store.Number(5), "paymentProcessor": store.String("test"),
	}))}, formManager())
	var operationError *ridu.OperationError
	if !errors.As(err, &operationError) || !hasIssueCode(operationError.Issues, "multiple_payment_fields") {
		t.Fatalf("multiple payment error = %#v / %v", operationError, err)
	}
}

func TestPaymentTotalRejectsNegativeAuthoritativeAmount(t *testing.T) {
	_, err := formbuilder.GetPaymentTotal(10, []formbuilder.PriceCondition{{
		FieldToUse: "quantity", Condition: "hasValue", Operator: "multiply", ValueType: "valueOfField", ValueForOperator: "quantity",
	}}, map[string]store.Value{"quantity": store.Number(-1)})
	if err == nil || !strings.Contains(err.Error(), "negative total") {
		t.Fatalf("negative total error = %v", err)
	}
}

func newApplication(t *testing.T, config formbuilder.Config) *ridu.App {
	t.Helper()
	application, err := ridu.New(ridu.Config{Name: "Forms", Plugins: []ridu.Plugin{formbuilder.New(config)}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	return application
}

func createForm(t *testing.T, application *ridu.App, values store.Values) store.Document {
	t.Helper()
	created, err := application.Local().Create(t.Context(), "forms", values, formManager())
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func formManager() *store.Document { return &store.Document{ID: "form-manager"} }

func formField(name string, fieldType formbuilder.FieldType, required bool) store.Value {
	return store.Object(store.Values{"_key": store.String(name), "blockType": store.String(string(fieldType)), "name": store.String(name), "label": store.String(name), "required": store.Boolean(required)})
}

func submissionValue(name string, value store.Value) store.Value {
	return store.Object(store.Values{"_key": store.String(name), "field": store.String(name), "value": value})
}

func hasIssueCode(issues []schema.Issue, code string) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
