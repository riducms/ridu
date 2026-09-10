package generate

import (
	"fmt"
	"sort"
	"strings"

	"github.com/riducms/ridu/internal/blocktypes"
	"github.com/riducms/ridu/schema"
)

type goBlocks struct {
	catalog          *blocktypes.Catalog
	pluginParameters map[string][]string
	localized        bool
	models           map[schema.StableID]string
	nested           map[string]string
	nestedNames      map[schema.StableID]string
	nestedFields     map[string][]schema.Field
	nestedModes      map[string]goModelMode
	arrayUpdates     map[string]string
	arrayReads       map[string]string
	arrayRows        map[string]bool
	referenceNames   map[schema.StableID]string
	references       map[string]goBlockReference
	selectNames      map[schema.StableID]string
	selects          map[schema.StableID]schema.Field
}

func newGoBlocks(c *blocktypes.Catalog, snapshot schema.Snapshot) (*goBlocks, error) {
	g := &goBlocks{selectNames: map[schema.StableID]string{}, selects: map[schema.StableID]schema.Field{}, catalog: c, localized: snapshot.Application.Localization != nil, models: map[schema.StableID]string{}, nested: map[string]string{}, nestedNames: map[schema.StableID]string{}, nestedFields: map[string][]schema.Field{}, referenceNames: map[schema.StableID]string{}, references: map[string]goBlockReference{}, nestedModes: map[string]goModelMode{}, arrayUpdates: map[string]string{}, arrayReads: map[string]string{}, arrayRows: map[string]bool{}}
	g.pluginParameters = map[string][]string{}
	for _, plugin := range snapshot.Plugins {
		for _, mapping := range plugin.FieldTypes {
			g.pluginParameters[mapping.Key] = mapping.EmbeddedTypes
		}
	}
	used := map[string]int{}
	for _, collection := range append(snapshot.Collections, snapshot.Globals...) {
		name := exportedGoIdentifier(collection.Labels.Singular)
		if name == "" {
			name = exportedGoIdentifier(string(collection.Slug))
		}
		used[name]++
		if used[name] > 1 {
			name += fmt.Sprint(used[name])
		}
		g.models[collection.ID] = name
	}
	// Allocate named child contracts from the same collision-safe member names
	// used by their parent. Reject collisions across different nested paths before
	// schema-bearing maps can overwrite an earlier definition.
	claimed := map[string]schema.StableID{}
	var namingError error
	claim := func(name string, id schema.StableID) {
		if previous, ok := claimed[name]; ok && previous != id && namingError == nil {
			namingError = fmt.Errorf("generated Go symbol collides: %s (fields %s and %s)", name, previous, id)
		}
		claimed[name] = id
	}
	var register func([]schema.Field, string, bool, bool)
	register = func(fields []schema.Field, prefix string, inBlock, arrayRow bool) {
		reserved := []string{"Key", "MarshalJSON", "UnmarshalJSON"}
		if arrayRow {
			reserved = append(reserved, "RowKey")
		}
		if inBlock {
			reserved = append(reserved, "BlockType", "BlockKey")
		}
		members := goModelFieldNames(schema.Collection{Fields: fields}, reserved...)
		for index, f := range fields {
			if f.Select != nil {
				name := prefix + members[index]
				g.selectNames[f.ID] = name
				claim(name, f.ID)
			}
			if f.Relationship != nil && f.Relationship.Polymorphic {
				name := prefix + members[index] + "Reference"
				g.referenceNames[f.ID] = name
				for _, mode := range g.modes() {
					claim(name+goBlockSuffix(mode), f.ID)
				}
			}
			if f.Nested != nil {
				name := prefix + members[index]
				if f.Type == schema.FieldTypeArray {
					name += "Row"
				}
				if inBlock {
					for _, mode := range g.modes() {
						claim(name+goBlockSuffix(mode), f.ID)
					}
				}
				g.nestedNames[f.ID] = name
				register(f.Nested.ResolvedFields(), name, inBlock, inBlock && f.Type == schema.FieldTypeArray)
			}
		}
	}

	for _, resource := range append(snapshot.Collections, snapshot.Globals...) {
		register(resource.Fields, g.models[resource.ID], false, false)
	}
	for _, variant := range c.Variants {
		register(variant.Block.ResolvedFields(), variant.Name, true, false)
	}
	return g, namingError
}
func (g *goBlocks) modes() []goModelMode {
	modes := []goModelMode{goOutput, goCreate, goUpdate}
	if g.localized {
		modes = append(modes, goAllLocales, goAllLocalesValue)
	}
	return modes
}
func goFieldAppearsInMode(f schema.Field, mode goModelMode) bool {
	return f.Category != schema.FieldCategoryPresentation || goOutputMode(mode) && (f.Type == schema.FieldTypeJoin || f.Type == schema.FieldTypeVirtual)
}
func goOutputMode(mode goModelMode) bool {
	return mode == goOutput || mode == goAllLocales || mode == goAllLocalesValue
}
func goBlockSuffix(mode goModelMode) string {
	if mode == goAllLocalesValue {
		return "AllLocalesValue"
	}
	if mode == goUpdate {
		return "Update"
	}
	if mode == goCreate {
		return "Input"
	}
	if mode == goAllLocales {
		return "AllLocales"
	}
	return ""
}
func (g *goBlocks) presence(f schema.Field, typ string, mode goModelMode, optional bool, lossless ...bool) string {
	if goOutputMode(mode) && !f.Required && len(lossless) > 0 && lossless[0] {
		return "*BlockOptional[" + typ + "]"
	}
	if (f.Type == schema.FieldTypeBlocks || f.Type == schema.FieldTypeArray) && (mode == goCreate || mode == goUpdate) && f.Required {
		if optional {
			return "*core.NonNullInput[" + typ + "]"
		}
		return "core.NonNullInput[" + typ + "]"
	}
	return goPresenceType(f, typ, mode, optional)
}
func (g *goBlocks) fieldType(f schema.Field, mode goModelMode, plugins map[string]string, inBlock bool) string {
	if mode == goAllLocales && f.Localized {
		f.Localized = false
		typ := g.fieldType(f, goAllLocalesValue, plugins, inBlock)
		if !f.Required {
			typ = goPresenceType(f, typ, goOutput, true)
		}
		return "map[string]" + typ
	}
	if f.Select != nil {
		name := g.selectNames[f.ID]
		g.selects[f.ID] = f
		if f.Select.HasMany {
			return "[]" + name
		}
		return name
	}
	if f.Relationship != nil && f.Relationship.Polymorphic {
		base := g.referenceNames[f.ID]
		name := base + goBlockSuffix(mode)
		g.references[name] = goBlockReference{base: base, field: f, mode: mode}
		if f.Relationship.HasMany {
			return "[]" + name
		}
		return name
	}
	if f.Type == schema.FieldTypeBlocks {
		return g.catalog.Fields[f.ID].Name + goBlockSuffix(mode)
	}
	if f.Nested != nil {
		var b strings.Builder
		b.WriteString("struct { ")
		if f.Type == schema.FieldTypeArray {
			if goOutputMode(mode) {
				b.WriteString("Key string `json:\"_key\"`; ")
			} else {
				b.WriteString("Key string `json:\"_key,omitempty\"`; ")
			}
		}
		names := goNestedFieldNames(f.Nested.ResolvedFields(), inBlock && f.Type == schema.FieldTypeArray)
		for i, child := range f.Nested.ResolvedFields() {
			if !goFieldAppearsInMode(child, mode) {
				continue
			}
			typ := g.fieldType(child, mode, plugins, inBlock)
			optional := goOutputMode(mode) || mode == goUpdate || !generatedInputRequired(child) || generatedFieldHasDefault(child)
			typ = g.presence(child, typ, mode, optional, inBlock)
			tag := child.Name
			if optional {
				tag += goOmissionTag(child, mode)
			}
			b.WriteString("\n")
			writeGoFieldDoc(&b, names[i], child, typ, mode)
			fmt.Fprintf(&b, "%s %s `json:%q`; ", names[i], typ, tag)
		}
		b.WriteString("}")
		typ := b.String()
		if inBlock {
			name := g.nestedNames[f.ID] + goBlockSuffix(mode)
			g.nested[name] = typ
			g.nestedFields[name] = f.Nested.ResolvedFields()
			g.nestedModes[name] = mode
			if f.Type == schema.FieldTypeArray {
				g.arrayRows[name] = true
			}
			typ = name
		}
		if f.Type == schema.FieldTypeArray && mode == goUpdate && inBlock {
			container := strings.TrimSuffix(g.nestedNames[f.ID], "Row") + "Update"
			g.arrayUpdates[container] = g.nestedNames[f.ID]
			return container
		}
		if f.Type == schema.FieldTypeArray && inBlock && goOutputMode(mode) {
			container := strings.TrimSuffix(g.nestedNames[f.ID], "Row") + goBlockSuffix(mode)
			g.arrayReads[container] = typ
			return container
		}
		if f.Type == schema.FieldTypeArray {
			typ = "[]" + typ
		}
		return typ
	}
	if goOutputMode(mode) && f.Join != nil {
		if name := g.models[f.Join.CollectionID]; name != "" {
			if mode == goAllLocales || mode == goAllLocalesValue {
				name += "AllLocales"
			}
			return "[]" + name
		}
	}
	if goOutputMode(mode) {
		var id schema.StableID
		many := false
		if f.Relationship != nil && !f.Relationship.Polymorphic {
			id = f.Relationship.CollectionID
			many = f.Relationship.HasMany
		}
		if f.Upload != nil {
			id = f.Upload.CollectionID
			many = f.Upload.HasMany
		}
		if name := g.models[id]; name != "" {
			if mode == goAllLocales || mode == goAllLocalesValue {
				name += "AllLocales"
			}
			typ := "Reference[" + name + "]"
			if many {
				typ = "[]" + typ
			}
			return typ
		}
	}
	if f.Plugin != nil && len(g.pluginParameters[f.Plugin.Key]) > 0 && plugins[f.Plugin.Key] != "" {
		var args []string
		for _, payload := range schema.EmbeddedTypeFields(f, g.pluginParameters[f.Plugin.Key]) {
			args = append(args, g.catalog.Fields[payload.ID].Name+goBlockSuffix(mode)+"Payload")
		}
		return plugins[f.Plugin.Key] + "[" + strings.Join(args, ", ") + "]"
	}
	return goFieldType(f, mode, plugins)
}
func (g *goBlocks) write(out *strings.Builder, plugins map[string]string) {
	out.WriteString(goBlockRuntime)
	names := make([]schema.StableID, 0, len(g.catalog.Fields))
	for id := range g.catalog.Fields {
		names = append(names, id)
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	emitted := map[string]bool{}
	for _, id := range names {
		field := g.catalog.Fields[id]
		if emitted[field.Name] {
			continue
		}
		emitted[field.Name] = true
		for _, mode := range g.modes() {
			suffix := goBlockSuffix(mode)
			name := field.Name + suffix
			marker := "is" + name + "Block"
			writeGoBlockContainerDoc(out, name, mode)
			fmt.Fprintf(out, "type %sBlock interface { %s(); BlockType() string; BlockKey() string }\n", name, marker)
			if field.Embedded {
				fmt.Fprintf(out, "// %sPayload is one typed detached payload, with scalar JSON encoding.\ntype %sPayload struct{ Value %sBlock };func(p %sPayload)MarshalJSON()([]byte,error){if p.Value==nil{return nil,newContractError(ContractError{Operation:\"encode\",Container:%q,Reason:\"nil embedded payload is not allowed\"})};data,err:=json.Marshal(p.Value);if err!=nil{return nil,err};if bytes.Equal(bytes.TrimSpace(data),[]byte(\"null\")){return nil,newContractError(ContractError{Operation:\"encode\",Container:%q,Reason:\"nil embedded payload is not allowed\"})};return data,nil};func(p *%sPayload)UnmarshalJSON(data []byte)error{if bytes.Equal(bytes.TrimSpace(data),[]byte(\"null\")){return newContractError(ContractError{Container:%q,Reason:\"embedded payload must be an object\"})};var rows %s;if err:=json.Unmarshal(append(append([]byte{'['},data...),']'),&rows);err!=nil{return err};p.Value=rows[0];return nil}\n", name, name, name, name, name+"Payload", name+"Payload", name, name+"Payload", name)
			}

			writeGoBlockListDoc(out, name, mode)
			fmt.Fprintf(out, "type %s []%sBlock\n", name, name)
			fmt.Fprintf(out, "func(rows %s) MarshalJSON()([]byte,error){if rows==nil{return []byte(\"null\"),nil};encoded:=make([]json.RawMessage,len(rows));for index,row:=range rows{data,err:=json.Marshal(row);if err!=nil{return nil,blockRowError(ContractError{Operation:\"encode\",Container:%q,Reason:\"cannot encode variant\",Err:err},index)};if bytes.Equal(bytes.TrimSpace(data),[]byte(\"null\")){return nil,blockRowError(ContractError{Operation:\"encode\",Container:%q,Reason:\"nil block row is not allowed\"},index)};encoded[index]=data};return json.Marshal(encoded)}\n", name, name, name)

			fmt.Fprintf(out, "func (rows *%s) UnmarshalJSON(data []byte) error { var raw []json.RawMessage; if err:=json.Unmarshal(data,&raw);err!=nil{return newContractError(ContractError{Container:%q,Reason:\"malformed block list\",Err:err})}; if raw==nil{*rows=nil;return nil}; decoded:=make(%s,len(raw)); for index,data:=range raw {header,err:=decodeBlockHeaderFields(data,%q,%q); if err!=nil{return blockRowError(ContractError{Container:%q,Reason:\"malformed discriminator\",Err:err},index)}; switch header.Type {", name, name, name, field.Discriminator, field.Identity, name)
			for _, variant := range field.Variants {
				var slug string
				for _, v := range g.catalog.Variants {
					if v.Name == variant {
						slug = v.Block.Slug
					}
				}
				if mode == goUpdate {
					fmt.Fprintf(out, "case %q:if header.Key==\"\"{var value %sInput;if err:=json.Unmarshal(data,&value);err!=nil{return blockRowError(ContractError{Container:%q,Discriminator:header.Type,Reason:\"malformed new variant\",Err:err},index)};decoded[index]=&value}else{var value %sUpdate;if err:=json.Unmarshal(data,&value);err!=nil{return blockRowError(ContractError{Container:%q,Discriminator:header.Type,Reason:\"malformed keyed update\",Err:err},index)};decoded[index]=&value};", slug, variant, name, variant, name)
				} else {
					fmt.Fprintf(out, "case %q: var value %s; if err:=json.Unmarshal(data,&value);err!=nil{return blockRowError(ContractError{Container:%q,Discriminator:header.Type,Reason:\"malformed variant\",Err:err},index)};decoded[index]=&value;", slug, variant+suffix, name)
				}
			}
			fmt.Fprintf(out, "default: reason:=\"unknown discriminator\";if header.Type==\"\"{reason=\"missing discriminator\"};return blockRowError(ContractError{Container:%q,Discriminator:header.Type,Path:%q,Reason:reason},index) } }; *rows=decoded;return nil }\n", name, field.Discriminator)
			for _, variant := range field.Variants {
				fmt.Fprintf(out, "func (*%s) %s() {}\n", variant+suffix, marker)
				if mode == goUpdate {
					fmt.Fprintf(out, "func(*%sInput)%s(){}\n", variant, marker)
				}
			}
			if goOutputMode(mode) {
				g.writeRetain(out, field, mode)
			}
		}
	}
	for _, variant := range g.catalog.Variants {
		fmt.Fprintf(out, "// %sBlockType is the immutable stored discriminator.\nconst %sBlockType = %q\n", variant.Name, variant.Name, variant.Block.Slug)
		for _, mode := range g.modes() {
			name := variant.Name + goBlockSuffix(mode)
			writeGoBlockVariantDoc(out, name, mode)
			fmt.Fprintf(out, "type %s struct {\n", name)
			if mode == goCreate {
				out.WriteString("// Key identifies this occurrence; omit it for a new server-assigned identity.\n")
			} else {
				out.WriteString("// Key is the required identity of this existing occurrence.\n")
			}
			fmt.Fprintf(out, "Key string `json:\"%s", variant.Identity)
			if mode == goCreate {
				out.WriteString(",omitempty")
			}
			out.WriteString("\"`\n")
			names := goModelFieldNames(schema.Collection{Fields: variant.Block.ResolvedFields()}, "Key", "BlockType", "BlockKey", "MarshalJSON", "UnmarshalJSON")
			for i, f := range variant.Block.ResolvedFields() {
				if !goFieldAppearsInMode(f, mode) {
					continue
				}
				typ := g.fieldType(f, mode, plugins, true)
				optional := mode != goCreate || !generatedInputRequired(f) || generatedFieldHasDefault(f)
				typ = g.presence(f, typ, mode, optional, true)
				tag := f.Name
				if optional {
					tag += goOmissionTag(f, mode)
				}
				writeGoFieldDoc(out, names[i], f, typ, mode)
				fmt.Fprintf(out, "%s %s `json:%q`\n", names[i], typ, tag)
			}
			out.WriteString("}\n")
			fmt.Fprintf(out, "// BlockType returns the immutable stored discriminator.\nfunc (*%s) BlockType() string {return %q}\n", name, variant.Block.Slug)
			g.writeMarshalObject(out, name, variant.Block.ResolvedFields(), names, mode, plugins, variant.Block.Slug, true, variant.Discriminator, variant.Identity)
			fmt.Fprintf(out, "// BlockKey returns the occurrence identity, or an empty string for a nil or new block.\nfunc (value *%s) BlockKey() string {if value==nil{return \"\"};return value.Key}\n", name)
			fmt.Fprintf(out, "func(value *%s) UnmarshalJSON(data []byte) error {type payload %s;var decoded payload;var fields map[string]json.RawMessage;if err:=json.Unmarshal(data,&fields);err!=nil{return err};var discriminator string;if err:=json.Unmarshal(fields[%q],&discriminator);err!=nil{return blockFieldError(%q,\"invalid discriminator\",err)};if discriminator!=%q{return newContractError(ContractError{Container:%q,Discriminator:discriminator,Path:%q,Reason:\"incorrect discriminator\"})};", name, name, variant.Discriminator, variant.Discriminator, variant.Block.Slug, name, variant.Discriminator)
			if mode != goCreate {
				fmt.Fprintf(out, "var key string;if err:=json.Unmarshal(fields[%q],&key);err!=nil{return blockFieldError(%q,\"invalid identity\",err)};if strings.TrimSpace(key)==\"\"{return blockFieldError(%q,\"expected a nonempty block identity\",nil)};", variant.Identity, variant.Identity, variant.Identity)
			}

			if mode == goCreate {
				fmt.Fprintf(out, "if raw,ok:=fields[%q];ok{var key string;if err:=json.Unmarshal(raw,&key);err!=nil{return blockFieldError(%q,\"invalid identity\",err)};if strings.TrimSpace(key)==\"\"{return blockFieldError(%q,\"expected a nonempty block identity\",nil)}};", variant.Identity, variant.Identity, variant.Identity)
				for _, f := range variant.Block.ResolvedFields() {
					if generatedInputRequired(f) && !generatedFieldHasDefault(f) && goFieldAppearsInModel(f) {
						fmt.Fprintf(out, "if raw,ok:=fields[%q];!ok||bytes.Equal(bytes.TrimSpace(raw),[]byte(\"null\")){return blockFieldError(%q,\"required field is absent or null\",nil)};", f.Name, f.Name)
					}
				}
			}
			g.writeDecodeFields(out, variant.Block.ResolvedFields(), names, mode, plugins, true, true, variant.Identity)
			if goOutputMode(mode) {
				g.writeOutputNullChecks(out, variant.Block.ResolvedFields(), mode)
				g.writeOutputNulls(out, variant.Block.ResolvedFields(), names, mode, plugins)
			}
			if mode == goCreate || mode == goUpdate {
				g.writeInputNulls(out, variant.Block.ResolvedFields(), names, plugins, mode)
				g.writeInputUnknowns(out, variant.Block.ResolvedFields(), true, true, variant.Discriminator, variant.Identity)
			}
			out.WriteString("*value=" + name + "(decoded);return nil }\n")
		}
	}
	nestedNames := make([]string, 0, len(g.nested))
	for name := range g.nested {
		nestedNames = append(nestedNames, name)
	}
	sort.Strings(nestedNames)
	for _, name := range nestedNames {
		fmt.Fprintf(out, "type %s %s\n", name, g.nested[name])
		if g.arrayRows[name] {
			fmt.Fprintf(out, "// RowKey returns the occurrence identity, or an empty string for a nil or unkeyed row.\nfunc (value *%s) RowKey() string {if value==nil{return \"\"};return value.Key}\n", name)
		}
		g.writeMarshalObject(out, name, g.nestedFields[name], goNestedFieldNames(g.nestedFields[name], g.arrayRows[name]), g.nestedModes[name], plugins, "", g.arrayRows[name])
		if fields, ok := g.nestedFields[name]; ok {
			fmt.Fprintf(out, "func(value *%s) UnmarshalJSON(data []byte) error {type payload %s;var decoded payload;var fields map[string]json.RawMessage;if err:=json.Unmarshal(data,&fields);err!=nil{return err};if fields==nil{return fmt.Errorf(\"expected an object\")};", name, name)
			if g.nestedModes[name] == goUpdate && g.arrayRows[name] {
				out.WriteString("var key string;if err:=json.Unmarshal(fields[\"_key\"],&key);err!=nil{return blockFieldError(\"_key\",\"invalid identity\",err)};if strings.TrimSpace(key)==\"\"{return blockFieldError(\"_key\",\"keyed array update requires an identity\",nil)};")
			}
			names := goNestedFieldNames(fields, g.arrayRows[name])
			g.writeDecodeFields(out, fields, names, g.nestedModes[name], plugins, g.arrayRows[name], true)
			if mode := g.nestedModes[name]; mode == goCreate || mode == goUpdate {
				g.writeInputNulls(out, fields, names, plugins, mode)
				g.writeInputUnknowns(out, fields, false, g.arrayRows[name])
			} else {
				g.writeOutputNullChecks(out, fields, mode)
				g.writeOutputNulls(out, fields, names, mode, plugins)
			}
			fmt.Fprintf(out, "*value=%s(decoded);return nil}\n", name)
		}
	}

	g.writeReferences(out)
	g.writeArrayUpdates(out)
	g.writeArrayReads(out)

}

func (g *goBlocks) writeInputUnknowns(out *strings.Builder, fields []schema.Field, block, hasKey bool, wire ...string) {
	discriminator, key := "blockType", "_key"
	if len(wire) == 2 {
		discriminator, key = wire[0], wire[1]
	}
	allowed := []string{}
	if hasKey {
		allowed = append(allowed, key)
	}
	if block {
		allowed = append(allowed, discriminator)
	}
	for _, f := range fields {
		if f.Category != schema.FieldCategoryPresentation {
			allowed = append(allowed, f.Name)
		}
	}
	out.WriteString("for name:=range fields{switch name{")
	if len(allowed) > 0 {
		out.WriteString("case ")
		for i, name := range allowed {
			if i > 0 {
				out.WriteString(",")
			}
			fmt.Fprintf(out, "%q", name)
		}
		out.WriteString(":")
	}
	out.WriteString("default:return blockFieldError(name,\"unknown input field\",nil)}};")
}

func (g *goBlocks) writeInputNulls(out *strings.Builder, fields []schema.Field, names []string, plugins map[string]string, mode goModelMode) {
	for i, f := range fields {
		if !goFieldAppearsInMode(f, mode) {
			continue
		}
		if !generatedInputRequired(f) {
			typ := g.fieldType(f, mode, plugins, true)
			fmt.Fprintf(out, "if raw,ok:=fields[%q];ok&&bytes.Equal(bytes.TrimSpace(raw),[]byte(\"null\")){decoded.%s=core.Null[%s]()};", f.Name, names[i], typ)
		} else {
			condition := "!ok||"
			if mode == goUpdate || generatedFieldHasDefault(f) {
				condition = "ok&&"
			}
			fmt.Fprintf(out, "if raw,ok:=fields[%q];%sbytes.Equal(bytes.TrimSpace(raw),[]byte(\"null\")){return blockFieldError(%q,\"required field is absent or null\",nil)};", f.Name, condition, f.Name)
		}
	}
}

func (g *goBlocks) writeOutputNulls(out *strings.Builder, fields []schema.Field, names []string, mode goModelMode, plugins map[string]string) {
	for i, f := range fields {
		if f.Required || !goFieldAppearsInMode(f, mode) {
			continue
		}
		typ := g.fieldType(f, mode, plugins, true)
		fmt.Fprintf(out, "if raw,ok:=fields[%q];ok&&bytes.Equal(bytes.TrimSpace(raw),[]byte(\"null\")){decoded.%s=&BlockOptional[%s]{}};", f.Name, names[i], typ)
	}
}

func (g *goBlocks) writeOutputNullChecks(out *strings.Builder, fields []schema.Field, mode goModelMode) {
	for _, f := range fields {
		if !f.Required {
			continue
		}
		fmt.Fprintf(out, "if raw,ok:=fields[%q];ok{if bytes.Equal(bytes.TrimSpace(raw),[]byte(\"null\")){return blockFieldError(%q,\"null is not allowed\",nil)};", f.Name, f.Name)
		if f.Localized && mode == goAllLocales {
			fmt.Fprintf(out, "var locales map[string]json.RawMessage;if err:=json.Unmarshal(raw,&locales);err!=nil{return err};for locale,value:=range locales{if bytes.Equal(bytes.TrimSpace(value),[]byte(\"null\")){return blockFieldError(joinBlockPath(%q,locale),\"null is not allowed\",nil)}};", f.Name)
		}
		out.WriteString("};")
	}
}

type goBlockReference struct {
	base  string
	field schema.Field
	mode  goModelMode
}

func (g *goBlocks) writeReferences(out *strings.Builder) {
	names := make([]string, 0, len(g.references))
	for name := range g.references {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		ref := g.references[name]
		marker := "is" + name + "Target"
		fmt.Fprintf(out, "type %sTarget interface{%s()}\ntype %s struct{Value %sTarget}\nfunc(value %s) MarshalJSON()([]byte,error){if value.Value==nil{return nil,fmt.Errorf(\"%s: missing reference value\")};return json.Marshal(value.Value)}\n", name, marker, name, name, name, name)
		fmt.Fprintf(out, "func(value *%s) UnmarshalJSON(data []byte)error{var header struct{RelationTo string `json:\"relationTo\"`};if err:=json.Unmarshal(data,&header);err!=nil{return err};switch header.RelationTo{", name)
		for _, target := range ref.field.Relationship.Targets {
			member := ref.base + exportedGoIdentifier(string(target.CollectionSlug)) + goBlockSuffix(ref.mode)
			fmt.Fprintf(out, "case %q:var decoded %s;if err:=json.Unmarshal(data,&decoded);err!=nil{return err};value.Value=decoded;return nil;", target.CollectionSlug, member)
		}
		fmt.Fprintf(out, "default:return newContractError(ContractError{Container:%q,Discriminator:header.RelationTo,Path:\"relationTo\",Reason:\"unknown or missing relationship target\"})}}\n", name)
		for _, target := range ref.field.Relationship.Targets {
			member := ref.base + exportedGoIdentifier(string(target.CollectionSlug)) + goBlockSuffix(ref.mode)
			typ := "string"
			if goOutputMode(ref.mode) {
				targetName := g.models[target.CollectionID]
				if ref.mode == goAllLocales || ref.mode == goAllLocalesValue {
					targetName += "AllLocales"
				}
				typ = "Reference[" + targetName + "]"
			}
			fmt.Fprintf(out, "type %s struct{ID %s `json:\"id\"`}\nfunc(%s)%s(){}\nfunc(value %s) MarshalJSON()([]byte,error){return json.Marshal(struct{RelationTo string `json:\"relationTo\"`;ID %s `json:\"id\"`}{RelationTo:%q,ID:value.ID})}\n", member, typ, member, marker, member, typ, target.CollectionSlug)
			fmt.Fprintf(out, "func(value *%s) UnmarshalJSON(data []byte)error{type payload %s;var fields struct{RelationTo string `json:\"relationTo\"`;ID json.RawMessage `json:\"id\"`};if err:=json.Unmarshal(data,&fields);err!=nil{return err};if fields.RelationTo!=%q{return fmt.Errorf(\"incorrect relationship target\")};if len(fields.ID)==0||bytes.Equal(bytes.TrimSpace(fields.ID),[]byte(\"null\")){return fmt.Errorf(\"relationship id is required\")};var decoded payload;if err:=json.Unmarshal(fields.ID,&decoded.ID);err!=nil{return blockFieldError(\"id\",\"invalid relationship value\",err)};*value=%s(decoded);return nil}\n", member, member, target.CollectionSlug, member)

		}
	}
}

const goBlockRuntime = `
// BlockOptional preserves omitted, explicit null, and concrete nullable block children.
// A nil outer pointer is absent; a nil Value is explicit JSON null.
type BlockOptional[T any] struct {Value *T}
// Get returns the concrete value, or the zero value and false for omission or null.
// Inspect the outer pointer and Value only when omission and null must be distinguished.
func(value *BlockOptional[T]) Get()(T,bool){if value==nil||value.Value==nil{return *new(T),false};return *value.Value,true}
func(value BlockOptional[T]) MarshalJSON()([]byte,error){return json.Marshal(value.Value)}
func(value *BlockOptional[T]) UnmarshalJSON(data []byte)error{var decoded *T;if err:=json.Unmarshal(data,&decoded);err!=nil{return err};value.Value=decoded;return nil}

// ContractError identifies an encode, decode or retain failure without including the document or field payload.
// Path is relative to Container and includes nested field names and array indexes.
// Use errors.As to inspect it; Unwrap preserves the underlying JSON or plugin error.
type ContractError struct {
 Operation string
 Container string
 Path string
 Discriminator string
 Reason string
 Err error
}
func (err *ContractError) Error() string {
 location:=joinBlockPath(err.Container,err.Path)
 message:=err.Operation+" "+location+": "+err.Reason
 var nested *ContractError
 cause:=err.Err;for errors.As(cause,&nested){cause=nested.Err}
 if cause!=nil {message+=": "+cause.Error()}
 return message
}
func (err *ContractError) Unwrap() error {return err.Err}
func newContractError(detail ContractError) *ContractError {
 if detail.Operation=="" {detail.Operation="decode"}
 var nested *ContractError
 if errors.As(detail.Err,&nested) {
  detail.Path=joinBlockPath(detail.Path,nested.Path)
  detail.Reason=nested.Reason
  if detail.Discriminator=="" {detail.Discriminator=nested.Discriminator}
 }
 return &detail
}
func blockRowError(detail ContractError,index int) *ContractError {
 detail.Path=joinBlockPath(fmt.Sprintf("[%d]",index),detail.Path)
 return newContractError(detail)
}
func joinBlockPath(parent,child string) string {
 if parent=="" {return child};if child=="" {return parent}
 if strings.HasPrefix(child,"["){return parent+child};return parent+"."+child
}
func blockFieldError(path,reason string,err error) error {
 return newContractError(ContractError{Path:path,Reason:reason,Err:err})
}
type blockHeader struct {Type,Key string}
func decodeBlockHeader(data []byte,withDiscriminator bool)(blockHeader,error) {
 discriminator:="";if withDiscriminator {discriminator="blockType"};return decodeBlockHeaderFields(data,discriminator,"_key")
}
func decodeBlockHeaderFields(data []byte,discriminator,key string)(blockHeader,error) {
 var header blockHeader;var fields map[string]json.RawMessage
 if err:=json.Unmarshal(data,&fields);err!=nil{return header,err}
 if fields==nil{return header,fmt.Errorf("expected an object")}
 if discriminator!="" {if raw,ok:=fields[discriminator];ok{if err:=json.Unmarshal(raw,&header.Type);err!=nil{return header,blockFieldError(discriminator,"invalid discriminator",err)}}}
 if raw,ok:=fields[key];ok{if err:=json.Unmarshal(raw,&header.Key);err!=nil{return header,blockFieldError(key,"invalid identity",err)};if bytes.Equal(bytes.TrimSpace(raw),[]byte("null")){return header,blockFieldError(key,"null is not allowed",nil)}}
 return header,nil
}
func decodeBlockValue[T any](data []byte)(T,error){var value T;err:=json.Unmarshal(data,&value);return value,err}
func decodeBlockSlice[T any](data []byte,decode func([]byte)(T,error))([]T,error) {
 var raw []json.RawMessage;if err:=json.Unmarshal(data,&raw);err!=nil{return nil,err}
 if raw==nil{return nil,nil};values:=make([]T,len(raw))
 for index,row:=range raw {value,err:=decode(row);if err!=nil{return nil,blockFieldError(fmt.Sprintf("[%d]",index),"invalid array item",err)};values[index]=value}
 return values,nil
}
func decodeBlockLocales[T any](data []byte,decode func([]byte)(T,error))(map[string]T,error) {
 var raw map[string]json.RawMessage;if err:=json.Unmarshal(data,&raw);err!=nil{return nil,err}
 if raw==nil{return nil,nil};values:=make(map[string]T,len(raw))
 locales:=make([]string,0,len(raw));for locale:=range raw {locales=append(locales,locale)};sort.Strings(locales)
 for _,locale:=range locales {value,err:=decode(raw[locale]);if err!=nil{return nil,blockFieldError(locale,"invalid localized value",err)};values[locale]=value}
 return values,nil
}
func decodeBlockPointer[T any](data []byte,decode func([]byte)(T,error))(*T,error) {
 if bytes.Equal(bytes.TrimSpace(data),[]byte("null")){return nil,nil}
 value,err:=decode(data);if err!=nil{return nil,err};return &value,nil
}
func encodeBlockValue[T any](value T)([]byte,error){return json.Marshal(value)}
func encodeBlockSlice[T any](values []T,encode func(T)([]byte,error))([]byte,error) {
 if values==nil{return []byte("null"),nil};raw:=make([]json.RawMessage,len(values))
 for index,value:=range values {data,err:=encode(value);if err!=nil{return nil,blockRowError(ContractError{Operation:"encode",Reason:"invalid array item",Err:err},index)};raw[index]=data}
 return json.Marshal(raw)
}
func encodeBlockLocales[T any](values map[string]T,encode func(T)([]byte,error))([]byte,error) {
 if values==nil{return []byte("null"),nil};raw:=make(map[string]json.RawMessage,len(values))
 locales:=make([]string,0,len(values));for locale:=range values{locales=append(locales,locale)};sort.Strings(locales)
 for _,locale:=range locales {data,err:=encode(values[locale]);if err!=nil{return nil,newContractError(ContractError{Operation:"encode",Path:locale,Reason:"invalid localized value",Err:err})};raw[locale]=data}
 return json.Marshal(raw)
}
func encodeBlockPointer[T any](value *T,encode func(T)([]byte,error))([]byte,error) {
 if value==nil{return []byte("null"),nil}
 if _,ok:=any(value).(json.Marshaler);ok{return json.Marshal(value)}
 if _,ok:=any(value).(encoding.TextMarshaler);ok{return json.Marshal(value)}
 return encode(*value)
}
// Reference preserves canonical IDs and populated target documents.
type Reference[T any] struct { ID string; Document *T }
func (value Reference[T]) MarshalJSON() ([]byte,error) { if value.Document!=nil{return json.Marshal(value.Document)};return json.Marshal(value.ID) }
func (value *Reference[T]) UnmarshalJSON(data []byte) error { if bytes.Equal(bytes.TrimSpace(data),[]byte("null")){return fmt.Errorf("reference: null is not a value")}; var id string;if err:=json.Unmarshal(data,&id);err==nil{if id==""{return fmt.Errorf("reference: empty id")};*value=Reference[T]{ID:id};return nil};var document T;if err:=json.Unmarshal(data,&document);err!=nil{return err};var header struct{ID string ` + "`json:\"id\"`" + `};if err:=json.Unmarshal(data,&header);err!=nil{return err};if header.ID==""{return fmt.Errorf("populated reference: missing id")};*value=Reference[T]{ID:header.ID,Document:&document};return nil }
`

func (g *goBlocks) writeArrayUpdates(out *strings.Builder) {
	names := make([]string, 0, len(g.arrayUpdates))
	for name := range g.arrayUpdates {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		row := g.arrayUpdates[name]
		marker := "is" + name + "Row"
		fmt.Fprintf(out, "// %sRow admits new input and retained update pointers. RowKey exposes identity without a type assertion.\ntype %sRow interface{%s();RowKey() string}\ntype %s []%sRow\nfunc(*%sInput)%s(){}\nfunc(*%sUpdate)%s(){}\n", name, name, marker, name, name, row, marker, row, marker)
		fmt.Fprintf(out, "func(rows %s) MarshalJSON()([]byte,error){if rows==nil{return []byte(\"null\"),nil};encoded:=make([]json.RawMessage,len(rows));for i,row:=range rows{data,err:=json.Marshal(row);if err!=nil{return nil,blockRowError(ContractError{Operation:\"encode\",Reason:\"invalid array item\",Err:err},i)};if bytes.Equal(bytes.TrimSpace(data),[]byte(\"null\")){return nil,blockRowError(ContractError{Operation:\"encode\",Container:%q,Reason:\"nil array row is not allowed\"},i)};encoded[i]=data};return json.Marshal(encoded)}\n", name, name)
		fmt.Fprintf(out, "func(rows *%s) UnmarshalJSON(data []byte)error{var raw []json.RawMessage;if err:=json.Unmarshal(data,&raw);err!=nil{return err};if raw==nil{*rows=nil;return nil};decoded:=make(%s,len(raw));for i,data:=range raw{header,err:=decodeBlockHeader(data,false);if err!=nil{return blockRowError(ContractError{Reason:\"invalid array header\",Err:err},i)};if header.Key==\"\"{var value %sInput;if err:=json.Unmarshal(data,&value);err!=nil{return blockRowError(ContractError{Container:%q,Reason:\"invalid array item\",Err:err},i)};decoded[i]=&value}else{var value %sUpdate;if err:=json.Unmarshal(data,&value);err!=nil{return blockRowError(ContractError{Container:%q,Reason:\"invalid array item\",Err:err},i)};decoded[i]=&value}};*rows=decoded;return nil}\n", name, name, row, name, row, name)
	}
}
