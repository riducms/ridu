package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

func TestSQLiteProjectMigrationsRejectPostgreSQLOnlyRuntimeOptions(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*migration.ProjectRequest)
	}{
		{name: "lock timeout", configure: func(request *migration.ProjectRequest) { request.LockTimeout = time.Second }},
		{name: "statement timeout", configure: func(request *migration.ProjectRequest) { request.StatementTimeout = time.Second }},
		{name: "batch timeout", configure: func(request *migration.ProjectRequest) { request.BatchTimeout = time.Second }},
		{name: "idle transaction timeout", configure: func(request *migration.ProjectRequest) { request.IdleTransactionTimeout = time.Second }},
		{name: "stop after phase", configure: func(request *migration.ProjectRequest) { request.StopAfterPhase = "phase-001" }},
		{name: "stop after step", configure: func(request *migration.ProjectRequest) { request.StopAfterStep = "step-0001" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := migration.ProjectRequest{
				Action:    migration.ProjectVerify,
				Directory: filepath.Join(t.TempDir(), "missing-artifacts"),
			}
			test.configure(&request)

			err := ProjectMigrations().RunProjectMigration(context.Background(), request, schema.Manifest{})
			if err == nil || err.Error() != "SQLite project migrations do not accept PostgreSQL-only timeout or stop-boundary options" {
				t.Fatalf("PostgreSQL-only %s error = %v", test.name, err)
			}
		})
	}
}
