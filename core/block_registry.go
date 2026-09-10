package core

import (
	"fmt"

	"github.com/riducms/ridu/field"
)

func bindConfigBlocks(config *Config) error {
	registry, err := field.NewBlockRegistry(config.Blocks...)
	if err != nil {
		return err
	}
	config.Blocks = registry.Blocks()
	for i := range config.Collections {
		fields, err := registry.BindAt(config.Collections[i].Fields, fmt.Sprintf("collections[%d].fields", i))
		if err != nil {
			return err
		}
		config.Collections[i].Fields = fields
	}
	for i := range config.Globals {
		fields, err := registry.BindAt(config.Globals[i].Fields, fmt.Sprintf("globals[%d].fields", i))
		if err != nil {
			return err
		}
		config.Globals[i].Fields = fields
	}
	return nil
}
