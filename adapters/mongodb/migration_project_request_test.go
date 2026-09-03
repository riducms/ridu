package mongodb

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	ridumigration "github.com/riducms/ridu/migration"
	"github.com/riducms/ridu/schema"
)

func TestMongoDBProjectMigrationsRejectPostgreSQLOnlyRuntimeOptions(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*ridumigration.ProjectRequest)
	}{
		{name: "lock timeout", configure: func(request *ridumigration.ProjectRequest) { request.LockTimeout = time.Second }},
		{name: "statement timeout", configure: func(request *ridumigration.ProjectRequest) { request.StatementTimeout = time.Second }},
		{name: "batch timeout", configure: func(request *ridumigration.ProjectRequest) { request.BatchTimeout = time.Second }},
		{name: "idle transaction timeout", configure: func(request *ridumigration.ProjectRequest) { request.IdleTransactionTimeout = time.Second }},
		{name: "stop after phase", configure: func(request *ridumigration.ProjectRequest) { request.StopAfterPhase = "phase-001" }},
		{name: "stop after step", configure: func(request *ridumigration.ProjectRequest) { request.StopAfterStep = "step-0001" }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := ridumigration.ProjectRequest{
				Action:    ridumigration.ProjectVerify,
				Directory: filepath.Join(t.TempDir(), "missing-artifacts"),
			}
			test.configure(&request)

			err := ProjectMigrations().RunProjectMigration(context.Background(), request, schema.Manifest{})
			if err == nil || err.Error() != "MongoDB project migrations do not accept PostgreSQL-only timeout or stop-boundary options" {
				t.Fatalf("PostgreSQL-only %s error = %v", test.name, err)
			}
		})
	}
}
