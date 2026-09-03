package migration

import (
	"strings"
	"testing"
	"time"
)

func TestProjectRequestValidatesDatabaseSelectionWithoutExposingIt(t *testing.T) {
	secretURL := "mongodb://project-user:project-password@example.invalid/ridu"
	tests := []struct {
		name    string
		request ProjectRequest
		want    string
	}{
		{name: "file apply", request: ProjectRequest{Action: ProjectApply, DatabasePath: "/tmp/ridu.sqlite", Directory: "/tmp/migrations"}},
		{name: "network apply", request: ProjectRequest{Action: ProjectApply, DatabaseURL: secretURL, Directory: "/tmp/migrations"}},
		{name: "offline verify", request: ProjectRequest{Action: ProjectVerify, Directory: "/tmp/migrations"}},
		{name: "network verify", request: ProjectRequest{Action: ProjectVerify, DatabaseURL: secretURL, Directory: "/tmp/migrations"}},
		{name: "missing database", request: ProjectRequest{Action: ProjectApply, Directory: "/tmp/migrations"}, want: "requires a database path or URL"},
		{name: "ambiguous database", request: ProjectRequest{Action: ProjectApply, DatabasePath: "/tmp/ridu.sqlite", DatabaseURL: secretURL, Directory: "/tmp/migrations"}, want: "cannot select both"},
		{name: "negative lock wait", request: ProjectRequest{Action: ProjectApply, DatabaseURL: secretURL, Directory: "/tmp/migrations", LockWait: -time.Second}, want: "non-negative runtime bounds"},
		{name: "negative operation timeout", request: ProjectRequest{Action: ProjectVerify, DatabaseURL: secretURL, Directory: "/tmp/migrations", OperationTimeout: -time.Second}, want: "non-negative runtime bounds"},
		{name: "missing directory", request: ProjectRequest{Action: ProjectVerify, DatabaseURL: secretURL}, want: "requires an artifact directory"},
		{name: "unknown action", request: ProjectRequest{Action: "restore", DatabaseURL: secretURL, Directory: "/tmp/migrations"}, want: "unknown project migration action"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.request.Validate()
			if test.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() error = %v, want containing %q", err, test.want)
			}
			if strings.Contains(err.Error(), secretURL) || strings.Contains(err.Error(), "project-password") {
				t.Fatalf("Validate() exposed the credential-bearing database selection: %v", err)
			}
		})
	}
}
