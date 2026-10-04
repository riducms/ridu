package generate

import (
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
)

func TestGeneratedGoAuthDraftKeepsIdentityNonNull(t *testing.T) {
	manifest, err := core.Resolve(core.Config{Name: "Auth drafts", Admin: core.AdminConfig{User: "users"}, Collections: []core.Collection{{
		Slug: "users", Auth: true, Versions: true, VersionConfig: core.VersionConfig{Drafts: true},
		Fields: field.Fields{field.Email("email").Required().Unique(), field.Text("displayName").Required()},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := goClient(manifest)
	if err != nil {
		t.Fatal(err)
	}
	runGeneratedGoConsumer(t, generated, `package generated_test
import ("encoding/json"; "testing"; g "example.com/stable-consumer")
func TestAuthDraftIdentity(t *testing.T) {
 var draft g.UserDraft
 if err:=json.Unmarshal([]byte("{\"email\":null}"),&draft);err==nil {t.Fatal("draft accepted null identity")}
 for _,raw:=range []string{"{}","{\"email\":\"editor@example.test\",\"displayName\":null}"} {
  if err:=json.Unmarshal([]byte(raw),&draft);err!=nil {t.Fatalf("legal draft rejected: %s: %v",raw,err)}
  if _,err:=json.Marshal(draft);err!=nil {t.Fatal(err)}
 }
 email:="editor@example.test"
 draft=g.UserDraft{Email:&email}
 if raw,err:=json.Marshal(draft);err!=nil||string(raw)!="{\"email\":\"editor@example.test\"}" {t.Fatalf("identity encoding = %s, %v",raw,err)}
}
`)
}

func TestGeneratedGoDraftIdentitiesRejectSuppliedInvalidKeys(t *testing.T) {
	generated, err := goClient(draftInputApp(t).Manifest())
	if err != nil {
		t.Fatal(err)
	}
	runGeneratedGoConsumer(t, generated, `package generated_test
import ("encoding/json"; "testing"; g "example.com/stable-consumer")
func TestDraftIdentityAdmission(t *testing.T) {
 for _,raw:=range []string{
  "{\"layout\":[{\"blockType\":\"hero\",\"_key\":null}]}",
  "{\"layout\":[{\"blockType\":\"hero\",\"_key\":\"\"}]}",
  "{\"layout\":[{\"blockType\":\"hero\",\"_key\":\" \"}]}",
  "{\"rows\":[{\"_key\":null}]}",
  "{\"rows\":[{\"_key\":\"\"}]}",
  "{\"rows\":[{\"_key\":\" \"}]}",
 } {
  var draft g.PageDraft
  if err:=json.Unmarshal([]byte(raw),&draft);err==nil {t.Fatalf("draft silently accepted invalid identity: %s",raw)}
 }
 for _,raw:=range []string{
  "{\"layout\":[{\"blockType\":\"hero\"}],\"rows\":[{}]}",
  "{\"layout\":[{\"blockType\":\"hero\",\"_key\":\"hero-1\"}],\"rows\":[{\"_key\":\"row-1\"}]}",
 } {
  var draft g.PageDraft
  if err:=json.Unmarshal([]byte(raw),&draft);err!=nil {t.Fatalf("legal draft identity rejected: %s: %v",raw,err)}
  if _,err:=json.Marshal(draft);err!=nil {t.Fatal(err)}
 }
}
`)
}
