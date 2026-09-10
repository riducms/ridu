// This entry exposes the same executable generation and migration protocol as a
// generated application. Runtime serving is intentionally left to ridu new.
package main

import (
	"github.com/riducms/ridu"
	"github.com/riducms/ridu/adapters/sqlite"
	"github.com/riducms/ridu/examples/blocks/content"
	"log"
)

func main() {
	if err := ridu.Execute(content.Config(), ridu.WithProjectMigrations(sqlite.ProjectMigrations())); err != nil {
		log.Fatal(err)
	}
}
