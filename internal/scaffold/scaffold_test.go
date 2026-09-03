package scaffold_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/riducms/ridu/internal/projectfile"
	"github.com/riducms/ridu/internal/scaffold"
)

func TestCreateRendersProjectWithoutAbsoluteFrameworkPaths(t *testing.T) {
	target := filepath.Join(t.TempDir(), "acme-content")
	frameworkRoot := testModuleRoot(t)
	created, err := scaffold.Create(scaffold.Options{
		Target:           target,
		ModulePath:       "example.com/acme/acme-content",
		NPMScope:         "@acme",
		FrameworkVersion: "v1.2.3-beta.1",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created != target {
		t.Fatalf("created target = %q, want %q", created, target)
	}
	for _, relative := range []string{
		"go.mod",
		"package.json",
		"compose.yaml",
		"ridu.toml",
		"README.md",
		"PROJECT.md",
		"AGENTS.md",
		".ridu-agent-docs.json",
		".agents/skills/ridu-project/SKILL.md",
		".agents/skills/ridu-project/reference/fields.md",
		".agents/skills/payload-to-ridu/SKILL.md",
		".agents/skills/payload-to-ridu/references/migration-guide.md",
		"cmd/server/main.go",
		"content/config.go",
		"content/ridu_plugins.generated.go",
		"content/users.go",
		"content/posts.go",
		"internal/adminassets/assets.go",
		"internal/adminassets/dist/placeholder.txt",
		"admin/package.json",
		"admin/index.html",
		"admin/public/favicon.svg",
		"admin/tsconfig.json",
		"admin/vite.config.ts",
		"admin/src/main.ts",
		"admin/src/plugins.ts",
		"ridu.plugins.json",
	} {
		if _, err := os.Stat(filepath.Join(target, relative)); err != nil {
			t.Errorf("generated %s: %v", relative, err)
		}
	}
	compose, err := os.ReadFile(filepath.Join(target, "compose.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(compose), `"54329:5432"`) {
		t.Fatalf("generated database does not use Ridu's dedicated development port:\n%s", compose)
	}
	gitIgnore, err := os.ReadFile(filepath.Join(target, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gitIgnore), "!internal/adminassets/dist/\n") ||
		!strings.Contains(string(gitIgnore), "!internal/adminassets/dist/placeholder.txt\n") {
		t.Fatalf("generated project cannot retain its embedded admin placeholder:\n%s", gitIgnore)
	}

	if err := filepath.WalkDir(target, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(content), frameworkRoot) {
			t.Errorf("generated file %s contains local absolute path", path)
		}
		if strings.Contains(string(content), "replace github.com/riducms/ridu =>") {
			t.Errorf("generated file %s contains a local framework replace directive", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	contentConfig, err := os.ReadFile(filepath.Join(target, "content", "config.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(contentConfig), "ID:") {
		t.Fatalf("starter schema exposes routine stable-ID ceremony:\n%s", contentConfig)
	}
	usersContent, err := os.ReadFile(filepath.Join(target, "content", "users.go"))
	if err != nil {
		t.Fatal(err)
	}
	postsContent, err := os.ReadFile(filepath.Join(target, "content", "posts.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(usersContent), "var Users = ridu.Collection{") ||
		!strings.Contains(string(postsContent), "var Posts = ridu.Collection{") {
		t.Fatalf("generated starter does not declare static collections as variables:\nusers:\n%s\nposts:\n%s", usersContent, postsContent)
	}
	if !strings.Contains(string(usersContent), "Admin:  authenticatedOnly") ||
		!strings.Contains(string(usersContent), "Read:   authenticatedOnly") ||
		!strings.Contains(string(usersContent), "Update: authenticatedOnly") ||
		!strings.Contains(string(usersContent), "Delete: authenticatedOnly") ||
		!strings.Contains(string(postsContent), "Create: authenticatedOnly") ||
		!strings.Contains(string(postsContent), "Read:   authenticatedOnly") ||
		!strings.Contains(string(postsContent), "Update: authenticatedOnly") ||
		!strings.Contains(string(postsContent), "Delete: authenticatedOnly") {
		t.Fatalf("generated starter access is not fail closed:\nusers:\n%s\nposts:\n%s", usersContent, postsContent)
	}
	adminMain, err := os.ReadFile(filepath.Join(target, "admin", "src", "main.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(adminMain), "unknown as") {
		t.Fatalf("generated admin contains an unsafe integration cast:\n%s", adminMain)
	}
	if !strings.Contains(string(adminMain), `from "../../generated/ridu.generated"`) {
		t.Fatalf("generated admin does not import the direct generated client:\n%s", adminMain)
	}
	if !strings.Contains(string(adminMain), "mountAdmin<RiduConfig>") {
		t.Fatalf("generated admin does not bind the exact generated config type:\n%s", adminMain)
	}
	adminVite, err := os.ReadFile(filepath.Join(target, "admin", "vite.config.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(adminVite), `from "@riducms/build/vite"`) ||
		!strings.Contains(string(adminVite), `createAdminApplicationConfig({`) ||
		!strings.Contains(string(adminVite), `svelte: { preprocess: [lexicalPreprocess()] }`) ||
		!strings.Contains(string(adminVite), `schemaReloadSignal:`) ||
		!strings.Contains(string(adminVite), `cacheDir: resolve(adminRoot, "../.ridu/vite")`) {
		t.Fatalf("generated admin cannot consume the framework package source:\n%s", adminVite)
	}
	rootPackage, err := os.ReadFile(filepath.Join(target, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rootPackage), `"lexical": "0.49.0"`) ||
		!strings.Contains(string(rootPackage), `"@lexical/devtools-core": "0.49.0"`) ||
		!strings.Contains(string(rootPackage), `"nanoid": "3.3.18"`) {
		t.Fatalf("generated workspace does not pin its compatibility/security overrides:\n%s", rootPackage)
	}
	if !strings.Contains(string(rootPackage), `"workspaces": ["admin", ".ridu/packages/*"]`) ||
		strings.Contains(string(rootPackage), `"packages/*"`) {
		t.Fatalf("generated workspace exposes framework plumbing in packages/:\n%s", rootPackage)
	}
	if !strings.Contains(string(rootPackage), `"@riducms/sdk": "1.2.3-beta.1"`) {
		t.Fatalf("generated workspace does not own the SDK imported from generated/:\n%s", rootPackage)
	}
	if !strings.Contains(string(rootPackage), `"@riducms/plugin-richtext": "1.2.3-beta.1"`) {
		t.Fatalf("generated workspace does not own plugin types imported from generated/:\n%s", rootPackage)
	}
	if !strings.Contains(string(rootPackage), `"@riducms/plugin-seo": "1.2.3-beta.1"`) {
		t.Fatalf("generated workspace does not own the SEO admin package imported from generated/:\n%s", rootPackage)
	}
	if !strings.Contains(string(rootPackage), `"@riducms/plugin-form-builder": "1.2.3-beta.1"`) {
		t.Fatalf("generated workspace does not own the Form Builder package imported from generated/:\n%s", rootPackage)
	}
	if !strings.Contains(string(rootPackage), `"@riducms/cli": "1.2.3-beta.1"`) {
		t.Fatalf("generated workspace does not pin its matching project-local CLI:\n%s", rootPackage)
	}
	var generatedPackage struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
		Overrides       map[string]string `json:"overrides"`
		Scripts         map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(rootPackage, &generatedPackage); err != nil {
		t.Fatalf("decode generated package.json: %v", err)
	}
	for name, version := range generatedPackage.Overrides {
		if (name == "lexical" || strings.HasPrefix(name, "@lexical/")) && version != "0.49.0" {
			t.Fatalf("generated workspace override %s = %s, want the rich-text plugin's Lexical 0.49.0 line", name, version)
		}
	}
	for name, version := range generatedPackage.Dependencies {
		if strings.HasPrefix(name, "@riducms/") && version != "1.2.3-beta.1" {
			t.Fatalf("generated workspace dependency %s = %s, want exact coordinated version 1.2.3-beta.1", name, version)
		}
	}
	for name, version := range generatedPackage.DevDependencies {
		if strings.HasPrefix(name, "@riducms/") && version != "1.2.3-beta.1" {
			t.Fatalf("generated workspace development dependency %s = %s, want exact coordinated version 1.2.3-beta.1", name, version)
		}
	}
	for script, command := range map[string]string{
		"dev":       "ridu dev",
		"dev:admin": "npm run dev --workspace @acme/admin",
		"ridu":      "ridu",
		"check":     "ridu check",
		"build":     "ridu build",
	} {
		if generatedPackage.Scripts[script] != command {
			t.Fatalf("generated workspace script %s = %q, want %q", script, generatedPackage.Scripts[script], command)
		}
	}
	adminPackage, err := os.ReadFile(filepath.Join(target, "admin", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(adminPackage), `"@hvniel/lexical-svelte": "^0.1.1"`) {
		t.Fatalf("generated admin does not use the rich-text plugin's Lexical bridge line:\n%s", adminPackage)
	}
	if !strings.Contains(string(adminPackage), `"@riducms/ui": "1.2.3-beta.1"`) {
		t.Fatalf("generated admin does not declare the source package used by its TypeScript aliases:\n%s", adminPackage)
	}
	if strings.Contains(string(adminPackage), `"@riducms/sdk"`) || strings.Contains(string(adminPackage), "ridu-client") {
		t.Fatalf("generated admin duplicates the project-owned SDK or old client package:\n%s", adminPackage)
	}
	var generatedAdminPackage struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(adminPackage, &generatedAdminPackage); err != nil {
		t.Fatalf("decode generated admin package.json: %v", err)
	}
	for _, dependencies := range []map[string]string{generatedAdminPackage.Dependencies, generatedAdminPackage.DevDependencies} {
		for name, version := range dependencies {
			if strings.HasPrefix(name, "@riducms/") && version != "1.2.3-beta.1" {
				t.Fatalf("generated admin dependency %s = %s, want exact coordinated version 1.2.3-beta.1", name, version)
			}
		}
	}
	serverMain, err := os.ReadFile(filepath.Join(target, "cmd", "server", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	serverText := string(serverMain)
	formattedServer, err := format.Source(serverMain)
	if err != nil || !bytes.Equal(formattedServer, serverMain) {
		t.Fatalf("generated PostgreSQL runtime is not gofmt-formatted: %v\n%s", err, serverMain)
	}
	for _, productionBoundary := range []string{
		`if len(os.Args) == 1 {`,
		`options = runtimeOptions(applicationConfig)`,
		`AllowInsecureTransport:   envBool("RIDU_ALLOW_INSECURE_DATABASE")`,
		`MaxUploadLockConnections: envInt32("RIDU_POSTGRES_UPLOAD_LOCK_CONNECTIONS")`,
		`AllowedOrigins:          envList("RIDU_ALLOWED_ORIGINS")`,
		`AllowedHosts:            envList("RIDU_ALLOWED_HOSTS")`,
		`TrustedProxyCIDRs:       envList("RIDU_TRUSTED_PROXY_CIDRS")`,
		`StrictTransportSecurity: os.Getenv("RIDU_STRICT_TRANSPORT_SECURITY")`,
		`AllowUnverifiableReadiness: envBool("RIDU_ALLOW_UNVERIFIABLE_READINESS")`,
		`SkipReadinessPreflight:     envBool("RIDU_SKIP_READINESS_PREFLIGHT")`,
		`ShutdownTimeout:            envDuration("RIDU_SHUTDOWN_TIMEOUT")`,
		`WorkerDrainTimeout:         envDuration("RIDU_WORKER_DRAIN_TIMEOUT")`,
	} {
		if !strings.Contains(serverText, productionBoundary) {
			t.Errorf("generated server is missing production boundary %q:\n%s", productionBoundary, serverMain)
		}
	}
	adminTypeScript, err := os.ReadFile(filepath.Join(target, "admin", "tsconfig.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(adminTypeScript), `"@/*": ["./src/*"]`) ||
		!strings.Contains(string(adminTypeScript), `"allowImportingTsExtensions": true`) ||
		!strings.Contains(string(adminTypeScript), `"noEmit": true`) ||
		!strings.Contains(string(adminTypeScript), `"../generated/**/*.ts"`) {
		t.Fatalf("generated admin does not use its source-root import alias:\n%s", adminTypeScript)
	}
	for alias, sources := range map[string][]string{
		"@admin/*":           {"./node_modules/@riducms/admin/src/*", "../node_modules/@riducms/admin/src/*"},
		"@ui/*":              {"./node_modules/@riducms/ui/src/*", "../node_modules/@riducms/ui/src/*"},
		"@plugin-richtext/*": {"./node_modules/@riducms/plugin-richtext/src/*", "../node_modules/@riducms/plugin-richtext/src/*"},
		"@plugin-seo/*":      {"./node_modules/@riducms/plugin-seo/src/*", "../node_modules/@riducms/plugin-seo/src/*"},
	} {
		expected := fmt.Sprintf(`%q: [%q, %q]`, alias, sources[0], sources[1])
		if !strings.Contains(string(adminTypeScript), expected) {
			t.Fatalf("generated admin is missing collision-free aliases for %s:\n%s", alias, adminTypeScript)
		}
	}
	projectDefinition, err := os.ReadFile(filepath.Join(target, "ridu.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(projectDefinition), `client = "./generated/ridu.generated.ts"`) {
		t.Fatalf("generated project does not target one visible contract directory:\n%s", projectDefinition)
	}
	if _, err := os.Stat(filepath.Join(target, "packages")); !os.IsNotExist(err) {
		t.Fatalf("generated project should not contain a visible packages directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "Makefile")); !os.IsNotExist(err) {
		t.Fatalf("generated project should not contain a redundant Makefile: %v", err)
	}
	goModule, err := os.ReadFile(filepath.Join(target, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(goModule), "github.com/riducms/ridu v1.2.3-beta.1") {
		t.Fatalf("generated go.mod does not use the CLI release version:\n%s", goModule)
	}
	if !strings.Contains(string(goModule), "go 1.25.13") {
		t.Fatalf("generated go.mod does not require the security-patched Go toolchain:\n%s", goModule)
	}
	for _, relative := range []string{"admin/package.json"} {
		packageFile, err := os.ReadFile(filepath.Join(target, relative))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(packageFile), `"@riducms/admin": "1.2.3-beta.1"`) ||
			strings.Contains(string(packageFile), `"@riducms/admin": "^1.2.3-beta.1"`) {
			t.Fatalf("generated %s does not use the CLI release version:\n%s", relative, packageFile)
		}
	}
}

func TestCreateRendersEachSupportedPackageManager(t *testing.T) {
	for _, test := range []struct {
		manager        projectfile.PackageManager
		adminScript    string
		installCommand string
		runCommand     string
		riduCommand    string
		lockfile       string
		extraFile      string
	}{
		{projectfile.PackageManagerNPM, "npm run dev --workspace @acme/admin", "npm install", "npm run dev", "npm run ridu -- generate", "package-lock.json", ""},
		{projectfile.PackageManagerBun, "bun run --cwd admin dev", "bun install", "bun run dev", "bun run ridu -- generate", "bun.lock", ""},
		{projectfile.PackageManagerPNPM, "pnpm --dir admin run dev", "pnpm install", "pnpm run dev", "pnpm run ridu generate", "pnpm-lock.yaml", "pnpm-workspace.yaml"},
		{projectfile.PackageManagerYarn, "yarn --cwd admin run dev", "yarn install", "yarn run dev", "yarn run ridu generate", "yarn.lock", ".yarnrc.yml"},
	} {
		t.Run(string(test.manager), func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "content")
			_, err := scaffold.Create(scaffold.Options{
				Target:           target,
				ModulePath:       "example.com/acme/content",
				NPMScope:         "@acme",
				FrameworkVersion: "1.2.3",
				PackageManager:   test.manager,
			})
			if err != nil {
				t.Fatal(err)
			}
			project, err := os.ReadFile(filepath.Join(target, "ridu.toml"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(project), `package_manager = "`+string(test.manager)+`"`) {
				t.Fatalf("ridu.toml does not select %s:\n%s", test.manager, project)
			}
			manifest, err := os.ReadFile(filepath.Join(target, "package.json"))
			if err != nil {
				t.Fatal(err)
			}
			var packageJSON struct {
				PackageManager string            `json:"packageManager"`
				Scripts        map[string]string `json:"scripts"`
				Overrides      map[string]string `json:"overrides"`
				Resolutions    map[string]string `json:"resolutions"`
			}
			if err := json.Unmarshal(manifest, &packageJSON); err != nil {
				t.Fatal(err)
			}
			if packageJSON.PackageManager != "" {
				t.Fatalf("package.json forces packageManager %q", packageJSON.PackageManager)
			}
			if packageJSON.Scripts["dev:admin"] != test.adminScript || packageJSON.Scripts["check"] != "ridu check" || packageJSON.Scripts["build"] != "ridu build" {
				t.Fatalf("scripts = %#v", packageJSON.Scripts)
			}
			if packageJSON.Overrides["lexical"] != "0.49.0" || packageJSON.Resolutions["lexical"] != "0.49.0" {
				t.Fatalf("cross-manager dependency pins are incomplete: overrides=%#v resolutions=%#v", packageJSON.Overrides, packageJSON.Resolutions)
			}
			guide, err := os.ReadFile(filepath.Join(target, "PROJECT.md"))
			if err != nil {
				t.Fatal(err)
			}
			readme, err := os.ReadFile(filepath.Join(target, "README.md"))
			if err != nil {
				t.Fatal(err)
			}
			guidance := string(readme) + "\n" + string(guide)
			for _, expected := range []string{test.installCommand, test.runCommand, test.riduCommand, test.lockfile} {
				if !strings.Contains(guidance, expected) {
					t.Fatalf("%s generated guidance is missing %q:\n%s", test.manager, expected, guidance)
				}
			}
			for _, conditional := range []string{"pnpm-workspace.yaml", ".yarnrc.yml"} {
				_, statError := os.Stat(filepath.Join(target, conditional))
				if conditional == test.extraFile && statError != nil {
					t.Fatalf("%s scaffold missing %s: %v", test.manager, conditional, statError)
				}
				if conditional != test.extraFile && !os.IsNotExist(statError) {
					t.Fatalf("%s scaffold unexpectedly contains %s", test.manager, conditional)
				}
			}
			if test.manager == projectfile.PackageManagerPNPM {
				workspace, err := os.ReadFile(filepath.Join(target, "pnpm-workspace.yaml"))
				if err != nil || !strings.Contains(string(workspace), "linkWorkspacePackages: true") {
					t.Fatalf("pnpm workspace does not link release-rehearsal packages: %v\n%s", err, workspace)
				}
			}
		})
	}
}

func TestCreateBlankRendersOnlyTheRequiredAdminAuthCollection(t *testing.T) {
	target := filepath.Join(t.TempDir(), "blank-content")
	_, err := scaffold.Create(scaffold.Options{
		Target:           target,
		ModulePath:       "example.com/acme/blank-content",
		NPMScope:         "@acme",
		FrameworkVersion: "v1.2.3",
		Template:         scaffold.TemplateBlank,
	})
	if err != nil {
		t.Fatalf("Create blank: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "content", "posts.go")); !os.IsNotExist(err) {
		t.Fatalf("blank template installed starter posts: %v", err)
	}
	config, err := os.ReadFile(filepath.Join(target, "content", "config.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(config), "[]ridu.Collection{Users}") || strings.Contains(string(config), "Posts") {
		t.Fatalf("blank config is not auth-only:\n%s", config)
	}
	readme, err := os.ReadFile(filepath.Join(target, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), "Project template: `blank`") {
		t.Fatalf("blank README does not identify its template:\n%s", readme)
	}
}

func TestCreateSQLiteRendersAdapterSpecificRuntimeWithoutCompose(t *testing.T) {
	target := filepath.Join(t.TempDir(), "sqlite-content")
	_, err := scaffold.Create(scaffold.Options{
		Target:           target,
		ModulePath:       "example.com/acme/sqlite-content",
		NPMScope:         "@acme",
		FrameworkVersion: "v1.2.3",
		Database:         projectfile.DatabaseSQLite,
	})
	if err != nil {
		t.Fatalf("Create SQLite: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "compose.yaml")); !os.IsNotExist(err) {
		t.Fatalf("SQLite scaffold installed a PostgreSQL compose file: %v", err)
	}
	project, err := os.ReadFile(filepath.Join(target, "ridu.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(project), `database = "sqlite"`) {
		t.Fatalf("SQLite scaffold project selection:\n%s", project)
	}
	server, err := os.ReadFile(filepath.Join(target, "cmd", "server", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	serverText := string(server)
	if !strings.Contains(serverText, `"github.com/riducms/ridu/adapters/sqlite"`) ||
		!strings.Contains(serverText, `sqlite.Open(ctx, sqliteDatabasePath())`) ||
		!strings.Contains(serverText, `filepath.IsAbs(path)`) ||
		strings.Contains(serverText, `adapters/postgres`) || strings.Contains(serverText, `DATABASE_URL`) {
		t.Fatalf("SQLite scaffold runtime is not adapter-specific:\n%s", server)
	}
	formatted, err := format.Source(server)
	if err != nil {
		t.Fatalf("format generated SQLite runtime: %v\n%s", err, server)
	}
	if !bytes.Equal(formatted, server) {
		t.Fatalf("generated SQLite runtime is not gofmt-formatted:\n%s", server)
	}
	guide, err := os.ReadFile(filepath.Join(target, "PROJECT.md"))
	if err != nil {
		t.Fatal(err)
	}
	guideText := string(guide)
	if !strings.Contains(guideText, "`RIDU_SQLITE_PATH`") || strings.Contains(guideText, "`compose.yaml`") ||
		strings.Contains(guideText, "\n\n| `ridu.toml`") || strings.Contains(guideText, "\n\n| `RIDU_ALLOWED_HOSTS`") {
		t.Fatalf("SQLite scaffold guide is not adapter-specific or has a broken table:\n%s", guide)
	}
	for _, sqliteMigrationGuidance := range []string{
		"registered, compiled transaction-bound transform",
		"npm run ridu -- migrate create --name rename-post-title --transform rename-title --allow-destructive",
		"temporary SQLite database without a deployment path",
		"one `BEGIN IMMEDIATE` writer transaction",
		"quiesce every same-host process using the database file",
		"applied/pending artifact, phase, and step state",
		"npm run ridu -- migrate down --allow-destructive",
		"npm run ridu -- migrate reset --allow-destructive",
		"npm run ridu -- migrate refresh --allow-destructive",
		"npm run ridu -- migrate fresh --allow-destructive",
	} {
		if !strings.Contains(guideText, sqliteMigrationGuidance) {
			t.Errorf("SQLite scaffold guide is missing migration guidance %q:\n%s", sqliteMigrationGuidance, guide)
		}
	}
	for _, postgresOnlyGuidance := range []string{
		"asks you to confirm an unambiguous rename",
		"Replay committed history in a shadow schema",
		"Apply reviewed artifacts under the production migration lock",
		"checkpoint state",
		"drain old replicas",
	} {
		if strings.Contains(guideText, postgresOnlyGuidance) {
			t.Errorf("SQLite scaffold guide includes PostgreSQL-only migration guidance %q:\n%s", postgresOnlyGuidance, guide)
		}
	}
}

func TestCreateMongoDBRendersQualifiedReplicaSetGuidanceForStarterAndBlank(t *testing.T) {
	for _, selectedTemplate := range []scaffold.Template{scaffold.TemplateStarter, scaffold.TemplateBlank} {
		t.Run(string(selectedTemplate), func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "mongodb-"+string(selectedTemplate))
			_, err := scaffold.Create(scaffold.Options{
				Target:           target,
				ModulePath:       "example.com/acme/mongodb-" + string(selectedTemplate),
				NPMScope:         "@acme",
				FrameworkVersion: "v1.2.3",
				Template:         selectedTemplate,
				Database:         projectfile.DatabaseMongoDB,
			})
			if err != nil {
				t.Fatalf("Create MongoDB %s: %v", selectedTemplate, err)
			}

			project, err := os.ReadFile(filepath.Join(target, "ridu.toml"))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(project), `database = "mongodb"`) {
				t.Fatalf("MongoDB scaffold project selection:\n%s", project)
			}

			compose, err := os.ReadFile(filepath.Join(target, "compose.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			composeText := string(compose)
			for _, required := range []string{
				"mongodb:",
				"mongo:8.2.9-noble@sha256:007773db61cb1aa44e526fb7175fc582902e67d4e6cc5f13106445767d46c818",
				`["mongod", "--replSet", "ridu-rs0", "--bind_ip_all"]`,
				`"127.0.0.1:27029:27017"`,
				`rs.initiate({_id:"ridu-rs0"`,
			} {
				if !strings.Contains(composeText, required) {
					t.Errorf("MongoDB compose file is missing %q:\n%s", required, compose)
				}
			}
			if strings.Contains(composeText, "postgres:") || strings.Contains(composeText, "54329") {
				t.Fatalf("MongoDB scaffold leaked PostgreSQL Compose configuration:\n%s", compose)
			}

			server, err := os.ReadFile(filepath.Join(target, "cmd", "server", "main.go"))
			if err != nil {
				t.Fatal(err)
			}
			serverText := string(server)
			if !strings.Contains(serverText, `"github.com/riducms/ridu/adapters/mongodb"`) ||
				strings.Contains(serverText, `adapters/postgres`) || strings.Contains(serverText, `adapters/sqlite`) ||
				strings.Contains(serverText, "RIDU_SQLITE_PATH") || strings.Contains(serverText, "RIDU_POSTGRES_") {
				t.Fatalf("MongoDB scaffold runtime is not adapter-specific:\n%s", server)
			}
			formatted, err := format.Source(server)
			if err != nil || !bytes.Equal(formatted, server) {
				t.Fatalf("generated MongoDB runtime is not gofmt-formatted: %v\nactual:\n%s\nformatted:\n%s", err, server, formatted)
			}
			for _, required := range []string{
				`const internalDevelopmentServerArgument = "--ridu-internal-development-server"`,
				`internalDevelopmentServer := len(os.Args) == 2 && os.Args[1] == internalDevelopmentServerArgument`,
				`os.Args = os.Args[:1]`,
				`options = runtimeOptions(applicationConfig, internalDevelopmentServer)`,
				"backend, err := mongodb.OpenWithConfig(ctx, mongodb.Config{",
				"manifest, err := ridu.Resolve(applicationConfig)",
				"if err := backend.VerifyIndexes(ctx, manifest); err != nil {",
				"verify MongoDB development indexes",
				`AllowUnverifiableReadiness: internalDevelopmentServer`,
				`SkipReadinessPreflight:     internalDevelopmentServer`,
			} {
				if !strings.Contains(serverText, required) {
					t.Errorf("MongoDB runtime does not preserve production readiness and private development verification through %q:\n%s", required, server)
				}
			}
			verificationCall := strings.Index(serverText, "backend.VerifyIndexes(ctx, manifest)")
			developmentVerification := -1
			if verificationCall >= 0 {
				developmentVerification = strings.LastIndex(serverText[:verificationCall], "if internalDevelopmentServer {")
			}
			returnBackend := -1
			if verificationCall >= 0 {
				if relativeReturn := strings.Index(serverText[verificationCall:], "return backend, nil"); relativeReturn >= 0 {
					returnBackend = verificationCall + relativeReturn
				}
			}
			if developmentVerification < 0 || returnBackend < 0 || developmentVerification > verificationCall || verificationCall > returnBackend {
				t.Fatalf("MongoDB index verification is not confined to private development admission:\n%s", server)
			}
			for _, publicOverride := range []string{"RIDU_ALLOW_UNVERIFIABLE_READINESS", "RIDU_SKIP_READINESS_PREFLIGHT", "RIDU_INTERNAL_DEVELOPMENT_SERVER"} {
				if strings.Contains(serverText, publicOverride) {
					t.Errorf("MongoDB direct server start still accepts public readiness override %q:\n%s", publicOverride, server)
				}
			}
			if strings.Contains(serverText, "backend.SyncIndexes(") {
				t.Fatalf("MongoDB server startup must remain non-mutating:\n%s", server)
			}

			readme, err := os.ReadFile(filepath.Join(target, "README.md"))
			if err != nil {
				t.Fatal(err)
			}
			guide, err := os.ReadFile(filepath.Join(target, "PROJECT.md"))
			if err != nil {
				t.Fatal(err)
			}
			agentGuide, err := os.ReadFile(filepath.Join(target, ".agents", "skills", "ridu-project", "SKILL.md"))
			if err != nil {
				t.Fatal(err)
			}
			referenceNames := []string{"mongodb.md", "capabilities.md", "migrations.md", "production.md"}
			referenceTexts := make(map[string]string, len(referenceNames))
			var referenceDocumentation strings.Builder
			for _, name := range referenceNames {
				reference, readError := os.ReadFile(filepath.Join(target, ".agents", "skills", "ridu-project", "reference", name))
				if readError != nil {
					t.Fatalf("read generated MongoDB agent reference %s: %v", name, readError)
				}
				referenceTexts[name] = string(reference)
				referenceDocumentation.Write(reference)
				referenceDocumentation.WriteByte('\n')
			}
			readmeText := string(readme)
			guideText := string(guide)
			agentGuideText := string(agentGuide)
			topLevelDocumentation := readmeText + "\n" + guideText + "\n" + agentGuideText
			documentation := topLevelDocumentation + "\n" + referenceDocumentation.String()
			for _, rendered := range []struct {
				name string
				text string
			}{
				{name: "README.md", text: readmeText},
				{name: "PROJECT.md", text: guideText},
				{name: "ridu-project/SKILL.md", text: agentGuideText},
			} {
				for _, required := range []string{
					"digest-pinned MongoDB Community 8.2.9",
					"writable three-member",
					"one controlled operational identity",
					"both shadow verification",
					"release-gate evidence",
					"Local ARM runs are preflight only",
					"GitHub Linux",
					"x86-64 release job",
					"planner `1.0.0`",
					"planner `2.0.0`",
					"`npm run ridu -- check` and `npm run ridu -- build`",
					"do not inspect",
				} {
					if !strings.Contains(rendered.text, required) {
						t.Errorf("MongoDB generated %s is missing bounded production guidance %q", rendered.name, required)
					}
				}
				assertMongoDBCutoverGuidanceOrder(t, rendered.name, rendered.text)
			}
			for _, required := range []string{
				"ordinary generated starter and blank projects on Linux x86-64",
				"MongoDB Community 8.2.9",
				"writable three-member",
				"SCRAM-SHA-256",
				"CA- and hostname-verified TLS",
				"database-scoped application credential",
				"controlled backup credential",
				"mongodb://127.0.0.1:27029/ridu?directConnection=true&replicaSet=ridu-rs0",
				"configuration discovery",
				"last good server and data remain in place",
				"--accept-renames",
				"--transform",
				"--allow-maintenance",
				"ridu migrate up",
				"ridu migrate status",
				"required indexes",
				"mongodump",
				"mongorestore",
				"upload objects",
				"Atlas",
				"Amazon DocumentDB",
				"Azure Cosmos DB",
				"standalone servers",
				"point-in-time recovery",
			} {
				if !strings.Contains(documentation, required) {
					t.Errorf("MongoDB generated guidance is missing %q", required)
				}
			}
			for _, required := range []string{
				"reviewed create command with `--allow-destructive`",
				"This completes artifact creation only",
				"`npm run ridu -- migrate down`, `reset`, `refresh`, or `fresh` | Unsupported for MongoDB immutable artifacts",
			} {
				if !strings.Contains(guideText, required) {
					t.Errorf("MongoDB generated project guide is missing %q", required)
				}
			}
			for _, required := range []string{
				"MongoDB `down`, `reset`, `refresh`, and `fresh`",
				"remain unsupported",
			} {
				if !strings.Contains(agentGuideText, required) {
					t.Errorf("MongoDB embedded agent guidance is missing %q", required)
				}
			}
			for _, required := range []string{
				"Linux x86-64",
				"digest-pinned",
				"MongoDB Community 8.2.9",
				"three-member replica set",
				"SCRAM-SHA-256",
				"Atlas",
				"DocumentDB",
				"Cosmos DB",
				"planner-`1.0.0`",
				"supported immutable prefix",
				"planner contract `2.0.0`",
				"`ridu check` and `ridu build` remain offline",
			} {
				if !strings.Contains(referenceDocumentation.String(), required) {
					t.Errorf("MongoDB generated agent references are missing bounded production guidance %q", required)
				}
			}
			for _, required := range []string{"MongoDB planner contract `2.0.0`", "--allow-destructive", "--allow-maintenance"} {
				if !strings.Contains(referenceTexts["migrations.md"], required) {
					t.Errorf("generated migration reference is missing MongoDB lifecycle guidance %q", required)
				}
			}
			for _, stale := range []string{
				"development-qualified",
				"not production-qualified",
				"production migration application and readiness history are not qualified",
				"does not qualify MongoDB for production",
				"Other `ridu migrate` commands | Unavailable",
				"production deployment remains unavailable",
			} {
				if strings.Contains(documentation, stale) {
					t.Errorf("MongoDB generated guidance retains stale development-only wording %q", stale)
				}
			}
			for _, leaked := range []string{
				"RIDU_SQLITE_PATH",
				"RIDU_POSTGRES_UPLOAD_LOCK_CONNECTIONS",
				"one `BEGIN IMMEDIATE` writer transaction",
				"ridu migrate down --allow-destructive",
			} {
				if strings.Contains(topLevelDocumentation, leaked) {
					t.Errorf("MongoDB generated guidance leaked unsupported database behavior %q", leaked)
				}
			}

			postsPath := filepath.Join(target, "content", "posts.go")
			_, postsError := os.Stat(postsPath)
			if selectedTemplate == scaffold.TemplateStarter && postsError != nil {
				t.Fatalf("MongoDB starter omitted posts: %v", postsError)
			}
			if selectedTemplate == scaffold.TemplateBlank && !os.IsNotExist(postsError) {
				t.Fatalf("MongoDB blank installed starter posts: %v", postsError)
			}
		})
	}
}

func assertMongoDBCutoverGuidanceOrder(t *testing.T, name, guidance string) {
	t.Helper()
	position := 0
	for _, marker := range []string{
		"1. **Drain writers.**",
		"2. **Verify history.**",
		"3. **Capture the recovery point.**",
		"4. **Apply migrations.**",
		"5. **Confirm status and readiness.**",
		"6. **Start the release.**",
	} {
		next := strings.Index(guidance[position:], marker)
		if next < 0 {
			t.Errorf("MongoDB generated %s is missing ordered cutover marker %q", name, marker)
			return
		}
		position += next + len(marker)
	}
}

func TestCreateAcceptsSemanticBuildMetadataFromADirtyGoBuild(t *testing.T) {
	target := filepath.Join(t.TempDir(), "dirty-build-content")
	version := "v0.0.0-20260826220357-71330297d335+dirty"
	_, err := scaffold.Create(scaffold.Options{
		Target:           target,
		ModulePath:       "example.com/acme/dirty-build-content",
		NPMScope:         "@acme",
		FrameworkVersion: version,
	})
	if err != nil {
		t.Fatalf("Create with SemVer build metadata: %v", err)
	}
	goModule, err := os.ReadFile(filepath.Join(target, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(goModule), "github.com/riducms/ridu "+version) {
		t.Fatalf("generated go.mod does not preserve the development version:\n%s", goModule)
	}
	packageManifest, err := os.ReadFile(filepath.Join(target, "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(packageManifest), `"@riducms/sdk": "0.0.0-20260826220357-71330297d335+dirty"`) {
		t.Fatalf("generated package.json does not preserve SemVer build metadata:\n%s", packageManifest)
	}
}

func testModuleRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func TestCreateRejectsNamesModulesScopesAndExistingTargets(t *testing.T) {
	base := t.TempDir()
	tests := []struct {
		name    string
		options scaffold.Options
		want    string
	}{
		{"name", scaffold.Options{Target: filepath.Join(base, "Bad_Name"), ModulePath: "example.com/acme/bad", NPMScope: "@acme", FrameworkVersion: "1.2.3"}, "lowercase kebab-case"},
		{"module", scaffold.Options{Target: filepath.Join(base, "bad-module"), ModulePath: "bad-module", NPMScope: "@acme", FrameworkVersion: "1.2.3"}, "must begin with a domain"},
		{"scope", scaffold.Options{Target: filepath.Join(base, "bad-scope"), ModulePath: "example.com/acme/bad", NPMScope: "Acme", FrameworkVersion: "1.2.3"}, "npm scope"},
		{"version", scaffold.Options{Target: filepath.Join(base, "bad-version"), ModulePath: "example.com/acme/bad", NPMScope: "@acme", FrameworkVersion: "development"}, "semantic release"},
		{"template", scaffold.Options{Target: filepath.Join(base, "bad-template"), ModulePath: "example.com/acme/bad", NPMScope: "@acme", FrameworkVersion: "1.2.3", Template: "website"}, "unknown project template"},
		{"database", scaffold.Options{Target: filepath.Join(base, "bad-database"), ModulePath: "example.com/acme/bad", NPMScope: "@acme", FrameworkVersion: "1.2.3", Database: "mysql"}, "unknown database adapter"},
		{"package manager", scaffold.Options{Target: filepath.Join(base, "bad-manager"), ModulePath: "example.com/acme/bad", NPMScope: "@acme", FrameworkVersion: "1.2.3", PackageManager: "deno"}, "unknown package manager"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := scaffold.Create(test.options)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Create error = %v, want containing %q", err, test.want)
			}
		})
	}

	existing := filepath.Join(base, "existing")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := scaffold.Create(scaffold.Options{Target: existing, ModulePath: "example.com/acme/existing", NPMScope: "@acme", FrameworkVersion: "1.2.3"})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("Create existing error = %v", err)
	}
}
