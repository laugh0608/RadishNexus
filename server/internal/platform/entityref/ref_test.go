package entityref

import (
	"errors"
	"testing"
)

func TestParseFrozenCollaborationReferences(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw      string
		wantType string
		wantID   string
	}{
		{raw: "entity://thread/thr_01K4THREAD", wantType: "thread", wantID: "thr_01K4THREAD"},
		{raw: "entity://decision/dec_01K4DECISION", wantType: "decision", wantID: "dec_01K4DECISION"},
		{raw: "entity://ticket/tkt_01K4TICKET", wantType: "ticket", wantID: "tkt_01K4TICKET"},
		{raw: "entity://ci-run/cir_01K4CIRUN", wantType: "ci-run", wantID: "cir_01K4CIRUN"},
	}

	for _, test := range tests {
		ref, err := Parse(test.raw, M0Registry())
		if err != nil {
			t.Fatalf("Parse(%q) error = %v", test.raw, err)
		}
		if ref.Type != test.wantType || ref.ID != test.wantID {
			t.Fatalf("Parse(%q) = %#v", test.raw, ref)
		}
		if ref.URI() != test.raw {
			t.Fatalf("Ref.URI() = %q, want %q", ref.URI(), test.raw)
		}
	}
}

func TestParseRejectsNonCanonicalReferences(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want error
	}{
		{name: "unknown type", raw: "entity://message/msg_1", want: ErrUnknownType},
		{name: "wrong thread prefix", raw: "entity://thread/prototype-thread-1", want: ErrInvalidReference},
		{name: "wrong ticket prefix", raw: "entity://ticket/tic_1", want: ErrInvalidReference},
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
