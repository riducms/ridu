package core

import (
	"context"
	"encoding"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	operationengine "github.com/riducms/ridu/internal/operation"
	"github.com/riducms/ridu/schema"
	"github.com/riducms/ridu/store"
)

// AdminLoadContext supplies cancellable, identity-bound reads to an admin
// loader. List/Find/Global always use the current actor and content locale;
// their options cannot change identity or locale selection. There is deliberately no write API.
// Custom external reads must enforce the application's tenant policy too.
type AdminLoadContext struct {
	Context context.Context
	// Pathname is the normalized router-root path, without the /admin basename.
	Pathname string
	// RouteParams contains decoded core collection, document or global selectors.
	// These are request inputs, not authorization. Custom routes are literal paths.
	RouteParams     map[string]string
	locale          schema.LocaleCode
	actorCollection schema.CollectionSlug
	actor           *store.Document
	local           *LocalAPI
	auditRead       func(string, string)
}

// Actor returns a detached authenticated identity for application policy checks.
func (ctx AdminLoadContext) Actor() *store.Document {
	if ctx.actor == nil {
		return nil
	}
	actor := store.CloneDocument(*ctx.actor)
	return &actor
}

func (ctx AdminLoadContext) Locale() schema.LocaleCode              { return ctx.locale }
func (ctx AdminLoadContext) ActorCollection() schema.CollectionSlug { return ctx.actorCollection }

func (ctx AdminLoadContext) List(collection string, options ListOptions) (store.Page, error) {
	options.Actor, options.ActorCollection, options.Locale = ctx.actor, ctx.actorCollection, ctx.locale
	options.FallbackLocales, options.DisableFallback, options.AllLocales = nil, false, false
	return ctx.local.List(ctx.Context, collection, options)
}

func (ctx AdminLoadContext) Find(collection, id string, options FindOptions) (store.Document, error) {
	options.Actor, options.ActorCollection, options.Locale = ctx.actor, ctx.actorCollection, ctx.locale
	options.FallbackLocales, options.DisableFallback, options.AllLocales = nil, false, false
	document, err := ctx.local.Find(ctx.Context, collection, id, options)
	if err == nil && ctx.auditRead != nil {
		ctx.auditRead(collection, id)
	}
	return document, err
}

func (ctx AdminLoadContext) Global(slug string, options FindOptions) (store.Document, error) {
	options.Actor, options.ActorCollection, options.Locale = ctx.actor, ctx.actorCollection, ctx.locale
	options.FallbackLocales, options.DisableFallback, options.AllLocales = nil, false, false
	return ctx.local.Global(ctx.Context, slug, options)
}

// AdminLoaderDefinition is a compiled loader registered in AdminConfig.Loaders.
// Construct it with NewAdminLoader; it cannot be supplied over the wire.
type AdminLoaderDefinition struct {
	key    string
	input  reflect.Type
	output reflect.Type
	run    func(AdminLoadContext, url.Values) (json.RawMessage, error)
}

// NewAdminLoader derives browser contracts from Go JSON structs. Input fields
// use their json names as query parameter names. Missing parameters keep Go
// zero values; pointer fields distinguish absence. Outputs support JSON-tagged
// structs, scalar values, pointers and slices (not arbitrary interfaces/maps,
// recursive types or custom JSON codecs). Validation runs during Resolve/New.
// JSON names must be ASCII identifiers; omitempty is the only supported tag
// option. time.Time is an output string, not a query input. Integers must fit
// JavaScript's safe range; use decimal strings for wider identifiers/counters.
func NewAdminLoader[Input, Output any](key string, handler func(AdminLoadContext, Input) (Output, error)) AdminLoaderDefinition {
	definition := AdminLoaderDefinition{key: key, input: reflect.TypeFor[Input](), output: reflect.TypeFor[Output]()}
	if handler == nil {
		return definition
	}
	definition.run = func(ctx AdminLoadContext, values url.Values) (json.RawMessage, error) {
		var input Input
		if err := decodeAdminInput(reflect.ValueOf(&input).Elem(), values); err != nil {
			return nil, &operationengine.Error{Status: 400, Code: "bad_request", Message: err.Error()}
		}
		output, err := handler(ctx, input)
		if err != nil {
			return nil, err
		}
		if err := validateAdminNumbers(reflect.ValueOf(output)); err != nil {
			return nil, err
		}
		return json.Marshal(output)
	}
	return definition
}

func adminLoaderDescriptors(definitions []AdminLoaderDefinition) ([]schema.AdminLoader, error) {
	descriptors := make([]schema.AdminLoader, 0, len(definitions))
	seen := make(map[string]bool)
	for _, definition := range definitions {
		invalidKey := len(definition.key) > 80 || !schema.IsValidCollectionSlug(definition.key) || seen[definition.key]
		if invalidKey || definition.run == nil {
			return nil, fmt.Errorf("admin loader %q requires a unique key and handler", definition.key)
		}
		seen[definition.key] = true

		input, err := describeAdminData(definition.input, make(map[reflect.Type]bool))
		if err != nil {
			return nil, fmt.Errorf("admin loader %s input: %w", definition.key, err)
		}
		if input.Kind != "object" || input.Nullable {
			return nil, fmt.Errorf("admin loader %s input must be a struct", definition.key)
		}
		for name, field := range input.Fields {
			if field.Kind != "string" && field.Kind != "number" && field.Kind != "boolean" {
				return nil, fmt.Errorf("admin loader %s input.%s must be a scalar query parameter", definition.key, name)
			}
		}
		for index := 0; index < definition.input.NumField(); index++ {
			field := definition.input.Field(index)
			if !field.IsExported() || field.Tag.Get("json") == "-" {
				continue
			}
			kind := field.Type
			if kind.Kind() == reflect.Pointer {
				kind = kind.Elem()
			}
			if kind.Kind() == reflect.Pointer || kind == adminTimeType {
				return nil, fmt.Errorf("admin loader %s input.%s must be a scalar or a single scalar pointer", definition.key, field.Name)
			}
		}

		output, err := describeAdminData(definition.output, make(map[reflect.Type]bool))
		if err != nil {
			return nil, fmt.Errorf("admin loader %s output: %w", definition.key, err)
		}
		descriptors = append(descriptors, schema.AdminLoader{Key: definition.key, Input: input, Output: output})
	}
	// The executable definitions retain authoring order, while their public
	// manifest shape is sorted so generated artifacts remain deterministic.
	sort.Slice(descriptors, func(i, j int) bool { return descriptors[i].Key < descriptors[j].Key })
	return descriptors, schema.ValidateAdminLoaders(descriptors)
}

var adminTimeType = reflect.TypeFor[time.Time]()
var adminJSONMarshaler = reflect.TypeFor[json.Marshaler]()
var adminTextMarshaler = reflect.TypeFor[encoding.TextMarshaler]()

func describeAdminData(value reflect.Type, visiting map[reflect.Type]bool) (schema.AdminDataType, error) {
	if value == nil || visiting[value] {
		return schema.AdminDataType{}, fmt.Errorf("recursive or missing JSON type")
	}
	if value == adminTimeType {
		return schema.AdminDataType{Kind: "string"}, nil
	}
	if value == reflect.TypeFor[json.Number]() {
		return schema.AdminDataType{}, fmt.Errorf("json.Number is not supported; use a numeric type or decimal string")
	}
	if value.Kind() == reflect.Pointer {
		visiting[value] = true
		defer delete(visiting, value)
		inner, err := describeAdminData(value.Elem(), visiting)
		inner.Nullable = true
		return inner, err
	}
	customJSON := value.Implements(adminJSONMarshaler) || reflect.PointerTo(value).Implements(adminJSONMarshaler)
	customText := value.Implements(adminTextMarshaler) || reflect.PointerTo(value).Implements(adminTextMarshaler)
	// Generated TypeScript must describe the same wire shape Go emits. A custom
	// codec can change that shape without reflection being able to observe it.
	if customJSON || customText {
		return schema.AdminDataType{}, fmt.Errorf("custom JSON codecs are not supported: %s", value)
	}
	visiting[value] = true
	defer delete(visiting, value)
	result := schema.AdminDataType{}
	switch value.Kind() {
	case reflect.String:
		result.Kind = "string"
	case reflect.Bool:
		result.Kind = "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		result.Kind = "number"
	case reflect.Slice, reflect.Array:
		if value.Elem().Kind() == reflect.Uint8 {
			return result, fmt.Errorf("byte slices/arrays are not supported; return a string")
		}
		element, err := describeAdminData(value.Elem(), visiting)
		if err != nil {
			return result, err
		}
		result.Kind, result.Element, result.Nullable = "array", &element, value.Kind() == reflect.Slice
	case reflect.Struct:
		result.Kind, result.Fields = "object", make(map[string]schema.AdminDataType)
		for index := 0; index < value.NumField(); index++ {
			field := value.Field(index)
			if !field.IsExported() || field.Tag.Get("json") == "-" {
				continue
			}
			name, options, _ := strings.Cut(field.Tag.Get("json"), ",")
			if field.Anonymous || name == "" || (options != "" && options != "omitempty") {
				return result, fmt.Errorf("%s.%s requires an explicit json name and no embedded fields or codec options", value, field.Name)
			}
			if _, duplicate := result.Fields[name]; duplicate {
				return result, fmt.Errorf("duplicate JSON property %s", name)
			}
			descriptor, err := describeAdminData(field.Type, visiting)
			if err != nil {
				return result, fmt.Errorf("%s: %w", name, err)
			}
			descriptor.Optional = options == "omitempty"
			result.Fields[name] = descriptor
		}
	default:
		return result, fmt.Errorf("unsupported JSON type %s", value)
	}
	return result, nil
}

func decodeAdminInput(input reflect.Value, values url.Values) error {
	for index := 0; index < input.NumField(); index++ {
		field := input.Type().Field(index)
		if !field.IsExported() || field.Tag.Get("json") == "-" {
			continue
		}
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		items, present := values[name]
		if !present {
			continue
		}
		if len(items) != 1 {
			return fmt.Errorf("query parameter %s must occur once", name)
		}
		target := input.Field(index)
		if target.Kind() == reflect.Pointer {
			target.Set(reflect.New(target.Type().Elem()))
			target = target.Elem()
		}
		raw := items[0]
		var err error
		switch target.Kind() {
		case reflect.String:
			target.SetString(raw)
		case reflect.Bool:
			var value bool
			value, err = strconv.ParseBool(raw)
			target.SetBool(value)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			var value int64
			value, err = strconv.ParseInt(raw, 10, target.Type().Bits())
			target.SetInt(value)
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			var value uint64
			value, err = strconv.ParseUint(raw, 10, target.Type().Bits())
			target.SetUint(value)
		case reflect.Float32, reflect.Float64:
			var value float64
			value, err = strconv.ParseFloat(raw, target.Type().Bits())
			target.SetFloat(value)
		default:
			return fmt.Errorf("query parameter %s has an unsupported type", name)
		}
		if err != nil {
			return fmt.Errorf("query parameter %s must be a valid %s", name, target.Kind())
		}
		if err := validateAdminNumbers(target); err != nil {
			return fmt.Errorf("query parameter %s: %w", name, err)
		}
	}
	return nil
}

// JSON numbers must survive JavaScript parsing without integer rounding.
func validateAdminNumbers(value reflect.Value) error {
	switch value.Kind() {
	case reflect.Pointer:
		if !value.IsNil() {
			return validateAdminNumbers(value.Elem())
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if n := value.Int(); n < -(1<<53-1) || n > 1<<53-1 {
			return fmt.Errorf("integer is outside the JavaScript safe range; return a decimal string")
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if value.Uint() > 1<<53-1 {
			return fmt.Errorf("integer is outside the JavaScript safe range; return a decimal string")
		}
	case reflect.Float32, reflect.Float64:
		if math.IsNaN(value.Float()) || math.IsInf(value.Float(), 0) {
			return fmt.Errorf("number must be finite")
		}
	case reflect.Struct:
		if value.Type() == adminTimeType {
			return nil
		}
		for index := 0; index < value.NumField(); index++ {
			field := value.Type().Field(index)
			if !field.IsExported() || field.Tag.Get("json") == "-" {
				continue
			}
			if err := validateAdminNumbers(value.Field(index)); err != nil {
				return err
			}
		}
	case reflect.Array, reflect.Slice:
		for index := 0; index < value.Len(); index++ {
			if err := validateAdminNumbers(value.Index(index)); err != nil {
				return err
			}
		}
	}
	return nil
}
