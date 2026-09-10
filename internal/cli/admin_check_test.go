package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/riducms/ridu/internal/projectfile"
)

func TestEditorCheckRunsApplicationBuildAndRequiresFreshEvidence(t *testing.T) {
	for _, command := range []string{"bun", "node"} {
		if _, err := exec.LookPath(command); err != nil {
			t.Skipf("%s is required for the application build script contract", command)
		}
	}
	root := t.TempDir()
	admin := filepath.Join(root, "admin")
	if err := os.MkdirAll(admin, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, contents string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(root, "canonical.json"), "canonical schema")
	write(filepath.Join(admin, "package.json"), `{"scripts":{"build":"RIDU_TEST_BUILD_SETTING=staging node build.mjs"}}`)
	definition := projectfile.File{Root: root, Admin: "admin", Schema: "canonical.json", PackageManager: projectfile.PackageManagerBun}
	for _, scenario := range []struct {
		name    string
		script  string
		failure string
	}{
		{"verified", `import {readFileSync,writeFileSync} from 'node:fs';
if (process.env.RIDU_TEST_BUILD_SETTING !== 'staging') throw new Error('build script environment lost');
if (readFileSync(process.env.RIDU_ADMIN_CHECK_SCHEMA,'utf8') !== 'canonical schema') throw new Error('wrong manifest');
writeFileSync(process.env.RIDU_ADMIN_CHECK_RECEIPT,'checked\n');`, ""},
		{"skipped hook after previous success", `console.log('build skipped verification');`, "did not verify admin registrations"},
		{"failed build after verification", `import {writeFileSync} from 'node:fs'; writeFileSync(process.env.RIDU_ADMIN_CHECK_RECEIPT,'checked\n'); process.exit(7);`, "exit status"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			write(filepath.Join(admin, "build.mjs"), scenario.script)
			var output bytes.Buffer
			err := checkAdminRegistrations(context.Background(), definition, &output, &output)
			if scenario.failure == "" && err != nil {
				t.Fatalf("verification failed: %v\n%s", err, output.String())
			}
			if scenario.failure != "" && (err == nil || !strings.Contains(err.Error(), scenario.failure)) {
				t.Fatalf("verification error = %v, want %q\n%s", err, scenario.failure, output.String())
			}
			entries, err := os.ReadDir(filepath.Join(root, ".ridu"))
			if err != nil || len(entries) != 0 {
				t.Fatalf("verification left temporary output: %v, %v", entries, err)
			}
		})
	}
}
