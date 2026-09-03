// Package ridu provides the public application-authoring facade for the Ridu
// content-management framework.
//
// The facade re-exports the stable authoring and application contracts from
// package core while keeping ordinary application imports concise. Declarative
// fields and queries resolve into one schema, and every document operation
// enters the same internal operation engine.
package ridu
