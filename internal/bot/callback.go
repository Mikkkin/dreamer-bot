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
//	w:m:<id>               wish: put money aside (asks for the amount)
//	r:o|d|y|n:<id>         recipe: open, ask delete, delete, keep
//	r:c:<id>               recipe: «Приготовили», show the stars row
//	r:c:<id>:<0-5>         recipe: cooked, with stars (0 = without a rating)
//	r:b:<id>               recipe: back to the card buttons
//	r:s:<id>               recipe: add every ingredient to the shopping list
//	r:v:<id>:<cook>:<1-5>  recipe: rate a cooking (partner notification)
//	i:d|n:<id>             imported recipe: ask delete, keep (deleting is r:y)
//	k:<id>                 another random recipe than <id>
//	s:l:<page>             shopping list page
//	s:k|u:<item>:<page>    shopping item bought / not bought
//	s:x                    shopping: remove the bought items
//	d:<draft>:k:<w|r>      draft: switch kind
//	d:<draft>:cat          draft: show categories
//	d:<draft>:c:<id>       draft: choose category (0 = none)
//	d:<draft>:cu           draft: show cuisines
//	d:<draft>:cu:<id>      draft: choose cuisine (0 = none)
//	d:<draft>:co           draft: show courses
//	d:<draft>:co:<id>      draft: toggle course (0 = none)
//	d:<draft>:back         draft: back to the card
//	d:<draft>:f:<t|p|l|b>  draft: ask for title, price, link or text
//	d:<draft>:hot          draft: toggle «очень хочу»
//	d:<draft>:imp          draft: import its text as a recipe
//	d:<draft>:ok           draft: save
//	d:<draft>:no           draft: discard
const maxCallbackData = 64

// maxPage bounds page numbers so a forged payload cannot request absurd
// offsets.
const maxPage = 9999

// maxStars is the best rating; stars travel as a single digit.
const maxStars = 5

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
	opWishSave
	opRecipeOpen
	opRecipeAskDelete
	opRecipeDelete
	opRecipeKeep
	opRecipeCookAsk
	opRecipeCook
	opRecipeBack
	opRecipeShop
	opRecipeRate
	opImportAskDelete
	opImportKeep
	opCookAgain
	opShopList
	opShopCheck
	opShopUncheck
	opShopClear
	opDraftKind
	opDraftCategories
	opDraftCategory
	opDraftCuisines
	opDraftCuisine
	opDraftCourses
	opDraftCourse
	opDraftBack
	opDraftField
	opDraftHot
	opDraftSave
	opDraftCancel
	opDraftImport
)

// callback is a decoded button payload. Which fields are meaningful depends
// on op; the others stay zero.
type callback struct {
	op     cbOp
	id     int64 // wish, recipe, category, tag or shopping item ID
	cook   int64 // cooking ID of a rating
	stars  int   // 0..5, 0 = cooked without a rating
	draft  uint32
	status domain.Status
	page   int
	kind   entityKind
	field  draftField
}

var errBadCallback = errors.New("bot: malformed callback data")

// String encodes the callback. It never returns more than 64 bytes: an
// unknown op, out-of-range stars or an oversized payload (impossible with
// numeric IDs) degrade to a no-op button.
func (c callback) String() string {
	s := c.encode()
	if s == "" || len(s) > maxCallbackData {
		return "x"
	}
	return s
}

func (c callback) encode() string {
	id := strconv.FormatInt(c.id, 10)
	page := strconv.Itoa(c.page)
	draft := "d:" + strconv.FormatUint(uint64(c.draft), 10) + ":"
	switch c.op {
	case opNoop:
		return "x"
	case opWishList:
		return "l:w:" + string(c.status) + ":" + page
	case opRecipeList:
		return "l:r:" + page
	case opWishStatus:
		return "w:s:" + id + ":" + string(c.status)
	case opRecipeCook:
		if c.stars < 0 || c.stars > maxStars {
			return ""
		}
		return "r:c:" + id + ":" + strconv.Itoa(c.stars)
	case opRecipeRate:
		if c.stars < 1 || c.stars > maxStars {
			return ""
		}
		return "r:v:" + id + ":" + strconv.FormatInt(c.cook, 10) + ":" + strconv.Itoa(c.stars)
	case opCookAgain:
		return "k:" + id
	case opShopList:
		return "s:l:" + page
	case opShopCheck:
		return "s:k:" + id + ":" + page
	case opShopUncheck:
		return "s:u:" + id + ":" + page
	case opShopClear:
		return "s:x"
	case opDraftKind:
		return draft + "k:" + kindCode(c.kind)
	case opDraftCategories:
		return draft + "cat"
	case opDraftCategory:
		return draft + "c:" + id
	case opDraftCuisines:
		return draft + "cu"
	case opDraftCuisine:
		return draft + "cu:" + id
	case opDraftCourses:
		return draft + "co"
	case opDraftCourse:
		return draft + "co:" + id
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
	case opDraftImport:
		return draft + "imp"
	}
	if entity, action, ok := entityAction(c.op); ok {
		return entity + ":" + action + ":" + id
	}
	return ""
}

// entityOps are the ops encoded as <entity>:<action>:<id>.
var entityOps = [...]cbOp{
	opWishOpen, opWishAskDelete, opWishDelete, opWishKeep, opWishSave,
	opRecipeOpen, opRecipeAskDelete, opRecipeDelete, opRecipeKeep,
	opRecipeCookAsk, opRecipeBack, opRecipeShop,
	opImportAskDelete, opImportKeep,
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
	case opWishSave:
		return "w", "m", true
	case opRecipeOpen:
		return "r", "o", true
	case opRecipeAskDelete:
		return "r", "d", true
	case opRecipeDelete:
		return "r", "y", true
	case opRecipeKeep:
		return "r", "n", true
	case opRecipeCookAsk:
		return "r", "c", true
	case opRecipeBack:
		return "r", "b", true
	case opRecipeShop:
		return "r", "s", true
	case opImportAskDelete:
		return "i", "d", true
	case opImportKeep:
		return "i", "n", true
	}
	return "", "", false
}

func entityOp(entity, action string) (cbOp, bool) {
	for _, op := range entityOps {
		if e, a, _ := entityAction(op); e == entity && a == action {
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
	case "w", "r", "i":
		return parseEntityCallback(p)
	case "s":
		return parseShopCallback(p)
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
	case len(p) == 4 && p[0] == "r" && p[1] == "c":
		id, err := parseID(p[2], false)
		if err != nil {
			return callback{}, err
		}
		stars, err := parseStars(p[3], 0)
		if err != nil {
			return callback{}, err
		}
		return callback{op: opRecipeCook, id: id, stars: stars}, nil
	case len(p) == 5 && p[0] == "r" && p[1] == "v":
		id, err := parseID(p[2], false)
		if err != nil {
			return callback{}, err
		}
		cook, err := parseID(p[3], false)
		if err != nil {
			return callback{}, err
		}
		stars, err := parseStars(p[4], 1)
		if err != nil {
			return callback{}, err
		}
		return callback{op: opRecipeRate, id: id, cook: cook, stars: stars}, nil
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

func parseShopCallback(p []string) (callback, error) {
	switch {
	case len(p) == 2 && p[1] == "x":
		return callback{op: opShopClear}, nil
	case len(p) == 3 && p[1] == "l":
		page, err := parsePage(p[2])
		return callback{op: opShopList, page: page}, err
	case len(p) == 4 && (p[1] == "k" || p[1] == "u"):
		id, err := parseID(p[2], false)
		if err != nil {
			return callback{}, err
		}
		page, err := parsePage(p[3])
		if err != nil {
			return callback{}, err
		}
		op := opShopCheck
		if p[1] == "u" {
			op = opShopUncheck
		}
		return callback{op: op, id: id, page: page}, nil
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
		case "cu":
			c.op = opDraftCuisines
		case "co":
			c.op = opDraftCourses
		case "back":
			c.op = opDraftBack
		case "hot":
			c.op = opDraftHot
		case "ok":
			c.op = opDraftSave
		case "no":
			c.op = opDraftCancel
		case "imp":
			c.op = opDraftImport
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
	case "c", "cu", "co":
		c.op = draftPickOp(args[0])
		c.id, err = parseID(args[1], true)
		ok = err == nil
	}
	if !ok {
		return callback{}, errBadCallback
	}
	return c, nil
}

// draftPickOp maps the token of a draft chip (category, cuisine, course).
func draftPickOp(token string) cbOp {
	switch token {
	case "cu":
		return opDraftCuisine
	case "co":
		return opDraftCourse
	}
	return opDraftCategory
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

// parseStars accepts a single digit from lowest to maxStars.
func parseStars(s string, lowest int) (int, error) {
	if len(s) != 1 || s[0] < '0'+byte(lowest) || s[0] > '0'+maxStars {
		return 0, errBadCallback
	}
	return int(s[0] - '0'), nil
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
