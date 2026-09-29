package core

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/storage"
	"github.com/riducms/ridu/store"
)

const (
	// DefaultUploadGrantLifetime is used when a grant request omits a lifetime.
	DefaultUploadGrantLifetime = 10 * time.Minute
	// MaxUploadGrantLifetime bounds how long a leaked delivery URL stays usable.
	MaxUploadGrantLifetime = time.Hour
	// MaxUploadGrantsPerRequest bounds one grant request.
	MaxUploadGrantsPerRequest = 100

	uploadGrantVersion = "v1"
	uploadGrantDomain  = "ridu-upload-grant-v1"
	// uploadGrantClockSkew tolerates replicas whose clocks differ slightly.
	uploadGrantClockSkew = time.Minute
)

// UploadGrantRequest names one upload document and an optional configured
// image size. An empty Size selects the original object.
type UploadGrantRequest struct {
	ID   string
	Size string
}

// UploadGrant is a short-lived delivery URL for one upload object. The URL
// authorizes that object without carrying the session token.
type UploadGrant struct {
	ID        string
	Size      string
	URL       string
	ExpiresAt time.Time
}

// CreateUploadGrants mints delivery URLs for uploads that the session's owner
// can currently read. Each grant is bound to the session: logout, revocation,
// rotation, a password change, or expiry invalidates it.
func (application *App) CreateUploadGrants(ctx context.Context, token, collection string, requests []UploadGrantRequest, lifetime time.Duration) ([]UploadGrant, error) {
	resolved, exists := application.bySlug[collection]
	if !exists || resolved.Upload == nil {
		return nil, &operationengine.Error{Code: "not_found", Status: http.StatusNotFound, Message: fmt.Sprintf("upload collection %q was not found", collection)}
	}
	if len(requests) == 0 || len(requests) > MaxUploadGrantsPerRequest {
		return nil, &operationengine.Error{Code: "validation", Status: http.StatusUnprocessableEntity, Message: fmt.Sprintf("request between 1 and %d upload grants", MaxUploadGrantsPerRequest)}
	}
	if lifetime == 0 {
		lifetime = DefaultUploadGrantLifetime
	}
	if lifetime < time.Second || lifetime > MaxUploadGrantLifetime {
		return nil, &operationengine.Error{Code: "validation", Status: http.StatusUnprocessableEntity, Message: fmt.Sprintf("upload grant lifetime must be between 1 and %d seconds", int(MaxUploadGrantLifetime/time.Second))}
	}
	now := time.Now().UTC()
	session, err := application.resolveSession(ctx, token, now)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(requests))
	seen := make(map[string]struct{}, len(requests))
	for _, request := range requests {
		if request.ID == "" {
			return nil, &operationengine.Error{Code: "validation", Status: http.StatusUnprocessableEntity, Message: "every upload grant needs a document ID"}
		}
		if _, duplicate := seen[request.ID]; !duplicate {
			seen[request.ID] = struct{}{}
			ids = append(ids, request.ID)
		}
	}
	// One read applies the owner's current collection access to every item.
	page, err := application.local.List(ctx, collection, ListOptions{
		Where: query.In("id", ids...), Limit: len(ids),
		Actor: &session.User, ActorCollection: session.Collection,
	})
	if err != nil {
		return nil, err
	}
	documents := make(map[string]store.Document, len(page.Documents))
	for _, document := range page.Documents {
		documents[document.ID] = document
	}
	expiresAt := now.Add(lifetime).Truncate(time.Second)
	grants := make([]UploadGrant, len(requests))
	for index, request := range requests {
		document, readable := documents[request.ID]
		if !readable {
			return nil, &operationengine.Error{Code: "not_found", Status: http.StatusNotFound, Message: fmt.Sprintf("upload %q was not found", request.ID)}
		}
		key, deliveryURL, err := uploadObject(document, request.Size)
		if err != nil {
			return nil, err
		}
		grant := signUploadGrant(session.record, collection, key, expiresAt)
		separator := "?"
		if strings.Contains(deliveryURL, "?") {
			separator = "&"
		}
		grants[index] = UploadGrant{
			ID: request.ID, Size: request.Size, ExpiresAt: expiresAt,
			URL: deliveryURL + separator + "grant=" + grant,
		}
	}
	return grants, nil
}

// OpenUploadWithGrant streams one object authorized by a delivery grant. The
// grant's session and owner are reloaded, and the object is opened through the
// same collection read access as cookie delivery.
func (application *App) OpenUploadWithGrant(ctx context.Context, collection, key, grant string) (io.ReadCloser, storage.Object, error) {
	now := time.Now().UTC()
	sessionID, expiresAt, signature, valid := parseUploadGrant(grant)
	if !valid || !expiresAt.After(now) || expiresAt.After(now.Add(MaxUploadGrantLifetime+uploadGrantClockSkew)) {
		return nil, storage.Object{}, invalidUploadGrant()
	}
	record, err := application.auth.FindSessionByID(ctx, sessionID, now)
	if errors.Is(err, store.ErrNotFound) {
		return nil, storage.Object{}, invalidUploadGrant()
	}
	if err != nil {
		return nil, storage.Object{}, err
	}
	expected := uploadGrantSignature(record.TokenHash, collection, key, expiresAt)
	if !hmac.Equal(signature, expected) {
		return nil, storage.Object{}, invalidUploadGrant()
	}
	authCollection, exists := application.authByID[record.CollectionID]
	if !exists {
		return nil, storage.Object{}, invalidUploadGrant()
	}
	user, err := application.authUser(ctx, authCollection, record.UserID)
	if err != nil {
		if authOwnerError(err) != err {
			return nil, storage.Object{}, invalidUploadGrant()
		}
		return nil, storage.Object{}, err
	}
	return application.OpenUploadWithOptions(ctx, collection, key, FindOptions{Actor: &user, ActorCollection: authCollection.Slug})
}

func uploadObject(document store.Document, size string) (string, string, error) {
	source := document.Values
	if size != "" {
		sizes, _ := document.Values["sizes"].CopyObject()
		metadata, exists := sizes[size].CopyObject()
		if !exists {
			return "", "", &operationengine.Error{Code: "validation", Status: http.StatusUnprocessableEntity, Message: fmt.Sprintf("upload %q has no image size %q", document.ID, size)}
		}
		source = metadata
	}
	key, _ := source["objectKey"].StringValue()
	deliveryURL, _ := source["url"].StringValue()
	if key == "" || !strings.HasPrefix(deliveryURL, "/api/uploads/") {
		return "", "", &operationengine.Error{Code: "not_found", Status: http.StatusNotFound, Message: fmt.Sprintf("upload %q has no stored object", document.ID)}
	}
	return key, deliveryURL, nil
}

func signUploadGrant(session store.AuthSession, collection, key string, expiresAt time.Time) string {
	signature := uploadGrantSignature(session.TokenHash, collection, key, expiresAt)
	return uploadGrantVersion + "." + session.ID + "." + strconv.FormatInt(expiresAt.Unix(), 10) + "." + base64.RawURLEncoding.EncodeToString(signature)
}

// uploadGrantSignature keys the MAC with the session's stored token digest.
// The digest never leaves the server, so a grant cannot be used to mint other
// grants, and replacing or deleting the session invalidates every grant.
func uploadGrantSignature(tokenHash, collection, key string, expiresAt time.Time) []byte {
	mac := hmac.New(sha256.New, []byte(tokenHash))
	for _, part := range []string{uploadGrantDomain, collection, key, strconv.FormatInt(expiresAt.Unix(), 10)} {
		mac.Write([]byte(part))
		mac.Write([]byte{0})
	}
	return mac.Sum(nil)
}

func parseUploadGrant(grant string) (string, time.Time, []byte, bool) {
	if len(grant) > 512 {
		return "", time.Time{}, nil, false
	}
	parts := strings.Split(grant, ".")
	if len(parts) != 4 || parts[0] != uploadGrantVersion || !sessionIDShape(parts[1]) {
		return "", time.Time{}, nil, false
	}
	seconds, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || seconds <= 0 {
		return "", time.Time{}, nil, false
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || len(signature) != sha256.Size {
		return "", time.Time{}, nil, false
	}
	return parts[1], time.Unix(seconds, 0).UTC(), signature, true
}

// sessionIDShape accepts the 128-bit hexadecimal IDs Ridu issues, with or
// without the hyphens a UUID column adds when it returns them.
func sessionIDShape(id string) bool {
	digits := 0
	for _, character := range id {
		switch {
		case character >= '0' && character <= '9', character >= 'a' && character <= 'f', character >= 'A' && character <= 'F':
			digits++
		case character == '-':
		default:
			return false
		}
	}
	return digits == 32 && len(id) <= 36
}

func invalidUploadGrant() error {
	return &operationengine.Error{Code: "invalid_credential", Status: http.StatusUnauthorized, Message: "the upload grant is invalid or expired"}
}
