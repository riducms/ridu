package cli_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/riducms/ridu/internal/frameworkpackages"
)

const generatedMongoDBProjectTestEnvironment = "RIDU_MONGODB_GENERATED_PROJECT_TEST"

const generatedMongoDBSDKTest = `import { describe, expect, it } from "bun:test";

import { createClient } from "./generated/ridu.generated";

const baseURL = Bun.env.RIDU_MONGODB_GENERATED_API_URL;
if (!baseURL) throw new Error("RIDU_MONGODB_GENERATED_API_URL is required");

let cookie = "";
const fetchWithCookieJar: typeof fetch = async (input, init) => {
	const request = new Request(input, init);
	const headers = new Headers(request.headers);
	if (cookie !== "") headers.set("cookie", cookie);
	const response = await fetch(new Request(request, { headers }));
	const setCookie = response.headers.get("set-cookie");
	if (setCookie) cookie = setCookie.split(";", 1)[0] ?? "";
	return response;
};

describe("generated MongoDB SDK against the generated server", () => {
	it("keeps an authenticated cookie jar through CRUD", async () => {
		const client = createClient({ baseURL, fetch: fetchWithCookieJar });
		const login = await client.login("users", {
			email: "admin@mongodb-generated.test",
			password: "mongodb-generated-password",
		});
		expect(login.user.email).toBe("admin@mongodb-generated.test");
		expect((await client.session()).user.email).toBe("admin@mongodb-generated.test");

		const created = await client.create("posts", {
			title: "Generated SDK MongoDB CRUD",
			content: {
				version: 1,
				root: {
					type: "root",
					children: [
						{
							type: "paragraph",
							children: [{ type: "text", text: "Generated SDK rich text" }],
						},
					],
				},
			},
		});
		expect(created.title).toBe("Generated SDK MongoDB CRUD");
		expect((await client.find("posts", created.id)).id).toBe(created.id);

		const listed = await client.list("posts", {
			where: { title: { equals: "Generated SDK MongoDB CRUD" } },
			limit: 5,
		});
		expect(listed.docs.map(({ id }) => id)).toContain(created.id);

		const updated = await client.update("posts", created.id, {
			title: "Generated SDK MongoDB CRUD updated",
		});
		expect(updated.title).toBe("Generated SDK MongoDB CRUD updated");
		expect((await client.delete("posts", created.id)).deleted).toBe(true);
	});
});
`

func TestGeneratedMongoDBDevelopmentProject(t *testing.T) {
	if os.Getenv(generatedMongoDBProjectTestEnvironment) != "true" {
		t.Skip("set RIDU_MONGODB_GENERATED_PROJECT_TEST=true to run the generated MongoDB project qualification")
	}
	if runtime.GOOS == "windows" {
		t.Skip("the generated development process shutdown proof currently requires POSIX signals")
	}
	for _, executable := range []string{"bun", "docker", "go"} {
		if _, err := exec.LookPath(executable); err != nil {
			t.Fatalf("%s is required for the generated MongoDB project qualification: %v", executable, err)
		}
	}

	frameworkRoot := moduleRoot(t)
	setFrameworkProxy(t, frameworkRoot, testReleaseVersion)
	cliBinary := filepath.Join(t.TempDir(), "ridu")
	buildGeneratedProjectCLI(t, frameworkRoot, cliBinary)

	uniqueSuffix := fmt.Sprintf("%d", time.Now().UnixNano())
	starterRoot := newProjectTarget(t, "ridu-mongodb-starter-"+uniqueSuffix)
	blankRoot := newProjectTarget(t, "ridu-mongodb-blank-"+uniqueSuffix)
	poisonURL := "mongodb://ridu-redaction-user:ridu-redaction-password@127.0.0.1:1/ridu_redaction?directConnection=true&replicaSet=ridu-rs0"
	poisonRuntimeURL := "mongodb://ridu-redaction-user:ridu-redaction-password@127.0.0.1:27029/ridu_redaction?directConnection=true&replicaSet=ridu-rs0&connectTimeoutMS=1000&serverSelectionTimeoutMS=1000"
	poisonSecrets := []string{poisonURL, poisonRuntimeURL, "ridu-redaction-user", "ridu-redaction-password"}

	createGeneratedMongoDBProject(t, cliBinary, frameworkRoot, starterRoot, "starter", poisonURL, poisonSecrets)
	createGeneratedMongoDBProject(t, cliBinary, frameworkRoot, blankRoot, "blank", poisonURL, poisonSecrets)
	assertGeneratedMongoDBScaffold(t, starterRoot, true)
	assertGeneratedMongoDBScaffold(t, blankRoot, false)

	for _, root := range []string{starterRoot, blankRoot} {
		if err := frameworkpackages.Publish(frameworkRoot, root, testReleaseVersion); err != nil {
			t.Fatalf("publish release-shaped frontend packages into %s: %v", root, err)
		}
		output, err := runGeneratedCommand(90*time.Second, root, map[string]string{"DATABASE_URL": poisonURL}, cliBinary, "generate", "--check")
		if err != nil {
			t.Fatalf("offline generation check for %s: %v\n%s", root, err, output)
		}
		assertSecretsAbsent(t, output, poisonSecrets...)
	}

	assertProjectSecretsAbsent(t, starterRoot, poisonSecrets...)
	assertProjectSecretsAbsent(t, blankRoot, poisonSecrets...)

	registerGeneratedComposeCleanup(t, starterRoot)
	starterAPIPort := freeTCPPort(t)
	starterAdminPort := freeTCPPort(t)
	starterAPIURL := fmt.Sprintf("http://127.0.0.1:%d", starterAPIPort)
	starterAdminURL := fmt.Sprintf("http://127.0.0.1:%d", starterAdminPort)
	starterDev := startGeneratedMongoDBDevelopment(
		t,
		cliBinary,
		starterRoot,
		map[string]string{"DATABASE_URL": ""},
		"--address",
		fmt.Sprintf("127.0.0.1:%d", starterAPIPort),
		"--admin-port",
		fmt.Sprint(starterAdminPort),
	)
	waitForGeneratedURL(t, starterDev, starterAPIURL+"/readyz", 120*time.Second)
	waitForGeneratedURL(t, starterDev, starterAdminURL+"/admin/", 120*time.Second)
	proveGeneratedMongoDBDirectServerFailsClosed(
		t,
		starterRoot,
		"mongodb://127.0.0.1:27029/ridu?directConnection=true&replicaSet=ridu-rs0",
	)

	failedOutput, failedErr := runGeneratedCommand(
		45*time.Second,
		blankRoot,
		map[string]string{"DATABASE_URL": poisonRuntimeURL},
		cliBinary,
		"dev",
		"--no-docker",
		"--no-install",
		"--address",
		fmt.Sprintf("127.0.0.1:%d", freeTCPPort(t)),
		"--admin-port",
		fmt.Sprint(freeTCPPort(t)),
	)
	if failedErr == nil {
		t.Fatalf("development unexpectedly accepted invalid MongoDB credentials:\n%s", failedOutput)
	}
	if !strings.Contains(failedOutput, "connect to development MongoDB") {
		t.Fatalf("invalid MongoDB credential failure was not actionable (%v):\n%s", failedErr, failedOutput)
	}
	assertSecretsAbsent(t, failedOutput, poisonSecrets...)

	sdkTestPath := filepath.Join(starterRoot, "mongodb-generated-sdk.test.ts")
	if err := os.WriteFile(sdkTestPath, []byte(generatedMongoDBSDKTest), 0o600); err != nil {
		t.Fatalf("write generated SDK proof: %v", err)
	}
	playwrightOutput, err := runGeneratedCommand(
		4*time.Minute,
		frameworkRoot,
		map[string]string{
			"RIDU_MONGODB_GENERATED_ADMIN_URL":    starterAdminURL,
			"RIDU_MONGODB_GENERATED_API_URL":      starterAPIURL,
			"RIDU_MONGODB_GENERATED_PROJECT_ROOT": starterRoot,
			"RIDU_MONGODB_GENERATED_LOG":          starterDev.logPath,
		},
		"bunx",
		"playwright",
		"test",
		"--config",
		"playwright.mongodb-generated.config.ts",
	)
	if err != nil {
		t.Fatalf("generated MongoDB admin browser proof: %v\n%s\n%s", err, playwrightOutput, starterDev.logContents())
	}
	sdkOutput, err := runGeneratedCommand(
		90*time.Second,
		starterRoot,
		map[string]string{"RIDU_MONGODB_GENERATED_API_URL": starterAPIURL},
		"bun",
		"test",
		filepath.Base(sdkTestPath),
	)
	if err != nil {
		t.Fatalf("generated MongoDB SDK proof: %v\n%s\n%s", err, sdkOutput, starterDev.logContents())
	}

	shutdownStarted := time.Now()
	if err := starterDev.stop(12 * time.Second); err != nil {
		t.Fatalf("stop generated MongoDB starter development process: %v\n%s", err, starterDev.logContents())
	}
	if elapsed := time.Since(shutdownStarted); elapsed > 12*time.Second {
		t.Fatalf("generated MongoDB starter shutdown took %s", elapsed)
	}
	waitForGeneratedURLUnavailable(t, starterAPIURL+"/readyz", 10*time.Second)
	waitForGeneratedURLUnavailable(t, starterAdminURL+"/admin/", 10*time.Second)
	assertNoGeneratedServerBinaries(t, starterRoot)

	blankDatabaseURL := "mongodb://127.0.0.1:27029/ridu_blank_generated?directConnection=true&replicaSet=ridu-rs0"
	blankAPIPort := freeTCPPort(t)
	blankAdminPort := freeTCPPort(t)
	blankAPIURL := fmt.Sprintf("http://127.0.0.1:%d", blankAPIPort)
	blankAdminURL := fmt.Sprintf("http://127.0.0.1:%d", blankAdminPort)
	blankDev := startGeneratedMongoDBDevelopment(
		t,
		cliBinary,
		blankRoot,
		map[string]string{"DATABASE_URL": ""},
		"--no-docker",
		"--database-url",
		blankDatabaseURL,
		"--address",
		fmt.Sprintf("127.0.0.1:%d", blankAPIPort),
		"--admin-port",
		fmt.Sprint(blankAdminPort),
	)
	waitForGeneratedURL(t, blankDev, blankAPIURL+"/readyz", 120*time.Second)
	waitForGeneratedURL(t, blankDev, blankAdminURL+"/admin/", 120*time.Second)
	assertBlankGeneratedMongoDBRuntime(t, blankAPIURL)

	shutdownStarted = time.Now()
	if err := blankDev.stop(12 * time.Second); err != nil {
		t.Fatalf("stop generated MongoDB blank development process: %v\n%s", err, blankDev.logContents())
	}
	if elapsed := time.Since(shutdownStarted); elapsed > 12*time.Second {
		t.Fatalf("generated MongoDB blank shutdown took %s", elapsed)
	}
	waitForGeneratedURLUnavailable(t, blankAPIURL+"/readyz", 10*time.Second)
	waitForGeneratedURLUnavailable(t, blankAdminURL+"/admin/", 10*time.Second)
	assertNoGeneratedServerBinaries(t, blankRoot)
}

func buildGeneratedProjectCLI(t *testing.T, frameworkRoot, target string) {
	t.Helper()
	output, err := runGeneratedCommand(2*time.Minute, frameworkRoot, nil, "go", "build", "-o", target, "./cmd/ridu")
	if err != nil {
		t.Fatalf("build real Ridu CLI: %v\n%s", err, output)
	}
}

func proveGeneratedMongoDBDirectServerFailsClosed(t *testing.T, root, databaseURL string) {
	t.Helper()
	binary := filepath.Join(root, ".ridu", "mongodb-direct-server")
	output, err := runGeneratedCommand(2*time.Minute, root, nil, "go", "build", "-o", binary, "./cmd/server")
	if err != nil {
		t.Fatalf("build generated MongoDB direct server: %v\n%s", err, output)
	}
	address := fmt.Sprintf("127.0.0.1:%d", freeTCPPort(t))
	output, err = runGeneratedCommand(
		30*time.Second,
		root,
		map[string]string{
			"DATABASE_URL":                      databaseURL,
			"RIDU_ADDRESS":                      address,
			"RIDU_ALLOW_INSECURE_DATABASE":      "true",
			"RIDU_ALLOW_UNVERIFIABLE_READINESS": "true",
			"RIDU_SKIP_READINESS_PREFLIGHT":     "true",
		},
		binary,
	)
	if err == nil {
		t.Fatalf("generated MongoDB direct server accepted public development readiness overrides:\n%s", output)
	}
	if !strings.Contains(output, "production server requires an executable migration history") || !strings.Contains(output, "ridu build") {
		t.Fatalf("generated MongoDB direct server did not require the build-bound migration history (%v):\n%s", err, output)
	}
	waitForGeneratedURLUnavailable(t, "http://"+address+"/readyz", 2*time.Second)
}

func createGeneratedMongoDBProject(t *testing.T, binary, frameworkRoot, target, template, poisonURL string, secrets []string) {
	t.Helper()
	arguments := []string{
		"new",
		"--template",
		template,
		"--database",
		"mongodb",
		"--package-manager",
		"bun",
		"--release-version",
		testReleaseVersion,
		"--module",
		"example.com/fixture/mongodb-" + template,
		"--scope",
		"@fixture-mongodb-" + template,
		target,
	}
	if template == "blank" {
		arguments = append(arguments[:5], append([]string{"--no-agent"}, arguments[5:]...)...)
	}
	output, err := runGeneratedCommand(
		3*time.Minute,
		frameworkRoot,
		map[string]string{"DATABASE_URL": poisonURL},
		binary,
		arguments...,
	)
	if err != nil {
		t.Fatalf("create generated MongoDB %s project: %v\n%s", template, err, output)
	}
	assertSecretsAbsent(t, output, secrets...)
	goModule, err := os.ReadFile(filepath.Join(target, "go.mod"))
	if err != nil {
		t.Fatalf("read generated %s go.mod: %v", template, err)
	}
	if !strings.Contains(string(goModule), "github.com/riducms/ridu "+testReleaseVersion) || strings.Contains(string(goModule), "replace github.com/riducms/ridu") {
		t.Fatalf("generated MongoDB %s project did not consume the local release proxy:\n%s", template, goModule)
	}
}

func assertGeneratedMongoDBScaffold(t *testing.T, root string, starter bool) {
	t.Helper()
	project, err := os.ReadFile(filepath.Join(root, "ridu.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(project, []byte(`database = "mongodb"`)) {
		t.Fatalf("generated MongoDB project did not record its adapter:\n%s", project)
	}
	compose, err := os.ReadFile(filepath.Join(root, "compose.yaml"))
	if err != nil {
		t.Fatalf("read generated MongoDB compose fixture: %v", err)
	}
	for _, contract := range []string{"mongo:", "@sha256:", "27029:27017", "--replSet", "ridu-rs0"} {
		if !strings.Contains(string(compose), contract) {
			t.Fatalf("generated MongoDB compose fixture is missing %q:\n%s", contract, compose)
		}
	}
	_, postsError := os.Stat(filepath.Join(root, "content", "posts.go"))
	if starter && postsError != nil {
		t.Fatalf("generated MongoDB starter is missing posts: %v", postsError)
	}
	if !starter && !os.IsNotExist(postsError) {
		t.Fatalf("generated MongoDB blank unexpectedly contains posts: %v", postsError)
	}
	if starter {
		assertGeneratedMongoDBAgentGuidance(t, root)
	}
}

func assertGeneratedMongoDBAgentGuidance(t *testing.T, root string) {
	t.Helper()
	referencePath := filepath.Join(root, ".agents", "skills", "ridu-project", "reference", "mongodb.md")
	if _, err := os.Stat(referencePath); err != nil {
		t.Fatalf("generated MongoDB starter is missing its agent reference: %v", err)
	}
	skillPath := filepath.Join(root, ".agents", "skills", "ridu-project", "SKILL.md")
	encoded, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("read generated Ridu project skill: %v", err)
	}
	skill := string(encoded)
	if !strings.Contains(skill, "reference/mongodb.md") {
		t.Fatalf("generated Ridu project skill does not route MongoDB work to its reference:\n%s", skill)
	}
	if !strings.Contains(skill, "`npm run ridu -- check`") {
		t.Fatalf("generated Ridu project skill omits the MongoDB ridu check boundary:\n%s", skill)
	}
	for _, bypass := range []string{"bypass `ridu check`", "do not run `ridu check`", "skip `ridu check`"} {
		if strings.Contains(strings.ToLower(skill), bypass) {
			t.Fatalf("generated Ridu project skill tells MongoDB authors to bypass ridu check:\n%s", skill)
		}
	}
}

type generatedMongoDBDevelopment struct {
	command *exec.Cmd
	done    chan struct{}
	logFile *os.File
	logPath string

	mutex    sync.Mutex
	waitErr  error
	stopErr  error
	stopOnce sync.Once
}

func startGeneratedMongoDBDevelopment(t *testing.T, binary, root string, environment map[string]string, arguments ...string) *generatedMongoDBDevelopment {
	t.Helper()
	logPath := filepath.Join(root, ".ridu", "mongodb-generated-development.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		t.Fatal(err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open generated development log: %v", err)
	}
	command := exec.Command(binary, append([]string{"dev"}, arguments...)...)
	command.Dir = root
	command.Env = generatedEnvironment(environment)
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		t.Fatalf("start generated MongoDB development process: %v", err)
	}
	process := &generatedMongoDBDevelopment{
		command: command,
		done:    make(chan struct{}),
		logFile: logFile,
		logPath: logPath,
	}
	go func() {
		err := command.Wait()
		process.mutex.Lock()
		process.waitErr = err
		process.mutex.Unlock()
		close(process.done)
	}()
	t.Cleanup(func() {
		if err := process.stopProcess(12 * time.Second); err != nil {
			t.Errorf("clean up generated MongoDB development process: %v\n%s", err, process.logContents())
		}
	})
	return process
}

func (process *generatedMongoDBDevelopment) stopProcess(timeout time.Duration) error {
	process.stopOnce.Do(func() {
		select {
		case <-process.done:
		default:
			if err := process.command.Process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
				process.stopErr = err
			}
		}
		select {
		case <-process.done:
		case <-time.After(timeout):
			_ = process.command.Process.Kill()
			<-process.done
			process.stopErr = fmt.Errorf("timed out after %s", timeout)
		}
		if err := process.logFile.Close(); process.stopErr == nil && err != nil {
			process.stopErr = err
		}
		process.mutex.Lock()
		waitErr := process.waitErr
		process.mutex.Unlock()
		if process.stopErr == nil && waitErr != nil {
			process.stopErr = waitErr
		}
	})
	return process.stopErr
}

func (process *generatedMongoDBDevelopment) stop(timeout time.Duration) error {
	return process.stopProcess(timeout)
}

func (process *generatedMongoDBDevelopment) logContents() string {
	encoded, err := os.ReadFile(process.logPath)
	if err != nil {
		return fmt.Sprintf("read development log: %v", err)
	}
	return string(encoded)
}

func waitForGeneratedURL(t *testing.T, process *generatedMongoDBDevelopment, target string, timeout time.Duration) {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	client := &http.Client{Timeout: 2 * time.Second}
	for {
		request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, requestErr := client.Do(request)
		if requestErr == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode >= 200 && response.StatusCode < 400 {
				return
			}
		}
		select {
		case <-process.done:
			t.Fatalf("generated development process exited before %s became ready\n%s", target, process.logContents())
		case <-deadline.C:
			t.Fatalf("timed out waiting for %s\n%s", target, process.logContents())
		case <-ticker.C:
		}
	}
}

func waitForGeneratedURLUnavailable(t *testing.T, target string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 300 * time.Millisecond}
	for time.Now().Before(deadline) {
		response, err := client.Get(target)
		if err != nil {
			return
		}
		_ = response.Body.Close()
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s was still reachable after %s", target, timeout)
}

func assertBlankGeneratedMongoDBRuntime(t *testing.T, apiURL string) {
	t.Helper()
	type bootstrapEnvelope struct {
		Available bool `json:"available"`
	}
	type schemaEnvelope struct {
		Schema struct {
			Collections []struct {
				Slug string `json:"slug"`
			} `json:"collections"`
		} `json:"schema"`
	}
	var bootstrap bootstrapEnvelope
	readGeneratedJSON(t, apiURL+"/api/auth/users/bootstrap", &bootstrap)
	if !bootstrap.Available {
		t.Fatal("generated MongoDB blank project did not expose first-user bootstrap")
	}
	var manifest schemaEnvelope
	readGeneratedJSON(t, apiURL+"/api/schema", &manifest)
	if len(manifest.Schema.Collections) != 1 || manifest.Schema.Collections[0].Slug != "users" {
		t.Fatalf("generated MongoDB blank collections = %#v, want users only", manifest.Schema.Collections)
	}
}

func readGeneratedJSON(t *testing.T, target string, destination any) {
	t.Helper()
	response, err := (&http.Client{Timeout: 5 * time.Second}).Get(target)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		t.Fatalf("GET %s returned %s", target, response.Status)
	}
	if err := json.NewDecoder(response.Body).Decode(destination); err != nil {
		t.Fatalf("decode %s: %v", target, err)
	}
}

func assertNoGeneratedServerBinaries(t *testing.T, root string) {
	t.Helper()
	directory := filepath.Join(root, ".ridu", "dev")
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "server-") {
			t.Fatalf("generated development shutdown left active binary %s", filepath.Join(directory, entry.Name()))
		}
	}
}

func registerGeneratedComposeCleanup(t *testing.T, root string) {
	t.Helper()
	t.Cleanup(func() {
		output, err := runGeneratedCommand(90*time.Second, root, map[string]string{"DATABASE_URL": ""}, "docker", "compose", "down", "--volumes", "--remove-orphans")
		if err != nil {
			t.Errorf("clean up generated MongoDB compose project: %v\n%s", err, output)
		}
	})
}

func runGeneratedCommand(timeout time.Duration, directory string, environment map[string]string, name string, arguments ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	command := exec.CommandContext(ctx, name, arguments...)
	command.Dir = directory
	command.Env = generatedEnvironment(environment)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	if ctx.Err() != nil {
		return output.String(), fmt.Errorf("%s timed out after %s: %w", name, timeout, ctx.Err())
	}
	return output.String(), err
}

func generatedEnvironment(overrides map[string]string) []string {
	removed := make(map[string]bool, len(overrides))
	for name := range overrides {
		removed[name] = true
	}
	environment := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if !removed[name] {
			environment = append(environment, entry)
		}
	}
	for name, value := range overrides {
		if value != "" {
			environment = append(environment, name+"="+value)
		}
	}
	return environment
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve TCP port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func assertSecretsAbsent(t *testing.T, content string, secrets ...string) {
	t.Helper()
	for _, secret := range secrets {
		if strings.Contains(content, secret) {
			t.Fatalf("output exposed MongoDB credential %q:\n%s", secret, content)
		}
	}
}

func assertProjectSecretsAbsent(t *testing.T, root string, secrets ...string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case "node_modules", "packages":
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() > 8<<20 {
			return nil
		}
		encoded, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, secret := range secrets {
			if bytes.Contains(encoded, []byte(secret)) {
				return fmt.Errorf("%s contains MongoDB credential %q", path, secret)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
