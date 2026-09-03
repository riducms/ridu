package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/riducms/ridu/internal/projectfile"
)

const developmentBinaryDirectory = ".ridu/dev"

type developmentBinary struct {
	path string
}

func buildDevelopmentBinary(ctx context.Context, definition projectfile.File, stdout, stderr io.Writer) (developmentBinary, time.Duration, error) {
	started := time.Now()
	directory := filepath.Join(definition.Root, filepath.FromSlash(developmentBinaryDirectory))
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return developmentBinary{}, 0, fmt.Errorf("create development binary directory: %w", err)
	}
	pattern := "server-*"
	if runtime.GOOS == "windows" {
		pattern += ".exe"
	}
	temporary, err := os.CreateTemp(directory, pattern)
	if err != nil {
		return developmentBinary{}, 0, fmt.Errorf("reserve development binary: %w", err)
	}
	path := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(path)
		return developmentBinary{}, 0, fmt.Errorf("close development binary reservation: %w", err)
	}
	if err := os.Remove(path); err != nil {
		return developmentBinary{}, 0, fmt.Errorf("prepare development binary output: %w", err)
	}

	entry := "./" + strings.TrimPrefix(filepath.ToSlash(definition.Entry), "./")
	if err := runForeground(
		ctx,
		definition.Root,
		nil,
		stdout,
		stderr,
		"go",
		"build",
		"-buildvcs=false",
		"-ldflags=-s -w",
		"-o",
		path,
		entry,
	); err != nil {
		_ = os.Remove(path)
		return developmentBinary{}, time.Since(started), fmt.Errorf("build development server: %w", err)
	}
	return developmentBinary{path: path}, time.Since(started), nil
}

func (binary developmentBinary) remove() error {
	if binary.path == "" {
		return nil
	}
	if err := os.Remove(binary.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
