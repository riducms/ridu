//go:build windows

package sqlite

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSQLiteDSNWritesWindowsPathsAsSQLiteFileURIs(t *testing.T) {
	for _, test := range []struct {
		input    string
		dsn      string
		filePath string
	}{
		{input: `C:\Users\ridu\my app\.ridu\development.sqlite`, dsn: "file:///C:/Users/ridu/my%20app/.ridu/development.sqlite", filePath: `C:\Users\ridu\my app\.ridu\development.sqlite`},
		{input: "file:///C:/Users/ridu/app.sqlite?mode=rwc", dsn: "file:///C:/Users/ridu/app.sqlite?mode=rwc", filePath: `C:\Users\ridu\app.sqlite`},
		{input: "file:C:/Users/ridu/app.sqlite", dsn: "file:C:/Users/ridu/app.sqlite", filePath: `C:\Users\ridu\app.sqlite`},
		{input: `\\server\share\ridu\app.sqlite`, dsn: "file:////server/share/ridu/app.sqlite", filePath: `\\server\share\ridu\app.sqlite`},
		{input: "file://server/share/ridu/app.sqlite?mode=rwc", dsn: "file:////server/share/ridu/app.sqlite?mode=rwc", filePath: `\\server\share\ridu\app.sqlite`},
	} {
		dsn, filePath, memory, err := sqliteDSN(test.input)
		if err != nil || memory || dsn != test.dsn || filePath != test.filePath {
			t.Fatalf("sqliteDSN(%q) = %q, %q, %t, %v; want %q, %q", test.input, dsn, filePath, memory, err, test.dsn, test.filePath)
		}
	}
}

func TestSQLiteOpensAndInspectsAbsoluteWindowsPaths(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "development.sqlite")
	backend, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	inspection, exists, err := openSQLiteArtifactInspection(ctx, path)
	if err != nil || !exists {
		t.Fatalf("inspect %q = %t, %v", path, exists, err)
	}
	if err := inspection.Close(); err != nil {
		t.Fatal(err)
	}
}
