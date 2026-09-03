package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/riducms/ridu/internal/projectfile"
)

func packageManagerUserCommands(manager projectfile.PackageManager) (install, run string) {
	switch manager {
	case projectfile.PackageManagerBun:
		return "bun install", "bun run"
	case projectfile.PackageManagerPNPM:
		return "pnpm install", "pnpm run"
	case projectfile.PackageManagerYarn:
		return "yarn install", "yarn run"
	default:
		return "npm install", "npm run"
	}
}

func packageManagerRunCommand(manager projectfile.PackageManager, script string, arguments ...string) (string, []string) {
	command := string(manager)
	args := []string{"run", script}
	if manager == projectfile.PackageManagerBun && script == "dev" {
		args = []string{"run", "--silent", script}
	}
	if len(arguments) != 0 {
		if manager == projectfile.PackageManagerNPM || manager == projectfile.PackageManagerBun {
			args = append(args, "--")
		}
		args = append(args, arguments...)
	}
	return command, args
}

func packageManagerAddCommand(manager projectfile.PackageManager, dependency string) (string, []string) {
	switch manager {
	case projectfile.PackageManagerNPM:
		return "npm", []string{"install", "--save-exact", dependency}
	case projectfile.PackageManagerPNPM:
		return "pnpm", []string{"add", "--save-exact", dependency}
	case projectfile.PackageManagerYarn:
		return "yarn", []string{"add", "--exact", dependency}
	default:
		return "bun", []string{"add", "--exact", dependency}
	}
}

func packageManagerRemoveCommand(manager projectfile.PackageManager, dependency string) (string, []string) {
	if manager == projectfile.PackageManagerNPM {
		return "npm", []string{"uninstall", dependency}
	}
	return string(manager), []string{"remove", dependency}
}

func packageManagerInstallCommand(ctx context.Context, definition projectfile.File) (string, []string) {
	manager := definition.FrontendPackageManager()
	switch manager {
	case projectfile.PackageManagerNPM:
		if fileExists(filepath.Join(definition.Root, "package-lock.json")) {
			return "npm", []string{"ci"}
		}
		return "npm", []string{"install"}
	case projectfile.PackageManagerPNPM:
		arguments := []string{"install"}
		if fileExists(filepath.Join(definition.Root, "pnpm-lock.yaml")) {
			arguments = append(arguments, "--frozen-lockfile")
		}
		return "pnpm", arguments
	case projectfile.PackageManagerYarn:
		arguments := []string{"install"}
		if fileExists(filepath.Join(definition.Root, "yarn.lock")) {
			frozen := "--immutable"
			if output, err := commandOutput(ctx, definition.Root, nil, "yarn", "--version"); err == nil {
				majorText, _, _ := strings.Cut(strings.TrimSpace(output), ".")
				if major, parseError := strconv.Atoi(majorText); parseError == nil && major < 2 {
					frozen = "--frozen-lockfile"
				}
			}
			arguments = append(arguments, frozen)
		}
		return "yarn", arguments
	default:
		arguments := []string{"install"}
		if fileExists(filepath.Join(definition.Root, "bun.lock")) || fileExists(filepath.Join(definition.Root, "bun.lockb")) {
			arguments = append(arguments, "--frozen-lockfile")
		}
		return "bun", arguments
	}
}

func packageManagerLockfiles(definition projectfile.File) []string {
	return []string{
		filepath.Join(definition.Root, "bun.lock"),
		filepath.Join(definition.Root, "bun.lockb"),
		filepath.Join(definition.Root, "package-lock.json"),
		filepath.Join(definition.Root, "pnpm-lock.yaml"),
		filepath.Join(definition.Root, "yarn.lock"),
		filepath.Join(definition.Root, "pnpm-workspace.yaml"),
		filepath.Join(definition.Root, ".yarnrc.yml"),
	}
}

func packageManagerInstallHint(definition projectfile.File) string {
	install, _ := packageManagerUserCommands(definition.FrontendPackageManager())
	return install
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func runPackageManagerScript(ctx context.Context, definition projectfile.File, directory, script string, arguments []string, stdout, stderr io.Writer) error {
	command, commandArguments := packageManagerRunCommand(definition.FrontendPackageManager(), script, arguments...)
	if err := runForeground(ctx, directory, nil, stdout, stderr, command, commandArguments...); err != nil {
		return fmt.Errorf("%s: %w", strings.Join(append([]string{command}, commandArguments...), " "), err)
	}
	return nil
}
