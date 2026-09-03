// Command admin runs the framework repository's admin fixture and Vite server
// together. It is repository tooling, not a distributed Ridu command.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/riducms/ridu/internal/cli"
)

func main() {
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "determine framework root: %v\n", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(cli.RunFrameworkAdminDev(ctx, root, os.Stdout, os.Stderr))
}
