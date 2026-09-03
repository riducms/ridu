package core

import (
	"testing"

	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/internal/teststore"
)

func TestNewRegistersScheduledPublishingOnlyForVersionedCollections(t *testing.T) {
	backend := teststore.New()
	withoutVersions, err := New(Config{
		Name: "Plain",
		Collections: []Collection{{
			Slug:   "posts",
			Fields: []field.Definition{field.Text("title")},
		}},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := withoutVersions.taskRegistry[builtinPublishTask]; exists {
		t.Fatal("plain application registered the scheduled-publish task")
	}

	withVersions, err := New(Config{
		Name: "Versioned",
		Collections: []Collection{{
			Slug:     "posts",
			Versions: true,
			Fields:   []field.Definition{field.Text("title")},
		}},
	}, backend)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := withVersions.taskRegistry[builtinPublishTask]; !exists {
		t.Fatal("versioned application did not register the scheduled-publish task")
	}
}
