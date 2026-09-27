package core_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	localstorage "github.com/riducms/ridu/adapters/storage/local"
	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/protocol"
	"github.com/riducms/ridu/query"
	"github.com/riducms/ridu/store"
	"golang.org/x/crypto/bcrypt"
)

type transportFixture struct {
	application *ridu.App
	backend     *authOwnerReadFailureStore
	client      *http.Client
}

func newTransportFixture(t *testing.T) transportFixture {
	t.Helper()
	storage, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ownerPath, err := query.NewPath("owner")
	if err != nil {
		t.Fatal(err)
	}
	owned := func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
		if ctx.Actor == nil || ctx.ActorCollection != "users" {
			return ridu.Deny(), nil
		}
		return ridu.Where(query.Equal(ownerPath, query.String(ctx.Actor.ID))), nil
	}
	signedIn := func(ctx ridu.AccessContext) (ridu.AccessDecision, error) {
		if ctx.Actor == nil {
			return ridu.Deny(), nil
		}
		return ridu.Allow(), nil
	}
	backend := &authOwnerReadFailureStore{Store: teststore.New()}
	application, err := ridu.New(ridu.Config{
		Name: "session transports", Admin: ridu.AdminConfig{User: "users"},
		Storage: storage, StorageNamespace: "session-transports",
		Collections: []ridu.Collection{
			{
				Slug: "users", Auth: true, AuthConfig: ridu.AuthConfig{Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}},
				Access: ridu.CollectionAccess{Read: signedIn},
				Fields: field.Fields{field.Email("email").Required().Unique()},
			},
			{
				Slug: "media", Upload: true,
				UploadConfig: ridu.UploadConfig{MaxFileSize: 4096, MimeTypes: []string{"image/png"}, Private: true, ImageSizes: []ridu.ImageSize{{Name: "thumb", Width: 2, Height: 2, Fit: "cover"}}},
				Access:       ridu.CollectionAccess{Create: signedIn, Read: owned, Update: owned, Delete: owned},
				Fields:       field.Fields{field.Relationship("owner", "users").Required()},
			},
		},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	handler := application.Handler(ridu.HandlerOptions{AllowedOrigins: []string{"https://app.example.test"}})
	return transportFixture{application: application, backend: backend, client: handlerClient(handler)}
}

func (fixture transportFixture) user(t *testing.T, email string) store.Document {
	t.Helper()
	user := createTransportUser(t, fixture.application, email)
	return user
}

func createTransportUser(t *testing.T, application *ridu.App, email string) store.Document {
	t.Helper()
	user, err := application.Local().Create(context.Background(), "users", store.Values{"email": store.String(email)}, ridu.MutationOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := application.SetPassword(context.Background(), "users", user.ID, "correct-horse"); err != nil {
		t.Fatal(err)
	}
	return user
}

func (fixture transportFixture) do(t *testing.T, method, path string, body string, headers map[string]string) *http.Response {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request, err := http.NewRequest(method, "http://ridu.test"+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := fixture.client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func (fixture transportFixture) tokenLogin(t *testing.T, email string) protocol.SessionTokenEnvelope[map[string]any] {
	t.Helper()
	response := fixture.do(t, http.MethodPost, "/api/auth/users/login", `{"email":"`+email+`","password":"correct-horse","transport":"token"}`, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("token login = %d: %s", response.StatusCode, readBody(t, response))
	}
	if len(response.Cookies()) != 0 {
		t.Fatalf("token login set cookies: %#v", response.Cookies())
	}
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("token login Cache-Control = %q", response.Header.Get("Cache-Control"))
	}
	var envelope protocol.SessionTokenEnvelope[map[string]any]
	decodeResponse(t, response, &envelope)
	if envelope.Token == "" || envelope.Session.ID == "" || envelope.Session.Collection != "users" {
		t.Fatalf("token login envelope = %#v", envelope)
	}
	return envelope
}

func sessionHeader(token string) map[string]string {
	return map[string]string{"Authorization": "Session " + token}
}

func responseErrorCode(t *testing.T, response *http.Response) protocol.ErrorCode {
	t.Helper()
	var envelope protocol.ErrorEnvelope
	decodeResponse(t, response, &envelope)
	return envelope.Error.Code
}

func TestTokenTransportManagesSessionsWithoutCookies(t *testing.T) {
	fixture := newTransportFixture(t)
	user := fixture.user(t, "token@example.test")
	issued := fixture.tokenLogin(t, "token@example.test")
	header := sessionHeader(issued.Token)

	me := fixture.do(t, http.MethodGet, "/api/auth/me", "", header)
	var current protocol.SessionEnvelope[map[string]any]
	decodeResponse(t, me, &current)
	if current.Session.ID != issued.Session.ID || current.Session.User["id"] != user.ID {
		t.Fatalf("me = %#v", current)
	}

	sessions := fixture.do(t, http.MethodGet, "/api/auth/sessions", "", header)
	var listed protocol.AuthSessionsEnvelope
	decodeResponse(t, sessions, &listed)
	if len(listed.Sessions) != 1 || !listed.Sessions[0].Current {
		t.Fatalf("sessions = %#v", listed)
	}

	rotated := fixture.do(t, http.MethodPost, "/api/auth/rotate", "", header)
	if rotated.StatusCode != http.StatusOK || len(rotated.Cookies()) != 0 {
		t.Fatalf("rotate = %d cookies=%#v", rotated.StatusCode, rotated.Cookies())
	}
	var replacement protocol.SessionTokenEnvelope[map[string]any]
	decodeResponse(t, rotated, &replacement)
	if replacement.Token == "" || replacement.Token == issued.Token || replacement.Session.ID != issued.Session.ID || replacement.Session.ExpiresAt != issued.Session.ExpiresAt {
		t.Fatalf("rotation = %#v, issued %#v", replacement, issued)
	}
	if stale := fixture.do(t, http.MethodGet, "/api/auth/me", "", header); stale.StatusCode != http.StatusUnauthorized || responseErrorCode(t, stale) != protocol.ErrorInvalidCredential {
		t.Fatal("rotated token remained valid")
	}

	header = sessionHeader(replacement.Token)
	logout := fixture.do(t, http.MethodPost, "/api/auth/logout", "", header)
	if logout.StatusCode != http.StatusOK || len(logout.Cookies()) != 0 {
		t.Fatalf("header logout = %d cookies=%#v", logout.StatusCode, logout.Cookies())
	}
	if revoked := fixture.do(t, http.MethodGet, "/api/auth/me", "", header); revoked.StatusCode != http.StatusUnauthorized || responseErrorCode(t, revoked) != protocol.ErrorInvalidCredential {
		t.Fatal("logout did not revoke the header session")
	}
	// Logout is idempotent for a token another device already revoked.
	if again := fixture.do(t, http.MethodPost, "/api/auth/logout", "", header); again.StatusCode != http.StatusOK {
		t.Fatalf("repeated logout = %d: %s", again.StatusCode, readBody(t, again))
	}
}

func TestExplicitCredentialNeverFallsBackToCookie(t *testing.T) {
	fixture := newTransportFixture(t)
	fixture.user(t, "cookie@example.test")
	fixture.user(t, "header@example.test")
	cookieLogin := fixture.do(t, http.MethodPost, "/api/auth/users/login", `{"email":"cookie@example.test","password":"correct-horse"}`, nil)
	if cookieLogin.StatusCode != http.StatusOK || len(cookieLogin.Cookies()) != 1 {
		t.Fatalf("cookie login = %d", cookieLogin.StatusCode)
	}
	cookie := cookieLogin.Cookies()[0].String()
	issued := fixture.tokenLogin(t, "header@example.test")

	both := sessionHeader(issued.Token)
	both["Cookie"] = cookie
	me := fixture.do(t, http.MethodGet, "/api/auth/me", "", both)
	var current protocol.SessionEnvelope[map[string]any]
	decodeResponse(t, me, &current)
	if current.Session.User["email"] != "header@example.test" {
		t.Fatalf("header credential did not take precedence over the same-site cookie: %#v", current.Session.User)
	}

	for name, authorization := range map[string]string{
		"stale session": "Session not-a-session",
		"empty session": "Session",
		"stale API key": "Bearer ridu_missing_secret",
	} {
		t.Run(name, func(t *testing.T) {
			headers := map[string]string{"Authorization": authorization, "Cookie": cookie}
			for _, path := range []string{"/api/auth/me", "/api/collections/media"} {
				response := fixture.do(t, http.MethodGet, path, "", headers)
				if response.StatusCode != http.StatusUnauthorized || responseErrorCode(t, response) != protocol.ErrorInvalidCredential {
					t.Fatalf("%s with an invalid explicit credential = %d", path, response.StatusCode)
				}
			}
		})
	}

	// A stale token cannot block signing in again.
	relogin := fixture.do(t, http.MethodPost, "/api/auth/users/login", `{"email":"header@example.test","password":"correct-horse","transport":"token"}`, map[string]string{"Authorization": "Session not-a-session"})
	if relogin.StatusCode != http.StatusOK {
		t.Fatalf("login with stale credential = %d: %s", relogin.StatusCode, readBody(t, relogin))
	}
	unknown := fixture.do(t, http.MethodPost, "/api/auth/users/login", `{"email":"header@example.test","password":"correct-horse","transport":"jwt"}`, nil)
	if unknown.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown login transport = %d", unknown.StatusCode)
	}
	// Header logout revokes only the presented session and leaves the cookie.
	logout := fixture.do(t, http.MethodPost, "/api/auth/logout", "", both)
	if logout.StatusCode != http.StatusOK || len(logout.Cookies()) != 0 {
		t.Fatalf("header logout touched the cookie: %#v", logout.Cookies())
	}
	if still := fixture.do(t, http.MethodGet, "/api/auth/me", "", map[string]string{"Cookie": cookie}); still.StatusCode != http.StatusOK {
		t.Fatalf("header logout revoked the cookie session: %d", still.StatusCode)
	}
}

func TestSessionOutageIsNotReportedAsInvalidCredential(t *testing.T) {
	fixture := newTransportFixture(t)
	fixture.user(t, "outage@example.test")
	issued := fixture.tokenLogin(t, "outage@example.test")
	fixture.backend.armFindFailure(errors.New("database unavailable"), nil)
	response := fixture.do(t, http.MethodGet, "/api/auth/me", "", sessionHeader(issued.Token))
	if response.StatusCode < http.StatusInternalServerError {
		t.Fatalf("session read during an outage = %d: %s", response.StatusCode, readBody(t, response))
	}
	if recovered := fixture.do(t, http.MethodGet, "/api/auth/me", "", sessionHeader(issued.Token)); recovered.StatusCode != http.StatusOK {
		t.Fatalf("session did not survive the outage: %d", recovered.StatusCode)
	}
}

func TestCORSPreflightIsCacheableAndExposesRequestContext(t *testing.T) {
	fixture := newTransportFixture(t)
	preflight := fixture.do(t, http.MethodOptions, "/api/collections/media", "", map[string]string{
		"Origin": "https://app.example.test", "Access-Control-Request-Method": "PATCH",
		"Access-Control-Request-Headers": "authorization, content-type, if-match",
	})
	if preflight.StatusCode != http.StatusNoContent || preflight.Header.Get("Access-Control-Max-Age") != "600" {
		t.Fatalf("preflight = %d max-age %q", preflight.StatusCode, preflight.Header.Get("Access-Control-Max-Age"))
	}
	actual := fixture.do(t, http.MethodGet, "/api/collections/media", "", map[string]string{"Origin": "https://app.example.test"})
	if exposed := actual.Header.Get("Access-Control-Expose-Headers"); !strings.Contains(exposed, "X-Request-ID") || !strings.Contains(exposed, "Content-Disposition") {
		t.Fatalf("exposed headers = %q", exposed)
	}
}

func TestUploadGrantsDeliverPrivateObjectsWithoutTheSessionToken(t *testing.T) {
	fixture := newTransportFixture(t)
	owner := fixture.user(t, "owner@example.test")
	fixture.user(t, "other@example.test")
	identity := &ridu.AuthIdentity{Collection: "users", Actor: owner}
	source := image.NewRGBA(image.Rect(0, 0, 4, 2))
	source.Set(0, 0, color.RGBA{R: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		t.Fatal(err)
	}
	document, err := fixture.application.UploadForIdentity(context.Background(), "media", ridu.UploadInput{
		Filename: "photo.png", Reader: bytes.NewReader(encoded.Bytes()), Data: store.Values{"owner": store.String(owner.ID)},
	}, identity)
	if err != nil {
		t.Fatal(err)
	}
	issued := fixture.tokenLogin(t, "owner@example.test")
	header := sessionHeader(issued.Token)

	mint := fixture.do(t, http.MethodPost, "/api/uploads/media/grants", `{"items":[{"id":"`+document.ID+`"},{"id":"`+document.ID+`","size":"thumb"}],"expiresIn":120}`, header)
	if mint.StatusCode != http.StatusOK || mint.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("mint grants = %d: %s", mint.StatusCode, readBody(t, mint))
	}
	var grants protocol.UploadGrantsEnvelope
	decodeResponse(t, mint, &grants)
	if len(grants.Grants) != 2 || grants.Grants[1].Size != "thumb" {
		t.Fatalf("grants = %#v", grants)
	}
	for _, grant := range grants.Grants {
		if strings.Contains(grant.URL, issued.Token) {
			t.Fatalf("grant URL contains the session token: %s", grant.URL)
		}
		expires, err := time.Parse(time.RFC3339, grant.ExpiresAt)
		if err != nil || expires.Before(time.Now().Add(time.Minute)) || expires.After(time.Now().Add(3*time.Minute)) {
			t.Fatalf("grant expiry = %q, %v", grant.ExpiresAt, err)
		}
		delivered := fixture.do(t, http.MethodGet, grant.URL, "", nil)
		if delivered.StatusCode != http.StatusOK || delivered.Header.Get("Content-Type") != "image/png" {
			t.Fatalf("grant delivery = %d: %s", delivered.StatusCode, readBody(t, delivered))
		}
		readBody(t, delivered)
	}
	if anonymous := fixture.do(t, http.MethodGet, strings.Split(grants.Grants[0].URL, "?")[0], "", nil); anonymous.StatusCode != http.StatusUnauthorized {
		t.Fatalf("private object without a grant = %d", anonymous.StatusCode)
	}

	tampered, err := url.Parse(grants.Grants[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	grantValue := tampered.Query().Get("grant")
	query := tampered.Query()
	query.Set("grant", grantValue[:len(grantValue)-2]+"AA")
	tampered.RawQuery = query.Encode()
	if forged := fixture.do(t, http.MethodGet, tampered.String(), "", nil); forged.StatusCode != http.StatusUnauthorized || responseErrorCode(t, forged) != protocol.ErrorInvalidCredential {
		t.Fatal("tampered grant was accepted")
	}
	// A grant for the original cannot authorize the thumbnail's object.
	thumbPath := strings.Split(grants.Grants[1].URL, "?")[0]
	if swapped := fixture.do(t, http.MethodGet, thumbPath+"?grant="+url.QueryEscape(grantValue), "", nil); swapped.StatusCode != http.StatusUnauthorized {
		t.Fatalf("grant was reusable for another object: %d", swapped.StatusCode)
	}

	other := fixture.tokenLogin(t, "other@example.test")
	if denied := fixture.do(t, http.MethodPost, "/api/uploads/media/grants", `{"items":[{"id":"`+document.ID+`"}]}`, sessionHeader(other.Token)); denied.StatusCode != http.StatusNotFound {
		t.Fatalf("grant for another owner's upload = %d", denied.StatusCode)
	}
	for name, body := range map[string]string{
		"missing size":    `{"items":[{"id":"` + document.ID + `","size":"poster"}]}`,
		"empty items":     `{"items":[]}`,
		"too long":        `{"items":[{"id":"` + document.ID + `"}],"expiresIn":7200}`,
		"negative expiry": `{"items":[{"id":"` + document.ID + `"}],"expiresIn":-1}`,
	} {
		if invalid := fixture.do(t, http.MethodPost, "/api/uploads/media/grants", body, header); invalid.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("%s = %d: %s", name, invalid.StatusCode, readBody(t, invalid))
		}
	}
	if unauthenticated := fixture.do(t, http.MethodPost, "/api/uploads/media/grants", `{"items":[{"id":"`+document.ID+`"}]}`, nil); unauthenticated.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous grant request = %d", unauthenticated.StatusCode)
	}

	// Revocation ends every grant minted from the session.
	if logout := fixture.do(t, http.MethodPost, "/api/auth/logout", "", header); logout.StatusCode != http.StatusOK {
		t.Fatalf("logout = %d", logout.StatusCode)
	}
	if revoked := fixture.do(t, http.MethodGet, grants.Grants[0].URL, "", nil); revoked.StatusCode != http.StatusUnauthorized || responseErrorCode(t, revoked) != protocol.ErrorInvalidCredential {
		t.Fatal("grant outlived its session")
	}
}

func TestUploadGrantFollowsCurrentReadAccess(t *testing.T) {
	fixture := newTransportFixture(t)
	owner := fixture.user(t, "access@example.test")
	identity := &ridu.AuthIdentity{Collection: "users", Actor: owner}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	document, err := fixture.application.UploadForIdentity(context.Background(), "media", ridu.UploadInput{
		Filename: "gone.png", Reader: bytes.NewReader(encoded.Bytes()), Data: store.Values{"owner": store.String(owner.ID)},
	}, identity)
	if err != nil {
		t.Fatal(err)
	}
	issued := fixture.tokenLogin(t, "access@example.test")
	mint := fixture.do(t, http.MethodPost, "/api/uploads/media/grants", `{"items":[{"id":"`+document.ID+`"}]}`, sessionHeader(issued.Token))
	var grants protocol.UploadGrantsEnvelope
	decodeResponse(t, mint, &grants)
	if _, err := fixture.application.Local().Delete(context.Background(), "media", document.ID, ridu.MutationOptions{Actor: &owner, ActorCollection: "users"}); err != nil {
		t.Fatal(err)
	}
	response := fixture.do(t, http.MethodGet, grants.Grants[0].URL, "", nil)
	if response.StatusCode == http.StatusOK {
		t.Fatal("grant delivered an object after its document was deleted")
	}
	var envelope struct {
		Error json.RawMessage `json:"error"`
	}
	decodeResponse(t, response, &envelope)
}
