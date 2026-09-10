package core

import (
	"fmt"
	"github.com/riducms/ridu/schema"
)

func validateResourceHooks(config Config) error {
	var issues []schema.Issue
	for index, global := range config.Globals {
		if phases := unsupportedGlobalHookPhases(global.Hooks); len(phases) > 0 {
			issues = append(issues, schema.Issue{Code: "incompatible_global_hook", Path: fmt.Sprintf("globals[%d].hooks", index), Message: fmt.Sprintf("global hooks configure unsupported phases: %v", phases)})
		}
	}
	if len(issues) > 0 {
		return schema.NewValidationError(issues)
	}
	return nil
}

func unsupportedGlobalHookPhases(hooks CollectionHooks) []string {
	var phases []string
	if len(hooks.BeforeDuplicate) != 0 {
		phases = append(phases, "BeforeDuplicate")
	}
	if len(hooks.BeforeDelete) != 0 {
		phases = append(phases, "BeforeDelete")
	}
	if len(hooks.AfterDelete) != 0 {
		phases = append(phases, "AfterDelete")
	}
	return phases
}
