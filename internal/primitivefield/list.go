// Package primitivefield contains the private storage rules shared by text and
// number lists: their index restrictions and value-shape evolution. Their
// query contract is the membership package's.
package primitivefield

import "github.com/riducms/ridu/schema"

// IsList reports whether field is a text or number list.
func IsList(field schema.Field) bool {
	return field.Type == schema.FieldTypeTextList || field.Type == schema.FieldTypeNumberList
}
