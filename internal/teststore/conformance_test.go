package teststore

import (
	"testing"

	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
	"github.com/riducms/ridu/store/conformance"
)

func TestStoreConformance(t *testing.T) {
	conformance.Run(t, func(_ *testing.T, _ schema.Manifest) store.Store {
		return New()
	})
}
