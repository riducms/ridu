package migration

import (
	"strings"
	"testing"
)

func TestDigestArtifactHistoryAuthenticatesExactOrderedPairs(t *testing.T) {
	history := []ArtifactIdentity{
		{Name: "20260801000000.000000000_initial.ridu.json", Digest: strings.Repeat("a", 64)},
		{Name: "20260802000000.000000000_data-only.ridu.json", Digest: strings.Repeat("b", 64)},
	}
	const want = "c9b4d94b5d017b830a48bb57cbefee53b751d2de0cdafa2b431291f50b19cf55"
	if got, err := DigestArtifactHistory(history); err != nil || got != want {
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
			got, err := DigestArtifactHistory(variant)
			if err != nil {
				t.Fatal(err)
			}
			if got == want {
				t.Fatalf("changed history retained digest %s", got)
			}
		})
	}

	reordered := []ArtifactIdentity{history[1], history[0]}
	if _, err := DigestArtifactHistory(reordered); err == nil || !strings.Contains(err.Error(), "strictly ordered") {
		t.Fatalf("reordered history error = %v", err)
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
			if _, err := DigestArtifactHistory(history); err == nil {
				t.Fatal("malformed history was accepted")
			}
		})
	}
}
