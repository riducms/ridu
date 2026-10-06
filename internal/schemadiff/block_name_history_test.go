package schemadiff_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/schemadiff"
	"github.com/riducms/ridu/schema"
)

func TestReadManifestPreservesPriorGeneratedBlockSchema(t *testing.T) {
	current, err := ridu.Resolve(ridu.Config{
		Name: "Historical generated schema",
		Collections: []ridu.Collection{{
			Slug: "pages", Fields: field.Fields{
				field.Blocks("layout", field.Block{Slug: "card", Fields: field.Fields{field.Text("title")}}),
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	previousSnapshot := current.Snapshot()
	block := &previousSnapshot.Blocks[0]
	if previousSnapshot.Collections[0].Fields[0].Blocks.BlockReferences[0] != block.Slug {
		t.Fatalf("the inline block was not recorded as the container's definition: %#v", previousSnapshot.Blocks)
	}
	if len(block.Fields) != 2 || block.Fields[1].Name != "blockName" {
		t.Fatalf("unexpected current block fields: %#v", block.Fields)
	}
	block.Fields = block.Fields[:1]
	previous := schema.NewManifest(previousSnapshot)
	previousBytes, err := previous.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := schema.Parse(previousBytes); err == nil {
		t.Fatal("current parser accepted a prior generated schema without blockName")
	}
	path := filepath.Join(t.TempDir(), "ridu.schema.json")
	if err := os.WriteFile(path, previousBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	read, exists, err := schemadiff.ReadManifest(path)
	if err != nil || !exists {
		t.Fatalf("read prior generated manifest: exists=%t, error=%v", exists, err)
	}
	readBytes, err := read.Bytes()
	if err != nil || !bytes.Equal(readBytes, previousBytes) {
		t.Fatalf("prior generated manifest changed: %v\ngot: %s\nwant: %s", err, readBytes, previousBytes)
	}
	compact, err := schemadiff.CompactManifest(previous)
	if err != nil {
		t.Fatalf("compact prior development manifest: %v", err)
	}
	compactBytes, err := compact.Bytes()
	if err != nil || !bytes.Equal(compactBytes, previousBytes) {
		t.Fatalf("compaction added a field to historical schema: %v\ngot: %s\nwant: %s", err, compactBytes, previousBytes)
	}
	if read.SameStorage(current) {
		t.Fatal("added stored blockName child was hidden from schema comparison")
	}
}
