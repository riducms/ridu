package core_test

import (
	"net/http"
	"strings"
	"testing"

	ridu "github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
	"github.com/riducms/ridu/protocol"
	"golang.org/x/crypto/bcrypt"
)

func TestRESTAuthCreateUserHonorsDraftIntentWithoutStoringCredentials(t *testing.T) {
	app, err := ridu.New(ridu.Config{
		Name: "auth draft transport", Admin: ridu.AdminConfig{User: "users"},
		Localization: ridu.LocalizationConfig{DefaultLocale: "en", Locales: []ridu.Locale{{Code: "en", Label: "English"}, {Code: "fr", Label: "French"}}},
		Collections: []ridu.Collection{{
			Slug: "users", Auth: true, AuthConfig: ridu.AuthConfig{Password: ridu.PasswordPolicy{BcryptCost: bcrypt.MinCost}},
			Versions: true, VersionConfig: ridu.VersionConfig{Drafts: true},
			Fields: field.Fields{field.Email("email").Required().Unique(), field.Text("displayName").Localized().Required()},
		}}}, teststore.New())
	if err != nil {
		t.Fatal(err)
	}
	client := handlerClient(app.Handler(ridu.HandlerOptions{}))
	create := func(target, body, cookie string, want int) map[string]any {
		t.Helper()
		response := requestJSON(t, client, http.MethodPost, target, strings.NewReader(body), cookie)
		if response.StatusCode != want {
			t.Fatalf("auth create %s = %d, want %d: %s", target, response.StatusCode, want, readBody(t, response))
		}
		if want != http.StatusCreated {
			response.Body.Close()
			return nil
		}
		var envelope protocol.DocumentEnvelope[map[string]any]
		decodeResponse(t, response, &envelope)
		if _, present := envelope.Doc["password"]; present {
			t.Fatalf("auth response exposed password: %#v", envelope.Doc)
		}
		return envelope.Doc
	}
	base := "http://ridu.test/api/auth/users/create-user"
	first := create(base, `{"data":{"email":"first@example.test"},"password":"correct-horse"}`, "", http.StatusCreated)
	if first["_status"] != "draft" || first["displayName"] != nil {
		t.Fatalf("default auth draft = %#v", first)
	}
	login := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/auth/users/login", strings.NewReader(`{"email":"first@example.test","password":"correct-horse"}`), "")
	if login.StatusCode != http.StatusOK || len(login.Cookies()) != 1 {
		t.Fatalf("draft auth credential login = %d: %s", login.StatusCode, readBody(t, login))
	}
	cookie := login.Cookies()[0].String()
	login.Body.Close()
	second := create(base+"?draft=true", `{"data":{"email":"second@example.test"},"password":"correct-horse"}`, cookie, http.StatusCreated)
	if second["_status"] != "draft" {
		t.Fatalf("explicit auth draft = %#v", second)
	}
	secondLogin := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/auth/users/login", strings.NewReader(`{"email":"second@example.test","password":"correct-horse"}`), "")
	if secondLogin.StatusCode != http.StatusOK {
		t.Fatalf("explicit draft auth credential login = %d: %s", secondLogin.StatusCode, readBody(t, secondLogin))
	}
	secondLogin.Body.Close()
	french := create(base+"?draft=true&locale=fr", `{"data":{"email":"french@example.test","displayName":"Bonjour"},"password":"correct-horse"}`, cookie, http.StatusCreated)
	if french["_status"] != "draft" || french["displayName"] != "Bonjour" {
		t.Fatalf("draft auth create lost selected French locale: %#v", french)
	}
	create(base+"?draft=false", `{"data":{"email":"incomplete@example.test"},"password":"correct-horse"}`, cookie, http.StatusUnprocessableEntity)
	published := create(base+"?draft=false", `{"data":{"email":"published@example.test","displayName":"Published"},"password":"correct-horse"}`, cookie, http.StatusCreated)
	if published["_status"] != "published" {
		t.Fatalf("explicitly published auth user = %#v", published)
	}
	create(base+"?draft=invalid", `{"data":{"email":"invalid-query@example.test"},"password":"correct-horse"}`, cookie, http.StatusBadRequest)
	create(base+"?draft=true&draft=false", `{"data":{"email":"repeated-query@example.test"},"password":"correct-horse"}`, cookie, http.StatusBadRequest)
	create(base+"?draft=true", `{"data":{},"password":"correct-horse"}`, cookie, http.StatusUnprocessableEntity)
	create(base+"?draft=true", `{"data":{"email":"no-password@example.test"}}`, cookie, http.StatusUnprocessableEntity)
	generic := requestJSON(t, client, http.MethodPost, "http://ridu.test/api/collections/users?draft=true", strings.NewReader(`{"email":"generic@example.test"}`), cookie)
	if generic.StatusCode != http.StatusBadRequest {
		t.Fatalf("generic auth collection create = %d: %s", generic.StatusCode, readBody(t, generic))
	}
	generic.Body.Close()
}
