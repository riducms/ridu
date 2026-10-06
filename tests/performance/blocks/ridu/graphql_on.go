//go:build blocksgraphql

package main

import (
	"github.com/riducms/ridu"
	graphqlplugin "github.com/riducms/ridu/plugins/graphql"
)

const graphQLEnabled = true

func extraPlugins() []ridu.Plugin { return []ridu.Plugin{graphqlplugin.New()} }
