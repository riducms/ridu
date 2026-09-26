package uploads

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"mime"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/disintegration/imaging"
	resample "golang.org/x/image/draw"

	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/storage"
	"github.com/riducms/ridu/store"
)

var unsafeFilename = regexp.MustCompile("[^a-zA-Z0-9._-]+")
var storageNamespace = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{2,63}$`)

var managedMetadataFields = map[string]struct{}{
	"filename": {}, "mimeType": {}, "filesize": {}, "url": {}, "objectKey": {},
	"width": {}, "height": {}, "sizes": {}, "source": {}, "focalX": {}, "focalY": {},
	"cropX": {}, "cropY": {}, "cropWidth": {}, "cropHeight": {},
}

const (
	maximumImageDimension = 20_000
	maximumImagePixels    = 40_000_000
	maximumVariantPixels  = 100_000_000
	maximumImageVariants  = 64
	cleanupTimeout        = 10 * time.Second
	objectNamespace       = "objects"
)

type Manager struct {
	Backend   storage.Backend
	Locker    store.UploadObjectLocker
	Namespace string
	Admission *WorkAdmission
}

type Input struct {
	Filename          string
	Reader            io.Reader
	Values            store.Values
	FileAdmissionHeld bool
	Image             *protocol.UploadImageEdit
	// Source reuses an immutable source already owned by the current document.
	Source store.Values
}

type Prepared struct {
	Values  store.Values
	Keys    []string
	outcome *preparedOutcome
}

type preparedOutcome struct {
	release func()
	once    sync.Once
	err     error
}

// Release relinquishes the generated-object reservation after the owning
// document operation has committed. Rollback releases the same reservation
// after every staged object has been removed, so calling Release more than
// once is safe.
func (prepared Prepared) Release() {
	_ = prepared.finish(nil)
}

func (prepared Prepared) finish(action func() error) error {
	if prepared.outcome == nil {
		if action == nil {
			return nil
		}
		return action()
	}
	prepared.outcome.once.Do(func() {
		defer prepared.outcome.release()
		if action != nil {
			prepared.outcome.err = action()
		}
	})
	return prepared.outcome.err
}

type StorageError struct{ Cause error }

func (failure *StorageError) Error() string { return failure.Cause.Error() }
func (failure *StorageError) Unwrap() error { return failure.Cause }

func storageError(err error) error {
	if err == nil {
		return nil
	}
	return &StorageError{Cause: err}
}

func IsStorageError(err error) bool {
	var failure *StorageError
	return errors.As(err, &failure)
}

// ImageInput identifies an existing original image and the focal point used
// when regenerating configured cover variants.
type ImageInput struct {
	Filename   string
	Source     store.Values
	ObjectKey  string
	FocalX     float64
	FocalY     float64
	CropX      float64
	CropY      float64
	CropWidth  float64
	CropHeight float64
}

// Duplicate copies every stored object owned by an upload document into a
// fresh object-key namespace. The returned values replace only storage-owned
// metadata; callers remain responsible for duplicating the document itself.
func (manager Manager) Duplicate(ctx context.Context, collection schema.Collection, source store.Values) (Prepared, error) {
	if err := manager.ValidateObjectRoles(source); err != nil {
		return Prepared{}, err
	}
	if collection.Upload == nil {
		return Prepared{}, fmt.Errorf("collection %q is not upload-enabled", collection.Slug)
	}
	if manager.Backend == nil {
		return Prepared{}, fmt.Errorf("upload storage is not configured")
	}
	sourceKey, valid := source["objectKey"].StringValue()
	if !valid || sourceKey == "" {
		return Prepared{}, fmt.Errorf("upload document has no original object key")
	}
	if !manager.OwnsKey(sourceKey) {
		return Prepared{}, fmt.Errorf("upload document original object key is outside the application namespace")
	}
	prefix, err := randomPrefix(manager.Namespace, collection.Slug)
	if err != nil {
		return Prepared{}, err
	}
	filename, _ := source["filename"].StringValue()
	filename = filepath.Base(strings.TrimSpace(filename))
	if filename == "." || filename == "" {
		filename = filepath.Base(sourceKey)
	}
	newKey := prefix + "/" + filename
	type sizeCopy struct {
		name      string
		sourceKey string
		targetKey string
		metadata  store.Values
	}
	var copies []sizeCopy
	targetKeys := []string{newKey}
	privateSource, hasSource := source["source"].CopyObject()
	var privateSourceKey, newSourceKey string
	if hasSource {
		privateSourceKey, _ = privateSource["objectKey"].StringValue()
		if !manager.OwnsKey(privateSourceKey) {
			return Prepared{}, fmt.Errorf("upload source object key is outside the application namespace")
		}
		newSourceKey = prefix + "/source/" + filepath.Base(privateSourceKey)
		targetKeys = append(targetKeys, newSourceKey)
	}
	sizes, hasSizes := source["sizes"].CopyObject()
	if hasSizes {
		names := make([]string, 0, len(sizes))
		for name := range sizes {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			metadata, valid := sizes[name].CopyObject()
			if !valid {
				continue
			}
			sizeSourceKey, valid := metadata["objectKey"].StringValue()
			if !valid || sizeSourceKey == "" {
				continue
			}
			if !manager.OwnsKey(sizeSourceKey) {
				return Prepared{}, fmt.Errorf("upload size %q object key is outside the application namespace", name)
			}
			sizeKey := prefix + "/sizes/" + filepath.Base(sizeSourceKey)
			targetKeys = append(targetKeys, sizeKey)
			copies = append(copies, sizeCopy{name: name, sourceKey: sizeSourceKey, targetKey: sizeKey, metadata: metadata})
		}
	}
	prepared, err := manager.lockPreparation(ctx, targetKeys)
	if err != nil {
		return Prepared{}, err
	}
	prepared.Values = store.Values{}
	rollback := func(cause error) (Prepared, error) {
		if cleanupError := manager.Rollback(ctx, prepared); cleanupError != nil {
			return Prepared{}, storageError(errors.Join(cause, fmt.Errorf("rollback duplicated upload: %w", cleanupError)))
		}
		return Prepared{}, storageError(cause)
	}
	if err := manager.copyObject(ctx, sourceKey, newKey); err != nil {
		return rollback(fmt.Errorf("copy original upload: %w", err))
	}
	prepared.Values["objectKey"] = store.String(newKey)
	prepared.Values["url"] = store.String(deliveryURL(collection.Slug, newKey))
	if hasSource {
		if err := manager.copyObject(ctx, privateSourceKey, newSourceKey); err != nil {
			return rollback(fmt.Errorf("copy upload source: %w", err))
		}
		privateSource["objectKey"] = store.String(newSourceKey)
		prepared.Values["source"] = store.Object(privateSource)
	}

	if !hasSizes {
		return prepared, nil
	}
	duplicatedSizes := store.CloneValues(sizes)
	for _, copy := range copies {
		if err := manager.copyObject(ctx, copy.sourceKey, copy.targetKey); err != nil {
			return rollback(fmt.Errorf("copy upload size %q: %w", copy.name, err))
		}
		duplicatedMetadata := store.CloneValues(copy.metadata)
		duplicatedMetadata["objectKey"] = store.String(copy.targetKey)
		duplicatedMetadata["url"] = store.String(deliveryURL(collection.Slug, copy.targetKey))
		duplicatedSizes[copy.name] = store.Object(duplicatedMetadata)
	}
	prepared.Values["sizes"] = store.Object(duplicatedSizes)
	return prepared, nil
}

func (manager Manager) copyObject(ctx context.Context, sourceKey, targetKey string) error {
	reader, object, err := manager.Backend.Open(ctx, sourceKey)
	if err != nil {
		return err
	}
	defer reader.Close()
	if err := manager.Backend.Put(ctx, targetKey, reader, object.Size, object.ContentType); err != nil {
		return err
	}
	return nil
}

func (manager Manager) Prepare(ctx context.Context, collection schema.Collection, input Input) (Prepared, error) {
	if collection.Upload == nil {
		return Prepared{}, fmt.Errorf("collection %q is not upload-enabled", collection.Slug)
	}
	if manager.Backend == nil {
		return Prepared{}, fmt.Errorf("upload storage is not configured")
	}
	limit := collection.Upload.MaxFileSize
	if limit < 1 || limit > maximumUploadBytes {
		return Prepared{}, fmt.Errorf("upload limit must be between 1 and %d bytes", maximumUploadBytes)
	}
	if !input.FileAdmissionHeld {
		release, admissionError := manager.workAdmission().AcquireFile(ctx, limit)
		if admissionError != nil {
			return Prepared{}, admissionError
		}
		defer release()
	}
	encoded, err := io.ReadAll(io.LimitReader(input.Reader, limit+1))
	if err != nil {
		return Prepared{}, fmt.Errorf("read upload: %w", err)
	}
	if int64(len(encoded)) > limit {
		return Prepared{}, fmt.Errorf("file exceeds %d-byte upload limit", limit)
	}
	contentType := http.DetectContentType(encoded)
	if mediaType, _, parseError := mime.ParseMediaType(contentType); parseError == nil {
		contentType = mediaType
	}
	if !MIMEAllowed(contentType, collection.Upload.MimeTypes) {
		return Prepared{}, fmt.Errorf("detected MIME type %q is not allowed", contentType)
	}
	var source image.Image
	if strings.HasPrefix(contentType, "image/") {
		configuration, _, decodeError := image.DecodeConfig(bytes.NewReader(encoded))
		if decodeError != nil {
			return Prepared{}, fmt.Errorf("decode image metadata: %w", decodeError)
		}
		if err := validateImageDimensions(configuration.Width, configuration.Height); err != nil {
			return Prepared{}, err
		}
		imageBytes, estimateError := imageWorkBytes(collection, configuration.Width, configuration.Height, int64(len(encoded)), true)
		if estimateError != nil {
			return Prepared{}, estimateError
		}
		release, admissionError := manager.workAdmission().AcquireImage(ctx, imageBytes)
		if admissionError != nil {
			return Prepared{}, admissionError
		}
		defer release()
		source, decodeError = imaging.Decode(bytes.NewReader(encoded), imaging.AutoOrientation(true))
		if decodeError != nil {
			return Prepared{}, fmt.Errorf("decode image: %w", decodeError)
		}
	}
	edit := protocol.UploadImageEdit{FocalX: 50, FocalY: 50}
	if input.Image != nil {
		if contentType != "image/jpeg" && contentType != "image/png" {
			return Prepared{}, fmt.Errorf("only JPEG and PNG uploads support image editing")
		}
		edit = *input.Image
		if err := validateImageEdit(edit); err != nil {
			return Prepared{}, err
		}
	}
	filename := Filename(input.Filename, contentType)
	prefix, err := randomPrefix(manager.Namespace, collection.Slug)
	if err != nil {
		return Prepared{}, storageError(err)
	}
	key := prefix + "/" + filename
	keys := []string{key}
	privateSource := store.CloneValues(input.Source)
	newSource := source != nil && len(privateSource) == 0
	if source != nil {
		keys = append(keys, imageSizeObjectKeys(collection, prefix, contentType)...)
		if newSource {
			sourceKey := prefix + "/source/" + filename
			keys = append(keys, sourceKey)
			privateSource = store.Values{
				"objectKey": store.String(sourceKey), "width": store.Number(float64(source.Bounds().Dx())),
				"height": store.Number(float64(source.Bounds().Dy())), "mimeType": store.String(contentType),
				"filesize": store.Number(float64(len(encoded))),
			}
		}
	}
	prepared, err := manager.lockPreparation(ctx, keys)
	if err != nil {
		return Prepared{}, err
	}
	rollback := func(cause error) (Prepared, error) {
		return Prepared{}, storageError(errors.Join(cause, manager.Rollback(ctx, prepared)))
	}
	sourceBytes := encoded
	focalX, focalY := edit.FocalX, edit.FocalY
	if edit.CropWidth > 0 && edit.CropHeight > 0 {
		edit.FocalX = max(edit.CropX, min(edit.CropX+edit.CropWidth, edit.FocalX))
		edit.FocalY = max(edit.CropY, min(edit.CropY+edit.CropHeight, edit.FocalY))
		source = cropImage(source, edit.CropX, edit.CropY, edit.CropWidth, edit.CropHeight)
		focalX = clampPercentage((edit.FocalX - edit.CropX) * 100 / edit.CropWidth)
		focalY = clampPercentage((edit.FocalY - edit.CropY) * 100 / edit.CropHeight)
		encoded, err = encodeImage(source, contentType)
		if err != nil {
			return rollback(err)
		}
	}
	if err := manager.Backend.Put(ctx, key, bytes.NewReader(encoded), int64(len(encoded)), contentType); err != nil {
		return rollback(fmt.Errorf("store upload: %w", err))
	}
	if newSource {
		sourceKey, _ := privateSource["objectKey"].StringValue()
		if err := manager.Backend.Put(ctx, sourceKey, bytes.NewReader(sourceBytes), int64(len(sourceBytes)), contentType); err != nil {
			return rollback(fmt.Errorf("store image source: %w", err))
		}
	}
	values := ApplicationValues(input.Values)
	values["filename"] = store.String(filename)
	values["mimeType"] = store.String(contentType)
	values["filesize"] = store.Number(float64(len(encoded)))
	values["objectKey"] = store.String(key)
	values["url"] = store.String(deliveryURL(collection.Slug, key))
	if source != nil {
		values["source"] = store.Object(privateSource)
		values["width"] = store.Number(float64(source.Bounds().Dx()))
		values["height"] = store.Number(float64(source.Bounds().Dy()))
		values["focalX"], values["focalY"] = store.Number(edit.FocalX), store.Number(edit.FocalY)
		values["cropX"], values["cropY"] = store.Number(edit.CropX), store.Number(edit.CropY)
		values["cropWidth"], values["cropHeight"] = store.Number(edit.CropWidth), store.Number(edit.CropHeight)
		sizes, err := manager.storeImageSizes(ctx, collection, source, prefix, contentType, focalX, focalY)
		if err != nil {
			return rollback(err)
		}
		values["sizes"] = store.Object(sizes)
	} else {
		// Replacement with a non-image must clear every previous image property.
		for _, name := range []string{"source", "width", "height", "sizes", "focalX", "focalY", "cropX", "cropY", "cropWidth", "cropHeight"} {
			values[name] = store.Null()
		}
	}
	prepared.Values = values
	return prepared, nil
}

// RegenerateImage renders a new primary image and variants from the immutable
// source. Only freshly staged objects belong to the returned rollback resource.
func (manager Manager) RegenerateImage(ctx context.Context, collection schema.Collection, input ImageInput) (Prepared, error) {
	if collection.Upload == nil || manager.Backend == nil {
		return Prepared{}, fmt.Errorf("upload storage is not configured")
	}
	if !manager.OwnsKey(input.ObjectKey) {
		return Prepared{}, fmt.Errorf("image source key is outside the application namespace")
	}
	limit := collection.Upload.MaxFileSize
	release, err := manager.workAdmission().AcquireFile(ctx, limit)
	if err != nil {
		return Prepared{}, err
	}
	defer release()
	reader, object, err := manager.Backend.Open(ctx, input.ObjectKey)
	if err != nil {
		return Prepared{}, storageError(fmt.Errorf("open image source: %w", err))
	}
	defer reader.Close()
	if object.Size > limit {
		return Prepared{}, fmt.Errorf("image source exceeds upload limit")
	}
	filename := input.Filename
	if filename == "" {
		filename = filepath.Base(input.ObjectKey)
	}
	return manager.Prepare(ctx, collection, Input{
		Filename: filename, Reader: reader, Source: input.Source, FileAdmissionHeld: true,
		Image: &protocol.UploadImageEdit{FocalX: input.FocalX, FocalY: input.FocalY, CropX: input.CropX, CropY: input.CropY, CropWidth: input.CropWidth, CropHeight: input.CropHeight},
	})
}

func validateImageEdit(input protocol.UploadImageEdit) error {
	if math.IsNaN(input.FocalX) || math.IsInf(input.FocalX, 0) || math.IsNaN(input.FocalY) || math.IsInf(input.FocalY, 0) || input.FocalX < 0 || input.FocalX > 100 || input.FocalY < 0 || input.FocalY > 100 {
		return fmt.Errorf("focal coordinates must be between 0 and 100")
	}
	return validateCrop(ImageInput{CropX: input.CropX, CropY: input.CropY, CropWidth: input.CropWidth, CropHeight: input.CropHeight})
}

func validateCrop(input ImageInput) error {
	values := []float64{input.CropX, input.CropY, input.CropWidth, input.CropHeight}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 100 {
			return fmt.Errorf("crop coordinates must be between 0 and 100")
		}
	}
	if input.CropWidth == 0 && input.CropHeight == 0 {
		if input.CropX != 0 || input.CropY != 0 {
			return fmt.Errorf("cleared crop must start at zero")
		}
		return nil
	}
	if input.CropWidth == 0 || input.CropHeight == 0 || input.CropX+input.CropWidth > 100 || input.CropY+input.CropHeight > 100 {
		return fmt.Errorf("crop rectangle must have positive dimensions and remain within the image")
	}
	return nil
}

func cropImage(source image.Image, x, y, width, height float64) image.Image {
	bounds := source.Bounds()
	// Round dimensions independently: a pixel value converted to a percentage in
	// the editor can land just below its original integer in floating point.
	cropWidth := max(1, min(bounds.Dx(), int(math.Round(float64(bounds.Dx())*width/100))))
	cropHeight := max(1, min(bounds.Dy(), int(math.Round(float64(bounds.Dy())*height/100))))
	left := bounds.Min.X + min(bounds.Dx()-cropWidth, int(math.Round(float64(bounds.Dx())*x/100)))
	top := bounds.Min.Y + min(bounds.Dy()-cropHeight, int(math.Round(float64(bounds.Dy())*y/100)))
	right, bottom := left+cropWidth, top+cropHeight
	target := image.NewRGBA(image.Rect(0, 0, right-left, bottom-top))
	draw.Draw(target, target.Bounds(), source, image.Pt(left, top), draw.Src)
	return target
}

func clampPercentage(value float64) float64 {
	return max(0, min(100, value))
}

func validateImageDimensions(width, height int) error {
	if width < 1 || height < 1 || width > maximumImageDimension || height > maximumImageDimension || int64(width)*int64(height) > maximumImagePixels {
		return fmt.Errorf("image dimensions exceed the %d-pixel processing limit", maximumImagePixels)
	}
	return nil
}

func imageWorkBytes(collection schema.Collection, width, height int, encodedBytes int64, crop bool) (int64, error) {
	if collection.Upload == nil {
		return 0, fmt.Errorf("collection is not upload-enabled")
	}
	sourcePixels := int64(width) * int64(height)
	var aggregatePixels, largestVariant int64
	if len(collection.Upload.ImageSizes) > maximumImageVariants {
		return 0, fmt.Errorf("image configuration exceeds the %d-variant processing limit", maximumImageVariants)
	}
	for _, configured := range collection.Upload.ImageSizes {
		if err := validateImageDimensions(configured.Width, configured.Height); err != nil {
			return 0, fmt.Errorf("image size %q: %w", configured.Name, err)
		}
		pixels := int64(configured.Width) * int64(configured.Height)
		aggregatePixels += pixels
		largestVariant = max(largestVariant, pixels)
	}
	if aggregatePixels > maximumVariantPixels {
		return 0, fmt.Errorf("image variants exceed the %d-pixel aggregate processing limit", maximumVariantPixels)
	}
	sourceBytesPerPixel := int64(16)
	if crop {
		sourceBytesPerPixel = 20
	}
	return sourcePixels*sourceBytesPerPixel + largestVariant*40 + encodedBytes*2, nil
}

func (manager Manager) storeImageSizes(ctx context.Context, collection schema.Collection, source image.Image, prefix, sourceType string, focalX, focalY float64) (store.Values, error) {
	if len(collection.Upload.ImageSizes) > maximumImageVariants {
		return nil, fmt.Errorf("image configuration exceeds the %d-variant processing limit", maximumImageVariants)
	}
	var aggregatePixels int64
	for _, configured := range collection.Upload.ImageSizes {
		if err := validateImageDimensions(configured.Width, configured.Height); err != nil {
			return nil, fmt.Errorf("image size %q: %w", configured.Name, err)
		}
		aggregatePixels += int64(configured.Width) * int64(configured.Height)
	}
	if aggregatePixels > maximumVariantPixels {
		return nil, fmt.Errorf("image variants exceed the %d-pixel aggregate processing limit", maximumVariantPixels)
	}
	sizes := make(store.Values, len(collection.Upload.ImageSizes))
	keys := imageSizeObjectKeys(collection, prefix, sourceType)
	for index, configured := range collection.Upload.ImageSizes {
		if err := validateImageDimensions(configured.Width, configured.Height); err != nil {
			return nil, fmt.Errorf("image size %q: %w", configured.Name, err)
		}
		resized := resize(source, configured.Width, configured.Height, configured.Fit, focalX, focalY)
		outputType := imageOutputType(sourceType)
		output, err := encodeImage(resized, outputType)
		if err != nil {
			return nil, fmt.Errorf("encode image size %q: %w", configured.Name, err)
		}
		sizeKey := keys[index]
		if err := manager.Backend.Put(ctx, sizeKey, bytes.NewReader(output), int64(len(output)), outputType); err != nil {
			return nil, fmt.Errorf("store image size %q: %w", configured.Name, err)
		}
		sizes[configured.Name] = store.Object(store.Values{
			"url": store.String(deliveryURL(collection.Slug, sizeKey)), "width": store.Number(float64(configured.Width)),
			"height": store.Number(float64(configured.Height)), "mimeType": store.String(outputType),
			"filesize": store.Number(float64(len(output))), "objectKey": store.String(sizeKey),
		})
	}
	return sizes, nil
}

func imageSizeObjectKeys(collection schema.Collection, prefix, sourceType string) []string {
	keys := make([]string, len(collection.Upload.ImageSizes))
	for index, configured := range collection.Upload.ImageSizes {
		extension := ".jpg"
		if imageOutputType(sourceType) == "image/png" {
			extension = ".png"
		}
		keys[index] = prefix + "/sizes/" + configured.Name + extension
	}
	return keys
}

func (manager Manager) lockPreparation(ctx context.Context, keys []string) (Prepared, error) {
	if len(keys) == 0 {
		return Prepared{}, nil
	}
	if manager.Locker == nil {
		return Prepared{}, storageError(fmt.Errorf("upload object preparation requires store.UploadObjectLocker"))
	}
	release, err := manager.Locker.LockUploadObjects(ctx, keys)
	if err != nil {
		return Prepared{}, storageError(fmt.Errorf("lock generated upload objects: %w", err))
	}
	if release == nil {
		return Prepared{}, storageError(fmt.Errorf("upload object locker returned a nil release function"))
	}
	return Prepared{Keys: append([]string(nil), keys...), outcome: &preparedOutcome{release: release}}, nil
}

func randomPrefix(namespace string, slug schema.CollectionSlug) (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	prefix := string(slug)
	if namespace != "" {
		// Collection slugs are public routing names and may change. New object
		// identity is deliberately scoped only to the stable application storage
		// namespace; the document reference remains the collection ownership proof.
		prefix = "ridu/" + namespace + "/" + objectNamespace
	}
	return prefix + "/" + hex.EncodeToString(nonce[:]), nil
}

func deliveryURL(slug schema.CollectionSlug, key string) string {
	if strings.HasPrefix(key, string(slug)+"/") {
		return "/api/uploads/" + key
	}
	return "/api/uploads/" + string(slug) + "/" + key
}

func (manager Manager) Rollback(ctx context.Context, prepared Prepared) error {
	return prepared.finish(func() error {
		cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()
		var joined error
		for index, key := range prepared.Keys {
			if err := cleanupContext.Err(); err != nil {
				return errors.Join(joined, fmt.Errorf("upload cleanup stopped after %d of %d objects: %w", index, len(prepared.Keys), err))
			}
			if err := manager.Backend.Delete(cleanupContext, key); err != nil {
				joined = errors.Join(joined, fmt.Errorf("delete staged upload %q: %w", key, err))
			}
		}
		return joined
	})
}

// ApplicationValues returns only application-owned upload document values.
// Storage metadata is always replaced by the manager after byte processing.
func ApplicationValues(values store.Values) store.Values {
	result := store.CloneValues(values)
	for name := range managedMetadataFields {
		delete(result, name)
	}
	return result
}

// OwnsKey reports whether one object belongs to this exact application
// namespace. Collection ownership comes from the access-checked document
// reference rather than a mutable collection slug in the key.
func (manager Manager) OwnsKey(key string) bool {
	parts := strings.Split(key, "/")
	if manager.Namespace != "" && len(parts) >= 5 && parts[0] == "ridu" && parts[1] == manager.Namespace && parts[2] == objectNamespace {
		return validGeneratedKeyTail(parts[3:])
	}
	return false
}

func validGeneratedKeyTail(parts []string) bool {
	if (len(parts) != 2 && len(parts) != 3) || len(parts[0]) != 32 {
		return false
	}
	if _, err := hex.DecodeString(parts[0]); err != nil {
		return false
	}
	if len(parts) == 3 && parts[1] != "sizes" && parts[1] != "source" {
		return false
	}
	filename := parts[len(parts)-1]
	return filename != "" && filename != "." && filename != ".." && filename == filepath.Base(filename)
}

// ValidateNamespace rejects ambiguous or path-shaped storage ownership names.
func ValidateNamespace(namespace string) error {
	if !storageNamespace.MatchString(namespace) {
		return fmt.Errorf("storage namespace must be 3-64 lowercase letters, digits, underscores, or hyphens")
	}
	return nil
}

func MIMEAllowed(actual string, allowed []string) bool {
	for _, pattern := range allowed {
		if pattern == actual || strings.HasSuffix(pattern, "/*") && strings.HasPrefix(actual, strings.TrimSuffix(pattern, "*")) {
			return true
		}
	}
	return false
}

func Filename(original, contentType string) string {
	name := filepath.Base(strings.TrimSpace(original))
	name = unsafeFilename.ReplaceAllString(name, "-")
	name = strings.Trim(name, ".-")
	if name == "" {
		extensions, _ := mime.ExtensionsByType(contentType)
		extension := ""
		if len(extensions) != 0 {
			extension = extensions[0]
		}
		name = "upload" + extension
	}
	if len(name) > 180 {
		extension := filepath.Ext(name)
		name = strings.TrimSuffix(name, extension)[:160] + extension
	}
	return name
}

func imageOutputType(sourceType string) string {
	if sourceType == "image/jpeg" {
		return "image/jpeg"
	}
	return "image/png"
}

func encodeImage(source image.Image, contentType string) ([]byte, error) {
	var output bytes.Buffer
	var err error
	if contentType == "image/jpeg" {
		err = jpeg.Encode(&output, source, &jpeg.Options{Quality: 85})
	} else {
		err = png.Encode(&output, source)
	}
	return output.Bytes(), err
}

func resize(source image.Image, width, height int, fit string, focalX, focalY float64) image.Image {
	target := image.NewRGBA(image.Rect(0, 0, width, height))
	bounds := source.Bounds()
	sourceWidth, sourceHeight := float64(bounds.Dx()), float64(bounds.Dy())
	if fit == "contain" {
		scale := min(float64(width)/sourceWidth, float64(height)/sourceHeight)
		w, h := max(1, int(math.Round(sourceWidth*scale))), max(1, int(math.Round(sourceHeight*scale)))
		x, y := (width-w)/2, (height-h)/2
		resample.CatmullRom.Scale(target, image.Rect(x, y, x+w, y+h), source, bounds, draw.Src, nil)
		return target
	}
	scale := min(sourceWidth/float64(width), sourceHeight/float64(height))
	cropWidth, cropHeight := max(1, int(math.Round(float64(width)*scale))), max(1, int(math.Round(float64(height)*scale)))
	// Centre around the actual focal point, clamped where the crop reaches an edge.
	x := max(0, min(bounds.Dx()-cropWidth, int(math.Round(sourceWidth*focalX/100-float64(cropWidth)/2))))
	y := max(0, min(bounds.Dy()-cropHeight, int(math.Round(sourceHeight*focalY/100-float64(cropHeight)/2))))
	crop := image.Rect(bounds.Min.X+x, bounds.Min.Y+y, bounds.Min.X+x+cropWidth, bounds.Min.Y+y+cropHeight)
	resample.CatmullRom.Scale(target, target.Bounds(), source, crop, draw.Src, nil)
	return target
}

// ValidateObjectRoles keeps private sources out of public delivery positions.
// Import/adoption uses the same role layout as freshly prepared objects.
func (manager Manager) ValidateObjectRoles(values store.Values) error {
	seen := map[string]bool{}
	check := func(value store.Value, role string) error {
		key, valid := value.StringValue()
		if !valid || !manager.OwnsKey(key) {
			return fmt.Errorf("upload %s object key is outside the application namespace", role)
		}
		parts := strings.Split(key, "/")[3:]
		if role == "primary" && len(parts) != 2 || role != "primary" && (len(parts) != 3 || parts[1] != role) {
			return fmt.Errorf("upload %s object key has an invalid storage role", role)
		}
		if seen[key] {
			return fmt.Errorf("upload object keys must be distinct")
		}
		seen[key] = true
		return nil
	}
	if err := check(values["objectKey"], "primary"); err != nil {
		return err
	}
	if source, exists := values["source"].CopyObject(); exists {
		if err := check(source["objectKey"], "source"); err != nil {
			return err
		}
	}
	if sizes, exists := values["sizes"].CopyObject(); exists {
		for _, size := range sizes {
			if err := check(size.Get("objectKey"), "sizes"); err != nil {
				return err
			}
		}
	}
	return nil
}
