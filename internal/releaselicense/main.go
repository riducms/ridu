// Command releaselicense inventories licences for modules linked into a Go binary.
package main

import (
	"debug/buildinfo"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"

	"golang.org/x/mod/module"
)

func main() {
	binary := flag.String("binary", "", "Go binary to inspect")
	output := flag.String("output", "", "licence bundle output path")
	flag.Parse()
	if *binary == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "binary and output are required")
		os.Exit(2)
	}
	info, err := buildinfo.ReadFile(*binary)
	if err != nil {
		fatal(err)
	}
	cacheBytes, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		fatal(fmt.Errorf("resolve Go module cache: %w", err))
	}
	cache := strings.TrimSpace(string(cacheBytes))
	modules := append([]*debug.Module(nil), info.Deps...)
	sort.Slice(modules, func(i, j int) bool { return modules[i].Path < modules[j].Path })
	var bundle strings.Builder
	bundle.WriteString("Third-party Go module licences\n\nGenerated from the exact module inventory linked into this Ridu CLI binary.\n")
	for _, dependency := range modules {
		resolved := dependency
		if dependency.Replace != nil {
			resolved = dependency.Replace
		}
		if resolved.Version == "" {
			fatal(fmt.Errorf("linked module %s has a local replacement without a releasable licence source", dependency.Path))
		}
		escapedPath, err := module.EscapePath(resolved.Path)
		if err != nil {
			fatal(err)
		}
		escapedVersion, err := module.EscapeVersion(resolved.Version)
		if err != nil {
			fatal(err)
		}
		directory := filepath.Join(cache, escapedPath+"@"+escapedVersion)
		entries, err := os.ReadDir(directory)
		if err != nil {
			fatal(fmt.Errorf("read linked module %s: %w", dependency.Path, err))
		}
		var licences []string
		for _, entry := range entries {
			upper := strings.ToUpper(entry.Name())
			if !entry.IsDir() && (strings.HasPrefix(upper, "LICENSE") || strings.HasPrefix(upper, "LICENCE") || strings.HasPrefix(upper, "COPYING") || strings.HasPrefix(upper, "NOTICE")) {
				licences = append(licences, entry.Name())
			}
		}
		sort.Strings(licences)
		if len(licences) == 0 {
			fatal(fmt.Errorf("linked module %s@%s has no root licence file", dependency.Path, dependency.Version))
		}
		fmt.Fprintf(&bundle, "\n\n================================================================================\n%s %s\n", dependency.Path, dependency.Version)
		for _, name := range licences {
			contents, err := os.ReadFile(filepath.Join(directory, name))
			if err != nil {
				fatal(err)
			}
			fmt.Fprintf(&bundle, "\n--- %s ---\n%s", name, contents)
			if len(contents) == 0 || contents[len(contents)-1] != '\n' {
				bundle.WriteByte('\n')
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(*output, []byte(bundle.String()), 0o644); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
