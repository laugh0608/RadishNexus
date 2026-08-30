package entityref

import (
	"errors"
	"testing"
)

func TestParseCanonicalReference(t *testing.T) {
	t.Parallel()

	ref, err := Parse("entity://decision/dec_01K4EXAMPLE", M0Registry())
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if ref.Type != "decision" || ref.ID != "dec_01K4EXAMPLE" {
		t.Fatalf("Parse() = %#v", ref)
	}
	if got := ref.URI(); got != "entity://decision/dec_01K4EXAMPLE" {
		t.Fatalf("URI() = %q", got)
	}
}

func TestParseRejectsNonCanonicalReferences(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want error
	}{
		{name: "unknown type", raw: "entity://thread/thr_1", want: ErrUnknownType},
		{name: "uppercase type", raw: "entity://Decision/dec_1", want: ErrUnknownType},
		{name: "wrong prefix", raw: "entity://decision/tic_1", want: ErrInvalidReference},
		{name: "query", raw: "entity://decision/dec_1?view=full", want: ErrInvalidReference},
		{name: "fragment", raw: "entity://decision/dec_1#title", want: ErrInvalidReference},
		{name: "extra segment", raw: "entity://decision/dec_1/extra", want: ErrInvalidReference},
		{name: "non ascii", raw: "entity://decision/dec_萝卜", want: ErrInvalidReference},
		{name: "space", raw: "entity://decision/dec_1 2", want: ErrInvalidReference},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, err := Parse(test.raw, M0Registry())
			if !errors.Is(err, test.want) {
				t.Fatalf("Parse(%q) error = %v, want %v", test.raw, err, test.want)
			}
		})
	}
}

func TestRegistryCanReserveUnfrozenType(t *testing.T) {
	t.Parallel()

	registry := M0Registry()
	registry["thread"] = ""

	ref, err := Parse("entity://thread/prototype-thread-1", registry)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if ref.ID != "prototype-thread-1" {
		t.Fatalf("Parse() ID = %q", ref.ID)
	}
}
