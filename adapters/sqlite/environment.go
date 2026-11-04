package sqlite

import "github.com/riducms/ridu/internal/environment"

// ConfigFromEnvironment reads RIDU_SQLITE_PATH, which must name the database
// file by an absolute path or a file URI with one, so the server and a built
// binary's migrate up open the same file. Set other Config fields on the
// result before opening the store.
func ConfigFromEnvironment() (Config, error) {
	path, err := environment.SQLitePath()
	if err != nil {
		return Config{}, err
	}
	return Config{Path: path}, nil
}
