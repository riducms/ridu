package graphql

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	enginegraphql "github.com/graphql-go/graphql"
	"github.com/riducms/ridu/schema"
)

// GenerateSDL returns the deterministic schema definition for a resolved Ridu
// manifest. It does not enable network introspection or require a running app.
func GenerateSDL(manifest schema.Manifest, options ...Options) (string, error) {
	selected := Options{}
	if len(options) != 0 {
		selected = options[len(options)-1]
	}
	builder := newSchemaBuilder(manifest.Snapshot(), nil, nil, normalizedOptions(selected))
	executableSchema, err := builder.build()
	if err != nil {
		return "", err
	}
	return printSchema(executableSchema)
}

func printSchema(executableSchema enginegraphql.Schema) (string, error) {
	types := executableSchema.TypeMap()
	names := make([]string, 0, len(types))
	for name := range types {
		if strings.HasPrefix(name, "__") || isBuiltInScalar(name) {
			continue
		}
		names = append(names, name)
	}
	sort.Slice(names, func(left, right int) bool {
		order := func(name string) int {
			switch name {
			case "Query":
				return 0
			case "Mutation":
				return 1
			default:
				return 2
			}
		}
		if order(names[left]) != order(names[right]) {
			return order(names[left]) < order(names[right])
		}
		return names[left] < names[right]
	})
	var output strings.Builder
	for _, name := range names {
		current := types[name]
		switch typed := current.(type) {
		case *enginegraphql.Object:
			if err := printObject(&output, typed); err != nil {
				return "", err
			}
		case *enginegraphql.Interface:
			if err := printInterface(&output, typed); err != nil {
				return "", err
			}
		case *enginegraphql.InputObject:
			if err := printInput(&output, typed); err != nil {
				return "", err
			}
		case *enginegraphql.Enum:
			printEnum(&output, typed)
		case *enginegraphql.Union:
			printUnion(&output, typed)
		case *enginegraphql.Scalar:
			printDescription(&output, "", typed.Description())
			fmt.Fprintf(&output, "scalar %s\n\n", typed.Name())
		}
	}
	return strings.TrimRight(output.String(), "\n") + "\n", nil
}

func printUnion(output *strings.Builder, union *enginegraphql.Union) {
	printDescription(output, "", union.Description())
	types := union.Types()
	names := make([]string, len(types))
	for index, object := range types {
		names[index] = object.Name()
	}
	sort.Strings(names)
	fmt.Fprintf(output, "union %s = %s\n\n", union.Name(), strings.Join(names, " | "))
}

func printObject(output *strings.Builder, object *enginegraphql.Object) error {
	printDescription(output, "", object.Description())
	fmt.Fprintf(output, "type %s", object.Name())
	interfaces := object.Interfaces()
	if len(interfaces) != 0 {
		names := make([]string, len(interfaces))
		for index, current := range interfaces {
			names[index] = current.Name()
		}
		sort.Strings(names)
		fmt.Fprintf(output, " implements %s", strings.Join(names, " & "))
	}
	output.WriteString(" {\n")
	if err := printOutputFields(output, object.Fields()); err != nil {
		return err
	}
	output.WriteString("}\n\n")
	return nil
}

func printInterface(output *strings.Builder, current *enginegraphql.Interface) error {
	printDescription(output, "", current.Description())
	fmt.Fprintf(output, "interface %s {\n", current.Name())
	if err := printOutputFields(output, current.Fields()); err != nil {
		return err
	}
	output.WriteString("}\n\n")
	return nil
}

func printOutputFields(output *strings.Builder, fields enginegraphql.FieldDefinitionMap) error {
	for _, name := range sortedFieldNames(fields) {
		field := fields[name]
		printDescription(output, "  ", field.Description)
		fmt.Fprintf(output, "  %s", name)
		if len(field.Args) != 0 {
			arguments := append([]*enginegraphql.Argument(nil), field.Args...)
			sort.Slice(arguments, func(left, right int) bool { return arguments[left].Name() < arguments[right].Name() })
			multiline := false
			for _, argument := range arguments {
				multiline = multiline || argument.Description() != ""
			}
			if multiline {
				output.WriteString("(\n")
				for _, argument := range arguments {
					printDescription(output, "    ", argument.Description())
					output.WriteString("    ")
					if err := printArgument(output, argument); err != nil {
						return err
					}
					output.WriteByte('\n')
				}
				output.WriteString("  )")
			} else {
				output.WriteByte('(')
				for index, argument := range arguments {
					if index != 0 {
						output.WriteString(", ")
					}
					if err := printArgument(output, argument); err != nil {
						return err
					}
				}
				output.WriteByte(')')
			}
		}
		fmt.Fprintf(output, ": %s", field.Type.String())
		printDeprecation(output, field.DeprecationReason)
		output.WriteByte('\n')
	}
	return nil
}

func printArgument(output *strings.Builder, argument *enginegraphql.Argument) error {
	fmt.Fprintf(output, "%s: %s", argument.Name(), argument.Type.String())
	if argument.DefaultValue == nil {
		return nil
	}
	encoded, err := graphQLDefaultValue(argument.DefaultValue, argument.Type)
	if err != nil {
		return fmt.Errorf("print default value for argument %s: %w", argument.Name(), err)
	}
	fmt.Fprintf(output, " = %s", encoded)
	return nil
}

func printInput(output *strings.Builder, input *enginegraphql.InputObject) error {
	printDescription(output, "", input.Description())
	fmt.Fprintf(output, "input %s {\n", input.Name())
	fields := input.Fields()
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		field := fields[name]
		printDescription(output, "  ", field.Description())
		fmt.Fprintf(output, "  %s: %s", name, field.Type.String())
		if field.DefaultValue != nil {
			encoded, err := graphQLDefaultValue(field.DefaultValue, field.Type)
			if err != nil {
				return fmt.Errorf("print default value for input field %s.%s: %w", input.Name(), name, err)
			}
			fmt.Fprintf(output, " = %s", encoded)
		}
		output.WriteByte('\n')
	}
	output.WriteString("}\n\n")
	return nil
}

func graphQLDefaultValue(value interface{}, input enginegraphql.Input) (string, error) {
	switch typed := input.(type) {
	case *enginegraphql.NonNull:
		return graphQLDefaultValue(value, typed.OfType.(enginegraphql.Input))
	case *enginegraphql.List:
		reflected := reflect.ValueOf(value)
		if reflected.Kind() != reflect.Array && reflected.Kind() != reflect.Slice {
			return graphQLDefaultValue(value, typed.OfType.(enginegraphql.Input))
		}
		items := make([]string, reflected.Len())
		for index := 0; index < reflected.Len(); index++ {
			encoded, err := graphQLDefaultValue(reflected.Index(index).Interface(), typed.OfType.(enginegraphql.Input))
			if err != nil {
				return "", err
			}
			items[index] = encoded
		}
		return "[" + strings.Join(items, ", ") + "]", nil
	case *enginegraphql.Enum:
		encoded := typed.Serialize(value)
		if encoded == nil {
			return "", fmt.Errorf("value %v is not valid for enum %s", value, typed.Name())
		}
		return fmt.Sprint(encoded), nil
	case *enginegraphql.InputObject:
		reflected := reflect.ValueOf(value)
		if reflected.Kind() != reflect.Map || reflected.Type().Key().Kind() != reflect.String {
			return "", fmt.Errorf("input object default for %s must be a string-keyed map", typed.Name())
		}
		fields := typed.Fields()
		names := reflected.MapKeys()
		sort.Slice(names, func(left, right int) bool { return names[left].String() < names[right].String() })
		items := make([]string, 0, len(names))
		for _, reflectedName := range names {
			name := reflectedName.String()
			field, exists := fields[name]
			if !exists {
				return "", fmt.Errorf("input object %s has no field %s", typed.Name(), name)
			}
			encoded, err := graphQLDefaultValue(reflected.MapIndex(reflectedName).Interface(), field.Type)
			if err != nil {
				return "", err
			}
			items = append(items, name+": "+encoded)
		}
		return "{" + strings.Join(items, ", ") + "}", nil
	case *enginegraphql.Scalar:
		return graphQLUntypedLiteral(typed.Serialize(value))
	default:
		return "", fmt.Errorf("unsupported default-value input type %T", input)
	}
}

func graphQLUntypedLiteral(value interface{}) (string, error) {
	if value == nil {
		return "null", nil
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Interface, reflect.Ptr:
		if reflected.IsNil() {
			return "null", nil
		}
		return graphQLUntypedLiteral(reflected.Elem().Interface())
	case reflect.Array, reflect.Slice:
		items := make([]string, reflected.Len())
		for index := 0; index < reflected.Len(); index++ {
			encoded, err := graphQLUntypedLiteral(reflected.Index(index).Interface())
			if err != nil {
				return "", err
			}
			items[index] = encoded
		}
		return "[" + strings.Join(items, ", ") + "]", nil
	case reflect.Map:
		if reflected.Type().Key().Kind() != reflect.String {
			return "", fmt.Errorf("default-value object keys must be strings")
		}
		names := reflected.MapKeys()
		sort.Slice(names, func(left, right int) bool { return names[left].String() < names[right].String() })
		items := make([]string, len(names))
		for index, name := range names {
			if !isGraphQLName(name.String()) {
				return "", fmt.Errorf("default-value object key %q is not a GraphQL name", name.String())
			}
			encoded, err := graphQLUntypedLiteral(reflected.MapIndex(name).Interface())
			if err != nil {
				return "", err
			}
			items[index] = name.String() + ": " + encoded
		}
		return "{" + strings.Join(items, ", ") + "}", nil
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		encoded, err := json.Marshal(value)
		if err != nil {
			return "", fmt.Errorf("serialize scalar default: %w", err)
		}
		return string(encoded), nil
	default:
		return "", fmt.Errorf("default value of type %T cannot be represented as a GraphQL literal", value)
	}
}

func printEnum(output *strings.Builder, enum *enginegraphql.Enum) {
	printDescription(output, "", enum.Description())
	fmt.Fprintf(output, "enum %s {\n", enum.Name())
	values := append([]*enginegraphql.EnumValueDefinition(nil), enum.Values()...)
	sort.Slice(values, func(left, right int) bool { return values[left].Name < values[right].Name })
	for _, value := range values {
		printDescription(output, "  ", value.Description)
		fmt.Fprintf(output, "  %s", value.Name)
		printDeprecation(output, value.DeprecationReason)
		output.WriteByte('\n')
	}
	output.WriteString("}\n\n")
}

func printDescription(output *strings.Builder, indent, description string) {
	if description == "" {
		return
	}
	encoded, _ := json.Marshal(description)
	fmt.Fprintf(output, "%s%s\n", indent, encoded)
}

func printDeprecation(output *strings.Builder, reason string) {
	if reason == "" {
		return
	}
	encoded, _ := json.Marshal(reason)
	fmt.Fprintf(output, " @deprecated(reason: %s)", encoded)
}

func isGraphQLName(value string) bool {
	if value == "" || (value[0] != '_' && (value[0] < 'A' || value[0] > 'Z') && (value[0] < 'a' || value[0] > 'z')) {
		return false
	}
	for _, current := range value[1:] {
		if current != '_' && (current < 'A' || current > 'Z') && (current < 'a' || current > 'z') && (current < '0' || current > '9') {
			return false
		}
	}
	return true
}

func sortedFieldNames(fields enginegraphql.FieldDefinitionMap) []string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func isBuiltInScalar(name string) bool {
	switch name {
	case "String", "Boolean", "Int", "Float", "ID":
		return true
	default:
		return false
	}
}
