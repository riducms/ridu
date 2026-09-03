package migration

import (
	"context"
	"fmt"
	"time"

	"github.com/riducms/ridu/schema"
)

// ProjectAction is one lifecycle operation delegated from the portable CLI to
// an application's compiled migration driver when artifacts contain callbacks.
type ProjectAction string

const (
	ProjectApply   ProjectAction = "up"
	ProjectDown    ProjectAction = "down"
	ProjectReset   ProjectAction = "reset"
	ProjectRefresh ProjectAction = "refresh"
	ProjectFresh   ProjectAction = "fresh"
	ProjectVerify  ProjectAction = "verify"
)

// ProjectRequest contains only the database selection and structural paths
// selected by the invoking CLI. It carries no executable code and is private
// to the local project process. Credential-bearing URLs must be transported to
// that process outside its command-line arguments.
type ProjectRequest struct {
	Action                 ProjectAction
	DatabasePath           string
	DatabaseURL            string
	Directory              string
	AllowInsecureDatabase  bool
	AllowMaintenance       bool
	AllowUnbounded         bool
	LockWait               time.Duration
	OperationTimeout       time.Duration
	LockTimeout            time.Duration
	StatementTimeout       time.Duration
	BatchTimeout           time.Duration
	IdleTransactionTimeout time.Duration
	StopAfterPhase         string
	StopAfterStep          string
}

// ProjectDriver keeps application callbacks compiled into the project while
// letting the selected adapter own the enclosing database transaction.
type ProjectDriver interface {
	// Validate checks the complete executable registration before a project
	// command resolves config or changes database state.
	Validate() error
	DataTransforms() []DataTransformDescriptor
	// RunProjectMigration receives the exact manifest resolved by the process
	// that will execute the request. The driver must validate it against the
	// selected artifact history before opening or changing the database.
	RunProjectMigration(context.Context, ProjectRequest, schema.Manifest) error
}

// Validate rejects incomplete or unknown project migration requests.
func (request ProjectRequest) Validate() error {
	if request.DatabasePath != "" && request.DatabaseURL != "" {
		return fmt.Errorf("project migration %s cannot select both a database path and URL", request.Action)
	}
	if request.LockWait < 0 || request.OperationTimeout < 0 || request.LockTimeout < 0 ||
		request.StatementTimeout < 0 || request.BatchTimeout < 0 || request.IdleTransactionTimeout < 0 {
		return fmt.Errorf("project migration %s requires non-negative runtime bounds", request.Action)
	}
	switch request.Action {
	case ProjectApply, ProjectDown, ProjectReset, ProjectRefresh, ProjectFresh:
		if request.DatabasePath == "" && request.DatabaseURL == "" {
			return fmt.Errorf("project migration %s requires a database path or URL", request.Action)
		}
	case ProjectVerify:
	default:
		return fmt.Errorf("unknown project migration action %q", request.Action)
	}
	if request.Directory == "" {
		return fmt.Errorf("project migration %s requires an artifact directory", request.Action)
	}
	return nil
}
