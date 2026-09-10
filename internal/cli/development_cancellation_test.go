package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestDevelopmentFailureCancellationPolicy(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	for _, test := range []struct {
		name string
		ctx  context.Context
		err  error
		want int
	}{
		{"user interrupt", canceled, context.Canceled, 0},
		{"wrapped user interrupt", canceled, fmt.Errorf("prepare: %w", context.Canceled), 0},
		{"active context cancellation error", context.Background(), context.Canceled, 1},
		{"deadline", expired, context.DeadlineExceeded, 1},
		{"independent error after interrupt", canceled, errors.New("invalid configuration"), 1},
		{"missing executable after interrupt", canceled, &exec.Error{Name: "missing-program", Err: exec.ErrNotFound}, 1},
		{"deadline error after interrupt", canceled, context.DeadlineExceeded, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stderr bytes.Buffer
			result := developmentFailure(test.ctx, newCLIOutput(io.Discard, &stderr, cliOutputOptions{}), "start admin", test.err)
			if result != test.want || (stderr.Len() == 0) != (test.want == 0) {
				t.Fatalf("exit=%d output=%q", result, stderr.String())
			}
		})
	}
}

// A server may answer another readiness request while the supervisor's request
// is still pending. Interrupting then must not report a failed startup/reload.
func TestDevelopmentReadinessInterruptedDuringRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancel()
		<-r.Context().Done()
	}))
	defer server.Close()
	process := &managedProcess{done: make(chan struct{})}
	err := waitForDevelopmentURL(ctx, server.URL, process)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("readiness error=%v", err)
	}
	var stderr bytes.Buffer
	if code := developmentFailure(ctx, newCLIOutput(io.Discard, &stderr, cliOutputOptions{}), "start admin", err); code != 0 || stderr.Len() != 0 {
		t.Fatalf("exit=%d output=%q", code, stderr.String())
	}
}

func TestDevelopmentReadinessCanceledWithExitedProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	process := &managedProcess{done: make(chan struct{}), err: errors.New("terminated")}
	close(process.done)
	for range 50 {
		if err := waitForDevelopmentURL(ctx, "http://127.0.0.1:1", process); !errors.Is(err, context.Canceled) {
			t.Fatalf("readiness error=%v", err)
		}
	}
	if process, err := startManagedProcess(ctx, "server", t.TempDir(), nil, newCLIOutput(io.Discard, io.Discard, cliOutputOptions{}), "missing-program"); process != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled start=%v,%v", process, err)
	}
}

func TestDevelopmentFailurePreservesChildFailure(t *testing.T) {
	if os.Getenv("RIDU_DEV_CANCEL_TEST_CHILD") == "true" {
		os.Exit(7)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestDevelopmentFailurePreservesChildFailure$")
	command.Env = append(os.Environ(), "RIDU_DEV_CANCEL_TEST_CHILD=true")
	err := command.Run()
	if err == nil {
		t.Fatal("expected child failure")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stderr bytes.Buffer
	if code := developmentFailure(ctx, newCLIOutput(io.Discard, &stderr, cliOutputOptions{}), "prepare runtime", err); code != 1 || !strings.Contains(stderr.String(), "exit status 7") {
		t.Fatalf("exit=%d output=%q", code, stderr.String())
	}
}

func TestDevelopmentFailureCanceledForegroundProcess(t *testing.T) {
	if os.Getenv("RIDU_DEV_CANCEL_FOREGROUND_CHILD") == "true" {
		fmt.Println("ready")
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := runForeground(ctx, t.TempDir(), []string{"RIDU_DEV_CANCEL_FOREGROUND_CHILD=true"}, cancelDevelopmentOnWrite{cancel}, io.Discard, os.Args[0], "-test.run=^TestDevelopmentFailureCanceledForegroundProcess$")
	var childError *exec.ExitError
	if !errors.As(err, &childError) || !errors.Is(err, context.Canceled) || !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("foreground error=%v context=%v", err, ctx.Err())
	}
	var stderr bytes.Buffer
	if code := developmentFailure(ctx, newCLIOutput(io.Discard, &stderr, cliOutputOptions{}), "prepare runtime", fmt.Errorf("build: %w", err)); code != 0 || stderr.Len() != 0 {
		t.Fatalf("exit=%d output=%q", code, stderr.String())
	}
}

type cancelDevelopmentOnWrite struct{ cancel context.CancelFunc }

func (writer cancelDevelopmentOnWrite) Write(data []byte) (int, error) {
	writer.cancel()
	return len(data), nil
}
