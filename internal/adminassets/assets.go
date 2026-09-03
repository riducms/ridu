// Package adminassets owns the framework admin build embedded in Ridu servers.
package adminassets

import (
	"embed"
	"io/fs"
)

//go:embed dist
var distribution embed.FS

// FS returns the immutable production admin filesystem rooted at dist.
func FS() fs.FS {
	root, err := fs.Sub(distribution, "dist")
	if err != nil {
		panic(err)
	}
	return root
}
