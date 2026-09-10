package generate

import (
	"fmt"
	"strings"

	"github.com/riducms/ridu/schema"
)

// Decode each authored field separately so custom JSON codecs cannot discard its
// containing field, array index or locale. Presence and validation remain schema-owned.
func (g *goBlocks) writeDecodeFields(out *strings.Builder, fields []schema.Field, names []string, mode goModelMode, plugins map[string]string, hasKey, inBlock bool, wire ...string) {
	key := "_key"
	if len(wire) > 0 {
		key = wire[0]
	}
	if hasKey {
		fmt.Fprintf(out, `if raw,ok:=fields[%q];ok{if err:=json.Unmarshal(raw,&decoded.Key);err!=nil{return blockFieldError(%q,"invalid identity",err)}};`, key, key)
	}
	for i, field := range fields {
		if !goFieldAppearsInMode(field, mode) {
			continue
		}
		typ := g.fieldType(field, mode, plugins, inBlock)
		optional := mode != goCreate || !generatedInputRequired(field) || generatedFieldHasDefault(field)
		presence := g.presence(field, typ, mode, optional, inBlock)
		fmt.Fprintf(out, "if raw,ok:=fields[%q];ok{if bytes.Equal(bytes.TrimSpace(raw),[]byte(\"null\")){", field.Name)
		switch {
		case strings.HasPrefix(presence, "*core.Input["):
			fmt.Fprintf(out, "decoded.%s=core.Null[%s]();", names[i], typ)
		case !goOutputMode(mode) && generatedInputRequired(field):
			fmt.Fprintf(out, "return blockFieldError(%q,\"null is not allowed\",nil);", field.Name)
		default:
			fmt.Fprintf(out, "if err:=json.Unmarshal(raw,&decoded.%s);err!=nil{return blockFieldError(%q,\"invalid value\",err)};", names[i], field.Name)
		}
		fmt.Fprintf(out, "}else{child,err:=%s(raw);if err!=nil{return blockFieldError(%q,\"invalid value\",err)};", g.fieldDecoder(field, typ, mode, plugins, inBlock), field.Name)
		value := "child"
		switch {
		case strings.HasPrefix(presence, "*BlockOptional["):
			value = "&BlockOptional[" + typ + "]{Value:&child}"
		case strings.HasPrefix(presence, "*core.Input["):
			value = "core.Set(child)"
		case strings.HasPrefix(presence, "*core.NonNullInput["):
			value = "core.SetNonNull(child)"
		case strings.HasPrefix(presence, "core.NonNullInput["):
			value = "core.NonNull(child)"
		case presence == "*"+typ:
			value = "&child"
		}
		fmt.Fprintf(out, "decoded.%s=%s}};", names[i], value)
	}
}

func goBlockDecoder(typ string) string {
	return goBlockDecoderWith(typ, "")
}

func goBlockDecoderWith(typ, leaf string) string {
	for _, kind := range []struct{ prefix, function string }{
		{"[]", "decodeBlockSlice"}, {"map[string]", "decodeBlockLocales"}, {"*", "decodeBlockPointer"},
	} {
		if strings.HasPrefix(typ, kind.prefix) {
			child := strings.TrimPrefix(typ, kind.prefix)
			return fmt.Sprintf("(func(data []byte)(%s,error){return %s(data,%s)})", typ, kind.function, goBlockDecoderWith(child, leaf))
		}
	}
	if leaf != "" {
		return leaf
	}
	return "decodeBlockValue[" + typ + "]"
}

// Root groups remain anonymous Go structs. Decode their authored children with
// the same field boundaries as named block groups, without adding schema checks.
func (g *goBlocks) fieldDecoder(field schema.Field, typ string, mode goModelMode, plugins map[string]string, inBlock bool) string {
	if field.Type == schema.FieldTypeTextList || field.Type == schema.FieldTypeNumberList {
		element := "string"
		if field.Type == schema.FieldTypeNumberList {
			element = "float64"
		}
		leaf := fmt.Sprintf("(func(data []byte)(%s,error){var value %s;if bytes.Equal(bytes.TrimSpace(data),[]byte(\"null\")){return value,fmt.Errorf(\"null list items are not allowed\")};err:=json.Unmarshal(data,&value);return value,err})", element, element)
		return goBlockDecoderWith(typ, leaf)
	}
	// Blocks may also carry Nested row-label metadata. Their named list codec
	// owns decoding; presentation metadata must never select an object decoder.
	if inBlock || field.Nested == nil || field.Type != schema.FieldTypeGroup && field.Type != schema.FieldTypeArray {
		return goBlockDecoder(typ)
	}
	base := typ
	for {
		previous := base
		for _, prefix := range []string{"map[string]", "[]", "*"} {
			base = strings.TrimPrefix(base, prefix)
		}
		if base == previous {
			break
		}
	}
	if mode == goAllLocales && field.Localized {
		mode = goAllLocalesValue
	}
	var decode strings.Builder
	fmt.Fprintf(&decode, "(func(data []byte)(%s,error){var decoded %s;var fields map[string]json.RawMessage;if err:=json.Unmarshal(data,&fields);err!=nil{return decoded,err};err:=func()error{", base, base)
	names := goModelFieldNames(schema.Collection{Fields: field.Nested.ResolvedFields()}, "Key", "MarshalJSON", "UnmarshalJSON")
	g.writeDecodeFields(&decode, field.Nested.ResolvedFields(), names, mode, plugins, field.Type == schema.FieldTypeArray, false)
	decode.WriteString("return nil}();return decoded,err})")
	return goBlockDecoderWith(typ, decode.String())
}

func (g *goBlocks) writeModelDecoder(out *strings.Builder, name string, collection schema.Collection, mode goModelMode, plugins map[string]string, names []string) {
	fmt.Fprintf(out, "func(value *%s) UnmarshalJSON(data []byte)error{type payload %s;var decoded payload;var fields map[string]json.RawMessage;if err:=json.Unmarshal(data,&fields);err!=nil{return err};if fields==nil{return nil};err:=func()error{", name, name)
	if goOutputMode(mode) {
		metadata := []struct{ name, wire string }{{"ID", "id"}, {"CreatedAt", "createdAt"}, {"UpdatedAt", "updatedAt"}}
		if collection.Capabilities.Trash {
			metadata = append(metadata, struct{ name, wire string }{"DeletedAt", "deletedAt"})
		}
		if collection.Capabilities.Versions {
			metadata = append(metadata, struct{ name, wire string }{"Status", "_status"}, struct{ name, wire string }{"Revision", "_revision"})
		}
		for _, field := range metadata {
			fmt.Fprintf(out, "if raw,ok:=fields[%q];ok{if err:=json.Unmarshal(raw,&decoded.%s);err!=nil{return blockFieldError(%q,\"invalid metadata\",err)}};", field.wire, field.name, field.wire)
		}
	}
	var fields []schema.Field
	var fieldNames []string
	for i, field := range collection.Fields {
		if !goFieldAppearsInMode(field, mode) {
			continue
		}
		if !goOutputMode(mode) && field.Category == schema.FieldCategoryUpload && field.Upload == nil {
			continue
		}
		fields = append(fields, field)
		fieldNames = append(fieldNames, names[i])
	}
	g.writeDecodeFields(out, fields, fieldNames, mode, plugins, false, false)
	fmt.Fprintf(out, "return nil}();if err!=nil{return newContractError(ContractError{Container:%q,Reason:\"invalid document\",Err:err})};*value=%s(decoded);return nil}\n", name, name)
}

// Encode fields once into raw messages, retaining encoding/json's field order
// and omission rules while attaching the authored field to custom codec errors.
func (g *goBlocks) writeMarshalObject(out *strings.Builder, name string, fields []schema.Field, names []string, mode goModelMode, plugins map[string]string, discriminator string, hasKey bool, wire ...string) {
	discriminatorKey, key := "blockType", "_key"
	if len(wire) == 2 {
		discriminatorKey, key = wire[0], wire[1]
	}
	fmt.Fprintf(out, "func(value %s) MarshalJSON()([]byte,error){", name)
	if mode == goUpdate && hasKey {
		fmt.Fprintf(out, "if strings.TrimSpace(value.Key)==\"\"{return nil,newContractError(ContractError{Operation:\"encode\",Container:%q,Path:%q,Reason:\"keyed update requires an identity\"})};", name, key)
	}
	out.WriteString("var encoded struct{")
	if discriminator != "" {
		fmt.Fprintf(out, "BlockType string `json:%q`;", discriminatorKey)
	}
	if hasKey {
		tag := key
		if discriminator == "" || mode == goCreate {
			tag += ",omitempty"
		}
		fmt.Fprintf(out, "Key string `json:%q`;", tag)
	}
	for i, field := range fields {
		if !goFieldAppearsInMode(field, mode) {
			continue
		}
		tag := field.Name
		if mode != goCreate || !generatedInputRequired(field) || generatedFieldHasDefault(field) {
			tag += ",omitempty"
		}
		fmt.Fprintf(out, "%s json.RawMessage `json:%q`;", names[i], tag)
	}
	out.WriteString("};")
	if discriminator != "" {
		fmt.Fprintf(out, "encoded.BlockType=%q;", discriminator)
	}
	if hasKey {
		out.WriteString("encoded.Key=value.Key;")
	}
	for i, field := range fields {
		if !goFieldAppearsInMode(field, mode) {
			continue
		}
		typ := g.fieldType(field, mode, plugins, true)
		optional := mode != goCreate || !generatedInputRequired(field) || generatedFieldHasDefault(field)
		presence := g.presence(field, typ, mode, optional, true)
		if optional {
			if (strings.HasPrefix(presence, "[]") || strings.HasPrefix(presence, "map[")) && field.Type != schema.FieldTypeTextList && field.Type != schema.FieldTypeNumberList {
				fmt.Fprintf(out, "if len(value.%s)>0{", names[i])
			} else {
				fmt.Fprintf(out, "if value.%s!=nil{", names[i])
			}
		} else {
			out.WriteString("{")
		}
		switch {
		case strings.HasPrefix(presence, "*BlockOptional["):
			fmt.Fprintf(out, "data,err:=%s(value.%s.Value);", goBlockEncoder("*"+typ), names[i])
		case strings.HasPrefix(presence, "*core.Input[") || strings.Contains(presence, "core.NonNullInput["):
			fmt.Fprintf(out, "child,present:=value.%s.Get();data:=[]byte(\"null\");var err error;if present{data,err=%s(child)};", names[i], goBlockEncoder(typ))
		default:
			fmt.Fprintf(out, "data,err:=%s(value.%s);", goBlockEncoder(presence), names[i])
		}
		if strings.Contains(presence, "core.NonNullInput[") {
			out.WriteString("if err==nil&&bytes.Equal(bytes.TrimSpace(data),[]byte(\"null\")){err=fmt.Errorf(\"null is not allowed\")};")
		}
		fmt.Fprintf(out, "if err!=nil{return nil,newContractError(ContractError{Operation:\"encode\",Container:%q,Path:%q,Reason:\"invalid value\",Err:err})};encoded.%s=data};", name, field.Name, names[i])
	}
	out.WriteString("return json.Marshal(encoded)}\n")
}

func goBlockEncoder(typ string) string {
	for _, kind := range []struct{ prefix, function string }{
		{"[]", "encodeBlockSlice"}, {"map[string]", "encodeBlockLocales"}, {"*", "encodeBlockPointer"},
	} {
		if strings.HasPrefix(typ, kind.prefix) {
			child := strings.TrimPrefix(typ, kind.prefix)
			return fmt.Sprintf("(func(value %s)([]byte,error){return %s(value,%s)})", typ, kind.function, goBlockEncoder(child))
		}
	}
	return "encodeBlockValue[" + typ + "]"
}
