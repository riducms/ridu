package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"

	"example.com/ridu-documentation/content"
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/sqlite"
	localstorage "github.com/riducms/ridu/adapters/storage/local"
	"github.com/riducms/ridu/plugins/richtext"
	"github.com/riducms/ridu/store"
)

func main() {
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	databasePath := os.Getenv("RIDU_DOCS_DATABASE_PATH")
	uploadRoot := os.Getenv("RIDU_DOCS_UPLOAD_ROOT")
	if databasePath == "" || uploadRoot == "" {
		return fmt.Errorf("RIDU_DOCS_DATABASE_PATH and RIDU_DOCS_UPLOAD_ROOT are required")
	}
	backend, err := sqlite.Open(ctx, databasePath)
	if err != nil {
		return err
	}
	defer backend.Close()
	objects, err := localstorage.New(uploadRoot)
	if err != nil {
		return err
	}
	applicationConfig := content.Config()
	applicationConfig.Storage = objects
	applicationConfig.StorageNamespace = "documentation"
	application, err := ridu.New(applicationConfig, backend)
	if err != nil {
		return err
	}
	bootstrap := &store.Document{ID: "documentation-bootstrap", Values: store.Values{}}
	administrator, err := application.Local().Import(ctx, "users", store.Values{
		"email": store.String("docs@riducms.test"),
	}, ridu.ImportOptions{ID: "docs-user", Status: store.StatusPublished}, bootstrap)
	if err != nil {
		return fmt.Errorf("create documentation user: %w", err)
	}
	if err := application.SetPassword(ctx, "users", administrator.ID, "ridu-documentation"); err != nil {
		return fmt.Errorf("set documentation password: %w", err)
	}
	media, err := application.Upload(ctx, "media", ridu.UploadInput{
		Filename: "field-guide.png",
		Reader:   bytes.NewReader(samplePNG()),
		Data: store.Values{
			"alt":     store.String("Pink and green Ridu field guide cover"),
			"caption": store.String("Generated deterministically by the documentation playground."),
		},
		Actor: &administrator,
	})
	if err != nil {
		return fmt.Errorf("create documentation upload: %w", err)
	}
	category, err := application.Local().Import(ctx, "categories", store.Values{
		"name": store.String("Guides"),
		"slug": store.String("guides"),
	}, ridu.ImportOptions{ID: "docs-category", Status: store.StatusPublished}, &administrator)
	if err != nil {
		return fmt.Errorf("create category: %w", err)
	}
	_, err = application.Local().Import(ctx, "articles", store.Values{
		"title":    store.String("Build a focused content model"),
		"summary":  store.String("A small article used by the documentation playground."),
		"status":   store.String("published"),
		"category": store.String(category.ID),
		"cover":    store.String(media.ID),
		"content":  richDocument("Ridu keeps every authoring surface connected to executable Go configuration."),
	}, ridu.ImportOptions{ID: "docs-article", Status: store.StatusPublished}, &administrator)
	if err != nil {
		return fmt.Errorf("create article: %w", err)
	}
	_, err = application.Local().Import(ctx, "field-guide", store.Values{
		"title":        store.String("Field guide example"),
		"summary":      store.String("Focused controls without an unrelated kitchen-sink schema."),
		"contactEmail": store.String("editors@riducms.test"),
		"priority":     store.Number(7),
		"publishedAt":  store.String("2026-09-02T08:57:00Z"),
		"featured":     store.Boolean(true),
		"status":       store.List(store.String("review"), store.String("published")),
		"tone":         store.String("friendly"),
		"location":     store.List(store.Number(-0.1276), store.Number(51.5072)),
		"metadata":     store.Object(store.Values{"audience": store.String("Payload developers"), "verified": store.Boolean(true)}),
		"source":       store.String("export const cms = 'ridu';"),
		"slug":         store.String("field-guide-example"),
		"seo": store.Object(store.Values{
			"title":       store.String("Ridu field guide"),
			"description": store.String("Learn one field at a time."),
		}),
		"links": store.List(
			store.Object(store.Values{"_key": store.String("docs-link-fields"), "label": store.String("Fields overview"), "url": store.String("/docs/fields/")}),
			store.Object(store.Values{"_key": store.String("docs-link-reference"), "label": store.String("API reference"), "url": store.String("/reference/field/")}),
		),
		"layout": store.List(
			store.Object(store.Values{"_key": store.String("docs-callout"), "blockType": store.String("callout"), "tone": store.String("note"), "body": store.String("Start with the smallest working model.")}),
			store.Object(store.Values{"_key": store.String("docs-quote"), "blockType": store.String("quote"), "quote": store.String("One source of truth."), "attribution": store.String("Ridu")}),
		),
		"author":          store.String(administrator.ID),
		"cover":           store.String(media.ID),
		"firstName":       store.String("Ada"),
		"lastName":        store.String("Lovelace"),
		"internalName":    store.String("field-guide-capture"),
		"editorNotes":     store.String("Advanced settings remain layout-only."),
		"tabIntroduction": store.String("The first tab keeps top-level values top-level."),
		"settings":        store.Object(store.Values{"theme": store.String("Focused")}),
		"content":         richDocument("Rich text is supplied by a paired Go and admin plugin."),
	}, ridu.ImportOptions{ID: "docs-field-guide", Status: store.StatusPublished}, &administrator)
	if err != nil {
		return fmt.Errorf("create field guide: %w", err)
	}
	return nil
}

func richDocument(text string) store.Value {
	return store.Object(store.Values{
		"version": store.Number(richtext.DocumentVersion),
		"root": store.Object(store.Values{
			"type": store.String("root"),
			"children": store.List(store.Object(store.Values{
				"type": store.String("paragraph"),
				"children": store.List(store.Object(store.Values{
					"type": store.String("text"),
					"text": store.String(text),
				})),
			})),
		}),
	})
}

func samplePNG() []byte {
	canvas := image.NewRGBA(image.Rect(0, 0, 960, 540))
	for y := 0; y < canvas.Bounds().Dy(); y++ {
		for x := 0; x < canvas.Bounds().Dx(); x++ {
			fill := color.RGBA{R: 243, G: 71, B: 148, A: 255}
			if x > canvas.Bounds().Dx()/2 {
				fill = color.RGBA{R: 181, G: 228, B: 140, A: 255}
			}
			canvas.SetRGBA(x, y, fill)
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, canvas); err != nil {
		panic(err)
	}
	return encoded.Bytes()
}
