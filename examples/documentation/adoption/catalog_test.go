package adoption

import "testing"

func TestDocumentationFieldCatalogUsesEveryBuiltInConstructor(t *testing.T) {
	if got := len(Fields()); got != 24 {
		t.Fatalf("documentation field definitions = %d, want 24", got)
	}
}
