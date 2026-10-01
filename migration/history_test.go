package migration

import (
	"strings"
	"testing"

	"github.com/riducms/ridu/schema"
)

func TestDigestArtifactHistoryAuthenticatesHistoryHeadAndStorageSchema(t *testing.T) {
	history := []ArtifactIdentity{
		{Name: "20260801000000.000000000_initial.ridu.json", Digest: strings.Repeat("a", 64)},
		{Name: "20260802000000.000000000_data-only.ridu.json", Digest: strings.Repeat("b", 64)},
	}
	manifest := schema.NewManifest(schema.Snapshot{Version: schema.CurrentVersion, Application: schema.Application{Name: "History"}, Collections: []schema.Collection{{ID: "posts", Slug: "posts"}}})
	headDigest := strings.Repeat("f", 64)
	// The CLI links this digest into the executable and the runtime recomputes
	// it at startup, so its encoding must only change deliberately.
	const want = "79cc699da1ad5df099158aee63edd23a3bd99b468c94f4e7dcdec1ca61c33e7a"
	if got, err := DigestArtifactHistory(history, headDigest, manifest); err != nil || got != want {
		t.Fatalf("history digest = %q, %v; want %q", got, err, want)
	}

	variants := map[string][]ArtifactIdentity{
		"missing": history[:1],
		"renamed": {
			{Name: "20260801000000.000000000_initial-renamed.ridu.json", Digest: history[0].Digest},
			{Name: history[1].Name, Digest: history[1].Digest},
		},
		"altered digest": {
			history[0],
			{Name: history[1].Name, Digest: strings.Repeat("c", 64)},
		},
	}
	for name, variant := range variants {
		t.Run(name, func(t *testing.T) {
			got, err := DigestArtifactHistory(variant, headDigest, manifest)
			if err != nil {
				t.Fatal(err)
			}
			if got == want {
				t.Fatalf("changed history retained digest %s", got)
			}
		})
	}

	reordered := []ArtifactIdentity{history[1], history[0]}
	if _, err := DigestArtifactHistory(reordered, headDigest, manifest); err == nil || !strings.Contains(err.Error(), "strictly ordered") {
		t.Fatalf("reordered history error = %v", err)
	}
	if got, err := DigestArtifactHistory(history, strings.Repeat("e", 64), manifest); err != nil || got == want {
		t.Fatalf("changed ledger head retained history identity: %q, %v", got, err)
	}
	snapshot := manifest.Snapshot()
	snapshot.Collections[0].Admin = schema.CollectionAdmin{Hidden: true, Group: "Editorial"}
	if got, err := DigestArtifactHistory(history, headDigest, schema.NewManifest(snapshot)); err != nil || got != want {
		t.Fatalf("admin presentation changed history identity: %q, %v", got, err)
	}
	snapshot.Application.AllowIDOnCreate = true
	if got, err := DigestArtifactHistory(history, headDigest, schema.NewManifest(snapshot)); err != nil || got == want {
		t.Fatalf("runtime schema change retained history identity: %q, %v", got, err)
	}
}

func TestDigestArtifactHistoryRejectsMalformedIdentity(t *testing.T) {
	valid := ArtifactIdentity{Name: "20260801000000.000000000_initial.ridu.json", Digest: strings.Repeat("a", 64)}
	tests := map[string][]ArtifactIdentity{
		"empty history":    nil,
		"empty name":       {{Digest: valid.Digest}},
		"whitespace name":  {{Name: " initial", Digest: valid.Digest}},
		"control name":     {{Name: "initial\n", Digest: valid.Digest}},
		"malformed digest": {{Name: valid.Name, Digest: strings.Repeat("A", 64)}},
		"duplicate name":   {valid, valid},
	}
	for name, history := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := DigestArtifactHistory(history, strings.Repeat("f", 64), schema.Manifest{}); err == nil {
				t.Fatal("malformed history was accepted")
			}
		})
	}
	for _, head := range []string{"", strings.Repeat("A", 64), "not-a-digest"} {
		if _, err := DigestArtifactHistory([]ArtifactIdentity{valid}, head, schema.Manifest{}); err == nil {
			t.Fatalf("malformed head digest %q was accepted", head)
		}
	}
}
