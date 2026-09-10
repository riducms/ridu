package blocks_test

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/sqlite"
	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/examples/blocks/content"
	"github.com/riducms/ridu/examples/blocks/generated"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/schema"
)

func TestTypedArticleRoundTripPreservesRedactionLocalizationAndPopulation(t *testing.T) {
	ctx := t.Context()
	backend, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "articles.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	config := content.Config()
	manifest, err := ridu.Resolve(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Migrate(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	app, err := ridu.New(config, backend)
	if err != nil {
		t.Fatal(err)
	}
	assets := generated.AssetsCollection.With(app.Local())
	asset, err := assets.Create(ctx, generated.AssetCreate{Title: "Article image", URL: "/article.svg"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	inner := typedRichTextDocument(typedRichTextBlock(generated.CalloutDetailBlocksBlockInputPayload{Value: &generated.CTAInput{Label: "Nested action"}}))
	body := typedRichTextDocument(
		richtext.Node[generated.ArticlesBodyBlocksBlockInputPayload]{Type: "paragraph", Version: 1, Format: json.RawMessage(`"center"`), Children: []richtext.Node[generated.ArticlesBodyBlocksBlockInputPayload]{{Type: "text", Version: 1, Text: "Keep this prose", Format: json.RawMessage(`1`)}}},
		typedRichTextBlock(generated.ArticlesBodyBlocksBlockInputPayload{Value: &generated.CalloutInput{Title: "Original", Message: core.Set("English message"), Detail: core.Set(inner)}}),
		typedRichTextBlock(generated.ArticlesBodyBlocksBlockInputPayload{Value: &generated.MediaInput{Asset: asset.ID, Caption: core.Set("English caption")}}),
		typedRichTextBlock(generated.ArticlesBodyBlocksBlockInputPayload{Value: &generated.CTAInput{Label: "Outer action"}}),
	)
	articles := generated.ArticlesCollection.With(app.Local())
	created, err := articles.Create(ctx, generated.ArticleCreate{Title: "Typed article", Body: core.Set(body)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if created.Body == nil {
		t.Fatal("created article omitted body")
	}
	callout := created.Body.Root.Children[1].Fields.Value.(*generated.Callout)
	media := created.Body.Root.Children[2].Fields.Value.(*generated.Media)
	cta := created.Body.Root.Children[3].Fields.Value.(*generated.CTA)
	if callout.Key == "" || media.Key == "" || cta.Key == "" || callout.Key == media.Key || media.Key == cta.Key {
		t.Fatal("create did not assign distinct block identities")
	}
	innerRead, present := callout.Detail.Get()
	if !present {
		t.Fatal("nested rich text omitted")
	}
	innerKey := innerRead.Root.Children[0].Fields.Value.(*generated.CTA).Key
	if innerKey == "" {
		t.Fatal("nested occurrence has no identity")
	}
	// A typed translation updates only declared localized children in shared structure.
	translation, err := richtext.MapDocumentBlocks(*created.Body, generated.ArticlesBodyBlocksBlockPayload.Retain)
	if err != nil {
		t.Fatal(err)
	}
	translation.Root.Children[1].Fields.Value.(*generated.CalloutUpdate).Message = core.Set("French message")
	translated, err := articles.UpdateRevision(ctx, created.ID, generated.ArticleUpdate{Body: core.Set(translation)}, created.Revision, nil, core.TypedLocaleOptions{Locale: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	// This reader cannot see Message, while Media.Asset is a populated document.
	restricted := content.Config()
	for _, host := range []string{"body", "localizedBody"} {
		restricted.Collections[3].Fields, err = restricted.Collections[3].Fields.Edit(func(root *field.ChildrenDraft) error {
			return root.EditBranch(host, field.BranchSelector{Boundary: field.EmbeddedCase, Tree: "blocks", Case: "block", Slug: "callout"}, func(block *field.ChildrenDraft) error {
				for _, node := range block.Fields() {
					if node.Name() != "message" {
						continue
					}
					message, err := field.AsTextarea(node)
					if err != nil {
						return err
					}
					return block.Replace("message", message.Access(field.Access{Read: func(operation.AccessContext) (bool, error) { return false, nil }}))
				}
				return fmt.Errorf("callout message field missing")
			})
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	restrictedApp, err := ridu.New(restricted, backend)
	if err != nil {
		t.Fatal(err)
	}
	restrictedArticles := generated.ArticlesCollection.With(restrictedApp.Local())
	assetPath, err := query.ParsePath("body.blocks.block.media.asset")
	if err != nil {
		t.Fatal(err)
	}
	draft := true
	before, err := restrictedArticles.Find(ctx, created.ID, core.TypedReadOptions{Draft: &draft, Populate: []query.Population{{Path: assetPath, Depth: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	if before.Revision != translated.Revision || before.Body == nil {
		t.Fatal("reader did not see the latest revision")
	}
	if before.Body.Root.Children[1].Fields.Value.(*generated.Callout).Message != nil {
		t.Fatal("read did not redact localized message")
	}
	readMedia := before.Body.Root.Children[2].Fields.Value.(*generated.Media)
	if readMedia.Asset == nil || readMedia.Asset.Document == nil || readMedia.Asset.ID != asset.ID {
		t.Fatal("typed populated output missing asset document")
	}
	updates, err := richtext.MapDocumentBlocks(*before.Body, generated.ArticlesBodyBlocksBlockPayload.Retain)
	if err != nil {
		t.Fatal(err)
	}
	updatedTitle := "Edited through generated contracts"
	edit := updates.Root.Children[1].Fields.Value.(*generated.CalloutUpdate)
	edit.Title = &updatedTitle
	if edit.Message != nil || edit.Detail != nil || updates.Root.Children[2].Fields.Value.(*generated.MediaUpdate).Asset != nil {
		t.Fatal("retention copied authored or populated child values")
	}
	// Saving has one field-specific update type; ordinary prose and the node envelope
	// survive the payload conversion, without JSON re-encoding or arbitrary maps.
	saved, err := restrictedArticles.UpdateRevision(ctx, created.ID, generated.ArticleUpdate{Body: core.Set(updates)}, before.Revision, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restrictedArticles.UpdateRevision(ctx, created.ID, generated.ArticleUpdate{Body: core.Set(updates)}, before.Revision, nil); err == nil {
		t.Fatal("typed edit ignored stale revision")
	}
	for _, locale := range []schema.LocaleCode{"en", "fr"} {
		after, err := articles.Find(ctx, created.ID, core.TypedReadOptions{Draft: &draft, Locale: locale})
		if err != nil {
			t.Fatal(err)
		}
		if after.Revision != saved.Revision || after.Body == nil || len(after.Body.Root.Children) != 4 {
			t.Fatal("saved document shape changed")
		}
		paragraph := after.Body.Root.Children[0]
		if paragraph.Children[0].Text != "Keep this prose" || string(paragraph.Format) != `"center"` || string(paragraph.Children[0].Format) != `1` {
			t.Fatal("payload mapping lost ordinary node properties")
		}
		actual := after.Body.Root.Children[1].Fields.Value.(*generated.Callout)
		message, present := actual.Message.Get()
		want := "English message"
		if locale == "fr" {
			want = "French message"
		}
		if actual.Key != callout.Key || actual.Title == nil || *actual.Title != updatedTitle || !present || message != want {
			t.Fatal("typed edit damaged identity, title or locale")
		}
		detail, present := actual.Detail.Get()
		if !present || detail.Root.Children[0].Fields.Value.(*generated.CTA).Key != innerKey {
			t.Fatal("typed edit damaged untouched nested editor")
		}
		actualMedia := after.Body.Root.Children[2].Fields.Value.(*generated.Media)
		if actualMedia.Key != media.Key || actualMedia.Asset == nil || actualMedia.Asset.ID != asset.ID || actualMedia.Asset.Document != nil {
			t.Fatal("retained media lost its canonical reference")
		}
	}
}

func typedRichTextDocument[T any](nodes ...richtext.Node[T]) richtext.Document[T] {
	return richtext.Document[T]{Version: 1, Root: richtext.Node[T]{Type: "root", Version: 1, Children: nodes}}
}

func typedRichTextBlock[T any](payload T) richtext.Node[T] {
	return richtext.Node[T]{Type: "block", Version: 1, Fields: &payload}
}
