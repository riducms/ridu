// Package environment reads Ridu's standard environment variables. Execute,
// a built binary's migrate up and the official store adapters read them
// through this package, so each variable has one meaning and one error for a
// malformed value. An unset or blank variable reads as the zero value.
package environment

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Text returns the variable with surrounding whitespace removed.
func Text(name string) string {
	return strings.TrimSpace(os.Getenv(name))
}

// Bool parses a variable such as true, false, 1 or 0.
func Bool(name string) (bool, error) {
	raw := Text(name)
	if raw == "" {
		return false, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false, not %q", name, raw)
	}
	return value, nil
}

// Count parses a whole number of zero or more.
func Count(name string) (int, error) {
	value, err := count(name, strconv.IntSize)
	return int(value), err
}

// Count32 parses a whole number of zero or more that fits in 32 bits.
func Count32(name string) (int32, error) {
	value, err := count(name, 32)
	return int32(value), err
}

func count(name string, bits int) (int64, error) {
	raw := Text(name)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseInt(raw, 10, bits)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("%s must be a whole number of zero or more, not %q", name, raw)
	}
	return value, nil
}

// Duration parses a Go duration such as 30s or 2m. A negative duration is
// allowed; options that accept one use it to turn a bound off.
func Duration(name string) (time.Duration, error) {
	raw := Text(name)
	if raw == "" {
		return 0, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration such as 30s or 2m, not %q", name, raw)
	}
	return value, nil
}

// List splits a comma-separated variable and drops blank entries.
func List(name string) []string {
	var values []string
	for _, value := range strings.Split(os.Getenv(name), ",") {
		if value = strings.TrimSpace(value); value != "" {
			values = append(values, value)
		}
	}
	return values
}

// SQLitePath reads RIDU_SQLITE_PATH, which must name a file by an absolute
// path or a file URI with one: migrate up and the server run as separate
// processes, so both must open the same file whatever their working directory.
func SQLitePath() (string, error) {
	path := Text("RIDU_SQLITE_PATH")
	switch {
	case path == "":
		return "", fmt.Errorf("RIDU_SQLITE_PATH is required")
	case path == ":memory:":
		return "", fmt.Errorf("RIDU_SQLITE_PATH must be a file because migrations and the server run in separate processes")
	case !SQLitePathAbsolute(path):
		return "", fmt.Errorf("RIDU_SQLITE_PATH must be an absolute file path or a file URI with an absolute path")
	}
	return path, nil
}

// SQLitePathAbsolute reports whether path is an absolute file path or a file
// URI naming one, including Windows drive (file:///C:/…) and UNC forms.
func SQLitePathAbsolute(path string) bool {
	if !strings.HasPrefix(path, "file:") {
		return filepath.IsAbs(path)
	}
	parsed, err := url.Parse(path)
	if err != nil {
		return false
	}
	target := parsed.Path
	if target == "" {
		if target, err = url.PathUnescape(parsed.Opaque); err != nil {
			return false
		}
	}
	if parsed.Host != "" && !strings.EqualFold(parsed.Host, "localhost") {
		// A Windows UNC share names its server as the host.
		return true
	}
	if strings.HasPrefix(target, "/") && filepath.VolumeName(target[1:]) != "" {
		// A Windows drive path is written /C:/….
		target = target[1:]
	}
	return filepath.IsAbs(filepath.FromSlash(target))
}
