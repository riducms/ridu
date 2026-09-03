// Package jsonlimit validates untrusted JSON structure before application
// decoders allocate recursive values. Byte limits alone do not prevent deeply
// nested or highly fragmented documents from consuming disproportionate CPU
// and stack space.
package jsonlimit

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	// DefaultMaxDepth leaves room for Ridu's bounded rich-text documents and
	// their request envelope while rejecting pathological JSON nesting.
	DefaultMaxDepth = 128
	// DefaultMaxTokens is deliberately well above an ordinary one-megabyte
	// document, but provides a stable ceiling when callers raise byte limits.
	DefaultMaxTokens = 100_000
)

var (
	ErrTooDeep       = errors.New("JSON nesting exceeds the configured limit")
	ErrTooManyTokens = errors.New("JSON token count exceeds the configured limit")
	ErrDuplicateKey  = errors.New("JSON object contains a duplicate key")
)

type frame struct {
	kind      json.Delim
	expectKey bool
	keys      map[string]struct{}
}

// Validate rejects malformed JSON, excessive depth/token counts, and
// duplicate object keys. The caller still decodes the value into its concrete
// contract afterwards; this pass exists to bound and disambiguate that work.
func Validate(encoded []byte, maxDepth, maxTokens int) error {
	if maxDepth <= 0 {
		maxDepth = DefaultMaxDepth
	}
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}
	decoder := json.NewDecoder(bytesReader(encoded))
	decoder.UseNumber()
	stack := make([]frame, 0, 8)
	tokens := 0
	roots := 0
	rootInProgress := false

	completeValue := func() {
		if len(stack) == 0 {
			roots++
			rootInProgress = false
			return
		}
		parent := &stack[len(stack)-1]
		if parent.kind == '{' {
			parent.expectKey = true
		}
	}

	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("invalid JSON: %w", err)
		}
		tokens++
		if tokens > maxTokens {
			return ErrTooManyTokens
		}

		if delimiter, ok := token.(json.Delim); ok {
			switch delimiter {
			case '{', '[':
				if len(stack) == 0 {
					if rootInProgress || roots != 0 {
						return errors.New("JSON contains multiple values")
					}
					rootInProgress = true
				}
				entry := frame{kind: delimiter}
				if delimiter == '{' {
					entry.expectKey = true
					entry.keys = make(map[string]struct{})
				}
				stack = append(stack, entry)
				if len(stack) > maxDepth {
					return ErrTooDeep
				}
			case '}', ']':
				if len(stack) == 0 {
					return errors.New("JSON contains an unmatched closing delimiter")
				}
				current := stack[len(stack)-1]
				if current.kind == '{' && delimiter != '}' || current.kind == '[' && delimiter != ']' {
					return errors.New("JSON contains mismatched delimiters")
				}
				stack = stack[:len(stack)-1]
				completeValue()
			}
			continue
		}

		if len(stack) == 0 {
			if rootInProgress || roots != 0 {
				return errors.New("JSON contains multiple values")
			}
			rootInProgress = true
			completeValue()
			continue
		}
		current := &stack[len(stack)-1]
		if current.kind == '{' && current.expectKey {
			key, ok := token.(string)
			if !ok {
				return errors.New("JSON object key must be a string")
			}
			if _, exists := current.keys[key]; exists {
				return fmt.Errorf("%w %q", ErrDuplicateKey, key)
			}
			current.keys[key] = struct{}{}
			current.expectKey = false
			continue
		}
		completeValue()
	}
	if len(stack) != 0 || rootInProgress {
		return errors.New("JSON value is incomplete")
	}
	if roots != 1 {
		return errors.New("JSON must contain exactly one value")
	}
	return nil
}

// byteReader is the small read-only io.Reader needed by json.Decoder. Keeping
// it local avoids exporting implementation choices from this boundary package.
type byteReader struct {
	encoded []byte
	offset  int
}

func bytesReader(encoded []byte) *byteReader { return &byteReader{encoded: encoded} }

func (reader *byteReader) Read(target []byte) (int, error) {
	if reader.offset >= len(reader.encoded) {
		return 0, io.EOF
	}
	count := copy(target, reader.encoded[reader.offset:])
	reader.offset += count
	return count, nil
}
