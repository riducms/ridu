package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const (
	frameworkAdminAddress = "127.0.0.1:8080"
	frameworkAdminURL     = "http://127.0.0.1:5173/admin/"
)

// RunFrameworkAdminDev supervises the framework repository's deterministic
// browser fixture and framework-owned Vite admin. Generated applications use
// ridu dev instead.
func RunFrameworkAdminDev(ctx context.Context, root string, stdout, stderr io.Writer) int {
	output := newCLIOutput(stdout, stderr, cliOutputOptions{accessible: os.Getenv("RIDU_ACCESSIBLE") != ""})
	if err := verifyFrameworkAdminRoot(root); err != nil {
		output.Error("verify framework admin root", err)
		return 1
	}
	if err := writeAdminSchemaReloadSignal(root, false); err != nil {
		output.Error("prepare admin schema reload signal", err)
		return 1
	}

	serverEnvironment := []string{"RIDU_BROWSER_ADDRESS=" + frameworkAdminAddress}
	server, err := startManagedProcess(ctx, "server", root, serverEnvironment, output, "go", "run", "./tests/contracts/admin_server")
	if err != nil {
		return developmentFailure(ctx, output, "start admin fixture", err)
	}
	adminEnvironment := []string{"RIDU_FRAMEWORK_ADMIN_FIXTURE=true"}
	admin, err := startManagedProcess(ctx, "admin", root, adminEnvironment, output, "bun", "run", "--cwd", "admin", "dev", "--", "--host", "127.0.0.1", "--port", "5173", "--strictPort")
	if err != nil {
		server.stop()
		return developmentFailure(ctx, output, "start Vite admin", err)
	}
	stopProcesses := func() {
		server.stop()
		admin.stop()
	}

	if err := waitForDevelopmentURL(ctx, "http://"+frameworkAdminAddress+"/healthz", server); err != nil {
		stopProcesses()
		return developmentFailure(ctx, output, "start admin fixture", err)
	}
	if err := waitForDevelopmentURL(ctx, frameworkAdminURL, admin); err != nil {
		stopProcesses()
		return developmentFailure(ctx, output, "start Vite admin", err)
	}
	output.Info("Ridu framework admin is ready")
	fmt.Fprintf(stdout, "  Admin  %s\n  API    http://%s\n", frameworkAdminURL, frameworkAdminAddress)
	fmt.Fprintln(stdout, "  Login  editor@riducms.test / ridu-browser")
	output.Info("Watching framework Go and admin sources; press Ctrl+C to stop")

	watcher, err := newGoSourceWatcher(root)
	if err != nil {
		stopProcesses()
		output.Error("watch framework Go sources", err)
		return 1
	}
	defer watcher.Close()

	for {
		select {
		case <-ctx.Done():
			stopProcesses()
			return developmentFailure(ctx, output, "development stopped", ctx.Err())
		case <-server.done:
			if ctx.Err() == nil && !server.stopping.Load() {
				admin.stop()
				output.Error("admin fixture stopped", server.waitError())
				return 1
			}
		case <-admin.done:
			if ctx.Err() == nil && !admin.stopping.Load() {
				server.stop()
				output.Error("Vite admin stopped", admin.waitError())
				return 1
			}
		case watchError := <-watcher.Errors():
			output.Warn("watcher warning", watchError)
		case <-watcher.Changes():
			output.Info("Framework Go changed; restarting the admin fixture")
			server.stop()
			server, err = startManagedProcess(ctx, "server", root, serverEnvironment, output, "go", "run", "./tests/contracts/admin_server")
			if err != nil {
				admin.stop()
				return developmentFailure(ctx, output, "restart admin fixture", err)
			}
			if err := waitForDevelopmentURL(ctx, "http://"+frameworkAdminAddress+"/healthz", server); err != nil {
				stopProcesses()
				return developmentFailure(ctx, output, "restart admin fixture", err)
			}
			if err := writeAdminSchemaReloadSignal(root, false); err != nil {
				output.Warn("admin schema refresh", err)
				continue
			}
			output.Info("Admin fixture is ready; refreshing the manifest in place")
		}
	}
}

func verifyFrameworkAdminRoot(root string) error {
	for _, path := range []string{"go.mod", "admin/vite.config.ts", "tests/contracts/admin_server/main.go"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			return fmt.Errorf("framework admin development must run from the Ridu root: %s: %w", path, err)
		}
	}
	return nil
}
