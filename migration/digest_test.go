package migration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/riducms/ridu/schema"
)

func TestCanonicalIndentMatchesMarshalIndent(t *testing.T) {
	for _, value := range []any{
		map[string]any{},
		[]any{},
		map[string]any{"empty": map[string]any{}, "list": []any{}, "nested": []any{map[string]any{"a": []any{1, 2.5, nil, true}}}},
		map[string]any{"escaped": "quote \" backslash \\ {[,:]} <html> &   é", "key with \"quote\"": "\\"},
		[]any{"", "\\\\", "\\\"", json.RawMessage(`{"raw":[{},[]]}`)},
	} {
		compact, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		expected, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		var actual bytes.Buffer
		writeCanonicalIndent(&actual, compact)
		if !bytes.Equal(actual.Bytes(), append(expected, '\n')) {
			t.Fatalf("canonical indent differs:\n%s\nwant:\n%s", actual.Bytes(), expected)
		}
	}
}

func TestDigestManifestHashesTheCanonicalBytes(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "testdata", "manifests", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	example := filepath.Join("..", "examples", "blocks", "generated", "ridu.schema.json")
	paths = append(paths, example)
	checked := 0
	for _, path := range paths {
		encoded, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		manifest, err := schema.ParseHistorical(encoded)
		if err != nil {
			if path == example {
				t.Fatalf("parse %s: %v", path, err)
			}
			continue
		}
		canonical, err := manifest.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		expected := sha256.Sum256(canonical)
		digest, err := DigestManifest(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if digest != hex.EncodeToString(expected[:]) {
			t.Fatalf("%s digest = %s, want the canonical bytes digest %x", path, digest, expected)
		}
		checked++
	}
	if checked < 2 {
		t.Fatalf("checked %d manifests", checked)
	}
}
