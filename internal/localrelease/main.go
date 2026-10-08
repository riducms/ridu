// Command local-release stages the working tree as a published-style Ridu release and serves it
// locally. A project created with `npm create ridu@<version>` against it installs the CLI, the Go
// module and the npm packages exactly as it would from a real release, unlike a dogfood project,
// which links the packages as workspaces. It is repository tooling, not a distributed command.
package main

import (
	"fmt"
	"os"
)

const usage = `usage:
  go run ./internal/localrelease stage [--version 0.16.3-local.1] [--out .ridu/local-release]
  go run ./internal/localrelease serve [--out .ridu/local-release] [--address 127.0.0.1:4873]`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(arguments []string) int {
	if len(arguments) == 0 {
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
	switch arguments[0] {
	case "stage":
		return runStage(arguments[1:])
	case "serve":
		return runServe(arguments[1:])
	default:
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
}
