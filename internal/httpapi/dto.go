package httpapi

import (
	"context"
	"fmt"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/auth"
	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/service"
	"github.com/Mikkkin/dreamer-bot/internal/stores"
)

// Wire formats of docs/API.md. IDs are JSON numbers: Telegram user IDs and
// SQLite row IDs stay below 2^53, so JavaScript reads them exactly.

type personJSON struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type priceJSON struct {
	Amount    string `json:"amount"`
	Currency  string `json:"currency"`
	Formatted string `json:"formatted"`
}

type imageJSON struct {
	ID       int64  `json:"id"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	ThumbURL string `json:"thumb_url"`
	FullURL  string `json:"full_url"`
}

// savedJSON is the «Копим» progress of a wish. Percent is null without a
// price. Count is the number of contributions; wish lists leave it null
// because the list query aggregates only the total (see writeWish).
type savedJSON struct {
	Total   priceJSON `json:"total"`
	Percent *int      `json:"percent"`
	Count   *int      `json:"count"`
}

type wishJSON struct {
	ID          int64       `json:"id"`
	Title       string      `json:"title"`
	Note        string      `json:"note"`
	CategoryID  *int64      `json:"category_id"`
	Link        *string     `json:"link"`
	Price       *priceJSON  `json:"price"`
	Status      string      `json:"status"`
	Hot         bool        `json:"hot"`
	Saved       *savedJSON  `json:"saved"`
	Author      personJSON  `json:"author"`
	Images      []imageJSON `json:"images"`
	CreatedAt   string      `json:"created_at"`
	UpdatedAt   string      `json:"updated_at"`
	FulfilledAt *string     `json:"fulfilled_at"`
}

type savingJSON struct {
	ID        int64      `json:"id"`
	WishID    int64      `json:"wish_id"`
	Amount    priceJSON  `json:"amount"`
	User      personJSON `json:"user"`
	Note      string     `json:"note"`
	CreatedAt string     `json:"created_at"`
}

type ingredientJSON struct {
	Name      string  `json:"name"`
	Amount    *string `json:"amount"`
	Unit      *string `json:"unit"`
	Formatted string  `json:"formatted"`
}

// macrosJSON is one КБЖУ set as decimal strings with at most one decimal.
type macrosJSON struct {
	Kcal    string `json:"kcal"`
	Protein string `json:"protein"`
	Fat     string `json:"fat"`
	Carbs   string `json:"carbs"`
}

type nutritionJSON struct {
	Per100g    macrosJSON  `json:"per_100g"`
	WeightG    *int        `json:"weight_g"`
	Servings   *int        `json:"servings"`
	PerDish    *macrosJSON `json:"per_dish"`
	PerServing *macrosJSON `json:"per_serving"`
}

type cookingJSON struct {
	Count        int     `json:"count"`
	LastCookedAt *string `json:"last_cooked_at"`
	RatingAvg    *string `json:"rating_avg"`
	RatingCount  int     `json:"rating_count"`
}

type recipeJSON struct {
	ID          int64            `json:"id"`
	Title       string           `json:"title"`
	Link        *string          `json:"link"`
	Body        string           `json:"body"`
	CuisineID   *int64           `json:"cuisine_id"`
	CourseIDs   []int64          `json:"course_ids"`
	Ingredients []ingredientJSON `json:"ingredients"`
	Nutrition   *nutritionJSON   `json:"nutrition"`
	Cooking     cookingJSON      `json:"cooking"`
	Author      personJSON       `json:"author"`
	Images      []imageJSON      `json:"images"`
	CreatedAt   string           `json:"created_at"`
	UpdatedAt   string           `json:"updated_at"`
}

type recipeTagJSON struct {
	ID       int64  `json:"id"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Emoji    string `json:"emoji"`
	Position int    `json:"position"`
}

type ratingJSON struct {
	User    personJSON `json:"user"`
	Stars   int        `json:"stars"`
	Comment string     `json:"comment"`
	RatedAt string     `json:"rated_at"`
}

type cookJSON struct {
	ID       int64        `json:"id"`
	RecipeID int64        `json:"recipe_id"`
	CookedBy personJSON   `json:"cooked_by"`
	CookedAt string       `json:"cooked_at"`
	Ratings  []ratingJSON `json:"ratings"`
}

type quantityJSON struct {
	Amount    *string `json:"amount"`
	Unit      *string `json:"unit"`
	Formatted string  `json:"formatted"`
}

type shoppingItemJSON struct {
	ID        int64         `json:"id"`
	Name      string        `json:"name"`
	Quantity  *quantityJSON `json:"quantity"`
	Checked   bool          `json:"checked"`
	RecipeID  *int64        `json:"recipe_id"`
	AddedBy   personJSON    `json:"added_by"`
	CreatedAt string        `json:"created_at"`
	UpdatedAt string        `json:"updated_at"`
}

type storeJSON struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Emoji             string `json:"emoji"`
	SearchURLTemplate string `json:"search_url_template"`
	OpensApp          bool   `json:"opens_app"`
	Cart              bool   `json:"cart"`
}

type categoryJSON struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Emoji    string `json:"emoji"`
	Position int    `json:"position"`
}

type totalsJSON struct {
	Count int         `json:"count"`
	Sums  []priceJSON `json:"sums"`
}

type categoryStatsJSON struct {
	Category *categoryJSON         `json:"category"`
	ByStatus map[string]totalsJSON `json:"by_status"`
}

type statsJSON struct {
	Overall           map[string]totalsJSON `json:"overall"`
	Categories        []categoryStatsJSON   `json:"categories"`
	FulfilledThisYear int                   `json:"fulfilled_this_year"`
	Recipes           int                   `json:"recipes"`
	RecipesCooked     int                   `json:"recipes_cooked"`
	Saved             []priceJSON           `json:"saved"`
}

// presenter renders domain objects for one request. Author names are
// resolved from a single Users.List call per request.
type presenter struct {
	signer *auth.MediaSigner
	names  map[domain.UserID]string
}

func (s *server) presenter(ctx context.Context) (presenter, error) {
	users, err := s.svc.Users.List(ctx)
	if err != nil {
		return presenter{}, fmt.Errorf("list users: %w", err)
	}
	names := make(map[domain.UserID]string, len(users))
	for _, u := range users {
		names[u.ID] = u.DisplayName()
	}
	return presenter{signer: s.signer, names: names}, nil
}

func (p presenter) author(id domain.UserID) personJSON {
	name, ok := p.names[id]
	if !ok {
		name = domain.User{ID: id}.DisplayName()
	}
	return personJSON{ID: int64(id), Name: name}
}

func (p presenter) wish(w domain.Wish) wishJSON {
	out := wishJSON{
		ID:        int64(w.ID),
		Title:     w.Title,
		Note:      w.Note,
		Link:      w.Link,
		Status:    string(w.Status),
		Hot:       w.Hot,
		Author:    p.author(w.AuthorID),
		Images:    p.images(w.Images),
		CreatedAt: timestamp(w.CreatedAt),
		UpdatedAt: timestamp(w.UpdatedAt),
	}
	if w.CategoryID != nil {
		id := int64(*w.CategoryID)
		out.CategoryID = &id
	}
	if w.Price != nil {
		price := priceOf(*w.Price)
		out.Price = &price
	}
	if w.Saved != nil {
		saved := savedJSON{Total: priceOf(*w.Saved)}
		if pct, ok := w.SavedPercent(); ok {
			saved.Percent = &pct
		}
		out.Saved = &saved
	}
	if w.FulfilledAt != nil {
		at := timestamp(*w.FulfilledAt)
		out.FulfilledAt = &at
	}
	return out
}

func (p presenter) wishes(ws []domain.Wish) []wishJSON {
	out := make([]wishJSON, len(ws))
	for i, w := range ws {
		out[i] = p.wish(w)
	}
	return out
}

func (p presenter) saving(sv domain.Saving) savingJSON {
	return savingJSON{
		ID:        int64(sv.ID),
		WishID:    int64(sv.WishID),
		Amount:    priceOf(sv.Amount),
		User:      p.author(sv.UserID),
		Note:      sv.Note,
		CreatedAt: timestamp(sv.CreatedAt),
	}
}

func (p presenter) savings(ss []domain.Saving) []savingJSON {
	out := make([]savingJSON, len(ss))
	for i, sv := range ss {
		out[i] = p.saving(sv)
	}
	return out
}

func (p presenter) recipe(r domain.Recipe) recipeJSON {
	out := recipeJSON{
		ID:          int64(r.ID),
		Title:       r.Title,
		Link:        r.Link,
		Body:        r.Body,
		CourseIDs:   make([]int64, len(r.CourseIDs)),
		Ingredients: make([]ingredientJSON, len(r.Ingredients)),
		Nutrition:   nutritionOf(r.Nutrition),
		Cooking:     cookingOf(r.Cooking),
		Author:      p.author(r.AuthorID),
		Images:      p.images(r.Images),
		CreatedAt:   timestamp(r.CreatedAt),
		UpdatedAt:   timestamp(r.UpdatedAt),
	}
	if r.CuisineID != nil {
		id := int64(*r.CuisineID)
		out.CuisineID = &id
	}
	for i, id := range r.CourseIDs {
		out.CourseIDs[i] = int64(id)
	}
	for i, ing := range r.Ingredients {
		q := quantityOf(ing.Quantity)
		out.Ingredients[i] = ingredientJSON{Name: ing.Name, Amount: q.Amount, Unit: q.Unit, Formatted: q.Formatted}
	}
	return out
}

func (p presenter) recipes(rs []domain.Recipe) []recipeJSON {
	out := make([]recipeJSON, len(rs))
	for i, r := range rs {
		out[i] = p.recipe(r)
	}
	return out
}

func (p presenter) cook(c domain.Cook) cookJSON {
	out := cookJSON{
		ID:       int64(c.ID),
		RecipeID: int64(c.RecipeID),
		CookedBy: p.author(c.CookedBy),
		CookedAt: timestamp(c.CookedAt),
		Ratings:  make([]ratingJSON, len(c.Ratings)),
	}
	for i, r := range c.Ratings {
		out.Ratings[i] = ratingJSON{User: p.author(r.UserID), Stars: r.Stars, Comment: r.Comment, RatedAt: timestamp(r.RatedAt)}
	}
	return out
}

func (p presenter) cooks(cs []domain.Cook) []cookJSON {
	out := make([]cookJSON, len(cs))
	for i, c := range cs {
		out[i] = p.cook(c)
	}
	return out
}

func (p presenter) shoppingItem(it domain.ShoppingItem) shoppingItemJSON {
	out := shoppingItemJSON{
		ID:        int64(it.ID),
		Name:      it.Name,
		Checked:   it.Checked,
		AddedBy:   p.author(it.AddedBy),
		CreatedAt: timestamp(it.CreatedAt),
		UpdatedAt: timestamp(it.UpdatedAt),
	}
	if it.Quantity != nil {
		q := quantityOf(it.Quantity)
		out.Quantity = &q
	}
	if it.RecipeID != nil {
		id := int64(*it.RecipeID)
		out.RecipeID = &id
	}
	return out
}

func (p presenter) shoppingItems(items []domain.ShoppingItem) []shoppingItemJSON {
	out := make([]shoppingItemJSON, len(items))
	for i, it := range items {
		out[i] = p.shoppingItem(it)
	}
	return out
}

func (p presenter) images(imgs []domain.Image) []imageJSON {
	out := make([]imageJSON, len(imgs))
	for i, img := range imgs {
		out[i] = imageOf(p.signer, img)
	}
	return out
}

func imageOf(signer *auth.MediaSigner, img domain.Image) imageJSON {
	return imageJSON{
		ID:       int64(img.ID),
		Width:    img.Width,
		Height:   img.Height,
		ThumbURL: signer.URL(img.ID, string(service.VariantThumb)),
		FullURL:  signer.URL(img.ID, string(service.VariantFull)),
	}
}

// quantityOf renders an optional quantity: amount is null for "по вкусу" or
// a missing number, unit is null without a unit.
func quantityOf(q *domain.Quantity) quantityJSON {
	if q == nil {
		return quantityJSON{}
	}
	out := quantityJSON{Formatted: q.Format()}
	if amount := q.Amount(); amount != "" {
		out.Amount = &amount
	}
	if q.Unit != "" {
		unit := string(q.Unit)
		out.Unit = &unit
	}
	return out
}

// nutritionOf renders КБЖУ with the per-dish and per-serving values the
// domain derives from the weight and the servings.
func nutritionOf(n *domain.Nutrition) *nutritionJSON {
	if n == nil {
		return nil
	}
	out := &nutritionJSON{Per100g: macrosOf(n.Per100())}
	if n.WeightGrams > 0 {
		w := n.WeightGrams
		out.WeightG = &w
	}
	if n.Servings > 0 {
		s := n.Servings
		out.Servings = &s
	}
	if dish, ok := n.PerDish(); ok {
		m := macrosOf(dish)
		out.PerDish = &m
	}
	if serving, ok := n.PerServing(); ok {
		m := macrosOf(serving)
		out.PerServing = &m
	}
	return out
}

func macrosOf(m domain.Macros) macrosJSON {
	return macrosJSON{
		Kcal:    domain.DecimalTenths(m.Kcal),
		Protein: domain.DecimalTenths(m.Protein),
		Fat:     domain.DecimalTenths(m.Fat),
		Carbs:   domain.DecimalTenths(m.Carbs),
	}
}

func cookingOf(c domain.CookingSummary) cookingJSON {
	out := cookingJSON{Count: c.Count, RatingCount: c.RatingCount}
	if c.LastCookedAt != nil {
		at := timestamp(*c.LastCookedAt)
		out.LastCookedAt = &at
	}
	if avg, ok := c.AverageTenths(); ok {
		s := domain.DecimalTenths(avg)
		out.RatingAvg = &s
	}
	return out
}

func recipeTagOf(t domain.RecipeTag) recipeTagJSON {
	return recipeTagJSON{ID: int64(t.ID), Kind: string(t.Kind), Name: t.Name, Emoji: t.Emoji, Position: t.Position}
}

func storeOf(st stores.Store) storeJSON {
	return storeJSON{ID: st.ID, Name: st.Name, Emoji: st.Emoji, SearchURLTemplate: st.SearchTemplate, OpensApp: st.OpensApp, Cart: st.Cart}
}

func categoryOf(c domain.Category) categoryJSON {
	return categoryJSON{ID: int64(c.ID), Name: c.Name, Emoji: c.Emoji, Position: c.Position}
}

func priceOf(m domain.Money) priceJSON {
	return priceJSON{Amount: m.Decimal(), Currency: string(m.Currency), Formatted: m.Format()}
}

// prices renders money sums, always as an array (never null).
func prices(ms []domain.Money) []priceJSON {
	out := make([]priceJSON, len(ms))
	for i, m := range ms {
		out[i] = priceOf(m)
	}
	return out
}

func statsOf(st domain.Stats) statsJSON {
	out := statsJSON{
		Overall:           byStatus(st.Overall),
		Categories:        make([]categoryStatsJSON, len(st.Categories)),
		FulfilledThisYear: st.FulfilledThisYear,
		Recipes:           st.Recipes,
		RecipesCooked:     st.RecipesCooked,
		Saved:             prices(st.Saved),
	}
	for i, c := range st.Categories {
		entry := categoryStatsJSON{ByStatus: byStatus(c.ByStatus)}
		if c.Category != nil {
			cat := categoryOf(*c.Category)
			entry.Category = &cat
		}
		out.Categories[i] = entry
	}
	return out
}

// byStatus always emits every status key, with empty sums as [] not null.
func byStatus(m map[domain.Status]domain.StatusTotals) map[string]totalsJSON {
	out := make(map[string]totalsJSON, len(domain.Statuses))
	for _, st := range domain.Statuses {
		totals := m[st]
		out[string(st)] = totalsJSON{Count: totals.Count, Sums: prices(totals.Sums)}
	}
	return out
}

func timestamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }
