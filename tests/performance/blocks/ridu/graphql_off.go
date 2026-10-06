//go:build !blocksgraphql

package main

import "github.com/riducms/ridu"

const graphQLEnabled = false

func extraPlugins() []ridu.Plugin { return nil }
