package bot

import (
	"math"
	"strings"
	"testing"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

func TestCallbackRoundTrip(t *testing.T) {
	const maxID = math.MaxInt64
	const maxDraft = math.MaxUint32
	var cases []callback
	cases = append(cases,
		callback{op: opNoop},
		callback{op: opRecipeList, page: 0},
		callback{op: opRecipeList, page: maxPage},
		callback{op: opCookAgain, id: maxID},
	)
	for _, s := range domain.Statuses {
		cases = append(cases,
			callback{op: opWishList, status: s, page: maxPage},
			callback{op: opWishStatus, id: maxID, status: s},
		)
	}
	for _, op := range []cbOp{opWishOpen, opWishAskDelete, opWishDelete, opWishKeep,
		opRecipeOpen, opRecipeAskDelete, opRecipeDelete, opRecipeKeep} {
		cases = append(cases, callback{op: op, id: 1}, callback{op: op, id: maxID})
	}
	for _, op := range []cbOp{opDraftCategories, opDraftBack, opDraftHot, opDraftSave, opDraftCancel} {
		cases = append(cases, callback{op: op, draft: maxDraft})
	}
	cases = append(cases,
		callback{op: opDraftKind, draft: 1, kind: kindWish},
		callback{op: opDraftKind, draft: maxDraft, kind: kindRecipe},
		callback{op: opDraftCategory, draft: maxDraft, id: 0},
		callback{op: opDraftCategory, draft: maxDraft, id: maxID},
	)
	for _, f := range []draftField{fieldTitle, fieldPrice, fieldLink, fieldText} {
		cases = append(cases, callback{op: opDraftField, draft: maxDraft, field: f})
	}

	for _, c := range cases {
		s := c.String()
		if len(s) > maxCallbackData {
			t.Errorf("%+v encodes to %d bytes", c, len(s))
		}
		got, err := parseCallback(s)
		if err != nil {
			t.Errorf("parse(%q): %v", s, err)
			continue
		}
		if got != c {
			t.Errorf("round trip %q: got %+v, want %+v", s, got, c)
		}
	}
}

func TestCallbackExamples(t *testing.T) {
	tests := map[string]callback{
		"x":            {op: opNoop},
		"w:o:123":      {op: opWishOpen, id: 123},
		"w:s:123:done": {op: opWishStatus, id: 123, status: domain.StatusDone},
		"l:w:want:2":   {op: opWishList, status: domain.StatusWant, page: 2},
		"l:r:0":        {op: opRecipeList},
		"d:7:k:w":      {op: opDraftKind, draft: 7, kind: kindWish},
		"d:7:f:p":      {op: opDraftField, draft: 7, field: fieldPrice},
		"d:7:c:0":      {op: opDraftCategory, draft: 7},
		"k:5":          {op: opCookAgain, id: 5},
	}
	for s, want := range tests {
		if got := want.String(); got != s {
			t.Errorf("%+v encodes to %q, want %q", want, got, s)
		}
		if got, err := parseCallback(s); err != nil || got != want {
			t.Errorf("parse(%q) = %+v, %v", s, got, err)
		}
	}
}

func TestCallbackRejectsGarbage(t *testing.T) {
	bad := []string{
		"", ":", "x:", "x:1", "k", "k:0", "k:x",
		"w", "w:o", "w:o:", "w:o:0", "w:o:-1", "w:o:+1", "w:o:01", "w:o:1:2", "w:o: 1", "w:o:1 ",
		"w:o:١٢", // Arabic-Indic digits
		"w:o:99999999999999999999", "w:o:9223372036854775808",
		"w:s:1", "w:s:1:bogus", "w:s:1:DONE", "w:z:1", "r:s:1:done",
		"l", "l:w", "l:w:want", "l:w:want:x", "l:w:nope:0", "l:w:want:10000", "l:r", "l:r:-1", "l:r:1:2", "l:x:1",
		"d", "d:1", "d:0:ok", "d:x:ok", "d:01:ok", "d:4294967296:ok", "d:1:k", "d:1:k:z", "d:1:f:q",
		"d:1:c", "d:1:c:-1", "d:1:ok:1", "d:1:cat:2", "d:1:unknown",
		"wish:open:1", "<script>", strings.Repeat("w", 65),
	}
	for _, s := range bad {
		if c, err := parseCallback(s); err == nil {
			t.Errorf("parse(%q) accepted as %+v", s, c)
		}
	}
}

func TestCallbackStringNeverExceedsLimit(t *testing.T) {
	// An unknown op cannot be encoded and must degrade to a harmless no-op.
	if got := (callback{op: cbOp(250)}).String(); got != "x" {
		t.Errorf("unknown op encodes to %q", got)
	}
}
