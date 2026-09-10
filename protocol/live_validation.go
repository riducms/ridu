package protocol

import "github.com/riducms/ridu/store"

// LiveValidationRequest asks for explicitly enabled advisory checks of an
// unsaved snapshot. It cannot save, initialize defaults or run mutation hooks.
// Field paths refer to the submitted snapshot and are validated by the server.
type LiveValidationRequest struct {
	// ID selects an existing collection document. Global requests must omit it.
	ID       string                        `json:"id,omitempty"`
	Data     store.Values                  `json:"data"`
	Fields   []string                      `json:"fields"`
	Embedded []LiveValidationEmbeddedScope `json:"embedded,omitempty"`
}

// LiveValidationEmbeddedScope selects a declared plugin payload being edited
// before Apply. Chained field paths are relative to the previous payload.
// Identity is the item's ordinary stable key, never an internal occurrence ID.
type LiveValidationEmbeddedScope struct {
	Field       string       `json:"field"`
	TreeKey     string       `json:"treeKey"`
	CaseTag     string       `json:"caseTag"`
	VariantSlug string       `json:"variantSlug"`
	Identity    string       `json:"identity"`
	Data        store.Values `json:"data"`
}

// LiveValidationEvaluation distinguishes a completed check from unavailable
// typed input. A skipped check makes no claim that the input is valid.
type LiveValidationEvaluation struct {
	Path   string            `json:"path"`
	Target string            `json:"target,omitempty"`
	Status string            `json:"status"`
	Issues []ValidationIssue `json:"issues"`
}

// LiveValidationEnvelope carries advisory feedback, not authorization or a
// promise that a future save will pass authoritative validation.
type LiveValidationEnvelope struct {
	Evaluations []LiveValidationEvaluation `json:"evaluations"`
}
