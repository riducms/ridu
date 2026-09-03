// Package s3 provides a dependency-free S3-compatible object backend.
package s3

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/riducms/ridu/storage"
)

const defaultMaxSpoolBytes int64 = 256 << 20

type Config struct {
	Endpoint, Region, Bucket, AccessKey, SecretKey string
	Client                                         *http.Client
	// AllowInsecureEndpoint explicitly permits plaintext HTTP for local S3
	// emulators such as MinIO. Production credentials must use HTTPS.
	AllowInsecureEndpoint bool
	// MaxSpoolBytes bounds temporary disk use when Put receives a reader that
	// cannot be rewound for signing. Zero selects the Ridu upload limit.
	MaxSpoolBytes int64
	// SpoolDirectory optionally selects the directory for non-seekable Put
	// payloads. The operating system temporary directory is used by default.
	SpoolDirectory string
}

type Backend struct {
	endpoint                       *url.URL
	region, bucket, access, secret string
	client                         *http.Client
	maxSpoolBytes                  int64
	spoolDirectory                 string
	now                            func() time.Time
}

type listResult struct {
	Contents              []listedObject
	IsTruncated           bool   `xml:"IsTruncated"`
	NextContinuationToken string `xml:"NextContinuationToken"`
}

type listedObject struct {
	Key          string
	Size         int64
	LastModified time.Time
}

func New(config Config) (*Backend, error) {
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" {
		return nil, fmt.Errorf("invalid S3 endpoint")
	}
	if endpoint.Scheme != "https" && !(endpoint.Scheme == "http" && config.AllowInsecureEndpoint) {
		return nil, fmt.Errorf("S3 endpoint must use HTTPS; set AllowInsecureEndpoint only for trusted local emulators")
	}
	if config.Region == "" || config.Bucket == "" || config.AccessKey == "" || config.SecretKey == "" {
		return nil, fmt.Errorf("S3 region, bucket, access key, and secret key are required")
	}
	if config.MaxSpoolBytes < 0 {
		return nil, fmt.Errorf("S3 maximum spool size must not be negative")
	}
	if config.MaxSpoolBytes == 0 {
		config.MaxSpoolBytes = defaultMaxSpoolBytes
	}
	client := config.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Backend{
		endpoint:       endpoint,
		region:         config.Region,
		bucket:         config.Bucket,
		access:         config.AccessKey,
		secret:         config.SecretKey,
		client:         client,
		maxSpoolBytes:  config.MaxSpoolBytes,
		spoolDirectory: config.SpoolDirectory,
		now:            time.Now,
	}, nil
}

// Ping verifies bucket reachability and credentials without listing or
// mutating application objects.
func (backend *Backend) Ping(ctx context.Context) error {
	request, err := backend.request(ctx, http.MethodHead, "", nil, nil, "")
	if err != nil {
		return err
	}
	return backend.do(request, nil)
}

func (backend *Backend) Put(ctx context.Context, key string, source io.Reader, size int64, contentType string) (result error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateObjectKey(key); err != nil {
		return err
	}
	if size < 0 {
		return fmt.Errorf("S3 object size must not be negative")
	}
	if source == nil {
		return fmt.Errorf("S3 object source is required")
	}
	body, payloadHash, cleanup, err := backend.preparePayload(ctx, source, size)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer func() {
			result = errors.Join(result, cleanup())
		}()
	}
	request, err := backend.request(ctx, http.MethodPut, key, nil, body, payloadHash)
	if err != nil {
		return err
	}
	request.ContentLength = size
	request.Header.Set("Content-Type", contentType)
	return backend.do(request, nil)
}

func (backend *Backend) Open(ctx context.Context, key string) (io.ReadCloser, storage.Object, error) {
	request, err := backend.request(ctx, http.MethodGet, key, nil, nil, "")
	if err != nil {
		return nil, storage.Object{}, err
	}
	response, err := backend.client.Do(request)
	if err != nil {
		return nil, storage.Object{}, err
	}
	if response.StatusCode == http.StatusNotFound {
		_ = response.Body.Close()
		return nil, storage.Object{}, storage.ErrNotFound
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer response.Body.Close()
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return nil, storage.Object{}, fmt.Errorf("S3 GET %s: %s: %s", key, response.Status, strings.TrimSpace(string(message)))
	}
	modified, _ := http.ParseTime(response.Header.Get("Last-Modified"))
	return response.Body, storage.Object{Key: key, Size: response.ContentLength, ContentType: response.Header.Get("Content-Type"), ModifiedAt: modified}, nil
}

func (backend *Backend) Delete(ctx context.Context, key string) error {
	request, err := backend.request(ctx, http.MethodDelete, key, nil, nil, "")
	if err != nil {
		return err
	}
	return backend.do(request, nil)
}

func (backend *Backend) List(ctx context.Context, list storage.ListRequest) (storage.ListPage, error) {
	if list.Limit < 1 || list.Limit > storage.MaxListPageSize {
		return storage.ListPage{}, fmt.Errorf("S3 list limit must be between 1 and %d", storage.MaxListPageSize)
	}
	query := url.Values{"list-type": {"2"}, "prefix": {list.Prefix}, "max-keys": {strconv.Itoa(list.Limit)}}
	if list.Cursor != "" {
		query.Set("continuation-token", list.Cursor)
	}
	var result listResult
	request, err := backend.request(ctx, http.MethodGet, "", query, nil, "")
	if err != nil {
		return storage.ListPage{}, err
	}
	if err := backend.do(request, &result); err != nil {
		return storage.ListPage{}, err
	}
	if len(result.Contents) > list.Limit {
		return storage.ListPage{}, fmt.Errorf("S3 listing returned %d objects for limit %d", len(result.Contents), list.Limit)
	}
	objects := make([]storage.Object, len(result.Contents))
	for index, object := range result.Contents {
		if !strings.HasPrefix(object.Key, list.Prefix) {
			return storage.ListPage{}, fmt.Errorf("S3 listing returned key %q outside prefix %q", object.Key, list.Prefix)
		}
		if object.LastModified.IsZero() {
			return storage.ListPage{}, fmt.Errorf("S3 listing returned key %q without modification time", object.Key)
		}
		objects[index] = storage.Object{Key: object.Key, Size: object.Size, ModifiedAt: object.LastModified}
	}
	sort.Slice(objects, func(left, right int) bool { return objects[left].Key < objects[right].Key })
	for index := 1; index < len(objects); index++ {
		if objects[index-1].Key == objects[index].Key {
			return storage.ListPage{}, fmt.Errorf("S3 listing returned duplicate key %q", objects[index].Key)
		}
	}
	next := ""
	if result.IsTruncated {
		next = strings.TrimSpace(result.NextContinuationToken)
		if next == "" || next == list.Cursor {
			return storage.ListPage{}, fmt.Errorf("S3 listing returned an invalid continuation token")
		}
	}
	return storage.ListPage{Objects: objects, NextCursor: next}, nil
}

func (backend *Backend) SignedURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if ttl <= 0 || ttl > 7*24*time.Hour {
		return "", fmt.Errorf("signed URL lifetime must be between one second and seven days")
	}
	if err := validateObjectKey(key); err != nil {
		return "", err
	}
	now := backend.now().UTC()
	date := now.Format("20060102")
	scope := date + "/" + backend.region + "/s3/aws4_request"
	objectURL := backend.objectURL(key)
	query := objectURL.Query()
	query.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	query.Set("X-Amz-Credential", backend.access+"/"+scope)
	query.Set("X-Amz-Date", now.Format("20060102T150405Z"))
	query.Set("X-Amz-Expires", fmt.Sprintf("%d", int64(ttl/time.Second)))
	query.Set("X-Amz-SignedHeaders", "host")
	objectURL.RawQuery = query.Encode()
	canonical := strings.Join([]string{http.MethodGet, objectURL.EscapedPath(), objectURL.RawQuery, "host:" + objectURL.Host + "\n", "host", "UNSIGNED-PAYLOAD"}, "\n")
	stringToSign := strings.Join([]string{"AWS4-HMAC-SHA256", now.Format("20060102T150405Z"), scope, digest([]byte(canonical))}, "\n")
	query.Set("X-Amz-Signature", hex.EncodeToString(backend.sign(date, []byte(stringToSign))))
	objectURL.RawQuery = query.Encode()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return objectURL.String(), nil
}

func (backend *Backend) request(ctx context.Context, method, key string, query url.Values, body io.Reader, payloadHash string) (*http.Request, error) {
	if key != "" {
		if err := validateObjectKey(key); err != nil {
			return nil, err
		}
	}
	objectURL := backend.objectURL(key)
	objectURL.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, method, objectURL.String(), body)
	if err != nil {
		return nil, err
	}
	now := backend.now().UTC()
	if payloadHash == "" {
		payloadHash = digest(nil)
	}
	request.Header.Set("X-Amz-Date", now.Format("20060102T150405Z"))
	request.Header.Set("X-Amz-Content-Sha256", payloadHash)
	canonicalHeaders := "host:" + objectURL.Host + "\n" + "x-amz-content-sha256:" + payloadHash + "\n" + "x-amz-date:" + request.Header.Get("X-Amz-Date") + "\n"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonical := strings.Join([]string{method, objectURL.EscapedPath(), objectURL.Query().Encode(), canonicalHeaders, signedHeaders, payloadHash}, "\n")
	date := now.Format("20060102")
	scope := date + "/" + backend.region + "/s3/aws4_request"
	stringToSign := strings.Join([]string{"AWS4-HMAC-SHA256", request.Header.Get("X-Amz-Date"), scope, digest([]byte(canonical))}, "\n")
	signature := hex.EncodeToString(backend.sign(date, []byte(stringToSign)))
	request.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+backend.access+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+signature)
	return request, nil
}

func (backend *Backend) preparePayload(ctx context.Context, source io.Reader, declared int64) (io.Reader, string, func() error, error) {
	if seekable, ok := source.(io.ReadSeeker); ok {
		position, err := seekable.Seek(0, io.SeekCurrent)
		if err != nil {
			return nil, "", nil, fmt.Errorf("locate seekable S3 payload: %w", err)
		}
		stopCancellation := interruptReaderOnCancellation(ctx, source)
		payloadHash, hashError := hashDeclaredPayload(ctx, nil, seekable, declared)
		stopCancellation()
		_, seekError := seekable.Seek(position, io.SeekStart)
		if seekError != nil {
			seekError = fmt.Errorf("rewind seekable S3 payload: %w", seekError)
		}
		if err := errors.Join(hashError, seekError); err != nil {
			return nil, "", nil, err
		}
		return io.LimitReader(seekable, declared), payloadHash, nil, nil
	}

	if declared > backend.maxSpoolBytes {
		return nil, "", nil, fmt.Errorf("non-seekable S3 payload exceeds the %d-byte spool limit", backend.maxSpoolBytes)
	}
	temporary, err := os.CreateTemp(backend.spoolDirectory, ".ridu-s3-upload-*")
	if err != nil {
		return nil, "", nil, fmt.Errorf("create S3 payload spool: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := func() error {
		closeError := temporary.Close()
		if errors.Is(closeError, os.ErrClosed) {
			closeError = nil
		}
		removeError := os.Remove(temporaryPath)
		if errors.Is(removeError, os.ErrNotExist) {
			removeError = nil
		}
		if closeError != nil {
			closeError = fmt.Errorf("close S3 payload spool: %w", closeError)
		}
		if removeError != nil {
			removeError = fmt.Errorf("remove S3 payload spool: %w", removeError)
		}
		return errors.Join(closeError, removeError)
	}
	stopCancellation := interruptReaderOnCancellation(ctx, source)
	payloadHash, prepareError := hashDeclaredPayload(ctx, temporary, source, declared)
	stopCancellation()
	if prepareError == nil {
		if _, err := temporary.Seek(0, io.SeekStart); err != nil {
			prepareError = fmt.Errorf("rewind S3 payload spool: %w", err)
		}
	}
	if prepareError != nil {
		return nil, "", nil, errors.Join(prepareError, cleanup())
	}
	return temporary, payloadHash, cleanup, nil
}

func hashDeclaredPayload(ctx context.Context, destination io.Writer, source io.Reader, declared int64) (string, error) {
	hash := sha256.New()
	writer := io.Writer(hash)
	if destination != nil {
		writer = io.MultiWriter(destination, hash)
	}
	buffer := make([]byte, 32*1024)
	written := int64(0)
	emptyReads := 0
	for written < declared {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		remaining := declared - written
		readBuffer := buffer
		if remaining < int64(len(readBuffer)) {
			readBuffer = readBuffer[:remaining]
		}
		read, readError := source.Read(readBuffer)
		if read < 0 || read > len(readBuffer) {
			return "", fmt.Errorf("read S3 payload: invalid byte count %d", read)
		}
		if read > 0 {
			emptyReads = 0
			for offset := 0; offset < read; {
				count, err := writer.Write(readBuffer[offset:read])
				offset += count
				if err != nil {
					return "", fmt.Errorf("spool S3 payload: %w", err)
				}
				if count == 0 {
					return "", fmt.Errorf("spool S3 payload: %w", io.ErrShortWrite)
				}
			}
			written += int64(read)
		} else {
			emptyReads++
			if emptyReads >= 100 {
				return "", fmt.Errorf("read S3 payload: %w", io.ErrNoProgress)
			}
		}
		if readError != nil {
			if errors.Is(readError, io.EOF) {
				if written == declared {
					return hex.EncodeToString(hash.Sum(nil)), nil
				}
				return "", fmt.Errorf("S3 object size mismatch: declared %d bytes, received %d", declared, written)
			}
			if err := ctx.Err(); err != nil {
				return "", err
			}
			return "", fmt.Errorf("read S3 payload: %w", readError)
		}
	}

	emptyReads = 0
	var extra [1]byte
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		read, readError := source.Read(extra[:])
		if read > 0 {
			return "", fmt.Errorf("S3 object size mismatch: declared %d bytes, received more", declared)
		}
		if readError != nil {
			if errors.Is(readError, io.EOF) {
				return hex.EncodeToString(hash.Sum(nil)), nil
			}
			if err := ctx.Err(); err != nil {
				return "", err
			}
			return "", fmt.Errorf("read S3 payload: %w", readError)
		}
		emptyReads++
		if emptyReads >= 100 {
			return "", fmt.Errorf("read S3 payload: %w", io.ErrNoProgress)
		}
	}
}

func interruptReaderOnCancellation(ctx context.Context, source io.Reader) func() {
	closer, ok := source.(io.Closer)
	if !ok || ctx.Done() == nil {
		return func() {}
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-ctx.Done():
			_ = closer.Close()
		case <-stop:
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}

func (backend *Backend) objectURL(key string) *url.URL {
	cloned := *backend.endpoint
	cloned.Path = path.Join(backend.endpoint.Path, backend.bucket, key)
	return &cloned
}

func validateObjectKey(key string) error {
	if key == "" || strings.HasPrefix(key, "/") || strings.Contains(key, `\`) {
		return fmt.Errorf("invalid S3 object key")
	}
	for _, segment := range strings.Split(key, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("invalid S3 object key")
		}
	}
	return nil
}

func (backend *Backend) sign(date string, value []byte) []byte {
	dateKey := mac([]byte("AWS4"+backend.secret), []byte(date))
	regionKey := mac(dateKey, []byte(backend.region))
	serviceKey := mac(regionKey, []byte("s3"))
	return mac(mac(serviceKey, []byte("aws4_request")), value)
}

func (backend *Backend) do(request *http.Request, decoded any) error {
	response, err := backend.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("S3 %s %s: %s: %s", request.Method, request.URL.Path, response.Status, strings.TrimSpace(string(message)))
	}
	if decoded != nil {
		if err := xml.NewDecoder(response.Body).Decode(decoded); err != nil {
			return fmt.Errorf("decode S3 response: %w", err)
		}
	}
	return nil
}

func digest(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func mac(key, value []byte) []byte {
	hash := hmac.New(sha256.New, key)
	_, _ = hash.Write(value)
	return hash.Sum(nil)
}

var _ storage.Backend = (*Backend)(nil)
var _ storage.HealthBackend = (*Backend)(nil)
var _ storage.URLSigner = (*Backend)(nil)
