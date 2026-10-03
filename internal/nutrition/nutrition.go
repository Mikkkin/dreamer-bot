// Package nutrition estimates КБЖУ (energy, protein, fat, carbohydrates) of
// a recipe from its ingredient list, using the built-in food table in
// data/foods.json (USDA FoodData Central SR Legacy plus typical Russian
// label values; see data/README.md).
//
// The result is an estimate: names are matched to foods by a cautious rule
// set (match.go), household measures are converted with typical weights,
// and every ingredient that could not be counted is reported in Coverage
// instead of being silently treated as zero.
package nutrition

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

//go:embed data/foods.json
var foodsJSON []byte

// Values are energy (kcal) and macros (grams) in tenths.
type Values struct {
	Kcal, Protein, Fat, Carbs int
}

// Food is one entry of the table. Per100 is per 100 g of the edible part
// in the state the recipe names it (dry grains and pasta, raw meat).
type Food struct {
	ID       string
	Name     string
	Category string
	Per100   Values

	per100   [4]float64         // kcal, protein, fat, carbs per 100 g
	grams    map[string]float64 // grams in one unit; a missing unit is unknown, never 0
	percents []float64          // fat percentages named by the name and aliases
	family   int                // index into Table.families, or -1
}

// Item is one counted ingredient.
type Item struct {
	Name   string // as written in the recipe
	Food   string // the matched food's name
	Grams  int
	Values Values
}

// Coverage tells how much of the recipe the estimate covers. Total counts
// every ingredient except the Skipped ones («по вкусу»), so
// Counted + len(Missing) + len(NoAmount) == Total.
type Coverage struct {
	Counted, Total int
	// Missing are the names that match no food (or match ambiguously).
	Missing []string
	// NoAmount are matched names without an amount, or with an amount in a
	// measure that has no known weight for that food (see NoMeasure).
	NoAmount []string
	// Skipped are the «по вкусу» ingredients, left out of Total.
	Skipped []string
	// NoMeasure is the part of NoAmount that does have an amount, but in a
	// unit without a gram weight for the food («2 шт» of flour).
	NoMeasure []string
}

// Result is the estimate for a recipe. Per100 is per 100 g of the counted
// weight; PerServing is nil when the number of servings is unknown.
type Result struct {
	Items       []Item
	Total       Values
	WeightGrams int
	Per100      Values
	PerServing  *Values
	Coverage    Coverage
}

// Table is a parsed food table with its matching indexes. It is read-only
// and safe for concurrent use.
type Table struct {
	foods    []Food
	aliases  []alias
	exact    map[string][]int // stem sequence → aliases
	byStem   map[string][]int // stem → aliases containing it
	single   map[string][]int // one-stem alias → foods
	families [][]int          // fat variants of one product
}

type alias struct {
	food  int
	stems []string
	bare  bool // names no percentage
}

var (
	defaultOnce  sync.Once
	defaultTable *Table
)

// Default returns the embedded table, parsed on first use.
func Default() *Table {
	defaultOnce.Do(func() {
		t, err := parse(foodsJSON)
		if err != nil {
			panic("nutrition: embedded foods.json: " + err.Error())
		}
		defaultTable = t
	})
	return defaultTable
}

func parse(data []byte) (*Table, error) {
	var raw struct {
		Foods []struct {
			ID       string             `json:"id"`
			Name     string             `json:"name"`
			Aliases  []string           `json:"aliases"`
			Category string             `json:"category"`
			Per100   map[string]float64 `json:"per100"`
			Grams    map[string]float64 `json:"grams"`
		} `json:"foods"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if len(raw.Foods) == 0 {
		return nil, fmt.Errorf("no foods")
	}
	t := &Table{
		foods:  make([]Food, 0, len(raw.Foods)),
		exact:  make(map[string][]int),
		byStem: make(map[string][]int),
		single: make(map[string][]int),
	}
	familyOf := make(map[string]int)
	for i, rf := range raw.Foods {
		if rf.ID == "" || rf.Name == "" {
			return nil, fmt.Errorf("food #%d: no id or name", i)
		}
		f := Food{ID: rf.ID, Name: rf.Name, Category: rf.Category, grams: rf.Grams, family: -1}
		for j, k := range [4]string{"kcal", "protein", "fat", "carbs"} {
			v, ok := rf.Per100[k]
			if !ok || v < 0 || math.IsNaN(v) {
				return nil, fmt.Errorf("food %s: bad per100.%s", rf.ID, k)
			}
			f.per100[j] = v
		}
		f.Per100 = tenths(f.per100)
		for u, g := range rf.Grams {
			if !(g > 0) {
				return nil, fmt.Errorf("food %s: grams[%q] must be positive", rf.ID, u)
			}
		}
		for n, name := range append([]string{rf.Name}, rf.Aliases...) {
			q := analyze(name)
			stems := aliasStems(q)
			if len(stems) == 0 {
				continue
			}
			if q.hasPct && !slices.Contains(f.percents, q.percent) {
				f.percents = append(f.percents, q.percent)
			}
			if n == 0 && q.hasPct {
				// The name carries a fat level: group it with the other
				// levels of the same product.
				sorted := slices.Clone(stems)
				sort.Strings(sorted)
				key := strings.Join(sorted, " ")
				fam, ok := familyOf[key]
				if !ok {
					fam = len(t.families)
					familyOf[key] = fam
					t.families = append(t.families, nil)
				}
				f.family = fam
				t.families[fam] = append(t.families[fam], i)
			}
			ai := len(t.aliases)
			t.aliases = append(t.aliases, alias{food: i, stems: stems, bare: !q.hasPct})
			t.exact[q.key] = append(t.exact[q.key], ai)
			for _, s := range stems {
				t.byStem[s] = append(t.byStem[s], ai)
			}
			if len(stems) == 1 && !slices.Contains(t.single[stems[0]], i) {
				t.single[stems[0]] = append(t.single[stems[0]], i)
			}
		}
		t.foods = append(t.foods, f)
	}
	return t, nil
}

// Grams converts an amount of food f to grams, rounded. ok is false when
// the unit has no known weight for that food, or there is no amount.
func (t *Table) Grams(f Food, q domain.Quantity) (int, bool) {
	g, ok := grams(f, q)
	if !ok {
		return 0, false
	}
	return int(math.Round(g)), true
}

// maxBarePieces bounds how large a bare number may be and still be read as
// a count of pieces («Яйца 3»); «Картофель 500» is more likely grams, so
// it is reported as an unknown measure instead of 50 kg.
const maxBarePieces = 30

// grams converts g, кг, мл and л directly (мл by the food's density, else
// as water) and every other unit by the food's own measure from the table.
// A bare number counts pieces. «упаковка» falls back to the food's can,
// sachet or bar.
func grams(f Food, q domain.Quantity) (float64, bool) {
	if q.Hundredths <= 0 {
		return 0, false
	}
	amount := float64(q.Hundredths) / 100
	unit := canonicalUnit(q.Unit)
	switch unit {
	case domain.UnitGram:
		return amount, true
	case domain.UnitKilogram:
		return amount * 1000, true
	case domain.UnitMilliliter:
		return amount * f.density(), true
	case domain.UnitLiter:
		return amount * 1000 * f.density(), true
	case domain.UnitToTaste:
		return 0, false
	case "":
		if amount > maxBarePieces {
			return 0, false
		}
		unit = domain.UnitPiece
	case domain.UnitPack:
		for _, u := range [...]string{"упаковка", "банка", "пакетик", "плитка"} {
			if g, ok := f.grams[u]; ok {
				return amount * g, true
			}
		}
		return 0, false
	}
	g, ok := f.grams[string(unit)]
	if !ok {
		return 0, false
	}
	return amount * g, true
}

// density is grams per millilitre; foods without one are taken as water.
func (f Food) density() float64 {
	if d, ok := f.grams["мл"]; ok {
		return d
	}
	return 1
}

// unitSpellings maps compact spellings (lower case, no dots, spaces,
// slashes or hyphens) to unit codes, so Grams also accepts a unit that has
// not been through domain validation.
var unitSpellings = func() map[string]domain.Unit {
	m := make(map[string]domain.Unit)
	for u, forms := range map[domain.Unit][]string{
		domain.UnitGram:       {"г", "гр", "грамм", "грамма", "граммов", "граммы"},
		domain.UnitKilogram:   {"кг", "килограмм", "килограмма", "килограммов"},
		domain.UnitMilliliter: {"мл", "миллилитр", "миллилитра", "миллилитров"},
		domain.UnitLiter:      {"л", "литр", "литра", "литров"},
		domain.UnitPiece:      {"шт", "штука", "штуки", "штук"},
		domain.UnitTablespoon: {"стл", "столоваяложка", "столовыеложки", "столовыхложек", "столовойложки", "стложка", "стложки"},
		domain.UnitTeaspoon:   {"чл", "чайнаяложка", "чайныеложки", "чайныхложек", "чайнойложки", "чложка", "чложки"},
		domain.UnitCup:        {"стакан", "стакана", "стаканов"},
		domain.UnitPinch:      {"щепотка", "щепотки", "щепоток", "щепотку"},
		domain.UnitClove:      {"зубчик", "зубчика", "зубчиков", "зуб"},
		domain.UnitBunch:      {"пучок", "пучка", "пучков"},
		domain.UnitPack:       {"упаковка", "упаковки", "упаковок", "уп"},
		domain.UnitToTaste:    {"повкусу"},
	} {
		for _, s := range forms {
			m[s] = u
		}
	}
	return m
}()

var unitCompactor = strings.NewReplacer(".", "", " ", "", "\u00a0", "", "/", "", "-", "", "ё", "е")

func canonicalUnit(u domain.Unit) domain.Unit {
	s := unitCompactor.Replace(strings.ToLower(string(u)))
	if c, ok := unitSpellings[s]; ok {
		return c
	}
	return domain.Unit(s)
}

// toTaste reports an ingredient measured «по вкусу», either as its unit or,
// with no amount, inside its name («Соль по вкусу»).
func toTaste(ing domain.Ingredient) bool {
	if q := ing.Quantity; q != nil {
		if canonicalUnit(q.Unit) == domain.UnitToTaste {
			return true
		}
		if q.Hundredths > 0 {
			return false
		}
	}
	s := string(cleanRunes(ing.Name))
	return strings.Contains(s, "по вкусу") || strings.Contains(s, "по-вкусу")
}

// Compute estimates the recipe's КБЖУ. servings ≤ 0 means unknown and
// leaves PerServing nil.
func (t *Table) Compute(ings []domain.Ingredient, servings int) Result {
	res := Result{
		Items: []Item{},
		Coverage: Coverage{
			Missing: []string{}, NoAmount: []string{}, Skipped: []string{}, NoMeasure: []string{},
		},
	}
	var (
		total  [4]float64
		weight float64
	)
	for _, ing := range ings {
		name := strings.TrimSpace(ing.Name)
		cov := &res.Coverage
		if toTaste(ing) {
			cov.Skipped = append(cov.Skipped, name)
			continue
		}
		cov.Total++
		fi, ok := t.match(name)
		if !ok {
			cov.Missing = append(cov.Missing, name)
			continue
		}
		f := t.foods[fi]
		q := ing.Quantity
		if q == nil || q.Hundredths <= 0 {
			cov.NoAmount = append(cov.NoAmount, name)
			continue
		}
		g, ok := grams(f, *q)
		if !ok {
			cov.NoAmount = append(cov.NoAmount, name)
			cov.NoMeasure = append(cov.NoMeasure, name)
			continue
		}
		var v [4]float64
		for j := range v {
			v[j] = f.per100[j] * g / 100
			total[j] += v[j]
		}
		weight += g
		cov.Counted++
		res.Items = append(res.Items, Item{Name: name, Food: f.Name, Grams: int(math.Round(g)), Values: tenths(v)})
	}
	res.Total = tenths(total)
	res.WeightGrams = int(math.Round(weight))
	if weight > 0 {
		var p [4]float64
		for j := range p {
			p[j] = total[j] * 100 / weight
		}
		res.Per100 = tenths(p)
	}
	if servings > 0 {
		var s [4]float64
		for j := range s {
			s[j] = total[j] / float64(servings)
		}
		ps := tenths(s)
		res.PerServing = &ps
	}
	return res
}

// tenths rounds kcal, protein, fat and carbs to tenths.
func tenths(v [4]float64) Values {
	r := func(x float64) int { return int(math.Round(x * 10)) }
	return Values{Kcal: r(v[0]), Protein: r(v[1]), Fat: r(v[2]), Carbs: r(v[3])}
}
