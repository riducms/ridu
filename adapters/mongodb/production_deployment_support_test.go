package mongodb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/riducms/ridu/internal/frameworkpackages"
	"github.com/riducms/ridu/internal/frameworkproxy"
	"github.com/riducms/ridu/internal/migrationartifact"
	ridumigration "github.com/riducms/ridu/migration"
	"go.mongodb.org/mongo-driver/v2/bson"
)

const (
	mongoDBProductionDeploymentTestEnvironment = "RIDU_MONGODB_PRODUCTION_DEPLOYMENT_TEST"
	mongoDBProductionDeploymentRelease         = "v0.0.0-mongodb-deployment.1"
	mongoDBProductionContainerFixtureRoot      = "/ridu-fixture"
	mongoDBProductionContainerProjectRoot      = "/ridu-project"
	mongoDBProductionContainerUploadRoot       = "/ridu-uploads"
	mongoDBProductionDeploymentFirstTestPort   = 20000
	mongoDBProductionDeploymentTestPortCount   = 10000
)

var mongoDBProductionDeploymentPortLeases = struct {
	sync.Mutex
	ports map[int]struct{}
}{ports: make(map[int]struct{})}

const mongoDBProductionDeploymentSDKCanary = `import { describe, expect, it } from "bun:test";

import { createClient } from "./generated/ridu.generated";

const baseURL = Bun.env.RIDU_MONGODB_DEPLOYMENT_API_URL;
const email = Bun.env.RIDU_MONGODB_DEPLOYMENT_EMAIL;
const password = Bun.env.RIDU_MONGODB_DEPLOYMENT_PASSWORD;
const authorID = Bun.env.RIDU_MONGODB_DEPLOYMENT_AUTHOR_ID;
if (!baseURL || !email || !password || !authorID) {
	throw new Error("MongoDB deployment SDK canary environment is incomplete");
}

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

describe("release-built generated Fetch SDK", () => {
	it("runs authenticated CRUD, typed reads, and relationship population", async () => {
		const client = createClient({ baseURL, fetch: fetchWithCookieJar });
		const login = await client.login("users", { email, password });
		expect(login.user.email).toBe(email);

		const created = await client.create("posts", {
			title: "MongoDB generated SDK deployment canary",
			status: "published",
			author: authorID,
			content: {
				version: 1,
				root: {
					type: "root",
					children: [{
						type: "paragraph",
						children: [{ type: "text", text: "Generated SDK release canary" }],
					}],
				},
			},
		});
		expect((await client.find("posts", created.id)).title).toBe(created.title);

		const populated = await client.find("posts", created.id, { populate: { author: true } });
		if (typeof populated.author !== "object" || populated.author === null) {
			throw new Error("generated SDK relationship was not populated");
		}
		expect(populated.author.id).toBe(authorID);
		expect(populated.author.email).toBe(email);

		const listed = await client.list("posts", {
			where: { title: { equals: created.title } },
			limit: 5,
		});
		expect(listed.docs.map(({ id }) => id)).toContain(created.id);
		const updated = await client.publishChanges("posts", created.id, {
			title: "MongoDB generated SDK deployment canary updated",
		});
		expect(updated.title).toBe("MongoDB generated SDK deployment canary updated");
		expect((await client.versions("posts", created.id)).length).toBeGreaterThanOrEqual(2);
		expect((await client.delete("posts", created.id)).deleted).toBe(true);
	});
});
`

type mongoDBProductionDeploymentHarness struct {
	fixture       *mongoDBProductionFixture
	frameworkRoot string
	cliBinary     string
	runtimeImage  string
	goArch        string
	environment   map[string]string
}

type mongoDBProductionDeploymentDatabase struct {
	name         string
	username     string
	password     string
	hostURL      string
	containerURL string
	hosts        []string
}

type mongoDBProductionDeploymentScopeSentinel struct {
	database    string
	collection  string
	documentID  string
	beforeValue string
	afterValue  string
}

type mongoDBProductionDeploymentProject struct {
	root       string
	template   string
	modulePath string
	uploadRoot string
	database   mongoDBProductionDeploymentDatabase
}

type mongoDBProductionDeploymentData struct {
	email        string
	password     string
	userID       string
	postID       string
	version      int
	mediaID      string
	uploadURL    string
	objectKey    string
	fileBody     string
	postTitle    string
	rolloutTitle string
	postStatus   string
	session      []*http.Cookie
}

type mongoDBProductionDeploymentAdminCanary struct {
	PostID  string `json:"postID"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

func newMongoDBProductionDeploymentHarness(t *testing.T) *mongoDBProductionDeploymentHarness {
	t.Helper()
	if testing.Short() {
		t.Skip("MongoDB generated-project production deployment proof is disabled in short mode")
	}
	if runtime.GOOS == "windows" {
		t.Skip("MongoDB generated-project production deployment interruption proof requires POSIX process signals")
	}
	for _, executable := range []string{"bun", "docker", "go"} {
		if _, err := exec.LookPath(executable); err != nil {
			t.Fatalf("%s is required for the MongoDB production deployment proof: %v", executable, err)
		}
	}
	frameworkRoot := mongoDBProductionDeploymentModuleRoot(t)
	fixture := newMongoDBProductionFixture(t)
	fixture.start(t)
	bootstrapContext, bootstrapCancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer bootstrapCancel()
	fixture.bootstrap(bootstrapContext, t)

	root := t.TempDir()
	moduleCache := filepath.Join(root, "go-module-cache")
	buildCache := filepath.Join(root, "go-build-cache")
	for _, path := range []string{moduleCache, buildCache} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	proxyURL, err := frameworkproxy.Publish(frameworkRoot, filepath.Join(root, "proxy"), mongoDBProductionDeploymentRelease)
	if err != nil {
		t.Fatalf("publish MongoDB deployment framework release: %v", err)
	}
	harness := &mongoDBProductionDeploymentHarness{
		fixture:       fixture,
		frameworkRoot: frameworkRoot,
		cliBinary:     filepath.Join(root, "ridu"),
		runtimeImage:  mongoDBProductionDeploymentImage(t, fixture.composePath),
		environment: map[string]string{
			"GOWORK":     "off",
			"GOFLAGS":    "-modcacherw",
			"GOMODCACHE": moduleCache,
			"GOCACHE":    buildCache,
			"GOPROXY":    proxyURL + ",https://proxy.golang.org,direct",
			"GONOSUMDB":  "github.com/riducms/ridu",
		},
	}
	output, err := harness.run(2*time.Minute, frameworkRoot, nil, "docker", "version", "--format", "{{.Server.Os}}/{{.Server.Arch}}")
	if err != nil {
		t.Fatalf("inspect Docker server platform: %v\n%s", err, output)
	}
	platform := strings.TrimSpace(output)
	if !strings.HasPrefix(platform, "linux/") {
		t.Fatalf("MongoDB production deployment proof requires a Linux Docker server, got %q", platform)
	}
	harness.goArch = mongoDBProductionDeploymentGoArch(t, strings.TrimPrefix(platform, "linux/"))
	output, err = harness.run(3*time.Minute, frameworkRoot, nil, "go", "build", "-trimpath", "-o", harness.cliBinary, "./cmd/ridu")
	if err != nil {
		t.Fatalf("build real Ridu CLI for MongoDB deployment proof: %v\n%s", err, output)
	}
	return harness
}

func mongoDBProductionDeploymentModuleRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve MongoDB deployment test source path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func mongoDBProductionDeploymentImage(t *testing.T, composePath string) string {
	t.Helper()
	encoded, err := os.ReadFile(composePath)
	if err != nil {
		t.Fatal(err)
	}
	image, err := mongoDBProductionDeploymentImageFromCompose(string(encoded))
	if err != nil {
		t.Fatalf("validate MongoDB production fixture image in %s: %v", composePath, err)
	}
	return image
}

func mongoDBProductionDeploymentImageFromCompose(encoded string) (string, error) {
	imageCount := 0
	for _, line := range strings.Split(string(encoded), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "image:") {
			image := strings.TrimSpace(strings.TrimPrefix(line, "image:"))
			if image == "" {
				return "", errors.New("runtime image is empty")
			}
			imageCount++
			if image != mongoDBProductionFixtureImage {
				return "", fmt.Errorf("runtime image = %q, want exact qualified image %q", image, mongoDBProductionFixtureImage)
			}
		}
	}
	if imageCount == 0 {
		return "", errors.New("runtime image is missing")
	}
	return mongoDBProductionFixtureImage, nil
}

func mongoDBProductionDeploymentGoArch(t *testing.T, dockerArch string) string {
	t.Helper()
	switch strings.TrimSpace(dockerArch) {
	case "amd64", "x86_64":
		return "amd64"
	case "arm64", "aarch64":
		return "arm64"
	default:
		t.Fatalf("unsupported Linux Docker architecture %q", dockerArch)
		return ""
	}
}

func (harness *mongoDBProductionDeploymentHarness) run(
	timeout time.Duration,
	directory string,
	overrides map[string]string,
	name string,
	arguments ...string,
) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	command := exec.CommandContext(ctx, name, arguments...)
	command.Dir = directory
	command.Env = mongoDBProductionDeploymentEnvironment(harness.environment, overrides)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	err := command.Run()
	if ctx.Err() != nil {
		return output.String(), fmt.Errorf("%s timed out after %s: %w", name, timeout, ctx.Err())
	}
	return output.String(), err
}

func mongoDBProductionDeploymentEnvironment(base, overrides map[string]string) []string {
	values := make(map[string]string, len(base)+len(overrides))
	for _, entry := range os.Environ() {
		name, value, found := strings.Cut(entry, "=")
		if found {
			values[name] = value
		}
	}
	for name, value := range base {
		values[name] = value
	}
	for name, value := range overrides {
		values[name] = value
	}
	environment := make([]string, 0, len(values))
	for name, value := range values {
		environment = append(environment, name+"="+value)
	}
	return environment
}

func (harness *mongoDBProductionDeploymentHarness) newProject(
	t *testing.T,
	template string,
	database mongoDBProductionDeploymentDatabase,
) mongoDBProductionDeploymentProject {
	t.Helper()
	root := filepath.Join(t.TempDir(), "mongodb-"+template)
	modulePath := "example.com/ridu/mongodb-production-" + template
	poisonURL := "mongodb://offline-deployment-user:offline-deployment-secret@127.0.0.1:1/offline"
	output, err := harness.run(
		4*time.Minute,
		harness.frameworkRoot,
		map[string]string{"DATABASE_URL": poisonURL},
		harness.cliBinary,
		"new",
		"--template", template,
		"--database", "mongodb",
		"--no-agent",
		"--release-version", mongoDBProductionDeploymentRelease,
		"--module", modulePath,
		"--scope", "@ridu-mongodb-production-"+template,
		root,
	)
	if err != nil {
		t.Fatalf("create generated MongoDB %s deployment project: %v\n%s", template, err, mongoDBProductionRedacted(output, poisonURL, "offline-deployment-user", "offline-deployment-secret"))
	}
	mongoDBProductionAssertSecretsAbsent(t, output, poisonURL, "offline-deployment-user", "offline-deployment-secret")
	if err := frameworkpackages.Publish(harness.frameworkRoot, root, mongoDBProductionDeploymentRelease); err != nil {
		t.Fatalf("publish release-shaped frontend packages into generated MongoDB %s project: %v", template, err)
	}
	project := mongoDBProductionDeploymentProject{
		root: root, template: template, modulePath: modulePath, database: database,
	}
	if template == "starter" {
		project.uploadRoot = filepath.Join(t.TempDir(), "uploads")
		if err := os.MkdirAll(project.uploadRoot, 0o750); err != nil {
			t.Fatal(err)
		}
		mongoDBProductionConfigureGeneratedUploads(t, project)
		output, err := harness.run(
			30*time.Second,
			project.root,
			nil,
			"gofmt", "-w", "content/config.go", "content/media.go", "content/posts.go", "cmd/server/main.go",
		)
		if err != nil {
			t.Fatalf("format generated MongoDB starter deployment fixture: %v\n%s", err, output)
		}
	}
	return project
}

func (harness *mongoDBProductionDeploymentHarness) provisionDatabase(t *testing.T, label string) mongoDBProductionDeploymentDatabase {
	t.Helper()
	token := mongoDBProductionRandomHex(t, 6)
	databaseName, username := mongoDBProductionDeploymentDatabaseIdentity(label, token)
	database := mongoDBProductionDeploymentDatabase{
		name:     databaseName,
		username: username,
		password: mongoDBProductionRandomSecret(t, 32),
	}
	admin := harness.fixture.authenticatedAdminClient(t, harness.fixture.seedURL(harness.fixture.caPath, true))
	defer disconnectMongoDBProductionClient(t, admin)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if err := admin.Database(database.name).RunCommand(ctx, bson.D{
		{Key: "createUser", Value: database.username},
		{Key: "pwd", Value: database.password},
		{Key: "mechanisms", Value: bson.A{"SCRAM-SHA-256"}},
		{Key: "roles", Value: bson.A{bson.D{{Key: "role", Value: "dbOwner"}, {Key: "db", Value: database.name}}}},
	}).Err(); err != nil {
		t.Fatalf("provision MongoDB deployment database %s: %v", database.name, err)
	}
	database.hostURL = mongoDBProductionURL(
		harness.fixture.hosts(), database.name, database.username, database.password,
		harness.fixture.caPath, true, false, true,
	)
	database.containerURL = mongoDBProductionURL(
		harness.fixture.hosts(), database.name, database.username, database.password,
		filepath.Join(mongoDBProductionContainerFixtureRoot, "ca.pem"), true, false, true,
	)
	database.hosts = harness.fixture.hosts()
	return database
}

func mongoDBProductionDeploymentDatabaseIdentity(label, token string) (string, string) {
	return "ridu_deployment_" + label + "_" + token, "ridu_application_" + token
}

func mongoDBProductionConfigureGeneratedUploads(t *testing.T, project mongoDBProductionDeploymentProject) {
	t.Helper()
	postsPath := filepath.Join(project.root, "content", "posts.go")
	mongoDBProductionReplaceFile(t, postsPath,
		"\tSlug: \"posts\",",
		"\tSlug:     \"posts\",\n\tVersions: true,",
	)
	configPath := filepath.Join(project.root, "content", "config.go")
	mongoDBProductionReplaceFile(t, configPath,
		"\t\tPlugins:     installedPlugins(),\n\t\tCollections: []ridu.Collection{Users, Posts},",
		"\t\tPlugins:          installedPlugins(),\n\t\tStorageNamespace: \"mongodb-production-deployment\",\n\t\tCollections:      []ridu.Collection{Users, Posts, Media},",
	)
	media := `package content

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/field"
)

var Media = ridu.Collection{
	Slug:   "media",
	Upload: true,
	UploadConfig: ridu.UploadConfig{
		MaxFileSize: 1 << 20,
		MimeTypes:   []string{"text/plain"},
	},
	Access: ridu.CollectionAccess{
		Create: authenticatedOnly,
		Read:   authenticatedOnly,
		Update: authenticatedOnly,
		Delete: authenticatedOnly,
	},
	Fields: field.Fields{field.Text("alt").Required()},
}
`
	if err := os.WriteFile(filepath.Join(project.root, "content", "media.go"), []byte(media), 0o644); err != nil {
		t.Fatal(err)
	}
	serverPath := filepath.Join(project.root, "cmd", "server", "main.go")
	mongoDBProductionReplaceFile(t, serverPath,
		"\t\"github.com/riducms/ridu/adapters/mongodb\"\n\t\"github.com/riducms/ridu/store\"",
		"\t\"github.com/riducms/ridu/adapters/mongodb\"\n\tlocalstorage \"github.com/riducms/ridu/adapters/storage/local\"\n\t\"github.com/riducms/ridu/storage\"\n\t\"github.com/riducms/ridu/store\"",
	)
	mongoDBProductionReplaceFile(t, serverPath,
		"\t\t}),\n\t\tridu.WithAddress(env(\"RIDU_ADDRESS\", \":8080\")),",
		"\t\t}),\n\t\tridu.WithUploadStorage(func(context.Context) (storage.Backend, error) {\n\t\t\troot := strings.TrimSpace(os.Getenv(\"RIDU_UPLOAD_ROOT\"))\n\t\t\tif root == \"\" {\n\t\t\t\treturn nil, fmt.Errorf(\"RIDU_UPLOAD_ROOT is required\")\n\t\t\t}\n\t\t\treturn localstorage.New(root)\n\t\t}),\n\t\tridu.WithAddress(env(\"RIDU_ADDRESS\", \":8080\")),",
	)
}

func mongoDBProductionReplaceFile(t *testing.T, path, old, replacement string) {
	t.Helper()
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(encoded), old) != 1 {
		t.Fatalf("generated deployment fixture %s does not contain one replacement boundary %q", path, old)
	}
	encoded = []byte(strings.Replace(string(encoded), old, replacement, 1))
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
}

func (database mongoDBProductionDeploymentDatabase) secrets(extra ...string) []string {
	secrets := []string{database.password, database.username, database.hostURL, database.containerURL}
	if parsed, err := url.Parse(database.hostURL); err == nil {
		secrets = append(secrets, parsed.Host)
	}
	secrets = append(secrets, strings.Join(database.hosts, ","))
	secrets = append(secrets, database.hosts...)
	return append(secrets, extra...)
}

func mongoDBProductionAssertSecretsAbsent(t *testing.T, output string, secrets ...string) {
	t.Helper()
	for _, secret := range secrets {
		if secret != "" && strings.Contains(output, secret) {
			t.Fatalf("MongoDB deployment output exposed credential-bearing material")
		}
	}
}

func mongoDBProductionRedacted(output string, secrets ...string) string {
	for _, secret := range secrets {
		if secret != "" {
			output = strings.ReplaceAll(output, secret, "[REDACTED]")
		}
	}
	return output
}

func (harness *mongoDBProductionDeploymentHarness) buildLinuxProject(
	t *testing.T,
	project mongoDBProductionDeploymentProject,
	outputPath string,
) string {
	t.Helper()
	poisonURL := "mongodb://offline-build-user:offline-build-secret@127.0.0.1:1/offline"
	secrets := []string{poisonURL, "offline-build-user", "offline-build-secret"}
	output, err := harness.run(
		8*time.Minute,
		project.root,
		map[string]string{"DATABASE_URL": poisonURL},
		harness.cliBinary,
		"build", "--output", outputPath,
	)
	if err != nil {
		t.Fatalf("build generated MongoDB %s native release pipeline: %v\n%s", project.template, err, mongoDBProductionRedacted(output, secrets...))
	}
	mongoDBProductionAssertSecretsAbsent(t, output, secrets...)

	absolute := filepath.Join(project.root, filepath.FromSlash(outputPath))
	if runtime.GOOS != "linux" || runtime.GOARCH != harness.goArch {
		// Linux CI retains and runs the literal ridu-build artifact. A non-target
		// development host runs the complete native release/admin pipeline first,
		// then replaces only the final Go server with an equivalent Linux build
		// carrying the same validated migration-history identity.
		files, historyErr := migrationartifact.ReadAll(filepath.Join(project.root, "migrations"))
		if historyErr != nil {
			t.Fatalf("read generated MongoDB migration history for Linux build: %v", historyErr)
		}
		identities := make([]ridumigration.ArtifactIdentity, len(files))
		for index, file := range files {
			identities[index] = ridumigration.ArtifactIdentity{Name: file.Name, Digest: file.Digest}
		}
		historyDigest, historyErr := ridumigration.DigestArtifactHistory(identities)
		if historyErr != nil {
			t.Fatalf("digest generated MongoDB migration history for Linux build: %v", historyErr)
		}
		temporary := absolute + ".linux-" + mongoDBProductionRandomHex(t, 4)
		defer os.Remove(temporary)
		crossOutput, crossErr := harness.run(
			8*time.Minute,
			project.root,
			map[string]string{
				"DATABASE_URL": poisonURL,
				"GOOS":         "linux",
				"GOARCH":       harness.goArch,
				"CGO_ENABLED":  "0",
			},
			"go", "build", "-trimpath",
			"-ldflags=-X=github.com/riducms/ridu/core.executableMigrationHistoryDigest="+historyDigest,
			"-o", temporary, "./cmd/server",
		)
		if crossErr != nil {
			t.Fatalf("cross-compile generated MongoDB %s Linux server: %v\n%s", project.template, crossErr, mongoDBProductionRedacted(crossOutput, secrets...))
		}
		mongoDBProductionAssertSecretsAbsent(t, crossOutput, secrets...)
		if err := os.Rename(temporary, absolute); err != nil {
			t.Fatalf("install generated MongoDB %s Linux server: %v", project.template, err)
		}
	}
	binary, err := os.Open(absolute)
	if err != nil {
		t.Fatal(err)
	}
	defer binary.Close()
	header := make([]byte, 4)
	if _, err := io.ReadFull(binary, header); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(header, []byte{0x7f, 'E', 'L', 'F'}) {
		t.Fatalf("generated MongoDB deployment binary %s is not Linux ELF", absolute)
	}
	return outputPath
}

type mongoDBProductionDeploymentServer struct {
	harness *mongoDBProductionDeploymentHarness
	project mongoDBProductionDeploymentProject
	name    string
	address string
	logPath string
	logFile *os.File
	command *exec.Cmd
	done    chan struct{}

	mutex    sync.Mutex
	waitErr  error
	stopOnce sync.Once
	stopErr  error
}

var errMongoDBProductionDeploymentExitTimeout = errors.New("MongoDB deployment process exit timed out")

func (harness *mongoDBProductionDeploymentHarness) startServer(
	t *testing.T,
	project mongoDBProductionDeploymentProject,
	binaryPath string,
	databaseURL string,
	label string,
) *mongoDBProductionDeploymentServer {
	t.Helper()
	port := mongoDBProductionDeploymentFreePort(t)
	address := fmt.Sprintf("127.0.0.1:%d", port)
	name := "ridu-mongodb-deploy-" + label + "-" + mongoDBProductionRandomHex(t, 4)
	root := t.TempDir()
	environmentPath := filepath.Join(root, "server.env")
	environment := strings.Join([]string{
		"DATABASE_URL=" + databaseURL,
		"RIDU_ADDRESS=0.0.0.0:" + fmt.Sprint(port),
		"RIDU_SECURE_COOKIES=false",
		"RIDU_SHUTDOWN_TIMEOUT=6s",
		"RIDU_WORKER_DRAIN_TIMEOUT=6s",
		"RIDU_READINESS_DRAIN_DELAY=100ms",
		"RIDU_UPLOAD_ROOT=" + mongoDBProductionContainerUploadRoot,
	}, "\n") + "\n"
	if err := os.WriteFile(environmentPath, []byte(environment), 0o600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(root, "server.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	containerBinary := filepath.Join(mongoDBProductionContainerProjectRoot, filepath.ToSlash(binaryPath))
	arguments := []string{
		"run", "--rm", "--name", name, "--network", "host",
		"--env-file", environmentPath,
		"--volume", project.root + ":" + mongoDBProductionContainerProjectRoot + ":ro",
		"--volume", harness.fixture.root + ":" + mongoDBProductionContainerFixtureRoot + ":ro",
	}
	if project.uploadRoot != "" {
		arguments = append(arguments, "--volume", project.uploadRoot+":"+mongoDBProductionContainerUploadRoot)
	}
	arguments = append(arguments, "--entrypoint", containerBinary, harness.runtimeImage)
	command := exec.Command("docker", arguments...)
	command.Dir = project.root
	command.Env = mongoDBProductionDeploymentEnvironment(harness.environment, nil)
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		t.Fatalf("start generated MongoDB Linux deployment container: %v", err)
	}
	server := &mongoDBProductionDeploymentServer{
		harness: harness, project: project, name: name, address: address,
		logPath: logPath, logFile: logFile, command: command, done: make(chan struct{}),
	}
	go func() {
		err := command.Wait()
		server.mutex.Lock()
		server.waitErr = err
		server.mutex.Unlock()
		close(server.done)
	}()
	t.Cleanup(func() {
		stopErr := server.forceStop(12 * time.Second)
		logs := server.logs()
		secrets := project.database.secrets(databaseURL)
		mongoDBProductionAssertSecretsAbsent(t, logs, secrets...)
		if stopErr != nil {
			t.Errorf("clean up MongoDB deployment server %s: %v\n%s", name, stopErr, mongoDBProductionRedacted(logs, secrets...))
		}
	})
	return server
}

func (server *mongoDBProductionDeploymentServer) baseURL() string {
	return "http://" + server.address
}

func (server *mongoDBProductionDeploymentServer) logs() string {
	encoded, err := os.ReadFile(server.logPath)
	if err != nil {
		return fmt.Sprintf("read deployment log: %v", err)
	}
	return string(encoded)
}

func (server *mongoDBProductionDeploymentServer) wait(timeout time.Duration) error {
	select {
	case <-server.done:
		server.mutex.Lock()
		defer server.mutex.Unlock()
		return server.waitErr
	case <-time.After(timeout):
		return fmt.Errorf("%w: server %s did not exit within %s", errMongoDBProductionDeploymentExitTimeout, server.name, timeout)
	}
}

func (server *mongoDBProductionDeploymentServer) expectedFailure(timeout time.Duration) (string, error, error) {
	waitErr := server.wait(timeout)
	if errors.Is(waitErr, errMongoDBProductionDeploymentExitTimeout) {
		removeErr := server.forceRemove(timeout)
		return server.logs(), nil, errors.Join(waitErr, removeErr)
	}
	server.stopOnce.Do(func() {
		server.stopErr = server.logFile.Close()
	})
	return server.logs(), waitErr, server.stopErr
}

func (server *mongoDBProductionDeploymentServer) stop(timeout time.Duration) error {
	server.stopOnce.Do(func() {
		select {
		case <-server.done:
		default:
			output, err := server.harness.run(timeout, server.project.root, nil, "docker", "stop", "--time", "6", server.name)
			if err != nil {
				server.stopErr = fmt.Errorf("stop container: %w: %s", err, strings.TrimSpace(output))
			}
		}
		if err := server.wait(timeout); server.stopErr == nil && err != nil {
			server.stopErr = err
		}
		if err := server.logFile.Close(); server.stopErr == nil && err != nil {
			server.stopErr = err
		}
	})
	return server.stopErr
}

func (server *mongoDBProductionDeploymentServer) forceStop(timeout time.Duration) error {
	select {
	case <-server.done:
		return server.stop(timeout)
	default:
	}
	if err := server.stop(timeout); err == nil {
		return nil
	}
	removeErr := server.forceRemove(timeout)
	_ = server.logFile.Close()
	return errors.Join(server.stopErr, removeErr)
}

func (server *mongoDBProductionDeploymentServer) forceRemove(timeout time.Duration) error {
	output, removeErr := server.harness.run(timeout, server.project.root, nil, "docker", "rm", "--force", server.name)
	select {
	case <-server.done:
		if removeErr != nil {
			return fmt.Errorf("force-remove deployment container: %w: %s", removeErr, strings.TrimSpace(output))
		}
		return nil
	case <-time.After(timeout):
		_ = server.command.Process.Kill()
		select {
		case <-server.done:
		case <-time.After(2 * time.Second):
			return errors.Join(removeErr, fmt.Errorf("docker run process for %s did not exit after force removal", server.name))
		}
	}
	if removeErr != nil {
		return fmt.Errorf("force-remove deployment container: %w: %s", removeErr, strings.TrimSpace(output))
	}
	return fmt.Errorf("docker run process for %s required a host-process kill after force removal", server.name)
}

func mongoDBProductionDeploymentWaitReady(t *testing.T, server *mongoDBProductionDeploymentServer, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		response, err := client.Get(server.baseURL() + "/readyz")
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		select {
		case <-server.done:
			t.Fatalf("generated MongoDB deployment server exited before readiness\n%s", mongoDBProductionRedacted(server.logs(), server.project.database.secrets()...))
		default:
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for generated MongoDB deployment readiness\n%s", mongoDBProductionRedacted(server.logs(), server.project.database.secrets()...))
}

func mongoDBProductionDeploymentWaitUnavailable(t *testing.T, target string, timeout time.Duration) {
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
	t.Fatalf("deployment endpoint %s remained available after %s", target, timeout)
}

func mongoDBProductionDeploymentFreePort(t *testing.T) int {
	t.Helper()
	seed, err := strconv.ParseUint(mongoDBProductionRandomHex(t, 4), 16, 32)
	if err != nil {
		t.Fatalf("parse deployment port seed: %v", err)
	}

	// Listening on :0 returns an ephemeral client port. The generated server is
	// launched in a host-networked container after that probe closes, so a busy
	// test can legitimately reuse the same ephemeral port for an outgoing socket
	// before the container binds it. Probe a process-reserved, non-ephemeral test
	// range instead and retain the lease for the lifetime of the calling test.
	mongoDBProductionDeploymentPortLeases.Lock()
	defer mongoDBProductionDeploymentPortLeases.Unlock()
	for offset := 0; offset < mongoDBProductionDeploymentTestPortCount; offset++ {
		port := mongoDBProductionDeploymentFirstTestPort + (int(seed)+offset)%mongoDBProductionDeploymentTestPortCount
		if _, reserved := mongoDBProductionDeploymentPortLeases.ports[port]; reserved {
			continue
		}
		listener, listenErr := net.Listen("tcp4", fmt.Sprintf("0.0.0.0:%d", port))
		if listenErr != nil {
			continue
		}
		if closeErr := listener.Close(); closeErr != nil {
			t.Fatalf("release deployment port probe %d: %v", port, closeErr)
		}
		mongoDBProductionDeploymentPortLeases.ports[port] = struct{}{}
		t.Cleanup(func() {
			mongoDBProductionDeploymentPortLeases.Lock()
			delete(mongoDBProductionDeploymentPortLeases.ports, port)
			mongoDBProductionDeploymentPortLeases.Unlock()
		})
		return port
	}
	t.Fatalf("find an unused deployment port in %d-%d", mongoDBProductionDeploymentFirstTestPort, mongoDBProductionDeploymentFirstTestPort+mongoDBProductionDeploymentTestPortCount-1)
	return 0
}

func mongoDBProductionDeploymentHTTPClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar, Timeout: 8 * time.Second}
}

func mongoDBProductionDeploymentRequest(
	t *testing.T,
	client *http.Client,
	method string,
	target string,
	body io.Reader,
	contentType string,
	wantStatus int,
	destination any,
) []byte {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, target, body)
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	defer response.Body.Close()
	encoded, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != wantStatus {
		t.Fatalf("%s %s returned %s, want %d: %s", method, target, response.Status, wantStatus, encoded)
	}
	if destination != nil {
		if err := json.Unmarshal(encoded, destination); err != nil {
			t.Fatalf("decode %s %s: %v\n%s", method, target, err, encoded)
		}
	}
	return encoded
}

func mongoDBProductionDeploymentDocument(t *testing.T, envelope map[string]any) map[string]any {
	t.Helper()
	document, ok := envelope["doc"].(map[string]any)
	if !ok {
		t.Fatalf("MongoDB deployment response has no document: %#v", envelope)
	}
	return document
}

func mongoDBProductionDeploymentBootstrapStarter(t *testing.T, baseURL string) mongoDBProductionDeploymentData {
	t.Helper()
	data := mongoDBProductionDeploymentData{
		email: "admin@mongodb-production.test", password: "mongodb-production-password",
		fileBody:     "MongoDB deployment upload survives backup and restore.\n",
		postTitle:    "MongoDB deployment continuity",
		rolloutTitle: "MongoDB deployment continuity after rollout",
		postStatus:   "published",
	}
	client := mongoDBProductionDeploymentHTTPClient(t)
	var envelope map[string]any
	mongoDBProductionDeploymentRequest(
		t, client, http.MethodPost, baseURL+"/api/auth/users/create-user",
		strings.NewReader(fmt.Sprintf(`{"data":{"email":%q},"password":%q}`, data.email, data.password)),
		"application/json", http.StatusCreated, &envelope,
	)
	data.userID = fmt.Sprint(mongoDBProductionDeploymentDocument(t, envelope)["id"])
	mongoDBProductionDeploymentLogin(t, client, baseURL, data.email, data.password)
	sessionURL, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	data.session = client.Jar.Cookies(sessionURL)
	if len(data.session) == 0 {
		t.Fatal("MongoDB deployment login returned no reusable session cookie")
	}

	envelope = nil
	post := map[string]any{
		"title": data.postTitle, "status": data.postStatus, "author": data.userID,
		"content": map[string]any{
			"version": 1,
			"root": map[string]any{
				"type": "root",
				"children": []any{map[string]any{
					"type":     "paragraph",
					"children": []any{map[string]any{"type": "text", "text": "MongoDB deployment rich text"}},
				}},
			},
		},
	}
	postBody, err := json.Marshal(post)
	if err != nil {
		t.Fatal(err)
	}
	mongoDBProductionDeploymentRequest(t, client, http.MethodPost, baseURL+"/api/collections/posts", bytes.NewReader(postBody), "application/json", http.StatusCreated, &envelope)
	data.postID = fmt.Sprint(mongoDBProductionDeploymentDocument(t, envelope)["id"])

	var multipartBody bytes.Buffer
	writer := multipart.NewWriter(&multipartBody)
	file, err := writer.CreateFormFile("file", "deployment.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(file, data.fileBody); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("data", `{"alt":"Deployment proof"}`); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	envelope = nil
	mongoDBProductionDeploymentRequest(t, client, http.MethodPost, baseURL+"/api/collections/media", &multipartBody, writer.FormDataContentType(), http.StatusCreated, &envelope)
	media := mongoDBProductionDeploymentDocument(t, envelope)
	data.mediaID = fmt.Sprint(media["id"])
	data.uploadURL = fmt.Sprint(media["url"])
	data.objectKey = fmt.Sprint(media["objectKey"])
	if data.userID == "" || data.postID == "" || data.mediaID == "" || data.uploadURL == "" || data.objectKey == "" {
		t.Fatalf("MongoDB deployment fixture returned incomplete identities: %s", data)
	}
	return data
}

func mongoDBProductionDeploymentLogin(t *testing.T, client *http.Client, baseURL, email, password string) {
	t.Helper()
	body, err := json.Marshal(map[string]string{"email": email, "password": password})
	if err != nil {
		t.Fatal(err)
	}
	mongoDBProductionDeploymentRequest(t, client, http.MethodPost, baseURL+"/api/auth/users/login", bytes.NewReader(body), "application/json", http.StatusOK, nil)
}

func mongoDBProductionDeploymentAuthenticatedClient(
	t *testing.T,
	baseURL string,
	data mongoDBProductionDeploymentData,
) *http.Client {
	t.Helper()
	if len(data.session) == 0 {
		t.Fatal("MongoDB deployment session cookie is unavailable")
	}
	target, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	client := mongoDBProductionDeploymentHTTPClient(t)
	client.Jar.SetCookies(target, data.session)
	return client
}

func (harness *mongoDBProductionDeploymentHarness) runSDKCanary(
	t *testing.T,
	project mongoDBProductionDeploymentProject,
	baseURL string,
	data mongoDBProductionDeploymentData,
) {
	t.Helper()
	testPath := filepath.Join(project.root, "mongodb-production-sdk.test.ts")
	if err := os.WriteFile(testPath, []byte(mongoDBProductionDeploymentSDKCanary), 0o600); err != nil {
		t.Fatalf("write generated MongoDB deployment SDK canary: %v", err)
	}
	overrides := map[string]string{
		"RIDU_MONGODB_DEPLOYMENT_API_URL":   baseURL,
		"RIDU_MONGODB_DEPLOYMENT_EMAIL":     data.email,
		"RIDU_MONGODB_DEPLOYMENT_PASSWORD":  data.password,
		"RIDU_MONGODB_DEPLOYMENT_AUTHOR_ID": data.userID,
	}
	output, err := harness.run(2*time.Minute, project.root, overrides, "bun", "test", filepath.Base(testPath))
	secrets := []string{data.email, data.password}
	if err != nil {
		t.Fatalf("run generated MongoDB deployment SDK canary: %v\n%s", err, mongoDBProductionRedacted(output, secrets...))
	}
	mongoDBProductionAssertSecretsAbsent(t, output, secrets...)
}

func (harness *mongoDBProductionDeploymentHarness) runAdminBrowserCanary(
	t *testing.T,
	server *mongoDBProductionDeploymentServer,
	data mongoDBProductionDeploymentData,
) mongoDBProductionDeploymentAdminCanary {
	t.Helper()
	playwrightCLI := filepath.Join(harness.frameworkRoot, "node_modules", "@playwright", "test", "cli.js")
	if _, err := os.Stat(playwrightCLI); err != nil {
		t.Fatalf("the pinned Playwright CLI is required for the MongoDB production admin proof: %v", err)
	}
	resultRoot := t.TempDir()
	resultPath := filepath.Join(resultRoot, "admin-canary.json")
	overrides := map[string]string{
		"RIDU_MONGODB_PRODUCTION_ADMIN_URL":             server.baseURL(),
		"RIDU_MONGODB_PRODUCTION_ADMIN_EMAIL":           data.email,
		"RIDU_MONGODB_PRODUCTION_ADMIN_PASSWORD":        data.password,
		"RIDU_MONGODB_PRODUCTION_ADMIN_RESULT":          resultPath,
		"RIDU_MONGODB_PRODUCTION_PLAYWRIGHT_OUTPUT_DIR": filepath.Join(resultRoot, "playwright"),
	}
	output, err := harness.run(
		3*time.Minute,
		harness.frameworkRoot,
		overrides,
		"bunx", "playwright", "test", "--config", "playwright.mongodb-production.config.ts",
	)
	secrets := []string{data.email, data.password}
	if err != nil {
		t.Fatalf(
			"run generated MongoDB production admin browser canary: %v\n%s\n%s",
			err,
			mongoDBProductionRedacted(output, secrets...),
			mongoDBProductionRedacted(server.logs(), append(server.project.database.secrets(), secrets...)...),
		)
	}
	mongoDBProductionAssertSecretsAbsent(t, output, secrets...)
	encoded, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatalf("read generated MongoDB production admin browser result: %v", err)
	}
	var canary mongoDBProductionDeploymentAdminCanary
	if err := json.Unmarshal(encoded, &canary); err != nil {
		t.Fatalf("decode generated MongoDB production admin browser result: %v", err)
	}
	if canary.PostID == "" || canary.Title == "" || canary.Content == "" {
		t.Fatalf("generated MongoDB production admin browser result is incomplete: %#v", canary)
	}
	return canary
}

func mongoDBProductionDeploymentAssertAdminCanary(
	t *testing.T,
	baseURL string,
	data mongoDBProductionDeploymentData,
	canary mongoDBProductionDeploymentAdminCanary,
) {
	t.Helper()
	client := mongoDBProductionDeploymentAuthenticatedClient(t, baseURL, data)
	var envelope map[string]any
	mongoDBProductionDeploymentRequest(
		t, client, http.MethodGet, baseURL+"/api/collections/posts/"+canary.PostID,
		nil, "", http.StatusOK, &envelope,
	)
	document := mongoDBProductionDeploymentDocument(t, envelope)
	if document["title"] != canary.Title {
		t.Fatalf("MongoDB deployment browser-authored title changed: got %#v, want %q", document["title"], canary.Title)
	}
	content, err := json.Marshal(document["content"])
	if err != nil || !strings.Contains(string(content), canary.Content) {
		t.Fatalf("MongoDB deployment browser-authored rich text changed: value=%#v error=%v", document["content"], err)
	}
}

func mongoDBProductionDeploymentAssertStarter(
	t *testing.T,
	baseURL string,
	data mongoDBProductionDeploymentData,
	wantSummary bool,
) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	var lastError string
	for time.Now().Before(deadline) {
		client := mongoDBProductionDeploymentAuthenticatedClient(t, baseURL, data)
		var postEnvelope map[string]any
		postResponse, postErr := client.Get(baseURL + "/api/collections/posts/" + data.postID)
		if postErr != nil {
			lastError = postErr.Error()
			time.Sleep(200 * time.Millisecond)
			continue
		}
		postEncoded, readErr := io.ReadAll(postResponse.Body)
		_ = postResponse.Body.Close()
		if readErr != nil || postResponse.StatusCode != http.StatusOK || json.Unmarshal(postEncoded, &postEnvelope) != nil {
			lastError = fmt.Sprintf("post: status=%s body=%s error=%v", postResponse.Status, postEncoded, readErr)
			time.Sleep(200 * time.Millisecond)
			continue
		}
		post := mongoDBProductionDeploymentDocument(t, postEnvelope)
		expectedTitle := data.postTitle
		if wantSummary {
			expectedTitle = data.rolloutTitle
		}
		if post["title"] != expectedTitle || post["status"] != data.postStatus {
			t.Fatalf("MongoDB deployment post changed: %#v", post)
		}
		content, marshalErr := json.Marshal(post["content"])
		if marshalErr != nil || !strings.Contains(string(content), "MongoDB deployment rich text") {
			t.Fatalf("MongoDB deployment rich text changed: value=%#v error=%v", post["content"], marshalErr)
		}
		if wantSummary && post["summary"] != "resumed migration" {
			t.Fatalf("MongoDB deployment schema-changing field = %#v", post["summary"])
		}
		var populatedEnvelope map[string]any
		mongoDBProductionDeploymentRequest(
			t, client, http.MethodGet, baseURL+"/api/collections/posts/"+data.postID+"?depth=1",
			nil, "", http.StatusOK, &populatedEnvelope,
		)
		populatedAuthor, ok := mongoDBProductionDeploymentDocument(t, populatedEnvelope)["author"].(map[string]any)
		if !ok || populatedAuthor["id"] != data.userID || populatedAuthor["email"] != data.email {
			t.Fatalf("MongoDB deployment relationship population changed: %#v", populatedAuthor)
		}
		var mediaEnvelope map[string]any
		mongoDBProductionDeploymentRequest(t, client, http.MethodGet, baseURL+"/api/collections/media/"+data.mediaID, nil, "", http.StatusOK, &mediaEnvelope)
		media := mongoDBProductionDeploymentDocument(t, mediaEnvelope)
		if media["objectKey"] != data.objectKey || media["url"] != data.uploadURL || media["alt"] != "Deployment proof" {
			t.Fatalf("MongoDB deployment upload metadata changed: %#v", media)
		}
		uploadTarget := data.uploadURL
		if strings.HasPrefix(uploadTarget, "/") {
			uploadTarget = baseURL + uploadTarget
		}
		encoded := mongoDBProductionDeploymentRequest(t, client, http.MethodGet, uploadTarget, nil, "", http.StatusOK, nil)
		if string(encoded) != data.fileBody {
			t.Fatalf("MongoDB deployment upload body = %q, want %q", encoded, data.fileBody)
		}
		return
	}
	t.Fatalf("MongoDB deployment data did not recover before deadline: %s", lastError)
}

func mongoDBProductionDeploymentUpdateSummary(t *testing.T, baseURL string, data mongoDBProductionDeploymentData) {
	t.Helper()
	client := mongoDBProductionDeploymentAuthenticatedClient(t, baseURL, data)
	var envelope map[string]any
	body, err := json.Marshal(map[string]string{"title": data.rolloutTitle, "summary": "resumed migration"})
	if err != nil {
		t.Fatal(err)
	}
	mongoDBProductionDeploymentRequest(
		t, client, http.MethodPost, baseURL+"/api/collections/posts/"+data.postID+"/publish",
		bytes.NewReader(body), "application/json", http.StatusOK, &envelope,
	)
	updated := mongoDBProductionDeploymentDocument(t, envelope)
	if updated["title"] != data.rolloutTitle || updated["summary"] != "resumed migration" {
		t.Fatalf("MongoDB deployment summary update failed: %#v", envelope)
	}
}

func mongoDBProductionDeploymentCaptureRestoreVersion(
	t *testing.T,
	baseURL string,
	data mongoDBProductionDeploymentData,
) mongoDBProductionDeploymentData {
	t.Helper()
	client := mongoDBProductionDeploymentAuthenticatedClient(t, baseURL, data)
	var envelope struct {
		Versions []struct {
			Revision int            `json:"Revision"`
			Snapshot map[string]any `json:"Snapshot"`
		} `json:"versions"`
	}
	mongoDBProductionDeploymentRequest(
		t, client, http.MethodGet, baseURL+"/api/collections/posts/"+data.postID+"/versions",
		nil, "", http.StatusOK, &envelope,
	)
	if len(envelope.Versions) < 2 {
		t.Fatalf("MongoDB deployment version history has %d entries after rollout update, want at least 2", len(envelope.Versions))
	}
	for _, version := range envelope.Versions {
		if version.Revision <= 0 || version.Snapshot["title"] != data.postTitle || version.Snapshot["summary"] == "resumed migration" {
			continue
		}
		content, err := json.Marshal(version.Snapshot["content"])
		if err == nil && strings.Contains(string(content), "MongoDB deployment rich text") {
			data.version = version.Revision
			return data
		}
	}
	t.Fatalf("MongoDB deployment history retained no pre-cutover canonical snapshot: %#v", envelope.Versions)
	return data
}

func mongoDBProductionDeploymentRestoreCapturedVersion(
	t *testing.T,
	baseURL string,
	data mongoDBProductionDeploymentData,
) {
	t.Helper()
	if data.version <= 0 {
		t.Fatal("MongoDB deployment restore version was not captured")
	}
	client := mongoDBProductionDeploymentAuthenticatedClient(t, baseURL, data)
	var envelope map[string]any
	mongoDBProductionDeploymentRequest(
		t, client, http.MethodPost,
		baseURL+"/api/collections/posts/"+data.postID+"/restore/"+strconv.Itoa(data.version),
		nil, "", http.StatusOK, &envelope,
	)
	restored := mongoDBProductionDeploymentDocument(t, envelope)
	if restored["title"] != data.postTitle || restored["status"] != data.postStatus {
		t.Fatalf("MongoDB deployment restored version is not the pre-cutover snapshot: %#v", restored)
	}
	if summary, present := restored["summary"]; present && summary != nil && summary != "" {
		t.Fatalf("MongoDB deployment restored version retained rollout-only summary: %#v", restored)
	}
	content, err := json.Marshal(restored["content"])
	if err != nil || !strings.Contains(string(content), "MongoDB deployment rich text") {
		t.Fatalf("MongoDB deployment restored version lost rich text: value=%#v error=%v", restored["content"], err)
	}
}

func mongoDBProductionDeploymentAssertBlank(t *testing.T, baseURL string) {
	t.Helper()
	client := mongoDBProductionDeploymentHTTPClient(t)
	const email = "blank@mongodb-production.test"
	const password = "mongodb-blank-production-password"
	var envelope map[string]any
	mongoDBProductionDeploymentRequest(
		t, client, http.MethodPost, baseURL+"/api/auth/users/create-user",
		strings.NewReader(`{"data":{"email":"blank@mongodb-production.test"},"password":"mongodb-blank-production-password"}`),
		"application/json", http.StatusCreated, &envelope,
	)
	mongoDBProductionDeploymentLogin(t, client, baseURL, email, password)
	var schemaEnvelope struct {
		Schema struct {
			Collections []struct {
				Slug string `json:"slug"`
			} `json:"collections"`
		} `json:"schema"`
	}
	mongoDBProductionDeploymentRequest(t, client, http.MethodGet, baseURL+"/api/schema", nil, "", http.StatusOK, &schemaEnvelope)
	if len(schemaEnvelope.Schema.Collections) != 1 || schemaEnvelope.Schema.Collections[0].Slug != "users" {
		t.Fatalf("generated MongoDB blank production schema = %#v", schemaEnvelope.Schema.Collections)
	}
}

func mongoDBProductionDeploymentAddIndexedFields(t *testing.T, project mongoDBProductionDeploymentProject, count int) {
	t.Helper()
	var fields strings.Builder
	for index := 1; index <= count; index++ {
		fmt.Fprintf(&fields, "\t\tfield.Text(\"deploymentIndex%02d\").Index(),\n", index)
	}
	fields.WriteString("\t\tfield.Text(\"summary\"),\n")
	path := filepath.Join(project.root, "content", "posts.go")
	mongoDBProductionReplaceFile(t, path,
		"\t\trichtext.Field(\"content\"),\n",
		"\t\trichtext.Field(\"content\"),\n"+fields.String(),
	)
}

func (harness *mongoDBProductionDeploymentHarness) startProjectCommand(
	t *testing.T,
	project mongoDBProductionDeploymentProject,
	databaseURL string,
	arguments ...string,
) (*exec.Cmd, string, <-chan error) {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "project-command.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(harness.cliBinary, arguments...)
	command.Dir = project.root
	command.Env = mongoDBProductionDeploymentEnvironment(harness.environment, map[string]string{"DATABASE_URL": databaseURL})
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		err := command.Wait()
		_ = logFile.Close()
		done <- err
		close(done)
	}()
	return command, logPath, done
}

func (harness *mongoDBProductionDeploymentHarness) interruptMigrationAfterProgress(
	t *testing.T,
	project mongoDBProductionDeploymentProject,
	artifact migrationartifact.File,
) {
	t.Helper()
	command, logPath, done := harness.startProjectCommand(t, project, project.database.hostURL, "migrate", "up", "--advisory-lock-wait", "45s")
	commandFinished := false
	defer func() {
		if commandFinished {
			return
		}
		_ = command.Process.Kill()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Errorf("interrupted MongoDB migration process did not stop within 5s")
		}
	}()
	observer, err := OpenWithConfig(t.Context(), productionMongoDBStoreConfig(project.database.hostURL, "ridu-deployment-observer"))
	if err != nil {
		_ = command.Process.Kill()
		<-done
		commandFinished = true
		t.Fatalf("open MongoDB deployment migration observer: %v", err)
	}
	defer observer.Close()
	totalSteps := 0
	for _, phase := range artifact.Artifact.Phases {
		totalSteps += len(phase.Steps)
	}
	deadline := time.Now().Add(45 * time.Second)
	completed := 0
	for time.Now().Before(deadline) {
		state, stateErr := observer.readMongoMigrationLedgerState(t.Context())
		if stateErr == nil {
			completed = 0
			artifactComplete := false
			for _, row := range state.artifacts {
				artifactComplete = artifactComplete || row.Name == artifact.Name
			}
			for _, row := range state.steps {
				if row.ArtifactName == artifact.Name && row.State == mongoMigrationStepComplete {
					completed++
				}
			}
			if completed > 0 && completed < totalSteps && !artifactComplete {
				if err := command.Process.Signal(syscall.SIGSTOP); err != nil {
					t.Fatalf("pause MongoDB migration after durable progress: %v", err)
				}
				if err := command.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
					t.Fatalf("interrupt MongoDB migration process: %v", err)
				}
				waitErr := <-done
				commandFinished = true
				if waitErr == nil {
					t.Fatal("interrupted MongoDB migration process exited successfully")
				}
				encoded, _ := os.ReadFile(logPath)
				mongoDBProductionAssertSecretsAbsent(t, string(encoded), project.database.secrets()...)
				return
			}
		}
		select {
		case waitErr := <-done:
			commandFinished = true
			encoded, _ := os.ReadFile(logPath)
			t.Fatalf("MongoDB migration finished before interruption (completed %d/%d, err=%v):\n%s", completed, totalSteps, waitErr, mongoDBProductionRedacted(string(encoded), project.database.secrets()...))
		default:
		}
		time.Sleep(5 * time.Millisecond)
	}
	_ = command.Process.Kill()
	<-done
	commandFinished = true
	encoded, _ := os.ReadFile(logPath)
	t.Fatalf("timed out waiting for partial MongoDB migration progress (completed %d/%d):\n%s", completed, totalSteps, mongoDBProductionRedacted(string(encoded), project.database.secrets()...))
}

func (harness *mongoDBProductionDeploymentHarness) backupDatabase(
	t *testing.T,
	database mongoDBProductionDeploymentDatabase,
	excludedDatabases ...string,
) string {
	t.Helper()
	archivePath := filepath.Join(t.TempDir(), "database.archive.gz")
	archive, err := os.OpenFile(archivePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	adminURL := harness.databaseToolsAdminURL(t, database)
	secrets := database.secrets(adminURL, harness.fixture.adminUser, harness.fixture.adminPassword)
	configPath, containerConfigPath := harness.databaseToolsConfig(t, adminURL, "mongodump")
	defer func() {
		if err := os.Remove(configPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Errorf("remove MongoDB deployment backup config: %v", err)
		}
	}()
	arguments := []string{
		"compose", "--project-name", harness.fixture.project, "--file", harness.fixture.composePath,
		"exec", "-T", "mongodb-1", "mongodump",
		"--config", containerConfigPath, "--archive", "--gzip", "--dumpDbUsersAndRoles",
	}
	command := exec.CommandContext(ctx, "docker", arguments...)
	command.Dir = harness.frameworkRoot
	command.Env = harness.fixture.environment
	command.Stdout = archive
	var stderr bytes.Buffer
	command.Stderr = &stderr
	err = command.Run()
	closeErr := archive.Close()
	if ctx.Err() != nil {
		t.Fatalf("MongoDB deployment backup timed out: %v", ctx.Err())
	}
	if err != nil || closeErr != nil {
		t.Fatalf("backup MongoDB deployment database: %v\n%s", errors.Join(err, closeErr), mongoDBProductionRedacted(stderr.String(), secrets...))
	}
	mongoDBProductionAssertSecretsAbsent(t, stderr.String(), secrets...)
	mongoDBProductionDeploymentAssertDatabaseToolsScope(
		t, "backup", stderr.String(), database.name,
		append([]string{"admin", harness.fixture.database}, excludedDatabases...), secrets...,
	)
	return archivePath
}

func (harness *mongoDBProductionDeploymentHarness) seedOutOfScopeDatabase(
	t *testing.T,
	target mongoDBProductionDeploymentDatabase,
) mongoDBProductionDeploymentScopeSentinel {
	t.Helper()
	token := mongoDBProductionRandomHex(t, 6)
	sentinel := mongoDBProductionDeploymentScopeSentinel{
		database:    "ridu_deployment_out_of_scope_" + token,
		collection:  "restore_scope_sentinel",
		documentID:  "scope-sentinel-" + token,
		beforeValue: "before-backup-" + token,
		afterValue:  "after-backup-" + token,
	}
	if sentinel.database == target.name || sentinel.database == harness.fixture.database {
		t.Fatal("MongoDB deployment out-of-scope sentinel database overlaps an in-scope database")
	}
	adminURL := harness.fixture.seedURL(harness.fixture.caPath, true)
	secrets := target.secrets(adminURL, harness.fixture.adminUser, harness.fixture.adminPassword)
	admin := harness.fixture.authenticatedAdminClient(t, adminURL)
	defer mongoDBProductionDeploymentDisconnectRedacted(t, admin, secrets...)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, err := admin.Database(sentinel.database).Collection(sentinel.collection).InsertOne(ctx, bson.D{
		{Key: "_id", Value: sentinel.documentID},
		{Key: "value", Value: sentinel.beforeValue},
	})
	if err != nil {
		mongoDBProductionAssertSecretsAbsent(t, err.Error(), secrets...)
		t.Fatalf("seed out-of-scope MongoDB deployment sentinel: %s", mongoDBProductionRedacted(err.Error(), secrets...))
	}
	t.Cleanup(func() {
		cleanupAdmin := harness.fixture.authenticatedAdminClient(t, adminURL)
		defer mongoDBProductionDeploymentDisconnectRedacted(t, cleanupAdmin, secrets...)
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		if err := cleanupAdmin.Database(sentinel.database).Drop(cleanupContext); err != nil {
			mongoDBProductionAssertSecretsAbsent(t, err.Error(), secrets...)
			t.Errorf("drop out-of-scope MongoDB deployment sentinel: %s", mongoDBProductionRedacted(err.Error(), secrets...))
		}
	})
	return sentinel
}

func (harness *mongoDBProductionDeploymentHarness) mutateOutOfScopeDatabase(
	t *testing.T,
	target mongoDBProductionDeploymentDatabase,
	sentinel mongoDBProductionDeploymentScopeSentinel,
) {
	t.Helper()
	adminURL := harness.fixture.seedURL(harness.fixture.caPath, true)
	secrets := target.secrets(adminURL, harness.fixture.adminUser, harness.fixture.adminPassword)
	admin := harness.fixture.authenticatedAdminClient(t, adminURL)
	defer mongoDBProductionDeploymentDisconnectRedacted(t, admin, secrets...)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	result, err := admin.Database(sentinel.database).Collection(sentinel.collection).UpdateOne(
		ctx,
		bson.D{{Key: "_id", Value: sentinel.documentID}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "value", Value: sentinel.afterValue}}}},
	)
	if err != nil {
		mongoDBProductionAssertSecretsAbsent(t, err.Error(), secrets...)
		t.Fatalf("mutate out-of-scope MongoDB deployment sentinel: %s", mongoDBProductionRedacted(err.Error(), secrets...))
	}
	if result.MatchedCount != 1 || result.ModifiedCount != 1 {
		t.Fatalf(
			"mutate out-of-scope MongoDB deployment sentinel matched %d and modified %d documents, want 1 and 1",
			result.MatchedCount, result.ModifiedCount,
		)
	}
}

func (harness *mongoDBProductionDeploymentHarness) assertOutOfScopeDatabase(
	t *testing.T,
	target mongoDBProductionDeploymentDatabase,
	sentinel mongoDBProductionDeploymentScopeSentinel,
) {
	t.Helper()
	adminURL := harness.fixture.seedURL(harness.fixture.caPath, true)
	secrets := target.secrets(adminURL, harness.fixture.adminUser, harness.fixture.adminPassword)
	admin := harness.fixture.authenticatedAdminClient(t, adminURL)
	defer mongoDBProductionDeploymentDisconnectRedacted(t, admin, secrets...)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var document struct {
		Value string `bson:"value"`
	}
	err := admin.Database(sentinel.database).Collection(sentinel.collection).
		FindOne(ctx, bson.D{{Key: "_id", Value: sentinel.documentID}}).Decode(&document)
	if err != nil {
		mongoDBProductionAssertSecretsAbsent(t, err.Error(), secrets...)
		t.Fatalf("read out-of-scope MongoDB deployment sentinel after restore: %s", mongoDBProductionRedacted(err.Error(), secrets...))
	}
	if document.Value != sentinel.afterValue {
		t.Fatalf("out-of-scope MongoDB deployment sentinel changed during target restore: got %q, want %q", document.Value, sentinel.afterValue)
	}
}

func mongoDBProductionDeploymentDisconnectRedacted(
	t *testing.T,
	client interface{ Disconnect(context.Context) error },
	secrets ...string,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Disconnect(ctx); err != nil {
		mongoDBProductionAssertSecretsAbsent(t, err.Error(), secrets...)
		t.Errorf("disconnect MongoDB deployment proof client: %s", mongoDBProductionRedacted(err.Error(), secrets...))
	}
}

func (harness *mongoDBProductionDeploymentHarness) dropDatabase(
	t *testing.T,
	database mongoDBProductionDeploymentDatabase,
) {
	t.Helper()
	admin := harness.fixture.authenticatedAdminClient(t, harness.fixture.seedURL(harness.fixture.caPath, true))
	defer disconnectMongoDBProductionClient(t, admin)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if err := admin.Database(database.name).Drop(ctx); err != nil {
		t.Fatalf("drop temporary MongoDB deployment database before restore: %v", err)
	}
}

func (harness *mongoDBProductionDeploymentHarness) dropScopedDatabaseUser(
	t *testing.T,
	database mongoDBProductionDeploymentDatabase,
) {
	t.Helper()
	adminURL := harness.fixture.seedURL(harness.fixture.caPath, true)
	secrets := database.secrets(adminURL, harness.fixture.adminUser, harness.fixture.adminPassword)
	admin := harness.fixture.authenticatedAdminClient(t, adminURL)
	defer mongoDBProductionDeploymentDisconnectRedacted(t, admin, secrets...)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if err := admin.Database(database.name).RunCommand(ctx, bson.D{{Key: "dropUser", Value: database.username}}).Err(); err != nil {
		encoded := err.Error()
		mongoDBProductionAssertSecretsAbsent(t, encoded, secrets...)
		t.Fatalf("drop scoped MongoDB deployment user before recovery restore: %s", mongoDBProductionRedacted(encoded, secrets...))
	}
}

func (harness *mongoDBProductionDeploymentHarness) restoreDatabase(
	t *testing.T,
	database mongoDBProductionDeploymentDatabase,
	archivePath string,
	excludedDatabases ...string,
) {
	t.Helper()
	archive, err := os.Open(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	adminURL := harness.databaseToolsAdminURL(t, database)
	secrets := database.secrets(adminURL, harness.fixture.adminUser, harness.fixture.adminPassword)
	configPath, containerConfigPath := harness.databaseToolsConfig(t, adminURL, "mongorestore")
	defer func() {
		if err := os.Remove(configPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Errorf("remove MongoDB deployment restore config: %v", err)
		}
	}()
	arguments := []string{
		"compose", "--project-name", harness.fixture.project, "--file", harness.fixture.composePath,
		"exec", "-T", "mongodb-1", "mongorestore",
		"--config", containerConfigPath, "--archive", "--gzip", "--drop", "--restoreDbUsersAndRoles",
	}
	command := exec.CommandContext(ctx, "docker", arguments...)
	command.Dir = harness.frameworkRoot
	command.Env = harness.fixture.environment
	command.Stdin = archive
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	err = command.Run()
	if ctx.Err() != nil {
		t.Fatalf("MongoDB deployment restore timed out: %v", ctx.Err())
	}
	if err != nil {
		t.Fatalf("restore MongoDB deployment database: %v\n%s", err, mongoDBProductionRedacted(output.String(), secrets...))
	}
	mongoDBProductionAssertSecretsAbsent(t, output.String(), secrets...)
	mongoDBProductionDeploymentAssertDatabaseToolsScope(
		t, "restore", output.String(), database.name,
		append([]string{"admin", harness.fixture.database}, excludedDatabases...), secrets...,
	)
}

func mongoDBProductionDeploymentAssertDatabaseToolsScope(
	t *testing.T,
	operation string,
	output string,
	target string,
	excluded []string,
	secrets ...string,
) {
	t.Helper()
	if !strings.Contains(output, target+".") {
		t.Fatalf("MongoDB deployment %s output did not name the scoped database %q:\n%s", operation, target, mongoDBProductionRedacted(output, secrets...))
	}
	for _, database := range excluded {
		if database != "" && database != target && strings.Contains(output, database+".") {
			t.Fatalf("MongoDB deployment %s crossed into database %q:\n%s", operation, database, mongoDBProductionRedacted(output, secrets...))
		}
	}
}

func (harness *mongoDBProductionDeploymentHarness) assertScopedDatabaseUserRestored(
	t *testing.T,
	database mongoDBProductionDeploymentDatabase,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	backend, err := OpenWithConfig(ctx, productionMongoDBStoreConfig(database.hostURL, "ridu-deployment-restored-user-proof"))
	if err != nil {
		encoded := err.Error()
		mongoDBProductionAssertSecretsAbsent(t, encoded, database.secrets()...)
		t.Fatalf("connect with restored scoped MongoDB deployment user: %s", mongoDBProductionRedacted(encoded, database.secrets()...))
	}
	defer func() {
		if err := backend.Close(); err != nil {
			encoded := err.Error()
			mongoDBProductionAssertSecretsAbsent(t, encoded, database.secrets()...)
			t.Errorf("close restored scoped MongoDB deployment user proof: %s", mongoDBProductionRedacted(encoded, database.secrets()...))
		}
	}()
	if err := backend.Ping(ctx); err != nil {
		encoded := err.Error()
		mongoDBProductionAssertSecretsAbsent(t, encoded, database.secrets()...)
		t.Fatalf("ping with restored scoped MongoDB deployment user: %s", mongoDBProductionRedacted(encoded, database.secrets()...))
	}
}

func (harness *mongoDBProductionDeploymentHarness) databaseToolsConfig(
	t *testing.T,
	adminURL string,
	label string,
) (string, string) {
	t.Helper()
	name := label + "-" + mongoDBProductionRandomHex(t, 6) + ".yml"
	hostPath := filepath.Join(harness.fixture.root, name)
	// A quoted JSON string is also a valid YAML scalar. Keeping the URI in this
	// mode-0600 file prevents Database Tools credentials from appearing in the
	// host or container process argv.
	contents := "uri: " + strconv.Quote(adminURL) + "\n"
	if err := os.WriteFile(hostPath, []byte(contents), 0o600); err != nil {
		t.Fatalf("write MongoDB deployment %s config: %v", label, err)
	}
	info, err := os.Stat(hostPath)
	if err != nil {
		t.Fatal(err)
	}
	if permissions := info.Mode().Perm(); permissions != 0o600 {
		t.Fatalf("MongoDB deployment %s config permissions = %o, want 600", label, permissions)
	}
	return hostPath, filepath.Join(mongoDBProductionContainerFixtureRoot, "secrets", name)
}

func (harness *mongoDBProductionDeploymentHarness) databaseToolsAdminURL(
	t *testing.T,
	database mongoDBProductionDeploymentDatabase,
) string {
	t.Helper()
	connectionURL := mongoDBProductionURL(
		harness.fixture.hosts(), database.name, harness.fixture.adminUser, harness.fixture.adminPassword,
		filepath.Join(mongoDBProductionContainerFixtureRoot, "secrets", "ca.pem"), true, false, true,
	)
	parsed, err := url.Parse(connectionURL)
	if err != nil {
		t.Fatalf("build MongoDB Database Tools administrator URL: %v", err)
	}
	query := parsed.Query()
	query.Set("authSource", "admin")
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func mongoDBProductionDeploymentCopyTree(source, target string) error {
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o750)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		defer input.Close()
		info, err := entry.Info()
		if err != nil {
			return err
		}
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(output, input)
		return errors.Join(copyErr, output.Close())
	})
}
