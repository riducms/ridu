//go:build windows

package cli

import "testing"

func TestResolveSQLiteMigrationDatabasePathWritesWindowsFileURIs(t *testing.T) {
	for _, test := range []struct {
		projectRoot string
		input       string
		resolved    string
	}{
		{projectRoot: `C:\Users\ridu\my app`, input: "file:.ridu/development.sqlite", resolved: "file:///C:/Users/ridu/my%20app/.ridu/development.sqlite"},
		{projectRoot: `\\server\share\app`, input: "file:.ridu/development.sqlite?mode=rwc", resolved: "file://server/share/app/.ridu/development.sqlite?mode=rwc"},
		{projectRoot: `C:\Users\ridu\app`, input: "file:///D:/data/app.sqlite", resolved: "file:///D:/data/app.sqlite"},
		{projectRoot: `C:\Users\ridu\app`, input: "file:D:/data/app.sqlite", resolved: "file:D:/data/app.sqlite"},
		{projectRoot: `C:\Users\ridu\app`, input: `D:\data\app.sqlite`, resolved: `D:\data\app.sqlite`},
	} {
		resolved, err := resolveSQLiteMigrationDatabasePath(test.projectRoot, test.input)
		if err != nil || resolved != test.resolved {
			t.Fatalf("resolve %q in %q = %q, %v; want %q", test.input, test.projectRoot, resolved, err, test.resolved)
		}
	}
}
