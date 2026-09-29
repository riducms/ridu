package query

import "fmt"

// Direction controls one stable sort term.
type Direction string

const (
	Ascending  Direction = "asc"
	Descending Direction = "desc"
)

// Sort is one validated ordered field term. Stores append ID as a stable
// tiebreaker when callers do not provide it explicitly.
type Sort struct {
	Path      Path
	Direction Direction
}

// Asc sorts by a field from lowest to highest, as in Asc("title").
func Asc[P FieldPath](path P) Sort {
	return Sort{Path: pathOf(path), Direction: Ascending}
}

// Desc sorts by a field from highest to lowest, as in Desc("createdAt").
func Desc[P FieldPath](path P) Sort {
	return Sort{Path: pathOf(path), Direction: Descending}
}

// NewSort validates a sort term whose direction is chosen at run time, such as
// one read from a request. Use Asc or Desc for a sort written in your code.
func NewSort(path Path, direction Direction) (Sort, error) {
	if path.String() == "" {
		return Sort{}, fmt.Errorf("sort requires a field path")
	}
	if direction != Ascending && direction != Descending {
		return Sort{}, fmt.Errorf("unknown sort direction %q", direction)
	}
	return Sort{Path: path, Direction: direction}, nil
}

// Population asks a store to replace one relationship ID with its projected
// target document. Depth is bounded by the operation engine.
type Population struct {
	Path  Path
	Depth int
	// Select is nil for the complete populated target. A non-nil empty slice
	// returns only document metadata.
	Select []Path
}
