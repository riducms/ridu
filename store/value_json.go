package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// Values and Value encode and decode JSON in one pass over the data. Their
// output and accepted input match encoding/json for the same Go values: keys are
// sorted, strings use HTML-safe escapes, numbers use its float formatting, and
// decoding replaces invalid UTF-8 and unpaired surrogates with U+FFFD.

// maxJSONDepth matches encoding/json's nesting limit, so adapter reads cannot
// exhaust the stack on hostile stored data.
const maxJSONDepth = 10000

// errEncoding marks a failed fast encode. The caller then asks encoding/json to
// encode the same Go values, so errors match its messages and wrapping exactly.
var errEncoding = errors.New("store value cannot be encoded as JSON")

// MarshalJSON encodes the field map as a JSON object. A nil map encodes as null.
func (values Values) MarshalJSON() ([]byte, error) {
	if values == nil {
		return []byte("null"), nil
	}
	encoded, err := appendObjectJSON(make([]byte, 0, 256), values)
	if err != nil {
		_, err = json.Marshal(map[string]Value(values))
		return nil, err
	}
	return encoded, nil
}

// UnmarshalJSON decodes a JSON object into the field map, adding to existing
// entries like encoding/json does for maps. JSON null sets the map to nil.
func (values *Values) UnmarshalJSON(data []byte) error {
	decoder := jsonDecoder{data: data}
	decoder.skipSpace()
	if decoder.literal("null") {
		if err := decoder.end(); err != nil {
			return err
		}
		*values = nil
		return nil
	}
	if decoder.pos >= len(data) || data[decoder.pos] != '{' {
		return errors.New("store values must be a JSON object")
	}
	if *values == nil {
		*values = Values{}
	}
	if err := decoder.object(*values); err != nil {
		return err
	}
	return decoder.end()
}

func (value Value) MarshalJSON() ([]byte, error) {
	return value.AppendJSON(make([]byte, 0, 64))
}

// AppendJSON appends the encoding MarshalJSON returns to dst, so a caller can
// encode many values into one buffer. It returns nil and the encoding/json error
// when value contains a non-finite number or an unencodable time.
func (value Value) AppendJSON(dst []byte) ([]byte, error) {
	encoded, err := appendValueJSON(dst, value)
	if err != nil {
		return nil, value.encodingError()
	}
	return encoded, nil
}

// encodingError reproduces the error encoding/json reports for this value. It
// runs only after a fast encode fails.
func (value Value) encodingError() error {
	var err error
	switch value.kind {
	case ValueNumber:
		_, err = json.Marshal(value.number)
	case ValueObject:
		_, err = json.Marshal(map[string]Value(value.object))
	case ValueList:
		items, _ := value.CopyList()
		_, err = json.Marshal(items)
	case ValueDocument:
		_, err = json.Marshal(documentMap(*value.document))
	default:
		err = fmt.Errorf("unknown store value kind %q", value.kind)
	}
	if err == nil {
		return errEncoding
	}
	return err
}

// documentMap is the Go shape a populated document encodes as.
func documentMap(document Document) map[string]any {
	result := map[string]any{
		"id": document.ID, "createdAt": document.CreatedAt, "updatedAt": document.UpdatedAt,
	}
	if document.Status != "" {
		result["_status"] = document.Status
	}
	if document.Revision > 0 {
		result["_revision"] = document.Revision
	}
	for name, value := range document.Values {
		result[name] = value
	}
	return result
}

func (value *Value) UnmarshalJSON(data []byte) error {
	decoder := jsonDecoder{data: data}
	decoded, err := decoder.value()
	if err != nil {
		return err
	}
	if err := decoder.end(); err != nil {
		return err
	}
	*value = decoded
	return nil
}

func appendValueJSON(dst []byte, value Value) ([]byte, error) {
	switch value.kind {
	case ValueNull:
		return append(dst, "null"...), nil
	case ValueString:
		return appendStringJSON(dst, value.text), nil
	case ValueNumber:
		return appendNumberJSON(dst, value.number)
	case ValueBoolean:
		return strconv.AppendBool(dst, value.boolean), nil
	case ValueObject:
		return appendObjectJSON(dst, value.object)
	case ValueList:
		dst = append(dst, '[')
		first := true
		var failure error
		value.list.visit(func(item Value) bool {
			if !first {
				dst = append(dst, ',')
			}
			first = false
			dst, failure = appendValueJSON(dst, item)
			return failure == nil
		})
		if failure != nil {
			return nil, failure
		}
		return append(dst, ']'), nil
	case ValueDocument:
		if value.document == nil {
			return append(dst, "null"...), nil
		}
		return appendDocumentJSON(dst, *value.document)
	default:
		return nil, errEncoding
	}
}

func appendObjectJSON(dst []byte, values Values) ([]byte, error) {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	slices.Sort(names)
	dst = append(dst, '{')
	for index, name := range names {
		if index > 0 {
			dst = append(dst, ',')
		}
		dst = appendStringJSON(dst, name)
		dst = append(dst, ':')
		var err error
		if dst, err = appendValueJSON(dst, values[name]); err != nil {
			return nil, err
		}
	}
	return append(dst, '}'), nil
}

// appendDocumentJSON encodes a populated document as its metadata and fields in
// one object. A field with a metadata name replaces that metadata.
func appendDocumentJSON(dst []byte, document Document) ([]byte, error) {
	metadata := map[string]func([]byte) ([]byte, error){
		"id": func(dst []byte) ([]byte, error) { return appendStringJSON(dst, document.ID), nil },
		"createdAt": func(dst []byte) ([]byte, error) {
			return appendTimeJSON(dst, document.CreatedAt)
		},
		"updatedAt": func(dst []byte) ([]byte, error) {
			return appendTimeJSON(dst, document.UpdatedAt)
		},
	}
	if document.Status != "" {
		metadata["_status"] = func(dst []byte) ([]byte, error) {
			return appendStringJSON(dst, string(document.Status)), nil
		}
	}
	if document.Revision > 0 {
		metadata["_revision"] = func(dst []byte) ([]byte, error) {
			return strconv.AppendInt(dst, int64(document.Revision), 10), nil
		}
	}
	names := make([]string, 0, len(metadata)+len(document.Values))
	for name := range metadata {
		if _, replaced := document.Values[name]; !replaced {
			names = append(names, name)
		}
	}
	for name := range document.Values {
		names = append(names, name)
	}
	slices.Sort(names)
	dst = append(dst, '{')
	for index, name := range names {
		if index > 0 {
			dst = append(dst, ',')
		}
		dst = appendStringJSON(dst, name)
		dst = append(dst, ':')
		var err error
		if value, exists := document.Values[name]; exists {
			dst, err = appendValueJSON(dst, value)
		} else {
			dst, err = metadata[name](dst)
		}
		if err != nil {
			return nil, err
		}
	}
	return append(dst, '}'), nil
}

func appendTimeJSON(dst []byte, value time.Time) ([]byte, error) {
	encoded, err := value.MarshalJSON()
	if err != nil {
		return nil, errEncoding
	}
	return append(dst, encoded...), nil
}

// appendNumberJSON matches encoding/json's float64 encoding, including its
// exponent threshold and its error for NaN and infinities.
func appendNumberJSON(dst []byte, number float64) ([]byte, error) {
	if math.IsInf(number, 0) || math.IsNaN(number) {
		return nil, errEncoding
	}
	format := byte('f')
	if magnitude := math.Abs(number); magnitude != 0 && (magnitude < 1e-6 || magnitude >= 1e21) {
		format = 'e'
	}
	dst = strconv.AppendFloat(dst, number, format, -1, 64)
	if format == 'e' {
		// Shorten e-09 to e-9, as encoding/json does.
		if n := len(dst); n >= 4 && dst[n-4] == 'e' && dst[n-3] == '-' && dst[n-2] == '0' {
			dst[n-2] = dst[n-1]
			dst = dst[:n-1]
		}
	}
	return dst, nil
}

const hexDigits = "0123456789abcdef"

// appendStringJSON matches encoding/json's HTML-safe string encoding.
func appendStringJSON(dst []byte, text string) []byte {
	dst = append(dst, '"')
	start := 0
	for index := 0; index < len(text); {
		if character := text[index]; character < utf8.RuneSelf {
			if character >= 0x20 && character != '"' && character != '\\' && character != '<' && character != '>' && character != '&' {
				index++
				continue
			}
			dst = append(dst, text[start:index]...)
			switch character {
			case '"', '\\':
				dst = append(dst, '\\', character)
			case '\b':
				dst = append(dst, '\\', 'b')
			case '\f':
				dst = append(dst, '\\', 'f')
			case '\n':
				dst = append(dst, '\\', 'n')
			case '\r':
				dst = append(dst, '\\', 'r')
			case '\t':
				dst = append(dst, '\\', 't')
			default:
				dst = append(dst, '\\', 'u', '0', '0', hexDigits[character>>4], hexDigits[character&0xF])
			}
			index++
			start = index
			continue
		}
		character, size := utf8.DecodeRuneInString(text[index:])
		if character == utf8.RuneError && size == 1 {
			dst = append(dst, text[start:index]...)
			dst = append(dst, `\ufffd`...)
			index += size
			start = index
			continue
		}
		if character == ' ' || character == ' ' {
			dst = append(dst, text[start:index]...)
			dst = append(dst, '\\', 'u', '2', '0', '2', hexDigits[character&0xF])
			index += size
			start = index
			continue
		}
		index += size
	}
	dst = append(dst, text[start:]...)
	return append(dst, '"')
}

// jsonDecoder parses one JSON value directly into store values. It validates
// the complete grammar itself because UnmarshalJSON can be called directly.
type jsonDecoder struct {
	data  []byte
	pos   int
	depth int
	// names reuses one string per distinct object key. Rich text and array rows
	// repeat the same few keys hundreds of times in one document.
	names map[string]string
}

func (decoder *jsonDecoder) fail(reason string) error {
	return fmt.Errorf("invalid JSON value at byte %d: %s", decoder.pos, reason)
}

func (decoder *jsonDecoder) skipSpace() {
	for decoder.pos < len(decoder.data) {
		switch decoder.data[decoder.pos] {
		case ' ', '\t', '\n', '\r':
			decoder.pos++
		default:
			return
		}
	}
}

func (decoder *jsonDecoder) end() error {
	decoder.skipSpace()
	if decoder.pos != len(decoder.data) {
		return decoder.fail("unexpected data after the value")
	}
	return nil
}

func (decoder *jsonDecoder) literal(word string) bool {
	if len(decoder.data)-decoder.pos >= len(word) && string(decoder.data[decoder.pos:decoder.pos+len(word)]) == word {
		decoder.pos += len(word)
		return true
	}
	return false
}

func (decoder *jsonDecoder) value() (Value, error) {
	decoder.skipSpace()
	if decoder.pos >= len(decoder.data) {
		return Value{}, decoder.fail("unexpected end of data")
	}
	switch character := decoder.data[decoder.pos]; {
	case character == '"':
		text, err := decoder.string()
		return Value{kind: ValueString, text: text}, err
	case character == '{':
		values := Values{}
		if err := decoder.object(values); err != nil {
			return Value{}, err
		}
		// The map was built here and is never exposed mutably, so it needs no copy.
		return Value{kind: ValueObject, object: values}, nil
	case character == '[':
		return decoder.list()
	case character == '-' || (character >= '0' && character <= '9'):
		number, err := decoder.number()
		return Value{kind: ValueNumber, number: number}, err
	case decoder.literal("true"):
		return Value{kind: ValueBoolean, boolean: true}, nil
	case decoder.literal("false"):
		return Value{kind: ValueBoolean}, nil
	case decoder.literal("null"):
		return Value{kind: ValueNull}, nil
	default:
		return Value{}, decoder.fail(fmt.Sprintf("unexpected character %q", character))
	}
}

func (decoder *jsonDecoder) enter() error {
	decoder.depth++
	if decoder.depth > maxJSONDepth {
		return decoder.fail("nesting is too deep")
	}
	return nil
}

// object reads members into values. The opening brace is at pos.
func (decoder *jsonDecoder) object(values Values) error {
	if err := decoder.enter(); err != nil {
		return err
	}
	decoder.pos++
	decoder.skipSpace()
	if decoder.pos < len(decoder.data) && decoder.data[decoder.pos] == '}' {
		decoder.pos++
		decoder.depth--
		return nil
	}
	for {
		decoder.skipSpace()
		if decoder.pos >= len(decoder.data) || decoder.data[decoder.pos] != '"' {
			return decoder.fail("expected an object key")
		}
		name, err := decoder.name()
		if err != nil {
			return err
		}
		decoder.skipSpace()
		if decoder.pos >= len(decoder.data) || decoder.data[decoder.pos] != ':' {
			return decoder.fail("expected ':' after an object key")
		}
		decoder.pos++
		member, err := decoder.value()
		if err != nil {
			return err
		}
		// A repeated key keeps its last value, as encoding/json does for maps.
		values[name] = member
		decoder.skipSpace()
		if decoder.pos >= len(decoder.data) {
			return decoder.fail("unexpected end of object")
		}
		switch decoder.data[decoder.pos] {
		case ',':
			decoder.pos++
		case '}':
			decoder.pos++
			decoder.depth--
			return nil
		default:
			return decoder.fail("expected ',' or '}' in object")
		}
	}
}

func (decoder *jsonDecoder) list() (Value, error) {
	if err := decoder.enter(); err != nil {
		return Value{}, err
	}
	decoder.pos++
	decoder.skipSpace()
	if decoder.pos < len(decoder.data) && decoder.data[decoder.pos] == ']' {
		decoder.pos++
		decoder.depth--
		return Value{kind: ValueList}, nil
	}
	var items []Value
	for {
		item, err := decoder.value()
		if err != nil {
			return Value{}, err
		}
		items = append(items, item)
		decoder.skipSpace()
		if decoder.pos >= len(decoder.data) {
			return Value{}, decoder.fail("unexpected end of list")
		}
		switch decoder.data[decoder.pos] {
		case ',':
			decoder.pos++
		case ']':
			decoder.pos++
			decoder.depth--
			return Value{kind: ValueList, list: ownValueList(items)}, nil
		default:
			return Value{}, decoder.fail("expected ',' or ']' in list")
		}
	}
}

func (decoder *jsonDecoder) number() (float64, error) {
	data, start := decoder.data, decoder.pos
	index := start
	negative := data[index] == '-'
	if negative {
		index++
	}
	digits := func() int {
		begin := index
		for index < len(data) && data[index] >= '0' && data[index] <= '9' {
			index++
		}
		return index - begin
	}
	switch {
	case index < len(data) && data[index] == '0':
		index++
	case digits() == 0:
		decoder.pos = index
		return 0, decoder.fail("expected a digit")
	}
	integerEnd := index
	if index < len(data) && data[index] == '.' {
		index++
		if digits() == 0 {
			decoder.pos = index
			return 0, decoder.fail("expected a digit after the decimal point")
		}
	}
	if index < len(data) && (data[index] == 'e' || data[index] == 'E') {
		index++
		if index < len(data) && (data[index] == '+' || data[index] == '-') {
			index++
		}
		if digits() == 0 {
			decoder.pos = index
			return 0, decoder.fail("expected a digit in the exponent")
		}
	}
	decoder.pos = index
	// Integers with at most 15 digits are exact in a float64, so they skip
	// ParseFloat and its string allocation.
	if integerEnd == index {
		mantissa := data[start:index]
		if negative {
			mantissa = mantissa[1:]
		}
		if len(mantissa) <= 15 {
			var integer int64
			for _, digit := range mantissa {
				integer = integer*10 + int64(digit-'0')
			}
			number := float64(integer)
			if negative {
				number = -number
			}
			return number, nil
		}
	}
	number, err := strconv.ParseFloat(string(data[start:index]), 64)
	if err != nil {
		return 0, fmt.Errorf("JSON number %s is out of range for a float64", data[start:index])
	}
	return number, nil
}

// name reads an object key, reusing an earlier identical key when it needs no
// unescaping. The map lookup with string(bytes) does not allocate.
func (decoder *jsonDecoder) name() (string, error) {
	data := decoder.data
	start := decoder.pos + 1
	for index := start; index < len(data); index++ {
		switch character := data[index]; {
		case character == '"':
			if name, ok := decoder.names[string(data[start:index])]; ok {
				decoder.pos = index + 1
				return name, nil
			}
			name, err := decoder.string()
			if err == nil {
				if decoder.names == nil {
					decoder.names = make(map[string]string, 16)
				}
				decoder.names[name] = name
			}
			return name, err
		case character == '\\' || character < 0x20 || character >= utf8.RuneSelf:
			return decoder.string()
		}
	}
	return decoder.string()
}

// string reads a quoted string at pos. Text without escapes, control
// characters, or non-ASCII bytes is copied directly.
func (decoder *jsonDecoder) string() (string, error) {
	data := decoder.data
	start := decoder.pos + 1
	for index := start; index < len(data); index++ {
		switch character := data[index]; {
		case character == '"':
			decoder.pos = index + 1
			return string(data[start:index]), nil
		case character == '\\' || character < 0x20 || character >= utf8.RuneSelf:
			return decoder.escapedString(start)
		}
	}
	decoder.pos = len(data)
	return "", decoder.fail("unterminated string")
}

func (decoder *jsonDecoder) escapedString(start int) (string, error) {
	data := decoder.data
	decoded := make([]byte, 0, len(data)-start)
	for index := start; index < len(data); {
		character := data[index]
		switch {
		case character == '"':
			decoder.pos = index + 1
			return string(decoded), nil
		case character < 0x20:
			decoder.pos = index
			return "", decoder.fail("control character in string")
		case character == '\\':
			if index+1 >= len(data) {
				decoder.pos = index
				return "", decoder.fail("unterminated escape")
			}
			switch escape := data[index+1]; escape {
			case '"', '\\', '/':
				decoded = append(decoded, escape)
				index += 2
			case 'b':
				decoded = append(decoded, '\b')
				index += 2
			case 'f':
				decoded = append(decoded, '\f')
				index += 2
			case 'n':
				decoded = append(decoded, '\n')
				index += 2
			case 'r':
				decoded = append(decoded, '\r')
				index += 2
			case 't':
				decoded = append(decoded, '\t')
				index += 2
			case 'u':
				first, ok := hexRune(data, index+2)
				if !ok {
					decoder.pos = index
					return "", decoder.fail("invalid unicode escape")
				}
				index += 6
				if utf16.IsSurrogate(first) {
					// A valid pair becomes one rune; an unpaired half becomes U+FFFD.
					if index+1 < len(data) && data[index] == '\\' && data[index+1] == 'u' {
						if second, ok := hexRune(data, index+2); ok {
							if pair := utf16.DecodeRune(first, second); pair != utf8.RuneError {
								decoded = utf8.AppendRune(decoded, pair)
								index += 6
								continue
							}
						}
					}
					first = utf8.RuneError
				}
				decoded = utf8.AppendRune(decoded, first)
			default:
				decoder.pos = index
				return "", decoder.fail("invalid escape")
			}
		case character < utf8.RuneSelf:
			decoded = append(decoded, character)
			index++
		default:
			runeValue, size := utf8.DecodeRune(data[index:])
			if runeValue == utf8.RuneError && size == 1 {
				decoded = utf8.AppendRune(decoded, utf8.RuneError)
			} else {
				decoded = append(decoded, data[index:index+size]...)
			}
			index += size
		}
	}
	decoder.pos = len(data)
	return "", decoder.fail("unterminated string")
}

func hexRune(data []byte, start int) (rune, bool) {
	if start+4 > len(data) {
		return 0, false
	}
	var result rune
	for _, character := range data[start : start+4] {
		result <<= 4
		switch {
		case character >= '0' && character <= '9':
			result |= rune(character - '0')
		case character >= 'a' && character <= 'f':
			result |= rune(character-'a') + 10
		case character >= 'A' && character <= 'F':
			result |= rune(character-'A') + 10
		default:
			return 0, false
		}
	}
	return result, true
}
