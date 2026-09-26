package content

import "github.com/riducms/ridu"

func Config() ridu.Config {
	return ridu.Config{
		Name: "Editorial",
		Admin: ridu.AdminConfig{
			User:    "users",
			Loaders: []ridu.AdminLoaderDefinition{PostSummary},
		},
		Collections: []ridu.Collection{Users, Posts},
	}
}
