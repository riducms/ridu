package generate

import (
	"fmt"
	"sort"
	"strings"

	"github.com/riducms/ridu/internal/blocktypes"
	"github.com/riducms/ridu/schema"
)

func goNestedFieldNames(fields []schema.Field, arrayRow bool) []string {
	reserved := []string{"Key", "MarshalJSON", "UnmarshalJSON"}
	if arrayRow {
		reserved = append(reserved, "RowKey")
	}
	return goModelFieldNames(schema.Collection{Fields: fields}, reserved...)
}

func (g *goBlocks) writeRetain(out *strings.Builder, field blocktypes.Field, mode goModelMode) {
	name := field.Name + goBlockSuffix(mode)
	update := field.Name + "Update"
	fmt.Fprintf(out, `// Retain creates fresh key-only updates in the same order, preserving every occurrence.
// Edit the returned update pointers, append new input pointers, or explicitly remove/reorder entries.
// No authored fields are copied, including redacted, localized or populated values.
// An absent list, nil row, empty key or duplicate key returns an error and no partial list.
// An explicitly empty list returns an empty update list. Use revision checks when saving edits.
func (rows *%s) Retain() (%s, error) {
 if rows==nil || *rows==nil {return nil, newContractError(ContractError{Operation:"retain",Container:%q,Reason:"cannot retain an absent block list"})}
 retained:=make(%s,len(*rows)); seen:=make(map[string]bool,len(*rows))
 for index,row:=range *rows {
  if row==nil || strings.TrimSpace(row.BlockKey())=="" {return nil,blockRowError(ContractError{Operation:"retain",Container:%q,Reason:"retained block requires a nonempty identity"},index)}
  key:=row.BlockKey()
  if seen[key] {return nil,blockRowError(ContractError{Operation:"retain",Container:%q,Reason:"duplicate retained block identity"},index)}
  seen[key]=true
  switch row.(type) {
`, name, update, name, update, name, name)
	for _, variant := range field.Variants {
		fmt.Fprintf(out, "case *%s: retained[index]=&%sUpdate{Key:key}\n", variant+goBlockSuffix(mode), variant)
	}
	fmt.Fprintf(out, "default:return nil,blockRowError(ContractError{Operation:\"retain\",Container:%q,Reason:\"unsupported retained block variant\"},index)\n}\n};return retained,nil\n}\n", name)
	if field.Embedded {
		fmt.Fprintf(out, `// Retain creates a key-only update for this embedded occurrence, without copying
// redacted, localized or populated authored fields. Use revision checks when saving.
func (payload %sPayload) Retain() (%sPayload, error) {
 rows := %s{payload.Value}
 retained, err := rows.Retain()
 if err != nil { return %sPayload{}, err }
 return %sPayload{Value: retained[0]}, nil
}
`, name, update, name, update, update)
	}
}

func (g *goBlocks) writeSelects(out *strings.Builder) {
	ids := make([]schema.StableID, 0, len(g.selects))
	for id := range g.selects {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		name := g.selectNames[id]
		field := g.selects[id]
		fmt.Fprintf(out, "// %s is a configured option for %s. The operation engine validates values.\ntype %s string\n", name, field.Name, name)
		for _, option := range field.Select.Options {
			suffix := exportedGoIdentifier(option.Value)
			if suffix == "" {
				suffix = "Empty"
			}
			constant := name + suffix
			fmt.Fprintf(out, "// %s selects %q.\nconst %s %s = %q\n", constant, option.Value, constant, name, option.Value)
		}
	}
}

// Array retention has the same identity-only contract as block retention. Keeping
// read arrays concrete preserves ordinary indexing; update unions admit pointers
// so callers can edit the retained rows without replacing interface values.
func (g *goBlocks) writeArrayReads(out *strings.Builder) {
	names := make([]string, 0, len(g.arrayReads))
	for name := range g.arrayReads {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		row := g.arrayReads[name]
		base := strings.TrimSuffix(row, goBlockSuffix(g.nestedModes[row]))
		update := strings.TrimSuffix(base, "Row") + "Update"
		fmt.Fprintf(out, `// %s contains concrete array rows. Use Retain before editing existing rows.
type %s []%s
func(rows %s) MarshalJSON()([]byte,error) {
 data,err:=encodeBlockSlice([]%s(rows),encodeBlockValue[%s])
 if err!=nil{return nil,newContractError(ContractError{Operation:"encode",Container:%q,Reason:"invalid array",Err:err})}
 return data,nil
}
func(rows *%s) UnmarshalJSON(data []byte)error {
 decoded,err:=decodeBlockSlice(data,decodeBlockValue[%s])
 if err!=nil{return newContractError(ContractError{Container:%q,Reason:"invalid array",Err:err})}
 *rows=%s(decoded);return nil
}
// Retain creates fresh key-only update pointers in the same order.
// It copies no authored fields. Edit the pointers, append new input pointers,
// or explicitly remove/reorder rows. Save with a revision check.
// Nil lists, empty keys and duplicate keys fail without returning a partial list.
// An explicitly empty list produces an empty update list.
func(rows %s) Retain()(%s,error) {
 if rows==nil{return nil,newContractError(ContractError{Operation:"retain",Container:%q,Reason:"cannot retain an absent or null array"})}
 retained:=make(%s,len(rows));seen:=make(map[string]bool,len(rows))
 for index,row:=range rows {
  if strings.TrimSpace(row.Key)==""{return nil,blockRowError(ContractError{Operation:"retain",Container:%q,Path:"_key",Reason:"retained array row requires a nonempty identity"},index)}
  if seen[row.Key]{return nil,blockRowError(ContractError{Operation:"retain",Container:%q,Path:"_key",Reason:"duplicate retained array identity"},index)}
  seen[row.Key]=true;retained[index]=&%sUpdate{Key:row.Key}
 }
 return retained,nil
}
`, name, name, row, name, row, row, name, name, row, name, name, name, update, name, update, name, name, base)
	}
}

func writeGoBlockContainerDoc(out *strings.Builder, name string, mode goModelMode) {
	fmt.Fprintf(out, "// %sBlock admits only generated block pointers; decoding returns those same pointer types.\n", name)
	if mode == goUpdate {
		out.WriteString("// Use keyed Update pointers for existing occurrences and Input pointers for new occurrences.\n")
	}
}

func writeGoBlockListDoc(out *strings.Builder, name string, mode goModelMode) {
	fmt.Fprintf(out, "// %s is an ordered list of block pointers. Nil rows are invalid.\n", name)
	switch mode {
	case goUpdate:
		out.WriteString("// Saving this list replaces order and membership: omitted occurrences are removed.\n// Start with a read list's Retain method to preserve untouched blocks.\n")
	case goAllLocales:
		out.WriteString("// Localized children contain locale-code maps; use ordinary output types for a single locale.\n")
	case goAllLocalesValue:
		out.WriteString("// This is one locale's value beneath a localized container in an all-locales response.\n// Children are ordinary values; populated target documents still have all-locales shapes.\n")
	}
}

func writeGoBlockVariantDoc(out *strings.Builder, name string, mode goModelMode) {
	fmt.Fprintf(out, "// %s is a generated block. Use *%s in block lists and type switches.\n", name, name)
	switch mode {
	case goCreate:
		out.WriteString("// Input supplies a new occurrence's required fields. An empty Key requests a server-assigned key.\n")
		fmt.Fprintf(out, "// Read this block as %s; edit an existing occurrence with %sUpdate.\n", strings.TrimSuffix(name, "Input"), strings.TrimSuffix(name, "Input"))
	case goUpdate:
		out.WriteString("// Update retains an existing occurrence by Key. Omitted children remain unchanged.\n// The engine verifies that the key belongs to an existing occurrence of this variant.\n")
		fmt.Fprintf(out, "// Start with the read list's Retain method; use %sInput for additions.\n", strings.TrimSuffix(name, "Update"))
	default:
		out.WriteString("// Authored children may be omitted by access rules or projection, even when required on create.\n")
		if mode == goOutput {
			fmt.Fprintf(out, "// Create with %sInput. To edit, retain the read list and modify its %sUpdate pointers.\n", name, name)
		}
	}
}

func writeGoFieldDoc(out *strings.Builder, name string, field schema.Field, typ string, mode goModelMode) {
	if goOutputMode(mode) {
		if strings.HasPrefix(typ, "*BlockOptional[") {
			fmt.Fprintf(out, "// %s: nil means omitted; a wrapper with nil Value means null. Get returns a concrete value when present.\n", name)
		} else {
			fmt.Fprintf(out, "// %s may be omitted by access rules or projection.\n", name)
		}
		if mode == goAllLocales && field.Localized {
			out.WriteString("// Locale-code map; missing locales are absent translations.\n")
		}
		return
	}
	switch {
	case strings.HasPrefix(typ, "*core.Input["):
		fmt.Fprintf(out, "// %s: nil omits the field; core.Set sends a value; core.Null sends explicit null.\n", name)
	case strings.HasPrefix(typ, "*core.NonNullInput["):
		fmt.Fprintf(out, "// %s: nil omits the field; core.SetNonNull sends a value and rejects JSON null.\n", name)
	case strings.HasPrefix(typ, "core.NonNullInput["):
		fmt.Fprintf(out, "// %s requires core.NonNull(value); JSON null is rejected.\n", name)
	case strings.HasPrefix(typ, "*"):
		fmt.Fprintf(out, "// %s: nil omits the field; a pointer sends a value. Null is not allowed.\n", name)
	default:
		fmt.Fprintf(out, "// %s is required when creating this value.\n", name)
	}
	if field.Type == schema.FieldTypeBlocks && mode == goUpdate {
		out.WriteString("// A supplied list replaces order and membership; use the read list's Retain method to preserve untouched blocks.\n")
	}
}
