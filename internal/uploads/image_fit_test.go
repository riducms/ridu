package uploads_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os"
	"strings"
	"testing"

	localstorage "github.com/riducms/ridu/adapters/storage/local"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/internal/uploads"
	"github.com/riducms/ridu/schema"
)

type storedSize struct {
	width, height  int
	mimeType, key  string
	decoded        image.Rectangle
	decodedFormat  string
	cornerOpaque   bool
	storedFilesize int
}

func prepareSizes(t *testing.T, encoded []byte, mimeTypes []string, sizes ...schema.ImageSize) map[string]storedSize {
	t.Helper()
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	collection := schema.Collection{Slug: "media", Upload: &schema.UploadSettings{MaxFileSize: 1 << 20, MimeTypes: mimeTypes, ImageSizes: sizes}}
	prepared, err := (uploads.Manager{Backend: backend, Locker: teststore.New(), Namespace: "test"}).Prepare(context.Background(), collection, uploads.Input{Filename: "photo", Reader: bytes.NewReader(encoded)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { prepared.Release() })
	stored, _ := prepared.Values["sizes"].CopyObject()
	result := map[string]storedSize{}
	for _, size := range sizes {
		metadata, _ := stored[size.Name].CopyObject()
		width, _ := metadata["width"].NumberValue()
		height, _ := metadata["height"].NumberValue()
		filesize, _ := metadata["filesize"].NumberValue()
		mimeType, _ := metadata["mimeType"].StringValue()
		key, _ := metadata["objectKey"].StringValue()
		reader, _, err := backend.Open(context.Background(), key)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		decoded, format, err := image.Decode(bytes.NewReader(body))
		if err != nil {
			t.Fatalf("%s: %v", size.Name, err)
		}
		_, _, _, alpha := decoded.At(decoded.Bounds().Min.X, decoded.Bounds().Min.Y).RGBA()
		result[size.Name] = storedSize{
			width: int(width), height: int(height), mimeType: mimeType, key: key,
			decoded: decoded.Bounds(), decodedFormat: format, cornerOpaque: alpha == 0xffff, storedFilesize: int(filesize),
		}
	}
	return result
}

func TestImageSizeFits(t *testing.T) {
	photo := image.NewRGBA(image.Rect(0, 0, 400, 200))
	for y := range 200 {
		for x := range 400 {
			photo.Set(x, y, color.RGBA{R: uint8(x / 2), G: uint8(y), B: 120, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, photo, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	sizes := prepareSizes(t, encoded.Bytes(), []string{"image/jpeg"},
		schema.ImageSize{Name: "inside", Width: 100, Height: 100, Fit: "inside"},
		schema.ImageSize{Name: "inside-wide", Width: 300, Height: 50, Fit: "inside"},
		schema.ImageSize{Name: "inside-larger", Width: 1000, Height: 1000, Fit: "inside"},
		schema.ImageSize{Name: "contain", Width: 100, Height: 100, Fit: "contain"},
		schema.ImageSize{Name: "cover", Width: 100, Height: 100, Fit: "cover"},
	)
	for name, want := range map[string][2]int{
		// inside keeps the 2:1 shape within the box and never enlarges.
		"inside": {100, 50}, "inside-wide": {100, 50}, "inside-larger": {400, 200},
		// contain and cover fill the box.
		"contain": {100, 100}, "cover": {100, 100},
	} {
		size := sizes[name]
		if size.width != want[0] || size.height != want[1] || size.decoded.Dx() != want[0] || size.decoded.Dy() != want[1] {
			t.Errorf("%s stored %d×%d, decoded %v; want %d×%d", name, size.width, size.height, size.decoded, want[0], want[1])
		}
		if size.mimeType != "image/jpeg" || size.decodedFormat != "jpeg" || !strings.HasSuffix(size.key, ".jpg") {
			t.Errorf("%s is %s (%s, %s), want JPEG", name, size.mimeType, size.decodedFormat, size.key)
		}
	}
}

func TestWebPUploadsMakeImageSizes(t *testing.T) {
	read := func(name string) []byte {
		t.Helper()
		encoded, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return encoded
	}
	webp := []string{"image/webp"}
	opaque := prepareSizes(t, read("opaque.webp"), webp,
		schema.ImageSize{Name: "inside", Width: 20, Height: 20, Fit: "inside"},
		schema.ImageSize{Name: "cover", Width: 10, Height: 10, Fit: "cover"},
		schema.ImageSize{Name: "contain", Width: 20, Height: 20, Fit: "contain"},
	)
	// A lossy photo without transparency becomes JPEG; contain's padding needs PNG.
	for name, want := range map[string]string{"inside": "image/jpeg", "cover": "image/jpeg", "contain": "image/png"} {
		size := opaque[name]
		extension := map[string]string{"image/jpeg": ".jpg", "image/png": ".png"}[want]
		if size.mimeType != want || !strings.HasSuffix(size.key, extension) || "image/"+size.decodedFormat != want {
			t.Errorf("opaque %s is %s (%s, %s), want %s", name, size.mimeType, size.decodedFormat, size.key, want)
		}
	}
	if size := opaque["inside"]; size.width != 20 || size.height != 10 {
		t.Errorf("opaque inside = %d×%d, want 20×10", size.width, size.height)
	}
	transparent := prepareSizes(t, read("transparent.webp"), webp, schema.ImageSize{Name: "inside", Width: 20, Height: 20, Fit: "inside"})
	if size := transparent["inside"]; size.mimeType != "image/png" || size.decodedFormat != "png" || size.cornerOpaque {
		t.Errorf("transparent inside is %s (%s), corner opaque %v; want PNG keeping transparency", size.mimeType, size.decodedFormat, size.cornerOpaque)
	}
	// A lossless WebP is usually a screenshot or graphic, so it stays sharp as PNG.
	lossless := prepareSizes(t, read("lossless.webp"), webp, schema.ImageSize{Name: "inside", Width: 20, Height: 20, Fit: "inside"})
	if size := lossless["inside"]; size.mimeType != "image/png" || size.decodedFormat != "png" || !strings.HasSuffix(size.key, ".png") {
		t.Errorf("lossless inside is %s (%s, %s), want PNG", size.mimeType, size.decodedFormat, size.key)
	}
}
