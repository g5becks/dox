package jsongolden_test

import (
	"testing"

	"github.com/g5becks/dox/internal/testutil/jsongolden"
)

func TestCanonical(t *testing.T) {
	t.Parallel()
	a, err := jsongolden.Canonical([]byte(`{"z":9007199254740993,"a":[2,1]}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := jsongolden.Canonical([]byte(`{ "a": [2,1], "z": 9007199254740993 }`))
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) {
		t.Fatalf("object order changed canonical result: %s vs %s", a, b)
	}
	if string(a) != "{\n  \"a\": [\n    2,\n    1\n  ],\n  \"z\": 9007199254740993\n}" {
		t.Fatalf("lost array order or exact integer: %s", a)
	}
}

func TestCanonicalRejectsInvalidOrMultipleDocuments(t *testing.T) {
	t.Parallel()
	for _, input := range []string{`{`, `{} {}`, `{} trailing`} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			if _, err := jsongolden.Canonical([]byte(input)); err == nil {
				t.Fatalf("accepted %q", input)
			}
		})
	}
}
