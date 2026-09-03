// Command playground-ridu-hydrate restores the ignored development-only
// packages and CLI used by the committed generated Ridu playground.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/riducms/ridu/internal/frameworkpackages"
)

const frameworkModule = "github.com/riducms/ridu"

func main() {
	os.Exit(run())
}

func run() int {
	flags := flag.NewFlagSet("playground-ridu-hydrate", flag.ContinueOnError)
	target := flags.String("target", "", "existing generated Ridu playground directory")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 || strings.TrimSpace(*target) == "" {
		fmt.Fprintln(os.Stderr, "usage: go run ./internal/dogfood/playground --target ./playground/ridu")
		return 2
	}

	frameworkRoot, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "determine framework root: %v\n", err)
		return 1
	}
	if err := requireFile(filepath.Join(frameworkRoot, "go.mod")); err != nil {
		fmt.Fprintf(os.Stderr, "run this command from the Ridu repository root: %v\n", err)
		return 1
	}

	projectRoot, err := filepath.Abs(*target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve playground target: %v\n", err)
		return 1
	}
	if err := requireFile(filepath.Join(projectRoot, "ridu.toml")); err != nil {
		fmt.Fprintf(os.Stderr, "target is not a generated Ridu project: %v\n", err)
		return 1
	}

	version, err := requiredFrameworkVersion(filepath.Join(projectRoot, "go.mod"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "read generated framework version: %v\n", err)
		return 1
	}
	if err := frameworkpackages.Publish(frameworkRoot, projectRoot, version); err != nil {
		fmt.Fprintf(os.Stderr, "hydrate frontend packages: %v\n", err)
		return 1
	}
	if err := installFrontendDependencies(projectRoot); err != nil {
		fmt.Fprintf(os.Stderr, "install playground frontend dependencies: %v\n", err)
		return 1
	}

	projectCLI := filepath.Join(projectRoot, ".ridu", "bin", "ridu")
	if err := os.MkdirAll(filepath.Dir(projectCLI), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "create project CLI directory: %v\n", err)
		return 1
	}
	build := exec.Command("go", "build", "-o", projectCLI, "./cmd/ridu")
	build.Dir = frameworkRoot
	build.Env = append(os.Environ(), "GOWORK=off")
	build.Stdout = os.Stdout
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "build playground CLI: %v\n", err)
		return 1
	}

	fmt.Printf("Hydrated Ridu playground at %s with %s.\n", projectRoot, version)
	fmt.Printf("Next:\n  cd %s\n  ./.ridu/bin/ridu dev\n", projectRoot)
	return 0
}

func installFrontendDependencies(projectRoot string) error {
	arguments := []string{"install"}
	if err := requireFile(filepath.Join(projectRoot, "bun.lock")); err == nil {
		arguments = append(arguments, "--frozen-lockfile")
	}
	command := exec.Command("bun", arguments...)
	command.Dir = projectRoot
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	return command.Run()
}

func requireFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	return nil
}

func requiredFrameworkVersion(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		for index := 0; index+1 < len(fields); index++ {
			if fields[index] == frameworkModule {
				return fields[index+1], nil
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("%s does not require %s", path, frameworkModule)
}
