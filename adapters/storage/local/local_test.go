package local_test

import (
	"context"
	"io"
	"strings"
	"testing"

	localstorage "github.com/riducms/ridu/adapters/storage/local"
)

func TestBackendRoundTripAndRejectsTraversal(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := backend.Put(context.Background(), "media/a.txt", strings.NewReader("hello"), 5, "text/plain"); err != nil {
		t.Fatal(err)
	}
	reader, object, err := backend.Open(context.Background(), "media/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if string(encoded) != "hello" || object.Size != 5 || object.ContentType != "text/plain; charset=utf-8" {
		t.Fatalf("object = %q (%d)", encoded, object.Size)
	}
	if err := backend.Put(context.Background(), "media/plain.html", strings.NewReader("not markup"), 10, "text/plain"); err != nil {
		t.Fatal(err)
	}
	plain, plainObject, err := backend.Open(context.Background(), "media/plain.html")
	if err != nil {
		t.Fatal(err)
	}
	if err := plain.Close(); err != nil {
		t.Fatal(err)
	}
	if plainObject.ContentType != "text/plain; charset=utf-8" {
		t.Fatalf("extension overrode sniffed content type: %q", plainObject.ContentType)
	}
	if err := backend.Put(context.Background(), "../escape", strings.NewReader("x"), 1, "text/plain"); err == nil {
		t.Fatal("expected traversal rejection")
	}
}

func TestPutRequiresTheDeclaredSizeAndPreservesExistingObject(t *testing.T) {
	backend, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Put(context.Background(), "object.txt", strings.NewReader("safe"), 4, "text/plain"); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		value    string
		declared int64
	}{
		{name: "short", value: "no", declared: 3},
		{name: "long", value: "unsafe", declared: 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := backend.Put(context.Background(), "object.txt", strings.NewReader(test.value), test.declared, "text/plain")
			if err == nil || !strings.Contains(err.Error(), "size mismatch") {
				t.Fatalf("Put error = %v", err)
			}
			reader, _, err := backend.Open(context.Background(), "object.txt")
			if err != nil {
				t.Fatal(err)
			}
			contents, readError := io.ReadAll(reader)
			closeError := reader.Close()
			if readError != nil || closeError != nil {
				t.Fatalf("read=%v close=%v", readError, closeError)
			}
			if string(contents) != "safe" {
				t.Fatalf("existing object = %q", contents)
			}
		})
	}
}
