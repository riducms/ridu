package queryaccess

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"io"
	"testing"

	"github.com/riducms/ridu"
	localstorage "github.com/riducms/ridu/adapters/storage/local"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/operation"
	fieldoperation "github.com/riducms/ridu/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
)

func uploadLookup(t *testing.T, factory Factory) {
	storage, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	allow := true
	privateMetadata := field.Access{Read: func(ctx fieldoperation.Context) (bool, error) {
		return ctx.Actor.ID != "" && ctx.Actor.ID == "writer", nil
	}}
	_, app := factory(t, ridu.Config{
		Name: "Private upload query metadata", Storage: storage, StorageNamespace: "query-access",
		Collections: []ridu.Collection{{Slug: "media", Upload: true,
			UploadConfig: ridu.UploadConfig{MimeTypes: []string{"image/png"}, ImageSizes: []ridu.ImageSize{{Name: "thumb", Width: 16, Height: 16, Fit: "cover"}}},
			Fields:       field.Fields{field.Text("objectKey").Access(privateMetadata), field.JSON("sizes").Access(privateMetadata)},
			Access: ridu.CollectionAccess{Read: func(ridu.AccessContext) (ridu.AccessDecision, error) {
				if allow {
					return ridu.Allow(), nil
				}
				return ridu.Deny(), nil
			}},
		}},
	})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 64, 48))); err != nil {
		t.Fatal(err)
	}
	document, err := app.Upload(t.Context(), "media", ridu.UploadInput{
		Filename: "test.png", Reader: bytes.NewReader(encoded.Bytes()), Actor: &store.Document{ID: "writer"},
	})
	if err != nil {
		t.Fatal(err)
	}
	original, _ := document.Values["objectKey"].StringValue()
	sizes, _ := document.Values["sizes"].CopyObject()
	thumb, _ := sizes["thumb"].CopyObject()
	thumbnail, _ := thumb["objectKey"].StringValue()
	if original == "" || thumbnail == "" || original == thumbnail {
		t.Fatalf("writer did not receive distinct original and thumbnail keys: %q, %q", original, thumbnail)
	}
	public, err := app.Local().Find(t.Context(), "media", document.ID, ridu.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"objectKey", "sizes"} {
		if _, found := public.Values[name]; found {
			t.Fatalf("anonymous read returned private %s", name)
		}
	}
	for _, key := range []string{original, thumbnail} {
		reader, _, err := app.OpenUpload(t.Context(), "media", key, nil)
		if err != nil {
			t.Fatalf("authorized download with private lookup metadata: %v", err)
		}
		data, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil || len(data) == 0 {
			t.Fatalf("download bytes=%d, read=%v, close=%v", len(data), readErr, closeErr)
		}
	}
	_, err = app.Local().List(t.Context(), "media", ridu.ListOptions{Where: query.Equal(path("objectKey"), query.String(original))})
	denied(t, err, "objectKey")
	_, err = app.Local().List(t.Context(), "media", ridu.ListOptions{Where: query.Equal(path("sizes.thumb.objectKey"), query.String(thumbnail))})
	denied(t, err, "sizes.thumb.objectKey")
	allow = false
	for _, key := range []string{original, thumbnail} {
		reader, _, err := app.OpenUpload(t.Context(), "media", key, nil)
		if reader != nil {
			reader.Close()
			t.Fatal("denied collection download returned a reader")
		}
		var failure *operation.Error
		if !errors.As(err, &failure) || failure.Code != "access_denied" {
			t.Fatalf("denied collection download: %v", err)
		}
	}
}
