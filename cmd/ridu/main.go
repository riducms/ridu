// Command ridu is the portable project and build orchestrator for Ridu.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/internal/cli"
)

// version may be supplied by a release builder with -ldflags -X. Ordinary
// `go install module/cmd/ridu@version` builds discover the module version from
// Go build information instead.
var version string

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	workingDirectory, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "determine working directory: %v\n", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return cli.Run(ctx, args, stdout, stderr, cli.Options{
		WorkingDirectory:     workingDirectory,
		Version:              cliVersion(),
		FrameworkVersion:     ridu.FrameworkVersion,
		Stdin:                os.Stdin,
		Interactive:          terminal(os.Stdin) && terminalWriter(stdout),
		Accessible:           os.Getenv("RIDU_ACCESSIBLE") != "",
		NewProjectResultFile: os.Getenv("RIDU_NEW_PROJECT_RESULT_FILE"),
	})
}

func terminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func terminalWriter(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	return ok && terminal(file)
}

func cliVersion() string {
	if version != "" {
		return version
	}
	if information, ok := debug.ReadBuildInfo(); ok && information.Main.Version != "" && information.Main.Version != "(devel)" {
		return information.Main.Version
	}
	return ridu.FrameworkVersion
}
