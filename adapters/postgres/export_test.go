package postgres

import "context"

// ExecForTest runs one raw statement so external tests can create physical
// drift that no public schema path is allowed to produce.
func (backend *Store) ExecForTest(ctx context.Context, statement string) error {
	_, err := backend.pool.Exec(ctx, statement)
	return err
}
