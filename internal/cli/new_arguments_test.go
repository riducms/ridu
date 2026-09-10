package cli

import (
	"flag"
	"io"
	"reflect"
	"testing"
)

func TestNewProjectOptionsAroundDirectory(t *testing.T) {
	for _, test := range []struct {
		name      string
		args      []string
		database  string
		noAgent   bool
		directory []string
		wantError bool
	}{
		{name: "directory first", args: []string{"content", "--database", "sqlite", "--no-agent"}, database: "sqlite", noAgent: true, directory: []string{"content"}},
		{name: "mixed", args: []string{"--no-agent", "content", "--database=sqlite"}, database: "sqlite", noAgent: true, directory: []string{"content"}},
		{name: "directory last", args: []string{"--database", "sqlite", "content"}, database: "sqlite", directory: []string{"content"}},
		{name: "boolean equals", args: []string{"content", "--no-agent=false"}, directory: []string{"content"}},
		{name: "dash value", args: []string{"content", "--database", "-value"}, database: "-value", directory: []string{"content"}},
		{name: "explicit terminator", args: []string{"--database", "sqlite", "--", "--content"}, database: "sqlite", directory: []string{"--content"}},
		{name: "terminator after directory", args: []string{"content", "--", "--no-agent"}, directory: []string{"content", "--no-agent"}},
		{name: "terminator as value", args: []string{"content", "--database", "--", "--no-agent"}, database: "--", noAgent: true, directory: []string{"content"}},
		{name: "multiple directories retained for command validation", args: []string{"one", "--no-agent", "two"}, noAgent: true, directory: []string{"one", "two"}},
		{name: "unknown", args: []string{"content", "--unknown"}, wantError: true},
		{name: "missing value", args: []string{"content", "--database"}, wantError: true},
		{name: "help after directory", args: []string{"content", "--help"}, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			flags := flag.NewFlagSet("new", flag.ContinueOnError)
			flags.SetOutput(io.Discard)
			database := flags.String("database", "", "database")
			noAgent := flags.Bool("no-agent", false, "skip agent")
			err := parseNewProjectFlags(flags, test.args)
			if test.wantError {
				if err == nil {
					t.Fatal("expected flag error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if *database != test.database || *noAgent != test.noAgent || !reflect.DeepEqual(flags.Args(), test.directory) {
				t.Fatalf("got database=%q no-agent=%t directory=%v", *database, *noAgent, flags.Args())
			}
		})
	}
}
