// Package core proves only the upper import edge in the isolated Gate 1 module.
// It is not application coordination and has no resolver, runtime or LocalAPI.
package core

import "example.com/ridu-gate1/field"

type Config struct{ Fields field.Fields }
