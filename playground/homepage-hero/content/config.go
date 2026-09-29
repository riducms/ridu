package content

import "github.com/riducms/ridu"

// Config is copied over a generated blank application by the homepage hero
// capture. The generated application and its production binary are what the
// screenshot shows; this file only supplies the journal content model.
func Config() ridu.Config {
	return ridu.Config{
		Name:             "Field Notes",
		Admin:            ridu.AdminConfig{User: "users"},
		StorageNamespace: "homepage-hero",
		Plugins:          installedPlugins(),
		Collections:      []ridu.Collection{Users, Media, Posts},
	}
}
