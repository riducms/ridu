package postgres

import "github.com/riducms/ridu/internal/environment"

// ConfigFromEnvironment reads a Ridu application's standard PostgreSQL
// variables: DATABASE_URL, RIDU_ALLOW_INSECURE_DATABASE for a plaintext
// remote database secured outside PostgreSQL, and
// RIDU_POSTGRES_UPLOAD_LOCK_CONNECTIONS. Set other PoolConfig fields on the
// result before opening the store.
func ConfigFromEnvironment() (PoolConfig, error) {
	insecure, err := environment.Bool("RIDU_ALLOW_INSECURE_DATABASE")
	if err != nil {
		return PoolConfig{}, err
	}
	uploadLocks, err := environment.Count32("RIDU_POSTGRES_UPLOAD_LOCK_CONNECTIONS")
	if err != nil {
		return PoolConfig{}, err
	}
	return PoolConfig{
		DatabaseURL:              environment.Text("DATABASE_URL"),
		AllowInsecureTransport:   insecure,
		MaxUploadLockConnections: uploadLocks,
	}, nil
}
