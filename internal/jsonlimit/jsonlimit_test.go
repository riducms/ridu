package jsonlimit_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/riducms/ridu/internal/jsonlimit"
)

func TestValidateAcceptsOneBoundedUnambiguousValue(t *testing.T) {
	encoded := []byte(`{"object":{"list":[1,true,null,"value"]}}`)
	if err := jsonlimit.Validate(encoded, 8, 32); err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsStructuralAbuseAndAmbiguity(t *testing.T) {
	tests := []struct {
		name    string
		encoded string
		depth   int
		tokens  int
		want    error
	}{
		{name: "duplicate", encoded: `{"value":1,"value":2}`, depth: 8, tokens: 32, want: jsonlimit.ErrDuplicateKey},
		{name: "depth", encoded: `[[[[0]]]]`, depth: 3, tokens: 32, want: jsonlimit.ErrTooDeep},
		{name: "tokens", encoded: `[1,2,3,4]`, depth: 8, tokens: 4, want: jsonlimit.ErrTooManyTokens},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := jsonlimit.Validate([]byte(test.encoded), test.depth, test.tokens)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
	for _, encoded := range []string{"", "{} {}", `{"missing":`, strings.Repeat("[", 4)} {
		if err := jsonlimit.Validate([]byte(encoded), 8, 32); err == nil {
			t.Fatalf("Validate(%q) succeeded", encoded)
		}
	}
}
