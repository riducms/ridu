package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/riducms/ridu/migration"
)

// replaceBaselineFunc is an adapter's ReplaceBaseline method.
type replaceBaselineFunc func(context.Context, string, migration.BaselineReplacementOptions) ([]string, error)

// replaceBaselineWithConfirmation runs ridu migrate baseline --replace. An
// adapter refuses to rewrite the history of a database exactly at its recorded
// head, which binaries built from the old history may still be serving, until
// the developer confirms. The confirmed run repeats every check under the
// adapter's migration lock. Without interactive input (nil), the refusal
// explains the flag instead.
func replaceBaselineWithConfirmation(ctx context.Context, replace replaceBaselineFunc, directory string, options migration.BaselineReplacementOptions, input io.Reader, prompt io.Writer) ([]string, error) {
	recorded, err := replace(ctx, directory, options)
	if !errors.Is(err, migration.ErrReplacementNeedsConfirmation) {
		return recorded, err
	}
	if input == nil {
		return nil, fmt.Errorf("%w; then rerun ridu migrate baseline --replace with --allow-production", err)
	}
	fmt.Fprintf(prompt, "Caution: %s.\n", err)
	confirmed, err := confirmBaselineReplacement(input, prompt)
	if err != nil {
		return nil, err
	}
	if !confirmed {
		return nil, fmt.Errorf("baseline replacement cancelled; the recorded history is unchanged")
	}
	options.AllowProduction = true
	return replace(ctx, directory, options)
}

// confirmBaselineReplacement asks until it reads yes or no. Enter keeps the
// recorded history.
func confirmBaselineReplacement(input io.Reader, prompt io.Writer) (bool, error) {
	reader := bufio.NewReader(input)
	for {
		fmt.Fprint(prompt, "Replace the recorded history anyway? [y/N] ")
		answer, err := reader.ReadString('\n')
		if err != nil && answer == "" {
			return false, fmt.Errorf("read baseline replacement confirmation: %w", err)
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "y", "yes":
			return true, nil
		case "", "n", "no":
			return false, nil
		}
		fmt.Fprintf(prompt, "Answer y to replace the recorded history or n to keep it (%q is neither).\n", strings.TrimSpace(answer))
		if err != nil {
			return false, fmt.Errorf("read baseline replacement confirmation: %w", err)
		}
	}
}

// interactiveInput returns stdin when a person can answer a prompt.
func interactiveInput(options Options) io.Reader {
	if !options.Interactive {
		return nil
	}
	return options.Stdin
}
