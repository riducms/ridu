// Command dogfood-new builds and exercises the real Ridu CLI against a local
// versioned module proxy. It is repository tooling, not a distributed command.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/riducms/ridu/internal/frameworkpackages"
	"github.com/riducms/ridu/internal/frameworkproxy"
)

func main() {
	os.Exit(run())
}

func run() int {
	flags := flag.NewFlagSet("dogfood-new", flag.ContinueOnError)
	target := flags.String("target", "", "new project directory to create")
	modulePath := flags.String("module", "example.com/ridu-dogfood", "generated Go module path")
	npmScope := flags.String("scope", "@riducms-dogfood", "generated npm scope")
	templateName := flags.String("template", "starter", "generated project template: starter or blank")
	database := flags.String("database", "", "generated project database: postgres, sqlite, or mongodb")
	packageManager := flags.String("package-manager", "", "generated project package manager: npm, bun, pnpm, or yarn (defaults to bun outside the wizard)")
	agent := flags.String("agent", "", "generated coding-agent guidance selection")
	release := flags.String("release", "", "semantic framework release to provision (defaults to a unique dogfood prerelease)")
	interactive := flags.Bool("interactive", false, "choose the project template in the real terminal wizard")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return 2
	}
	requestedTarget := strings.TrimSpace(*target)
	if flags.NArg() != 0 || (!*interactive && requestedTarget == "") {
		fmt.Fprintln(os.Stderr, "usage: go run ./internal/dogfood/new [--target /path/to/project] [--module path] [--scope @scope] [--template starter|blank] [--database postgres|sqlite|mongodb] [--package-manager npm|bun|pnpm|yarn] [--agent codex|claude|cursor|all|none] [--interactive]")
		return 2
	}
	frameworkRoot, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "determine framework root: %v\n", err)
		return 1
	}
	if err := verifyFrameworkRoot(frameworkRoot); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	temporaryRoot, err := os.MkdirTemp("", "ridu-dogfood-new-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create dogfood workspace: %v\n", err)
		return 1
	}
	defer os.RemoveAll(temporaryRoot)
	dogfoodRelease := strings.TrimSpace(*release)
	if dogfoodRelease == "" {
		dogfoodRelease = fmt.Sprintf("v0.0.0-dogfood.%d", time.Now().UnixNano())
	}

	proxyURL, err := frameworkproxy.Publish(frameworkRoot, filepath.Join(temporaryRoot, "proxy"), dogfoodRelease)
	if err != nil {
		fmt.Fprintf(os.Stderr, "publish framework checkout: %v\n", err)
		return 1
	}
	binary := filepath.Join(temporaryRoot, "ridu")
	build := exec.Command("go", "build", "-o", binary, "./cmd/ridu")
	build.Dir = frameworkRoot
	build.Env = append(os.Environ(), "GOWORK=off")
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "build real Ridu CLI: %v\n", err)
		return 1
	}

	commandArguments := []string{"new"}
	if !*interactive {
		commandArguments = append(commandArguments, "--template", *templateName)
	}
	if strings.TrimSpace(*database) != "" {
		commandArguments = append(commandArguments, "--database", *database)
	}
	selectedPackageManager := strings.TrimSpace(*packageManager)
	if selectedPackageManager == "" && !*interactive {
		selectedPackageManager = "bun"
	}
	if selectedPackageManager != "" {
		commandArguments = append(commandArguments, "--package-manager", selectedPackageManager)
	}
	if strings.TrimSpace(*agent) != "" {
		commandArguments = append(commandArguments, "--agent", *agent)
	}
	commandArguments = append(commandArguments,
		"--release-version", dogfoodRelease,
		"--module", *modulePath,
		"--scope", *npmScope,
	)
	if requestedTarget != "" {
		commandArguments = append(commandArguments, requestedTarget)
	}
	resultFile := filepath.Join(temporaryRoot, "new-project-result")
	command := exec.Command(binary, commandArguments...)
	command.Dir = frameworkRoot
	command.Env = append(os.Environ(),
		"GOWORK=off",
		"GOPROXY="+proxyURL+",https://proxy.golang.org,direct",
		"GONOSUMDB=github.com/riducms/ridu",
		"RIDU_NEW_PROJECT_RESULT_FILE="+resultFile,
	)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if *interactive {
		command.Stdin = os.Stdin
	}
	if err := command.Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			return exitError.ExitCode()
		}
		fmt.Fprintf(os.Stderr, "run real Ridu CLI: %v\n", err)
		return 1
	}
	createdTarget, created, err := readCreatedProject(resultFile, requestedTarget)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read created project result: %v\n", err)
		return 1
	}
	if !created {
		fmt.Fprintln(os.Stdout, "Dogfood project creation cancelled; no local packages were provisioned.")
		return 0
	}
	if err := frameworkpackages.Publish(frameworkRoot, createdTarget, dogfoodRelease); err != nil {
		fmt.Fprintf(os.Stderr, "provision unpublished frontend packages: %v\n", err)
		fmt.Fprintf(os.Stderr, "the generated project was kept at %s\n", createdTarget)
		return 1
	}
	projectBinary := filepath.Join(createdTarget, ".ridu", "bin", "ridu")
	if err := os.MkdirAll(filepath.Dir(projectBinary), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "create project CLI directory: %v\n", err)
		return 1
	}
	if err := copyExecutable(binary, projectBinary); err != nil {
		fmt.Fprintf(os.Stderr, "install project CLI: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stdout, "\nDogfood project ready at %s.\n", createdTarget)
	fmt.Fprintln(os.Stdout, "Unpublished Go and frontend packages are provisioned as ignored local workspaces.")
	fmt.Fprintf(os.Stdout, "\nNext:\n  cd %s\n  ./.ridu/bin/ridu dev\n", createdTarget)
	return 0
}

func readCreatedProject(resultPath, requestedTarget string) (string, bool, error) {
	encoded, err := os.ReadFile(resultPath)
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	created := strings.TrimSpace(string(encoded))
	if created == "" {
		return "", false, fmt.Errorf("creation result is empty")
	}
	created, err = filepath.Abs(created)
	if err != nil {
		return "", false, fmt.Errorf("resolve created project: %w", err)
	}
	if requestedTarget != "" {
		expected, err := filepath.Abs(requestedTarget)
		if err != nil {
			return "", false, fmt.Errorf("resolve requested target: %w", err)
		}
		if created != expected {
			return "", false, fmt.Errorf("CLI created %s, expected %s", created, expected)
		}
	}
	if _, err := os.Stat(filepath.Join(created, "ridu.toml")); err != nil {
		return "", false, fmt.Errorf("verify created project %s: %w", created, err)
	}
	return created, true, nil
}

func copyExecutable(source, target string) error {
	encoded, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".ridu-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(encoded); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Chmod(0o755); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, target)
}

func verifyFrameworkRoot(root string) error {
	moduleFile, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return fmt.Errorf("run this command from the Ridu repository root: %w", err)
	}
	if !strings.HasPrefix(string(moduleFile), "module github.com/riducms/ridu\n") {
		return fmt.Errorf("run this command from the Ridu repository root")
	}
	return nil
}
