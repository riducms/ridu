package generate

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/riducms/ridu/internal/projectfile"
)

func TestDiscoverProjectPreservesCommandErrorIdentity(t *testing.T) {
	for _, diagnostic := range []string{"", "invalid config\n"} {
		t.Run(diagnostic, func(t *testing.T) {
			cause := fmt.Errorf("command interrupted: %w", context.Canceled)
			_, err := discoverProjectOperation(context.Background(), projectfile.File{}, "test", "manifest", nil, func(context.Context, string, []string) ([]byte, []byte, error) { return nil, []byte(diagnostic), cause })
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation identity: %v", err)
			}
			want := "project manifest command failed: "
			if diagnostic != "" {
				want += "invalid config: "
			}
			want += cause.Error()
			if err.Error() != want {
				t.Fatalf("diagnostic=%q want %q", err, want)
			}
		})
	}
}
