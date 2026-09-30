package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/internal/cli"
)

// The ordinary CLI publishes and consumes this checkout through the same local
// release proxy as the other generated-project contracts. No replace directive
// or edited application config hides an obsolete authoring dependency.
func TestFreshUnifiedFieldStarterAndBlank(t *testing.T) {
	frameworkRoot := moduleRoot(t)
	setFrameworkProxy(t, frameworkRoot)
	for _, template := range []string{"starter", "blank"} {
		t.Run(template, func(t *testing.T) {
			target := newProjectTarget(t, "unified-"+template)
			options := cli.Options{WorkingDirectory: target, Version: testReleaseVersion, FrameworkVersion: ridu.FrameworkVersion}
			run := func(args ...string) {
				t.Helper()
				var stdout, stderr bytes.Buffer
				if code := cli.Run(t.Context(), args, &stdout, &stderr, options); code != 0 {
					t.Fatalf("ridu %v: %s\n%s", args, stdout.String(), stderr.String())
				}
			}
			run("new", "--template", template, "--database", "sqlite", "--module", "example.com/unified/"+template, "--scope", "@fixture", "--no-agent", target)
			run("generate", "--check")
			database := filepath.Join(target, ".ridu", "content.sqlite")
			if err := os.MkdirAll(filepath.Dir(database), 0755); err != nil {
				t.Fatal(err)
			}
			run("migrate", "up", "--database-path", database)
			run("generate", "--check")
			binary := filepath.Join(target, ".ridu", "server")
			build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "./cmd/server")
			build.Dir = target
			build.Env = os.Environ()
			if output, err := build.CombinedOutput(); err != nil {
				t.Fatalf("generated server build: %v\n%s", err, output)
			}
			address := fmt.Sprintf("127.0.0.1:%d", freeTCPPort(t))
			ctx, cancel := context.WithCancel(t.Context())
			logPath := filepath.Join(target, ".ridu", "server.log")
			log, err := os.Create(logPath)
			if err != nil {
				t.Fatal(err)
			}
			server := exec.CommandContext(ctx, binary)
			server.Dir = target
			// A directly built development entry has no release-embedded migration
			// history; the existing development readiness policy permits its local DB.
			server.Env = append(os.Environ(), "RIDU_SQLITE_PATH="+database, "RIDU_ADDRESS="+address, "RIDU_ALLOW_UNVERIFIABLE_READINESS=true")
			server.Stdout, server.Stderr = log, log
			if err := server.Start(); err != nil {
				cancel()
				log.Close()
				t.Fatal(err)
			}
			defer func() { cancel(); _ = server.Wait(); _ = log.Close() }()
			base := "http://" + address
			client := &http.Client{Timeout: time.Second}
			deadline := time.Now().Add(20 * time.Second)
			for {
				response, err := client.Get(base + "/api/auth/users/bootstrap")
				if err == nil {
					response.Body.Close()
					if response.StatusCode == http.StatusOK {
						break
					}
				}
				if time.Now().After(deadline) {
					data, _ := os.ReadFile(logPath)
					t.Fatalf("generated server did not become available: %v\n%s", err, data)
				}
				select {
				case <-t.Context().Done():
					t.Fatal(t.Context().Err())
				case <-time.After(50 * time.Millisecond):
				}
			}
			var bootstrap struct {
				Available bool `json:"available"`
			}
			readGeneratedJSON(t, base+"/api/auth/users/bootstrap", &bootstrap)
			if !bootstrap.Available {
				t.Fatal("fresh server did not expose first-user setup")
			}
			response, err := client.Post(base+"/api/auth/users/create-user", "application/json", strings.NewReader(`{"data":{"email":"admin@example.test"},"password":"correct-horse"}`))
			if err != nil {
				t.Fatal(err)
			}
			body, readErr := io.ReadAll(response.Body)
			response.Body.Close()
			if readErr != nil || response.StatusCode != http.StatusCreated {
				t.Fatalf("first-user bootstrap: HTTP %d, %v\n%s", response.StatusCode, readErr, body)
			}
			readGeneratedJSON(t, base+"/api/auth/users/bootstrap", &bootstrap)
			if bootstrap.Available {
				t.Fatal("first-user setup remained open after initialization")
			}
			var envelope struct {
				Schema struct {
					Collections []json.RawMessage `json:"collections"`
				} `json:"schema"`
			}
			readGeneratedJSON(t, base+"/api/schema", &envelope)
			want := 1
			if template == "starter" {
				want = 2
			}
			if len(envelope.Schema.Collections) != want {
				t.Fatalf("generated schema has %d collections, want %d", len(envelope.Schema.Collections), want)
			}
			t.Logf("%s: normal new → deterministic generation → SQLite migration → built server → first-user bootstrap", template)
		})
	}
}
