package sqlite

import (
	"fmt"
	"time"
)

// validateSQLiteTimes rejects instants that time.Time.UnixNano would wrap
// before they reach an INTEGER timestamp column.
func validateSQLiteTimes(scope string, values ...time.Time) error {
	for _, value := range values {
		normalized := value.UTC()
		if !decodeTime(encodeTime(normalized)).Equal(normalized) {
			return fmt.Errorf("%s is outside SQLite's nanosecond timestamp range", scope)
		}
	}
	return nil
}
