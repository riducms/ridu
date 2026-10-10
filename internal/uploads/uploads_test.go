package uploads_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	localstorage "github.com/riducms/ridu/adapters/storage/local"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/internal/uploads"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/storage"
	"github.com/riducms/ridu/store"
)

type rollbackBlockingBackend struct {
	storage.Backend
	deleting chan struct{}
	release  chan struct{}
	once     sync.Once
}

func (backend *rollbackBlockingBackend) Delete(ctx context.Context, key string) error {
	backend.once.Do(func() { close(backend.deleting) })
	select {
	case <-backend.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return backend.Backend.Delete(ctx, key)
}

type observingUploadLocker struct {
	store.UploadObjectLocker
	requested chan []string
}

func (locker *observingUploadLocker) LockUploadObjects(ctx context.Context, keys []string) (func(), error) {
	requested := append([]string(nil), keys...)
	select {
	case locker.requested <- requested:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return locker.UploadObjectLocker.LockUploadObjects(ctx, keys)
}

type countingDeleteFailureBackend struct {
	storage.Backend
	mu      sync.Mutex
	deletes int
	failure error
}

func (backend *countingDeleteFailureBackend) Delete(context.Context, string) error {
	backend.mu.Lock()
	backend.deletes++
	backend.mu.Unlock()
	return backend.failure
}

func (backend *countingDeleteFailureBackend) deleteCount() int {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	return backend.deletes
}

func TestWorkAdmissionBoundsConcurrencyAndHonorsCanceledWaiters(t *testing.T) {
	admission := uploads.NewWorkAdmission(10, 10, 2)
	releaseFirst, err := admission.AcquireFile(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	releaseSecond, err := admission.AcquireFile(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admission.AcquireFile(context.Background(), 1); !uploads.IsBusy(err) {
		t.Fatalf("full upload queue error = %v", err)
	}
	releaseFirst()
	releaseSecond()

	releaseImage, err := admission.AcquireImage(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := admission.AcquireImage(ctx, 1); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("canceled image waiter error = %v", err)
	}
	releaseImage()
}

func TestPrepareSniffsImageAndCreatesConfiguredSize(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := image.NewRGBA(image.Rect(0, 0, 4, 2))
	source.Set(0, 0, color.RGBA{R: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	collection := schema.Collection{Slug: "media", Upload: &schema.UploadSettings{MaxFileSize: 1024, MimeTypes: []string{"image/*"}, ImageSizes: []schema.ImageSize{{Name: "thumb", Width: 2, Height: 2, Fit: "cover"}}}}
	prepared, err := (uploads.Manager{Backend: backend, Locker: teststore.New(), Namespace: "test"}).Prepare(context.Background(), collection, uploads.Input{Filename: "../../unsafe name.png", Reader: bytes.NewReader(encoded.Bytes())})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Release()
	if len(prepared.Keys) != 3 {
		t.Fatalf("keys = %v", prepared.Keys)
	}
	if filename, _ := prepared.Values["filename"].StringValue(); filename != "unsafe-name.png" {
		t.Fatalf("filename = %q", filename)
	}
	if focalX, _ := prepared.Values["focalX"].NumberValue(); focalX != 50 {
		t.Fatalf("default focalX = %v", focalX)
	}
}

func TestRollbackHoldsGeneratedObjectLockUntilStagedDeletionFinishes(t *testing.T) {
	localBackend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	backend := &rollbackBlockingBackend{Backend: localBackend, deleting: make(chan struct{}), release: make(chan struct{})}
	defer func() {
		select {
		case <-backend.release:
		default:
			close(backend.release)
		}
	}()
	locker := &observingUploadLocker{UploadObjectLocker: teststore.New(), requested: make(chan []string, 2)}
	manager := uploads.Manager{Backend: backend, Locker: locker, Namespace: "test"}
	collection := schema.Collection{Slug: "media", Upload: &schema.UploadSettings{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}}}
	prepared, err := manager.Prepare(context.Background(), collection, uploads.Input{Filename: "staged.txt", Reader: bytes.NewBufferString("staged")})
	if err != nil {
		t.Fatal(err)
	}
	<-locker.requested

	rollbackDone := make(chan error, 1)
	go func() { rollbackDone <- manager.Rollback(context.Background(), prepared) }()
	select {
	case <-backend.deleting:
	case <-time.After(time.Second):
		t.Fatal("rollback did not begin deleting the staged object")
	}

	type lockResult struct {
		release func()
		err     error
	}
	acquired := make(chan lockResult, 1)
	go func() {
		release, lockError := locker.LockUploadObjects(context.Background(), prepared.Keys)
		acquired <- lockResult{release: release, err: lockError}
	}()
	select {
	case <-locker.requested:
	case <-time.After(time.Second):
		t.Fatal("competing cleanup did not request the generated-object lock")
	}
	select {
	case result := <-acquired:
		if result.release != nil {
			result.release()
		}
		t.Fatalf("generated-object lock released before rollback deletion finished: %v", result.err)
	default:
	}

	close(backend.release)
	if err := <-rollbackDone; err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-acquired:
		if result.err != nil {
			t.Fatal(result.err)
		}
		result.release()
	case <-time.After(time.Second):
		t.Fatal("competing cleanup did not acquire the lock after rollback")
	}
	if _, _, err := localBackend.Open(context.Background(), prepared.Keys[0]); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("rolled-back object remained in storage: %v", err)
	}
}

func TestPreparedReleaseRetainsObjectsAndMakesLaterRollbackANoop(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager := uploads.Manager{Backend: backend, Locker: teststore.New(), Namespace: "test"}
	collection := schema.Collection{Slug: "media", Upload: &schema.UploadSettings{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}}}
	prepared, err := manager.Prepare(context.Background(), collection, uploads.Input{Filename: "retained.txt", Reader: bytes.NewBufferString("retained")})
	if err != nil {
		t.Fatal(err)
	}
	prepared.Release()
	if err := manager.Rollback(context.Background(), prepared); err != nil {
		t.Fatalf("rollback after retained outcome = %v", err)
	}
	reader, _, err := backend.Open(context.Background(), prepared.Keys[0])
	if err != nil {
		t.Fatalf("retained prepared object was deleted: %v", err)
	}
	reader.Close()
}

func TestPreparedRollbackIsIdempotentAndReturnsStoredFailure(t *testing.T) {
	localBackend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	deleteFailure := errors.New("staged deletion failed")
	backend := &countingDeleteFailureBackend{Backend: localBackend, failure: deleteFailure}
	manager := uploads.Manager{Backend: backend, Locker: teststore.New(), Namespace: "test"}
	collection := schema.Collection{Slug: "media", Upload: &schema.UploadSettings{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}}}
	prepared, err := manager.Prepare(context.Background(), collection, uploads.Input{Filename: "failed.txt", Reader: bytes.NewBufferString("failed")})
	if err != nil {
		t.Fatal(err)
	}
	first := manager.Rollback(context.Background(), prepared)
	second := manager.Rollback(context.Background(), prepared)
	if !errors.Is(first, deleteFailure) || !errors.Is(second, deleteFailure) {
		t.Fatalf("stored rollback failures = first %v, second %v", first, second)
	}
	if backend.deleteCount() != 1 {
		t.Fatalf("idempotent rollback delete count = %d", backend.deleteCount())
	}
}

func TestPrepareRejectsEncodedImageDimensionsBeforeAllocation(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// A GIF logical screen can advertise billions of pixels in only a few bytes.
	oversized := []byte{'G', 'I', 'F', '8', '9', 'a', 0xff, 0xff, 0xff, 0xff, 0, 0, 0}
	collection := schema.Collection{Slug: "media", Upload: &schema.UploadSettings{MaxFileSize: 1024, MimeTypes: []string{"image/*"}}}
	if _, err := (uploads.Manager{Backend: backend, Locker: teststore.New(), Namespace: "test"}).Prepare(context.Background(), collection, uploads.Input{Filename: "bomb.gif", Reader: bytes.NewReader(oversized)}); err == nil {
		t.Fatal("oversized decoded image dimensions succeeded")
	}
	page, err := backend.List(context.Background(), storage.ListRequest{Limit: storage.MaxListPageSize})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Objects) != 0 {
		t.Fatalf("rejected image stored objects: %#v", page.Objects)
	}
}

func TestDuplicateCopiesOriginalAndDerivedSizes(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := image.NewRGBA(image.Rect(0, 0, 4, 2))
	source.Set(0, 0, color.RGBA{R: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	collection := schema.Collection{Slug: "media", Upload: &schema.UploadSettings{MaxFileSize: 1024, MimeTypes: []string{"image/*"}, ImageSizes: []schema.ImageSize{{Name: "thumb", Width: 2, Height: 2, Fit: "cover"}}}}
	manager := uploads.Manager{Backend: backend, Locker: teststore.New(), Namespace: "test"}
	prepared, err := manager.Prepare(context.Background(), collection, uploads.Input{Filename: "thumb.jpg", Reader: bytes.NewReader(encoded.Bytes())})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Release()
	duplicate, err := manager.Duplicate(context.Background(), collection, prepared.Values)
	if err != nil {
		t.Fatal(err)
	}
	defer duplicate.Release()
	if len(duplicate.Keys) != 3 {
		t.Fatalf("duplicated keys = %v", duplicate.Keys)
	}
	originalKey, _ := prepared.Values["objectKey"].StringValue()
	duplicateKey, _ := duplicate.Values["objectKey"].StringValue()
	if originalKey == duplicateKey {
		t.Fatalf("original key was reused: %q", originalKey)
	}
	originalSizes, _ := prepared.Values["sizes"].CopyObject()
	duplicateSizes, _ := duplicate.Values["sizes"].CopyObject()
	originalThumb, _ := originalSizes["thumb"].CopyObject()
	duplicateThumb, _ := duplicateSizes["thumb"].CopyObject()
	originalThumbKey, _ := originalThumb["objectKey"].StringValue()
	duplicateThumbKey, _ := duplicateThumb["objectKey"].StringValue()
	if originalKey == originalThumbKey {
		t.Fatalf("derived size overwrote same-named original: %q", originalKey)
	}
	if originalThumbKey == duplicateThumbKey {
		t.Fatalf("derived size key was reused: %q", originalThumbKey)
	}
	for _, key := range prepared.Keys {
		if err := backend.Delete(context.Background(), key); err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range duplicate.Keys {
		reader, _, err := backend.Open(context.Background(), key)
		if err != nil {
			t.Fatalf("duplicated object %q disappeared with source: %v", key, err)
		}
		reader.Close()
	}
}

func TestRegenerateImageUsesFocalPointForCoverCrop(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := image.NewRGBA(image.Rect(0, 0, 8, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 8; x++ {
			if x < 4 {
				source.Set(x, y, color.RGBA{R: 255, A: 255})
			} else {
				source.Set(x, y, color.RGBA{B: 255, A: 255})
			}
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	if err := backend.Put(context.Background(), "ridu/test/objects/0123456789abcdef0123456789abcdef/original.png", bytes.NewReader(encoded.Bytes()), int64(encoded.Len()), "image/png"); err != nil {
		t.Fatal(err)
	}
	collection := schema.Collection{Slug: "media", Upload: &schema.UploadSettings{MaxFileSize: 4096, MimeTypes: []string{"image/png"}, ImageSizes: []schema.ImageSize{{Name: "square", Width: 4, Height: 4, Fit: "cover"}}}}
	manager := uploads.Manager{Backend: backend, Locker: teststore.New(), Namespace: "test"}
	left, err := manager.RegenerateImage(context.Background(), collection, uploads.ImageInput{ObjectKey: "ridu/test/objects/0123456789abcdef0123456789abcdef/original.png", FocalX: 0, FocalY: 50})
	if err != nil {
		t.Fatal(err)
	}
	defer left.Release()
	right, err := manager.RegenerateImage(context.Background(), collection, uploads.ImageInput{ObjectKey: "ridu/test/objects/0123456789abcdef0123456789abcdef/original.png", FocalX: 100, FocalY: 50})
	if err != nil {
		t.Fatal(err)
	}
	defer right.Release()
	leftPixel := variantPixel(t, backend, left, "square")
	rightPixel := variantPixel(t, backend, right, "square")
	lr, _, lb, _ := leftPixel.RGBA()
	rr, _, rb, _ := rightPixel.RGBA()
	if lr <= lb || rb <= rr {
		t.Fatalf("focal crops were not biased left/right: left=%v right=%v", leftPixel, rightPixel)
	}
}

func TestRegenerateImageAppliesAndValidatesFreeformCrop(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := image.NewRGBA(image.Rect(0, 0, 8, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 8; x++ {
			colour := color.RGBA{R: 255, A: 255}
			if x >= 4 {
				colour = color.RGBA{B: 255, A: 255}
			}
			source.Set(x, y, colour)
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	if err := backend.Put(context.Background(), "ridu/test/objects/0123456789abcdef0123456789abcdef/original.png", bytes.NewReader(encoded.Bytes()), int64(encoded.Len()), "image/png"); err != nil {
		t.Fatal(err)
	}
	collection := schema.Collection{Slug: "media", Upload: &schema.UploadSettings{MaxFileSize: 4096, MimeTypes: []string{"image/png"}, ImageSizes: []schema.ImageSize{{Name: "square", Width: 4, Height: 4, Fit: "cover"}}}}
	manager := uploads.Manager{Backend: backend, Locker: teststore.New(), Namespace: "test"}
	prepared, err := manager.RegenerateImage(context.Background(), collection, uploads.ImageInput{ObjectKey: "ridu/test/objects/0123456789abcdef0123456789abcdef/original.png", FocalX: 50, FocalY: 50, CropX: 50, CropY: 0, CropWidth: 50, CropHeight: 100})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Release()
	pixel := variantPixel(t, backend, prepared, "square")
	red, _, blue, _ := pixel.RGBA()
	if blue <= red {
		t.Fatalf("cropped variant did not use selected blue half: %v", pixel)
	}
	if _, err := manager.RegenerateImage(context.Background(), collection, uploads.ImageInput{ObjectKey: "ridu/test/objects/0123456789abcdef0123456789abcdef/original.png", FocalX: 50, FocalY: 50, CropX: 75, CropWidth: 50, CropHeight: 100}); err == nil {
		t.Fatal("out-of-bounds crop succeeded")
	}
}

func TestRegenerateImageKeepsFocalCoordinatesRelativeToOriginalDuringCrop(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := image.NewRGBA(image.Rect(0, 0, 12, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 12; x++ {
			value := uint8(x * 20)
			source.Set(x, y, color.RGBA{R: value, G: value, B: value, A: 255})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	if err := backend.Put(context.Background(), "ridu/test/objects/0123456789abcdef0123456789abcdef/original.png", bytes.NewReader(encoded.Bytes()), int64(encoded.Len()), "image/png"); err != nil {
		t.Fatal(err)
	}
	collection := schema.Collection{Slug: "media", Upload: &schema.UploadSettings{MaxFileSize: 4096, MimeTypes: []string{"image/png"}, ImageSizes: []schema.ImageSize{{Name: "square", Width: 4, Height: 4, Fit: "cover"}}}}
	prepared, err := (uploads.Manager{Backend: backend, Locker: teststore.New(), Namespace: "test"}).RegenerateImage(context.Background(), collection, uploads.ImageInput{
		ObjectKey: "ridu/test/objects/0123456789abcdef0123456789abcdef/original.png", FocalX: 75, FocalY: 50,
		CropX: 25, CropY: 0, CropWidth: 50, CropHeight: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Release()
	pixel := variantPixel(t, backend, prepared, "square")
	red, _, _, _ := pixel.RGBA()
	if red>>8 < 130 {
		t.Fatalf("focal point was not translated from original to crop coordinates: %v", pixel)
	}
}

func TestUploadObjectOwnershipRequiresExactNamespace(t *testing.T) {
	manager := uploads.Manager{Namespace: "production-app"}
	for _, key := range []string{
		"media/0123456789abcdef0123456789abcdef/original.png",
		"ridu/production-app/media/0123456789abcdef0123456789abcdef/original.png",
		"ridu/production-app/objects/not-a-nonce/original.png",
		"ridu/other-app/objects/0123456789abcdef0123456789abcdef/original.png",
		"ridu/production-app/objects/0123456789abcdef0123456789abcdef/../../outside.png",
	} {
		if manager.OwnsKey(key) {
			t.Fatalf("manager accepted unowned key %q", key)
		}
	}
	if !manager.OwnsKey("ridu/production-app/objects/0123456789abcdef0123456789abcdef/original.png") {
		t.Fatal("manager rejected its namespaced object")
	}
}

func TestNewUploadKeysDoNotEncodeMutableCollectionSlug(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	collection := schema.Collection{ID: "media", Slug: "media", Upload: &schema.UploadSettings{MaxFileSize: 1024, MimeTypes: []string{"text/plain"}}}
	prepared, err := (uploads.Manager{Backend: backend, Locker: teststore.New(), Namespace: "production-app"}).Prepare(
		context.Background(), collection, uploads.Input{Filename: "note.txt", Reader: bytes.NewBufferString("safe")},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Release()
	key, _ := prepared.Values["objectKey"].StringValue()
	if !strings.HasPrefix(key, "ridu/production-app/objects/") || strings.Contains(key, "/media/") {
		t.Fatalf("new upload key = %q, want slug-independent application ownership", key)
	}
}

func variantPixel(t *testing.T, backend *localstorage.Backend, prepared uploads.Prepared, name string) color.Color {
	t.Helper()
	sizes, _ := prepared.Values["sizes"].CopyObject()
	metadata, _ := sizes[name].CopyObject()
	key, _ := metadata["objectKey"].StringValue()
	reader, _, err := backend.Open(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	encoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	decoded, _, err := image.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	return decoded.At(2, 2)
}

func TestPrepareOrientsEXIFBeforeApplyingSourceCrop(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 8, 4)), nil); err != nil {
		t.Fatal(err)
	}
	// Little-endian TIFF orientation 6: the 8x4 encoded image displays as 4x8.
	exif := []byte{'E', 'x', 'i', 'f', 0, 0, 'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 1, 3, 0, 1, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0}
	original := append([]byte{0xff, 0xd8, 0xff, 0xe1, 0, byte(len(exif) + 2)}, exif...)
	original = append(original, encoded.Bytes()[2:]...)
	collection := schema.Collection{Slug: "media", Upload: &schema.UploadSettings{MaxFileSize: 4096, MimeTypes: []string{"image/jpeg"}}}
	prepared, err := (uploads.Manager{Backend: backend, Locker: teststore.New(), Namespace: "orientation"}).Prepare(t.Context(), collection, uploads.Input{
		Filename: "phone.jpg", Reader: bytes.NewReader(original),
		Image: &protocol.UploadImageEdit{FocalX: 50, FocalY: 50, CropWidth: 50, CropHeight: 100},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Release()
	width, _ := prepared.Values["width"].NumberValue()
	height, _ := prepared.Values["height"].NumberValue()
	if width != 2 || height != 8 {
		t.Fatalf("oriented crop = %vx%v, want 2x8", width, height)
	}
	key, _ := prepared.Values["source"].Get("objectKey").StringValue()
	reader, _, err := backend.Open(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	retained, err := io.ReadAll(reader)
	if err != nil || !bytes.Equal(retained, original) {
		t.Fatal("private EXIF original was altered")
	}
}

func encodedImage(t *testing.T, picture image.Image, format string) []byte {
	t.Helper()
	var encoded bytes.Buffer
	var err error
	switch format {
	case "jpeg":
		err = jpeg.Encode(&encoded, picture, nil)
	case "png":
		err = png.Encode(&encoded, picture)
	case "gif":
		err = gif.Encode(&encoded, picture, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func storedObject(t *testing.T, backend *localstorage.Backend, value store.Value) []byte {
	t.Helper()
	key, _ := value.StringValue()
	reader, _, err := backend.Open(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	encoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func decodedSize(t *testing.T, encoded []byte) image.Point {
	t.Helper()
	configuration, _, err := image.DecodeConfig(bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	return image.Pt(configuration.Width, configuration.Height)
}

func TestMaxImageDimensionScalesTheStoredOriginal(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager := uploads.Manager{Backend: backend, Locker: teststore.New(), Namespace: "capped"}
	settings := &schema.UploadSettings{MaxFileSize: 1 << 20, MimeTypes: []string{"image/*"}, MaxImageDimension: 300, ImageSizes: []schema.ImageSize{{Name: "thumb", Width: 200, Height: 200, Fit: "cover"}}}
	collection := schema.Collection{Slug: "media", Upload: settings}
	for _, test := range []struct {
		name, format string
		picture      image.Image
		want         image.Point
	}{
		{name: "JPEG", format: "jpeg", picture: image.NewRGBA(image.Rect(0, 0, 1200, 800)), want: image.Pt(300, 200)},
		{name: "PNG", format: "png", picture: image.NewNRGBA(image.Rect(0, 0, 500, 1500)), want: image.Pt(100, 300)},
	} {
		t.Run(test.name, func(t *testing.T) {
			prepared, err := manager.Prepare(t.Context(), collection, uploads.Input{Filename: "large." + test.format, Reader: bytes.NewReader(encodedImage(t, test.picture, test.format))})
			if err != nil {
				t.Fatal(err)
			}
			defer prepared.Release()
			width, _ := prepared.Values["width"].NumberValue()
			height, _ := prepared.Values["height"].NumberValue()
			primary := storedObject(t, backend, prepared.Values["objectKey"])
			if int(width) != test.want.X || int(height) != test.want.Y || decodedSize(t, primary) != test.want {
				t.Fatalf("stored %v as %vx%v, want %v", decodedSize(t, primary), width, height, test.want)
			}
			if filesize, _ := prepared.Values["filesize"].NumberValue(); int(filesize) != len(primary) {
				t.Fatalf("filesize = %v, stored %d bytes", filesize, len(primary))
			}
			// The scaled image replaces the original as the private source too.
			source, _ := prepared.Values["source"].CopyObject()
			sourceWidth, _ := source["width"].NumberValue()
			if !bytes.Equal(storedObject(t, backend, source["objectKey"]), primary) || int(sourceWidth) != test.want.X {
				t.Fatalf("private source %v is not the scaled original", source)
			}
			if _, exists := prepared.Values["sizes"].Get("thumb").CopyObject(); !exists {
				t.Fatal("image size was not made from the scaled original")
			}
		})
	}
}

func TestMaxImageDimensionScalesTheUprightPicture(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	encoded := encodedImage(t, image.NewRGBA(image.Rect(0, 0, 1200, 600)), "jpeg")
	// Little-endian TIFF orientation 6: the 1200x600 encoded image displays as 600x1200.
	exif := []byte{'E', 'x', 'i', 'f', 0, 0, 'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 1, 3, 0, 1, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0}
	original := append([]byte{0xff, 0xd8, 0xff, 0xe1, 0, byte(len(exif) + 2)}, exif...)
	original = append(original, encoded[2:]...)
	collection := schema.Collection{Slug: "media", Upload: &schema.UploadSettings{MaxFileSize: 1 << 20, MimeTypes: []string{"image/jpeg"}, MaxImageDimension: 300}}
	prepared, err := (uploads.Manager{Backend: backend, Locker: teststore.New(), Namespace: "capped"}).Prepare(t.Context(), collection, uploads.Input{Filename: "phone.jpg", Reader: bytes.NewReader(original)})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Release()
	if size := decodedSize(t, storedObject(t, backend, prepared.Values["objectKey"])); size != image.Pt(150, 300) {
		t.Fatalf("scaled phone photo = %v, want upright 150x300", size)
	}
}

func TestMaxImageDimensionKeepsSmallerAndGIFOriginals(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manager := uploads.Manager{Backend: backend, Locker: teststore.New(), Namespace: "capped"}
	collection := schema.Collection{Slug: "media", Upload: &schema.UploadSettings{MaxFileSize: 1 << 20, MimeTypes: []string{"image/*"}, MaxImageDimension: 300}}
	for name, original := range map[string][]byte{
		"smaller JPEG": encodedImage(t, image.NewRGBA(image.Rect(0, 0, 300, 120)), "jpeg"),
		// A GIF may be animated, and Ridu decodes only its first frame.
		"larger GIF": encodedImage(t, image.NewPaletted(image.Rect(0, 0, 600, 400), color.Palette{color.Black, color.White}), "gif"),
	} {
		prepared, err := manager.Prepare(t.Context(), collection, uploads.Input{Filename: "kept", Reader: bytes.NewReader(original)})
		if err != nil {
			t.Fatal(err)
		}
		defer prepared.Release()
		if !bytes.Equal(storedObject(t, backend, prepared.Values["objectKey"]), original) || !bytes.Equal(storedObject(t, backend, prepared.Values["source"].Get("objectKey")), original) {
			t.Fatalf("%s: original was not kept as uploaded", name)
		}
	}
}

func TestRegeneratingAnOlderSourceAppliesTheCap(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	original := encodedImage(t, image.NewRGBA(image.Rect(0, 0, 1000, 500)), "png")
	sourceKey := "ridu/capped/objects/0123456789abcdef0123456789abcdef/source/older.png"
	if err := backend.Put(t.Context(), sourceKey, bytes.NewReader(original), int64(len(original)), "image/png"); err != nil {
		t.Fatal(err)
	}
	collection := schema.Collection{Slug: "media", Upload: &schema.UploadSettings{MaxFileSize: 1 << 20, MimeTypes: []string{"image/png"}, MaxImageDimension: 400}}
	manager := uploads.Manager{Backend: backend, Locker: teststore.New(), Namespace: "capped"}
	source := store.Values{"objectKey": store.String(sourceKey), "width": store.Number(1000), "height": store.Number(500), "mimeType": store.String("image/png"), "filesize": store.Number(float64(len(original)))}
	for _, test := range []struct {
		name      string
		cropWidth float64
		want      image.Point
	}{
		{name: "uncropped", want: image.Pt(400, 200)},
		{name: "square crop", cropWidth: 50, want: image.Pt(400, 400)},
	} {
		cropHeight := 0.0
		if test.cropWidth > 0 {
			cropHeight = 100
		}
		prepared, err := manager.RegenerateImage(t.Context(), collection, uploads.ImageInput{ObjectKey: sourceKey, Source: source, FocalX: 50, FocalY: 50, CropWidth: test.cropWidth, CropHeight: cropHeight})
		if err != nil {
			t.Fatal(err)
		}
		defer prepared.Release()
		if size := decodedSize(t, storedObject(t, backend, prepared.Values["objectKey"])); size != test.want {
			t.Fatalf("%s: primary = %v, want %v", test.name, size, test.want)
		}
		// The older private source is kept, so a later edit can still reach all of it.
		if key, _ := prepared.Values["source"].Get("objectKey").StringValue(); key != sourceKey {
			t.Fatalf("%s: private source moved to %q", test.name, key)
		}
	}
}

func TestCoverSizeFromAJPEGFollowsTheFocalPoint(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	picture := image.NewRGBA(image.Rect(0, 0, 400, 200))
	for y := range 200 {
		for x := range 400 {
			if x < 200 {
				picture.Set(x, y, color.RGBA{R: 255, A: 255})
			} else {
				picture.Set(x, y, color.RGBA{B: 255, A: 255})
			}
		}
	}
	original := encodedImage(t, picture, "jpeg")
	collection := schema.Collection{Slug: "media", Upload: &schema.UploadSettings{MaxFileSize: 1 << 20, MimeTypes: []string{"image/jpeg"}, ImageSizes: []schema.ImageSize{{Name: "square", Width: 50, Height: 50, Fit: "cover"}}}}
	manager := uploads.Manager{Backend: backend, Locker: teststore.New(), Namespace: "focal"}
	for focalX, red := range map[float64]bool{0: true, 100: false} {
		prepared, err := manager.Prepare(t.Context(), collection, uploads.Input{Filename: "halves.jpg", Reader: bytes.NewReader(original), Image: &protocol.UploadImageEdit{FocalX: focalX, FocalY: 50}})
		if err != nil {
			t.Fatal(err)
		}
		defer prepared.Release()
		r, _, b, _ := variantPixel(t, backend, prepared, "square").RGBA()
		if (r > b) != red {
			t.Fatalf("focal x %v: cover size pixel r=%d b=%d", focalX, r, b)
		}
	}
}

func TestPrepareRefusesAnImageOverTheBudgetWithWhatFits(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	collection := schema.Collection{Slug: "media", Upload: &schema.UploadSettings{MaxFileSize: 1 << 20, MimeTypes: []string{"image/*"}}}
	manager := uploads.Manager{Backend: backend, Locker: teststore.New(), Namespace: "budget", Admission: uploads.NewWorkAdmission(1<<30, 20<<20, 4)}
	_, err = manager.Prepare(t.Context(), collection, uploads.Input{Filename: "large.jpg", Reader: bytes.NewReader(encodedImage(t, image.NewRGBA(image.Rect(0, 0, 3000, 2000)), "jpeg"))})
	if err == nil || !strings.HasPrefix(err.Error(), "image is 3000×2000 (6.0 megapixels), more than this server can process at once; upload an image of at most about ") {
		t.Fatalf("over-budget image error = %v", err)
	}
	_, err = manager.Prepare(t.Context(), collection, uploads.Input{Filename: "bomb.gif", Reader: bytes.NewReader([]byte{'G', 'I', 'F', '8', '9', 'a', 0x50, 0xc3, 0x10, 0x00, 0, 0, 0})})
	if err == nil || err.Error() != "image is 50000×16 (0.8 megapixels); images can be at most 20000 pixels wide or high and 40 megapixels" {
		t.Fatalf("oversized image error = %v", err)
	}
}
