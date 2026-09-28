package operation

import "github.com/riducms/ridu/schema"

// Rejection is a hook's deliberate refusal of an operation. Unlike an ordinary
// hook error, its message and issues are meant for the caller: the engine
// returns them as a rejected operation instead of an internal failure.
type Rejection struct {
	Message string
	Issues  []schema.Issue
}

func (rejection *Rejection) Error() string { return rejection.Message }
