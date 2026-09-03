package cli

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/riducms/ridu/internal/projectfile"
)

func TestPackageManagerCommands(t *testing.T) {
	for _, test := range []struct {
		manager projectfile.PackageManager
		install string
		run     string
		add     []string
		remove  []string
	}{
		{projectfile.PackageManagerNPM, "npm install", "npm run", []string{"install", "--save-exact", "example@1.2.3"}, []string{"uninstall", "example"}},
		{projectfile.PackageManagerBun, "bun install", "bun run", []string{"add", "--exact", "example@1.2.3"}, []string{"remove", "example"}},
		{projectfile.PackageManagerPNPM, "pnpm install", "pnpm run", []string{"add", "--save-exact", "example@1.2.3"}, []string{"remove", "example"}},
		{projectfile.PackageManagerYarn, "yarn install", "yarn run", []string{"add", "--exact", "example@1.2.3"}, []string{"remove", "example"}},
	} {
		t.Run(string(test.manager), func(t *testing.T) {
			install, run := packageManagerUserCommands(test.manager)
			if install != test.install || run != test.run {
				t.Fatalf("user commands = %q, %q", install, run)
			}
			command, arguments := packageManagerRunCommand(test.manager, "dev", "--no-docker")
			wantRun := []string{"run", "dev", "--no-docker"}
			if test.manager == projectfile.PackageManagerBun {
				wantRun = []string{"run", "--silent", "dev", "--", "--no-docker"}
			} else if test.manager == projectfile.PackageManagerNPM {
				wantRun = []string{"run", "dev", "--", "--no-docker"}
			}
			if command != string(test.manager) || !reflect.DeepEqual(arguments, wantRun) {
				t.Fatalf("run command = %q %#v", command, arguments)
			}
			_, add := packageManagerAddCommand(test.manager, "example@1.2.3")
			if !reflect.DeepEqual(add, test.add) {
				t.Fatalf("add arguments = %#v, want %#v", add, test.add)
			}
			_, remove := packageManagerRemoveCommand(test.manager, "example")
			if !reflect.DeepEqual(remove, test.remove) {
				t.Fatalf("remove arguments = %#v, want %#v", remove, test.remove)
			}
		})
	}
}

func TestPackageManagerInstallCommandsHonorLockfiles(t *testing.T) {
	for _, test := range []struct {
		manager  projectfile.PackageManager
		lockfile string
		want     []string
	}{
		{projectfile.PackageManagerNPM, "package-lock.json", []string{"ci"}},
		{projectfile.PackageManagerBun, "bun.lock", []string{"install", "--frozen-lockfile"}},
		{projectfile.PackageManagerPNPM, "pnpm-lock.yaml", []string{"install", "--frozen-lockfile"}},
	} {
		t.Run(string(test.manager), func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, test.lockfile), nil, 0o644); err != nil {
				t.Fatal(err)
			}
			command, arguments := packageManagerInstallCommand(context.Background(), projectfile.File{Root: root, PackageManager: test.manager})
			if command != string(test.manager) || !reflect.DeepEqual(arguments, test.want) {
				t.Fatalf("install command = %q %#v, want %q %#v", command, arguments, test.manager, test.want)
			}
		})
	}
}

func TestPluginMutationSnapshotRestoresEveryPackageManagerLockfile(t *testing.T) {
	root := t.TempDir()
	definition := projectfile.File{
		Root:     root,
		Plugins:  "ridu.plugins.json",
		PluginGo: "content/ridu_plugins.generated.go",
	}
	for _, path := range []string{
		filepath.Join(root, "go.mod"),
		filepath.Join(root, "go.sum"),
		filepath.Join(root, "package.json"),
		definition.Absolute(definition.Plugins),
		definition.Absolute(definition.PluginGo),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lockfiles := packageManagerLockfiles(definition)
	for _, path := range lockfiles[:len(lockfiles)-1] {
		if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	rollback, err := snapshotPluginMutation(definition)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range lockfiles {
		if err := os.WriteFile(path, []byte("changed"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := rollback(); err != nil {
		t.Fatal(err)
	}

	for _, path := range lockfiles[:len(lockfiles)-1] {
		contents, err := os.ReadFile(path)
		if err != nil || string(contents) != "original" {
			t.Fatalf("restored %s = %q, %v", filepath.Base(path), contents, err)
		}
	}
	if _, err := os.Stat(lockfiles[len(lockfiles)-1]); !os.IsNotExist(err) {
		t.Fatalf("new package-manager file was not removed: %v", err)
	}
}
