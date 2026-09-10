// Package field defines the stored values, validation, access rules, hooks, and
// admin presentation of fields in Ridu collections and globals. Build a Fields
// value from constructors such as Text, Number, Select, Group, and Array, then
// assign it to ridu.Collection.Fields, ridu.Global.Fields, or a nested field.
//
// Constructors return concrete field values with fluent methods. Every method
// returns a refined copy, so a reusable field factory can be renamed, nested, or
// extended without changing the original. Access, Hooks, and Admin group related
// behavior; callbacks use occurrence-bound contexts from the operation package.
// Validate runs on authoritative saves. LiveValidate separately opts a writable
// field into advisory admin feedback without executing defaults or save hooks.
//
// Text and Number configure scalar values. TextList and NumberList configure
// ordered primitive slices, preserving duplicates and meaningful empty/zero
// items. Their typed callbacks receive []string and []float64 respectively;
// the whole list is one field occurrence. Use Array for rows with child fields
// and stable identities, or MultiSelect for a list drawn from fixed options.
//
// Use Fields.Edit for checked changes to a reusable field graph. Snapshot returns
// a detached View for inspecting a Node without exposing mutable configuration.
// Nested-field constructors and Fields.Edit snapshot their inputs. A top-level
// Fields slice remains caller-owned until config resolution, which snapshots the
// complete graph so later slice or map changes cannot alter the published schema.
package field
