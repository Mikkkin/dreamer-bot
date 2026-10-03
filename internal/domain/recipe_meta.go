package domain

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ---------------------------------------------------------------- tags --

// TagKind is one of the two independent ways to group recipes.
type TagKind string

const (
	// TagCuisine is the cuisine of a recipe (Русская, Азиатская…); a recipe
	// has at most one.
	TagCuisine TagKind = "cuisine"
	// TagCourse is the meal or course (Завтрак, Ужин, Первое, Десерт…); a
	// recipe may have several.
	TagCourse TagKind = "course"
)

// ParseTagKind validates a tag kind.
func ParseTagKind(raw string) (TagKind, error) {
	switch k := TagKind(raw); k {
	case TagCuisine, TagCourse:
		return k, nil
	default:
		return "", invalid("kind", "неизвестный вид тега")
	}
}

// RecipeTagID identifies a recipe tag.
type RecipeTagID int64

// MaxCoursesPerRecipe caps the course tags of one recipe.
const MaxCoursesPerRecipe = 8

// MaxRecipeTagsPerKind caps the cuisines and the courses (each), so the
// pickers in the Mini App and the bot's inline keyboard stay usable.
const MaxRecipeTagsPerKind = 30

// MaxCooksPerRecipe caps the cooking log of one recipe.
const MaxCooksPerRecipe = 500

// ErrRecipeTagGone reports a write that referenced a tag deleted meanwhile.
var ErrRecipeTagGone error = &ValidationError{Field: "cuisine_id", Message: "этой кухни или типа блюда уже нет — выберите другой"}

// RecipeTag is an editable label such as «🍜 Азиатская» or «🌙 Ужин».
type RecipeTag struct {
	ID        RecipeTagID
	Kind      TagKind
	Name      string
	Emoji     string
	Position  int
	CreatedAt time.Time
}

// NewRecipeTag validates and builds a tag.
func NewRecipeTag(kind TagKind, name, emoji string, now time.Time) (RecipeTag, error) {
	k, err := ParseTagKind(string(kind))
	if err != nil {
		return RecipeTag{}, err
	}
	t := RecipeTag{Kind: k, CreatedAt: now.UTC()}
	if err := t.rename(name, emoji); err != nil {
		return RecipeTag{}, err
	}
	return t, nil
}

func (t *RecipeTag) rename(name, emoji string) error {
	n, err := NormalizeCategoryName(name)
	if err != nil {
		return err
	}
	e, err := NormalizeEmoji(emoji)
	if err != nil {
		return err
	}
	t.Name, t.Emoji = n, e
	return nil
}

// RecipeTagPatch is a partial update of a tag; the kind never changes.
type RecipeTagPatch struct {
	Name  Optional[string]
	Emoji Optional[string]
}

// Apply merges the patch, validates the result and only then mutates t.
func (t *RecipeTag) Apply(p RecipeTagPatch) error {
	name, emoji := t.Name, t.Emoji
	if p.Name.Set {
		name = p.Name.Value
	}
	if p.Emoji.Set {
		emoji = p.Emoji.Value
	}
	next := *t
	if err := next.rename(name, emoji); err != nil {
		return err
	}
	*t = next
	return nil
}

// Label is the emoji and the name, e.g. "🍜 Азиатская".
func (t RecipeTag) Label() string { return t.Emoji + " " + t.Name }

// DefaultRecipeTags are seeded once, when the recipe tags are introduced.
func DefaultRecipeTags() []RecipeTag {
	seed := []struct {
		kind        TagKind
		emoji, name string
	}{
		{TagCuisine, "🥟", "Русская"},
		{TagCuisine, "🥐", "Европейская"},
		{TagCuisine, "🍝", "Итальянская"},
		{TagCuisine, "🍜", "Азиатская"},
		{TagCuisine, "🍢", "Кавказская"},
		{TagCuisine, "🌮", "Мексиканская"},
		{TagCourse, "🍳", "Завтрак"},
		{TagCourse, "🍱", "Обед"},
		{TagCourse, "🌙", "Ужин"},
		{TagCourse, "🍲", "Первое"},
		{TagCourse, "🍛", "Второе"},
		{TagCourse, "🥗", "Салат"},
		{TagCourse, "🥨", "Перекус"},
		{TagCourse, "🍰", "Десерт"},
		{TagCourse, "🥤", "Напиток"},
	}
	out := make([]RecipeTag, len(seed))
	pos := map[TagKind]int{}
	for i, s := range seed {
		out[i] = RecipeTag{Kind: s.kind, Name: s.name, Emoji: s.emoji, Position: pos[s.kind]}
		pos[s.kind]++
	}
	return out
}

// normalizeCourses de-duplicates course ids keeping the first occurrence and
// enforces the per-recipe limit. Existence and kind are checked by the
// service against storage.
func normalizeCourses(ids []RecipeTagID) ([]RecipeTagID, error) {
	out := make([]RecipeTagID, 0, len(ids))
	seen := make(map[RecipeTagID]bool, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return nil, invalid("course_ids", "неизвестный тип блюда")
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	if len(out) > MaxCoursesPerRecipe {
		return nil, invalid("course_ids", "не больше 8 типов блюда на рецепт")
	}
	return out, nil
}

// --------------------------------------------------------- ingredients --

// MaxIngredientsPerRecipe caps the ingredient list of one recipe.
const MaxIngredientsPerRecipe = 50

// Ingredient is one line of a recipe's ingredient list.
type Ingredient struct {
	Name     string
	Quantity *Quantity // nil = not specified
}

// NormalizeIngredients validates the list, keeping its order. Units are
// brought to their codes («чайные ложки» becomes «ч. л.»), and the
// quantities are copied, so the result never shares them with the input.
func NormalizeIngredients(in []Ingredient) ([]Ingredient, error) {
	if len(in) > MaxIngredientsPerRecipe {
		return nil, invalid("ingredients", "не больше 50 ингредиентов")
	}
	out := make([]Ingredient, 0, len(in))
	for _, ing := range in {
		name, err := NormalizeItemName(ing.Name)
		if err != nil {
			return nil, invalid("ingredients", "у каждого ингредиента должно быть название (до 80 символов)")
		}
		var qty *Quantity
		if q := ing.Quantity; q != nil {
			unit, err := ParseUnit(string(q.Unit))
			if err != nil {
				return nil, invalid("ingredients", "неизвестная единица у «"+name+"»")
			}
			if q.Hundredths < 0 || q.Hundredths > MaxQuantityHundredths || (unit == UnitToTaste && q.Hundredths != 0) {
				return nil, invalid("ingredients", "неверное количество у «"+name+"»")
			}
			if q.Hundredths != 0 || unit != "" {
				qty = &Quantity{Hundredths: q.Hundredths, Unit: unit}
			}
		}
		out = append(out, Ingredient{Name: name, Quantity: qty})
	}
	return out, nil
}

// -------------------------------------------------------------- КБЖУ --

// Nutrition limits. Values are stored in tenths (one decimal place).
const (
	tenthsPerUnit      = 10
	maxKcalTenths      = 900 * tenthsPerUnit // pure fat is ~900 kcal / 100 g
	maxMacroTenths     = 100 * tenthsPerUnit // grams per 100 g
	MaxDishWeightGrams = 20_000
	MaxServings        = 50
)

// Nutrition is the КБЖУ of a recipe: calories and protein/fat/carbs per
// 100 g (in tenths), plus the optional weight of the whole cooked dish and
// the number of servings, from which per-dish and per-serving values follow.
// Inside a Recipe, Servings always mirrors Recipe.Servings.
type Nutrition struct {
	KcalPer100    int // tenths of kcal
	ProteinPer100 int // tenths of a gram
	FatPer100     int
	CarbsPer100   int
	WeightGrams   int // 0 = unknown
	Servings      int // 0 = unknown; inside a Recipe, a copy of Recipe.Servings
}

// ParseTenths parses a non-negative decimal with at most one decimal place
// ("12", "12.5", "12,5") into tenths.
func ParseTenths(raw, field string, maxTenths int) (int, error) {
	s := strings.ReplaceAll(strings.TrimSpace(raw), ",", ".")
	if s == "" {
		return 0, nil
	}
	intPart, frac, _ := strings.Cut(s, ".")
	if intPart == "" || !isDigits(intPart) || !isDigits(frac) || len(frac) > 1 || len(intPart) > 5 {
		return 0, invalid(field, "число с одним знаком после запятой, например 12,5")
	}
	units, _ := strconv.Atoi(intPart)
	f := 0
	if frac != "" {
		f, _ = strconv.Atoi(frac)
	}
	v := units*tenthsPerUnit + f
	if v > maxTenths {
		return 0, invalid(field, "слишком большое значение")
	}
	return v, nil
}

// NewNutrition validates КБЖУ per 100 g given as decimal strings plus the
// optional dish weight (grams) and servings.
func NewNutrition(kcal, protein, fat, carbs string, weightGrams, servings int) (Nutrition, error) {
	var n Nutrition
	var err error
	if n.KcalPer100, err = ParseTenths(kcal, "kcal", maxKcalTenths); err != nil {
		return Nutrition{}, err
	}
	if n.ProteinPer100, err = ParseTenths(protein, "protein", maxMacroTenths); err != nil {
		return Nutrition{}, err
	}
	if n.FatPer100, err = ParseTenths(fat, "fat", maxMacroTenths); err != nil {
		return Nutrition{}, err
	}
	if n.CarbsPer100, err = ParseTenths(carbs, "carbs", maxMacroTenths); err != nil {
		return Nutrition{}, err
	}
	n.WeightGrams, n.Servings = weightGrams, servings
	if err := n.Validate(); err != nil {
		return Nutrition{}, err
	}
	// A blank value is unknown, not zero: all four are required (0 is fine).
	for _, v := range [...]struct{ raw, field string }{{kcal, "kcal"}, {protein, "protein"}, {fat, "fat"}, {carbs, "carbs"}} {
		if strings.TrimSpace(v.raw) == "" {
			return Nutrition{}, invalid(v.field, "укажите все четыре значения КБЖУ — можно 0")
		}
	}
	return n, nil
}

// Validate checks every field against its range.
func (n Nutrition) Validate() error {
	switch {
	case n.KcalPer100 < 0 || n.KcalPer100 > maxKcalTenths:
		return invalid("kcal", "калорийность от 0 до 900 ккал на 100 г")
	case n.ProteinPer100 < 0 || n.ProteinPer100 > maxMacroTenths,
		n.FatPer100 < 0 || n.FatPer100 > maxMacroTenths,
		n.CarbsPer100 < 0 || n.CarbsPer100 > maxMacroTenths:
		return invalid("nutrition", "белки, жиры и углеводы — от 0 до 100 г на 100 г")
	case n.WeightGrams < 0 || n.WeightGrams > MaxDishWeightGrams:
		return invalid("weight_g", "вес блюда от 1 до 20 000 г")
	case n.Servings < 0 || n.Servings > MaxServings:
		return invalid("servings", "порций от 1 до 50")
	}
	return nil
}

// Macros is one КБЖУ set in tenths.
type Macros struct{ Kcal, Protein, Fat, Carbs int }

// Per100 returns the per-100 g values.
func (n Nutrition) Per100() Macros {
	return Macros{n.KcalPer100, n.ProteinPer100, n.FatPer100, n.CarbsPer100}
}

// PerDish returns the values for the whole dish; ok is false without a weight.
func (n Nutrition) PerDish() (Macros, bool) {
	if n.WeightGrams <= 0 {
		return Macros{}, false
	}
	scale := func(v int) int { return roundDiv(v*n.WeightGrams, 100) }
	return Macros{scale(n.KcalPer100), scale(n.ProteinPer100), scale(n.FatPer100), scale(n.CarbsPer100)}, true
}

// PerServing returns the values for one serving; ok is false without both
// a weight and a number of servings.
func (n Nutrition) PerServing() (Macros, bool) {
	dish, ok := n.PerDish()
	if !ok || n.Servings <= 0 {
		return Macros{}, false
	}
	div := func(v int) int { return roundDiv(v, n.Servings) }
	return Macros{div(dish.Kcal), div(dish.Protein), div(dish.Fat), div(dish.Carbs)}, true
}

// FormatTenths renders tenths as "12,5" or "12" (Russian decimal comma).
func FormatTenths(v int) string {
	s := strconv.Itoa(v / tenthsPerUnit)
	if f := v % tenthsPerUnit; f != 0 {
		s += "," + strconv.Itoa(f)
	}
	return s
}

// DecimalTenths renders tenths as a canonical machine decimal ("12.5").
func DecimalTenths(v int) string {
	s := strconv.Itoa(v / tenthsPerUnit)
	if f := v % tenthsPerUnit; f != 0 {
		s += "." + strconv.Itoa(f)
	}
	return s
}

func roundDiv(a, b int) int { return (a + b/2) / b }

// ---------------------------------------------------- cooking & rating --

// CookID identifies one "we cooked it" entry.
type CookID int64

// MaxRatingCommentLen caps the comment of a rating.
const MaxRatingCommentLen = 280

// Rating is one person's stars for one cooking.
type Rating struct {
	UserID  UserID
	Stars   int // 1..5
	Comment string
	RatedAt time.Time
}

// NewRating validates stars and the optional comment.
func NewRating(user UserID, stars int, comment string, now time.Time) (Rating, error) {
	if stars < 1 || stars > 5 {
		return Rating{}, invalid("stars", "оценка от 1 до 5 звёзд")
	}
	c := cleanText(comment, true)
	if utf8.RuneCountInString(c) > MaxRatingCommentLen {
		return Rating{}, invalid("comment", "комментарий слишком длинный (максимум 280 символов)")
	}
	return Rating{UserID: user, Stars: stars, Comment: c, RatedAt: now.UTC()}, nil
}

// Cook is one time a recipe was cooked, with the ratings each person gave.
// Recipes stay in the list after cooking; cooks form their history.
type Cook struct {
	ID       CookID
	RecipeID RecipeID
	CookedBy UserID
	CookedAt time.Time
	Ratings  []Rating
}

// CookingSummary aggregates a recipe's cooking history for lists and cards.
type CookingSummary struct {
	Count        int
	LastCookedAt *time.Time
	RatingSum    int
	RatingCount  int
}

// AverageTenths is the average rating in tenths of a star (e.g. 45 = 4.5),
// ok is false when nobody has rated the recipe yet.
func (s CookingSummary) AverageTenths() (int, bool) {
	if s.RatingCount == 0 {
		return 0, false
	}
	return roundDiv(s.RatingSum*tenthsPerUnit, s.RatingCount), true
}
