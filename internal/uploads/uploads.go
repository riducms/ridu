package uploads

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
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

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/storage"
	"github.com/riducms/ridu/store"
)

var unsafeFilename = regexp.MustCompile("[^a-zA-Z0-9._-]+")
var storageNamespace = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{2,63}$`)

var managedMetadataFields = map[string]struct{}{
	"filename": {}, "mimeType": {}, "filesize": {}, "url": {}, "objectKey": {},
	"width": {}, "height": {}, "sizes": {}, "focalX": {}, "focalY": {},
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
	sizes, hasSizes := source["sizes"].ObjectValue()
	if hasSizes {
		names := make([]string, 0, len(sizes))
		for name := range sizes {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			metadata, valid := sizes[name].ObjectValue()
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
	if !mimeAllowed(contentType, collection.Upload.MimeTypes) {
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
		imageBytes, estimateError := imageWorkBytes(collection, configuration.Width, configuration.Height, int64(len(encoded)), false)
		if estimateError != nil {
			return Prepared{}, estimateError
		}
		release, admissionError := manager.workAdmission().AcquireImage(ctx, imageBytes)
		if admissionError != nil {
			return Prepared{}, admissionError
		}
		defer release()
		source, _, decodeError = image.Decode(bytes.NewReader(encoded))
		if decodeError != nil {
			return Prepared{}, fmt.Errorf("decode image: %w", decodeError)
		}
	}
	filename := safeFilename(input.Filename, contentType)
	prefix, err := randomPrefix(manager.Namespace, collection.Slug)
	if err != nil {
		return Prepared{}, storageError(err)
	}
	key := prefix + "/" + filename
	keys := []string{key}
	if source != nil {
		keys = append(keys, imageSizeObjectKeys(collection, prefix)...)
	}
	prepared, err := manager.lockPreparation(ctx, keys)
	if err != nil {
		return Prepared{}, err
	}
	if err := manager.Backend.Put(ctx, key, bytes.NewReader(encoded), int64(len(encoded)), contentType); err != nil {
		cleanupError := manager.Rollback(ctx, prepared)
		return Prepared{}, storageError(errors.Join(fmt.Errorf("store upload: %w", err), cleanupError))
	}
	values := ApplicationValues(input.Values)
	values["filename"] = store.String(filename)
	values["mimeType"] = store.String(contentType)
	values["filesize"] = store.Number(float64(len(encoded)))
	values["objectKey"] = store.String(key)
	values["url"] = store.String(deliveryURL(collection.Slug, key))

	if strings.HasPrefix(contentType, "image/") {
		bounds := source.Bounds()
		values["width"] = store.Number(float64(bounds.Dx()))
		values["height"] = store.Number(float64(bounds.Dy()))
		values["focalX"] = store.Number(50)
		values["focalY"] = store.Number(50)
		sizes, sizeError := manager.storeImageSizes(ctx, collection, source, prefix, 50, 50)
		if sizeError != nil {
			return Prepared{}, storageError(errors.Join(sizeError, manager.Rollback(ctx, prepared)))
		}
		if len(sizes) != 0 {
			values["sizes"] = store.Object(sizes)
		}
	}
	prepared.Values = values
	return prepared, nil
}

// RegenerateImage creates a fresh set of configured variants from an existing
// original. The caller owns committing Values to the document and rolling back
// Keys if that commit fails.
func (manager Manager) RegenerateImage(ctx context.Context, collection schema.Collection, input ImageInput) (Prepared, error) {
	if collection.Upload == nil {
		return Prepared{}, fmt.Errorf("collection %q is not upload-enabled", collection.Slug)
	}
	if manager.Backend == nil {
		return Prepared{}, storageError(fmt.Errorf("upload storage is not configured"))
	}
	if !manager.OwnsKey(input.ObjectKey) {
		return Prepared{}, storageError(fmt.Errorf("upload original object key is outside the application namespace"))
	}
	if math.IsNaN(input.FocalX) || math.IsInf(input.FocalX, 0) || math.IsNaN(input.FocalY) || math.IsInf(input.FocalY, 0) || input.FocalX < 0 || input.FocalX > 100 || input.FocalY < 0 || input.FocalY > 100 {
		return Prepared{}, fmt.Errorf("focal coordinates must be between 0 and 100")
	}
	if err := validateCrop(input); err != nil {
		return Prepared{}, err
	}
	reader, object, err := manager.Backend.Open(ctx, input.ObjectKey)
	if err != nil {
		return Prepared{}, storageError(fmt.Errorf("open original image: %w", err))
	}
	defer reader.Close()
	limit := collection.Upload.MaxFileSize
	if limit < 1 {
		limit = 10 << 20
	}
	if limit > maximumUploadBytes {
		return Prepared{}, fmt.Errorf("upload limit must not exceed %d bytes", maximumUploadBytes)
	}
	releaseFile, admissionError := manager.workAdmission().AcquireFile(ctx, limit)
	if admissionError != nil {
		return Prepared{}, admissionError
	}
	defer releaseFile()
	if object.Size > limit {
		return Prepared{}, storageError(fmt.Errorf("original image exceeds %d-byte upload limit", limit))
	}
	encoded, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return Prepared{}, storageError(fmt.Errorf("read original image: %w", err))
	}
	if int64(len(encoded)) > limit {
		return Prepared{}, storageError(fmt.Errorf("original image exceeds %d-byte upload limit", limit))
	}
	configuration, _, err := image.DecodeConfig(bytes.NewReader(encoded))
	if err != nil {
		return Prepared{}, storageError(fmt.Errorf("decode original image metadata: %w", err))
	}
	if err := validateImageDimensions(configuration.Width, configuration.Height); err != nil {
		return Prepared{}, storageError(err)
	}
	imageBytes, estimateError := imageWorkBytes(collection, configuration.Width, configuration.Height, int64(len(encoded)), input.CropWidth > 0 && input.CropHeight > 0)
	if estimateError != nil {
		return Prepared{}, estimateError
	}
	releaseImage, admissionError := manager.workAdmission().AcquireImage(ctx, imageBytes)
	if admissionError != nil {
		return Prepared{}, admissionError
	}
	defer releaseImage()
	source, _, err := image.Decode(bytes.NewReader(encoded))
	if err != nil {
		return Prepared{}, storageError(fmt.Errorf("decode original image: %w", err))
	}
	focalX, focalY := input.FocalX, input.FocalY
	if input.CropWidth > 0 && input.CropHeight > 0 {
		source = cropImage(source, input.CropX, input.CropY, input.CropWidth, input.CropHeight)
		focalX = clampPercentage((input.FocalX - input.CropX) * 100 / input.CropWidth)
		focalY = clampPercentage((input.FocalY - input.CropY) * 100 / input.CropHeight)
	}
	prefix, err := randomPrefix(manager.Namespace, collection.Slug)
	if err != nil {
		return Prepared{}, storageError(err)
	}
	prepared, err := manager.lockPreparation(ctx, imageSizeObjectKeys(collection, prefix))
	if err != nil {
		return Prepared{}, err
	}
	sizes, err := manager.storeImageSizes(ctx, collection, source, prefix, focalX, focalY)
	if err != nil {
		return Prepared{}, storageError(errors.Join(err, manager.Rollback(ctx, prepared)))
	}
	values := store.Values{"focalX": store.Number(input.FocalX), "focalY": store.Number(input.FocalY)}
	values["cropX"], values["cropY"] = store.Number(input.CropX), store.Number(input.CropY)
	values["cropWidth"], values["cropHeight"] = store.Number(input.CropWidth), store.Number(input.CropHeight)
	values["sizes"] = store.Object(sizes)
	prepared.Values = values
	return prepared, nil
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
	left := bounds.Min.X + int(float64(bounds.Dx())*x/100)
	top := bounds.Min.Y + int(float64(bounds.Dy())*y/100)
	right := bounds.Min.X + int(float64(bounds.Dx())*(x+width)/100)
	bottom := bounds.Min.Y + int(float64(bounds.Dy())*(y+height)/100)
	if right <= left {
		right = left + 1
	}
	if bottom <= top {
		bottom = top + 1
	}
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
	sourceBytesPerPixel := int64(8)
	if crop {
		sourceBytesPerPixel = 12
	}
	return sourcePixels*sourceBytesPerPixel + largestVariant*4 + encodedBytes*2, nil
}

func (manager Manager) storeImageSizes(ctx context.Context, collection schema.Collection, source image.Image, prefix string, focalX, focalY float64) (store.Values, error) {
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
	keys := imageSizeObjectKeys(collection, prefix)
	for index, configured := range collection.Upload.ImageSizes {
		if err := validateImageDimensions(configured.Width, configured.Height); err != nil {
			return nil, fmt.Errorf("image size %q: %w", configured.Name, err)
		}
		resized := resize(source, configured.Width, configured.Height, configured.Fit, focalX, focalY)
		var output bytes.Buffer
		if err := jpeg.Encode(&output, resized, &jpeg.Options{Quality: 85}); err != nil {
			return nil, fmt.Errorf("encode image size %q: %w", configured.Name, err)
		}
		sizeKey := keys[index]
		if err := manager.Backend.Put(ctx, sizeKey, bytes.NewReader(output.Bytes()), int64(output.Len()), "image/jpeg"); err != nil {
			return nil, fmt.Errorf("store image size %q: %w", configured.Name, err)
		}
		sizes[configured.Name] = store.Object(store.Values{
			"url": store.String(deliveryURL(collection.Slug, sizeKey)), "width": store.Number(float64(configured.Width)),
			"height": store.Number(float64(configured.Height)), "mimeType": store.String("image/jpeg"),
			"filesize": store.Number(float64(output.Len())), "objectKey": store.String(sizeKey),
		})
	}
	return sizes, nil
}

func imageSizeObjectKeys(collection schema.Collection, prefix string) []string {
	keys := make([]string, len(collection.Upload.ImageSizes))
	for index, configured := range collection.Upload.ImageSizes {
		keys[index] = prefix + "/sizes/" + configured.Name + ".jpg"
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
	if len(parts) == 3 && parts[1] != "sizes" {
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

func mimeAllowed(actual string, allowed []string) bool {
	for _, pattern := range allowed {
		if pattern == actual || strings.HasSuffix(pattern, "/*") && strings.HasPrefix(actual, strings.TrimSuffix(pattern, "*")) {
			return true
		}
	}
	return false
}

func safeFilename(original, contentType string) string {
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

func resize(source image.Image, width, height int, fit string, focalX, focalY float64) image.Image {
	target := image.NewRGBA(image.Rect(0, 0, width, height))
	sourceBounds := source.Bounds()
	sourceWidth, sourceHeight := float64(sourceBounds.Dx()), float64(sourceBounds.Dy())
	scaleX, scaleY := sourceWidth/float64(width), sourceHeight/float64(height)
	if fit == "contain" {
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				target.Set(x, y, color.Transparent)
			}
		}
		scale := max(scaleX, scaleY)
		drawWidth, drawHeight := int(sourceWidth/scale), int(sourceHeight/scale)
		offsetX, offsetY := (width-drawWidth)/2, (height-drawHeight)/2
		for y := 0; y < drawHeight; y++ {
			for x := 0; x < drawWidth; x++ {
				target.Set(offsetX+x, offsetY+y, source.At(sourceBounds.Min.X+int(float64(x)*scale), sourceBounds.Min.Y+int(float64(y)*scale)))
			}
		}
		return target
	}
	scale := min(scaleX, scaleY)
	cropWidth, cropHeight := float64(width)*scale, float64(height)*scale
	offsetX := (sourceWidth - cropWidth) * focalX / 100
	offsetY := (sourceHeight - cropHeight) * focalY / 100
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			target.Set(x, y, source.At(sourceBounds.Min.X+int(offsetX+float64(x)*scale), sourceBounds.Min.Y+int(offsetY+float64(y)*scale)))
		}
	}
	return target
}
