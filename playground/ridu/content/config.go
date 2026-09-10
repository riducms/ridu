package content

import (
	"github.com/riducms/ridu"
)

func Config() ridu.Config {
	return ridu.Config{
		Name:        "Ridu",
		Admin:       ridu.AdminConfig{User: "users"},
		Plugins:     installedPlugins(),
		Collections: []ridu.Collection{Users, Posts, Workshops},
	}
}
