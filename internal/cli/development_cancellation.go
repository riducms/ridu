package cli

import (
	"context"
	"errors"
)

// developmentFailure handles errors from context-aware development work. A user
// interrupt is a normal stop, but deadlines and independent failures still fail.
func developmentFailure(ctx context.Context, output *cliOutput, label string, err error) int {
	if errors.Is(ctx.Err(), context.Canceled) && errors.Is(err, context.Canceled) {
		return 0
	}
	output.Error(label, err)
	return 1
}
