package store

import "testing"

func TestCanonicalAuthIdentityTruthTable(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "ASCII case and Unicode edge whitespace", value: " \tUser@Example.COM\u00a0", want: "user@example.com"},
		{name: "long s remains distinct", value: "\u017f", want: "\u017f"},
		{name: "ASCII S lowers", value: "S", want: "s"},
		{name: "Kelvin sign follows Go lowercase", value: "\u212a", want: "k"},
		{name: "ASCII K lowers", value: "K", want: "k"},
		{name: "capital sigma lowers to sigma", value: "\u03a3", want: "\u03c3"},
		{name: "final sigma remains distinct", value: "\u03c2", want: "\u03c2"},
		{name: "normal sigma remains normal sigma", value: "\u03c3", want: "\u03c3"},
		{name: "ASCII I lowers", value: "I", want: "i"},
		{name: "dotted capital I follows Go lowercase", value: "\u0130", want: "i"},
		{name: "dotless small I remains distinct", value: "\u0131", want: "\u0131"},
		{name: "zero width space is not TrimSpace whitespace", value: "\u200bUser@Example.COM\u200b", want: "\u200buser@example.com\u200b"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := CanonicalAuthIdentity(test.value); got != test.want {
				t.Fatalf("CanonicalAuthIdentity(%q) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}
