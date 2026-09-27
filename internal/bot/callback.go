package bot

import (
	"errors"
	"strconv"
	"strings"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// Callback data is limited to 64 bytes by the Bot API, so buttons carry a
// compact, colon-separated payload. Only numeric IDs and fixed tokens are ever
// encoded, never user text:
//
//	x                      no-op (page indicator)
//	l:w:<status>:<page>    wish list tab and page
//	l:r:<page>             recipe list page
//	w:o|d|y|n:<id>         wish: open, ask delete, delete, keep
//	w:s:<id>:<status>      wish: set status
//	r:o|d|y|n:<id>         recipe: open, ask delete, delete, keep
//	k:<id>                 another random recipe than <id>
//	d:<draft>:k:<w|r>      draft: switch kind
//	d:<draft>:cat          draft: show categories
//	d:<draft>:c:<id>       draft: choose category (0 = none)
//	d:<draft>:back         draft: back to the card
//	d:<draft>:f:<t|p|l|b>  draft: ask for title, price, link or text
//	d:<draft>:hot          draft: toggle «очень хочу»
//	d:<draft>:ok           draft: save
//	d:<draft>:no           draft: discard
const maxCallbackData = 64

// maxPage bounds page numbers so a forged payload cannot request absurd
// offsets.
const maxPage = 9999

type cbOp uint8

const (
	opNoop cbOp = iota + 1
	opWishList
	opRecipeList
	opWishOpen
	opWishStatus
	opWishAskDelete
	opWishDelete
	opWishKeep
	opRecipeOpen
	opRecipeAskDelete
	opRecipeDelete
	opRecipeKeep
	opCookAgain
	opDraftKind
	opDraftCategories
	opDraftCategory
	opDraftBack
	opDraftField
	opDraftHot
	opDraftSave
	opDraftCancel
)

// callback is a decoded button payload. Which fields are meaningful depends
// on op; the others stay zero.
type callback struct {
	op     cbOp
	id     int64 // wish, recipe or category ID
	draft  uint32
	status domain.Status
	page   int
	kind   entityKind
	field  draftField
}

var errBadCallback = errors.New("bot: malformed callback data")

// String encodes the callback. It never returns more than 64 bytes: an
// unknown op or an oversized payload (impossible with numeric IDs) degrades
// to a no-op button.
func (c callback) String() string {
	s := c.encode()
	if s == "" || len(s) > maxCallbackData {
		return "x"
	}
	return s
}

func (c callback) encode() string {
	id := strconv.FormatInt(c.id, 10)
	draft := "d:" + strconv.FormatUint(uint64(c.draft), 10) + ":"
	switch c.op {
	case opNoop:
		return "x"
	case opWishList:
		return "l:w:" + string(c.status) + ":" + strconv.Itoa(c.page)
	case opRecipeList:
		return "l:r:" + strconv.Itoa(c.page)
	case opWishStatus:
		return "w:s:" + id + ":" + string(c.status)
	case opCookAgain:
		return "k:" + id
	case opDraftKind:
		return draft + "k:" + kindCode(c.kind)
	case opDraftCategories:
		return draft + "cat"
	case opDraftCategory:
		return draft + "c:" + id
	case opDraftBack:
		return draft + "back"
	case opDraftField:
		return draft + "f:" + fieldCode(c.field)
	case opDraftHot:
		return draft + "hot"
	case opDraftSave:
		return draft + "ok"
	case opDraftCancel:
		return draft + "no"
	}
	if entity, action, ok := entityAction(c.op); ok {
		return entity + ":" + action + ":" + id
	}
	return ""
}

// entityAction splits the per-entity ops into their wire tokens.
func entityAction(op cbOp) (entity, action string, ok bool) {
	switch op {
	case opWishOpen:
		return "w", "o", true
	case opWishAskDelete:
		return "w", "d", true
	case opWishDelete:
		return "w", "y", true
	case opWishKeep:
		return "w", "n", true
	case opRecipeOpen:
		return "r", "o", true
	case opRecipeAskDelete:
		return "r", "d", true
	case opRecipeDelete:
		return "r", "y", true
	case opRecipeKeep:
		return "r", "n", true
	}
	return "", "", false
}

func entityOp(entity, action string) (cbOp, bool) {
	for op := opWishOpen; op <= opRecipeKeep; op++ {
		if e, a, ok := entityAction(op); ok && e == entity && a == action {
			return op, true
		}
	}
	return 0, false
}

func kindCode(k entityKind) string {
	if k == kindRecipe {
		return "r"
	}
	return "w"
}

func parseKind(s string) (entityKind, bool) {
	switch s {
	case "w":
		return kindWish, true
	case "r":
		return kindRecipe, true
	}
	return 0, false
}

func fieldCode(f draftField) string {
	switch f {
	case fieldTitle:
		return "t"
	case fieldPrice:
		return "p"
	case fieldLink:
		return "l"
	case fieldText:
		return "b"
	}
	return ""
}

func parseField(s string) (draftField, bool) {
	for f := fieldTitle; f <= fieldText; f++ {
		if fieldCode(f) == s {
			return f, true
		}
	}
	return fieldNone, false
}

// parseCallback strictly decodes a payload produced by callback.String.
// Anything else, including payloads from older versions of the bot, is
// rejected so that the caller can answer «Кнопка устарела».
func parseCallback(s string) (callback, error) {
	if s == "" || len(s) > maxCallbackData {
		return callback{}, errBadCallback
	}
	p := strings.Split(s, ":")
	switch p[0] {
	case "x":
		if len(p) == 1 {
			return callback{op: opNoop}, nil
		}
	case "k":
		if len(p) == 2 {
			id, err := parseID(p[1], false)
			return callback{op: opCookAgain, id: id}, err
		}
	case "l":
		return parseListCallback(p)
	case "w", "r":
		return parseEntityCallback(p)
	case "d":
		return parseDraftCallback(p)
	}
	return callback{}, errBadCallback
}

func parseListCallback(p []string) (callback, error) {
	switch {
	case len(p) == 4 && p[1] == "w":
		status, err := domain.ParseStatus(p[2])
		if err != nil {
			return callback{}, errBadCallback
		}
		page, err := parsePage(p[3])
		return callback{op: opWishList, status: status, page: page}, err
	case len(p) == 3 && p[1] == "r":
		page, err := parsePage(p[2])
		return callback{op: opRecipeList, page: page}, err
	}
	return callback{}, errBadCallback
}

func parseEntityCallback(p []string) (callback, error) {
	switch {
	case len(p) == 4 && p[0] == "w" && p[1] == "s":
		id, err := parseID(p[2], false)
		if err != nil {
			return callback{}, err
		}
		status, err := domain.ParseStatus(p[3])
		if err != nil {
			return callback{}, errBadCallback
		}
		return callback{op: opWishStatus, id: id, status: status}, nil
	case len(p) == 3:
		op, ok := entityOp(p[0], p[1])
		if !ok {
			return callback{}, errBadCallback
		}
		id, err := parseID(p[2], false)
		return callback{op: op, id: id}, err
	}
	return callback{}, errBadCallback
}

func parseDraftCallback(p []string) (callback, error) {
	if len(p) < 3 {
		return callback{}, errBadCallback
	}
	draft, err := parseDraftID(p[1])
	if err != nil {
		return callback{}, err
	}
	c := callback{draft: draft}
	args := p[2:]
	if len(args) == 1 {
		switch args[0] {
		case "cat":
			c.op = opDraftCategories
		case "back":
			c.op = opDraftBack
		case "hot":
			c.op = opDraftHot
		case "ok":
			c.op = opDraftSave
		case "no":
			c.op = opDraftCancel
		default:
			return callback{}, errBadCallback
		}
		return c, nil
	}
	if len(args) != 2 {
		return callback{}, errBadCallback
	}
	var ok bool
	switch args[0] {
	case "k":
		c.op = opDraftKind
		c.kind, ok = parseKind(args[1])
	case "f":
		c.op = opDraftField
		c.field, ok = parseField(args[1])
	case "c":
		c.op = opDraftCategory
		c.id, err = parseID(args[1], true)
		ok = err == nil
	}
	if !ok {
		return callback{}, errBadCallback
	}
	return c, nil
}

// parseID accepts a canonical positive decimal int64 (no sign, no leading
// zeros). Zero is accepted only when allowZero is set.
func parseID(s string, allowZero bool) (int64, error) {
	if !canonicalDigits(s) {
		return 0, errBadCallback
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || (v == 0 && !allowZero) {
		return 0, errBadCallback
	}
	return v, nil
}

func parsePage(s string) (int, error) {
	if !canonicalDigits(s) {
		return 0, errBadCallback
	}
	v, err := strconv.Atoi(s)
	if err != nil || v > maxPage {
		return 0, errBadCallback
	}
	return v, nil
}

func parseDraftID(s string) (uint32, error) {
	if !canonicalDigits(s) {
		return 0, errBadCallback
	}
	v, err := strconv.ParseUint(s, 10, 32)
	if err != nil || v == 0 {
		return 0, errBadCallback
	}
	return uint32(v), nil
}

// canonicalDigits reports whether s is a non-empty run of ASCII digits
// without a redundant leading zero.
func canonicalDigits(s string) bool {
	if s == "" || len(s) > 20 || (len(s) > 1 && s[0] == '0') {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
