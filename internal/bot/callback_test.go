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
	for _, op := range entityOps {
		cases = append(cases, callback{op: op, id: 1}, callback{op: op, id: maxID})
	}
	for stars := 0; stars <= maxStars; stars++ {
		cases = append(cases, callback{op: opRecipeCook, id: maxID, stars: stars})
	}
	for stars := 1; stars <= maxStars; stars++ {
		cases = append(cases, callback{op: opRecipeRate, id: maxID, cook: maxID, stars: stars})
	}
	cases = append(cases,
		callback{op: opShopList, page: 0},
		callback{op: opShopList, page: maxPage},
		callback{op: opShopCheck, id: maxID, page: maxPage},
		callback{op: opShopUncheck, id: 1, page: 0},
		callback{op: opShopClear},
	)
	for _, op := range []cbOp{opDraftCategories, opDraftCuisines, opDraftCourses, opDraftBack, opDraftHot, opDraftSave, opDraftCancel} {
		cases = append(cases, callback{op: op, draft: maxDraft})
	}
	for _, op := range []cbOp{opDraftCategory, opDraftCuisine, opDraftCourse} {
		cases = append(cases, callback{op: op, draft: maxDraft, id: 0}, callback{op: op, draft: maxDraft, id: maxID})
	}
	cases = append(cases,
		callback{op: opDraftKind, draft: 1, kind: kindWish},
		callback{op: opDraftKind, draft: maxDraft, kind: kindRecipe},
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
		"w:m:12":       {op: opWishSave, id: 12},
		"r:c:4":        {op: opRecipeCookAsk, id: 4},
		"r:c:4:0":      {op: opRecipeCook, id: 4},
		"r:c:4:5":      {op: opRecipeCook, id: 4, stars: 5},
		"r:b:4":        {op: opRecipeBack, id: 4},
		"r:s:4":        {op: opRecipeShop, id: 4},
		"r:v:4:17:3":   {op: opRecipeRate, id: 4, cook: 17, stars: 3},
		"s:l:2":        {op: opShopList, page: 2},
		"s:k:31:0":     {op: opShopCheck, id: 31},
		"s:u:31:1":     {op: opShopUncheck, id: 31, page: 1},
		"s:x":          {op: opShopClear},
		"d:7:cu":       {op: opDraftCuisines, draft: 7},
		"d:7:cu:3":     {op: opDraftCuisine, draft: 7, id: 3},
		"d:7:co":       {op: opDraftCourses, draft: 7},
		"d:7:co:0":     {op: opDraftCourse, draft: 7},
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
		// New ops: wrong arity, out-of-range stars, zero IDs, non-canonical numbers.
		"w:m", "w:m:0", "w:m:1:2", "r:b:0", "r:s:", "r:s:1:2",
		"r:c:1:6", "r:c:1:-1", "r:c:1:01", "r:c:1:", "r:c:0:3", "r:c:1:3:4", "r:c:1:٣",
		"r:v:1:2", "r:v:1:2:0", "r:v:1:2:6", "r:v:1:0:3", "r:v:0:2:3", "r:v:1:2:3:4", "r:v:1:02:3",
		"w:v:1:2:3", "w:c:1:3", "r:x:1",
		"s", "s:", "s:x:1", "s:l", "s:l:-1", "s:l:10000", "s:k:1", "s:k:0:0", "s:k:1:x", "s:u:1:0:0", "s:z:1:0",
		"d:1:cu:x", "d:1:co:-1", "d:1:cu:01", "d:1:cu:1:2", "d:1:cou", "d:1:cuisine",
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
	// Stars outside the scale are never put on a button.
	for _, c := range []callback{
		{op: opRecipeCook, id: 1, stars: 6},
		{op: opRecipeCook, id: 1, stars: -1},
		{op: opRecipeRate, id: 1, cook: 1, stars: 0},
		{op: opRecipeRate, id: 1, cook: 1, stars: 6},
	} {
		if got := c.String(); got != "x" {
			t.Errorf("%+v encodes to %q", c, got)
		}
	}
}
