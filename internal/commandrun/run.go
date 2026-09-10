// Package commandrun preserves cancellation identity for foreground commands.
package commandrun

import (
	"context"
	"errors"
	"os/exec"
	"sync/atomic"
)

// Run executes a command created by exec.CommandContext with ctx. It preserves
// the context error when cancellation actually terminates the child, without
// interpreting platform-specific process exit statuses.
func Run(ctx context.Context, command *exec.Cmd) error {
	var terminated atomic.Bool
	if cancel := command.Cancel; cancel != nil {
		command.Cancel = func() error {
			err := cancel()
			if err == nil {
				terminated.Store(true)
			}
			return err
		}
	}
	err := command.Run()
	if err != nil && terminated.Load() {
		return errors.Join(err, ctx.Err())
	}
	return err
}
