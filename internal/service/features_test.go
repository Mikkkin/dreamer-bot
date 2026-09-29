package service_test

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

// ---- helpers

func (e *env) tags(t *testing.T) (cuisines, courses []domain.RecipeTag) {
	t.Helper()
	tags, err := e.svc.RecipeTags.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range tags {
		if tag.Kind == domain.TagCuisine {
			cuisines = append(cuisines, tag)
		} else {
			courses = append(courses, tag)
		}
	}
	return cuisines, courses
}

func (e *env) createRecipe(t *testing.T, actor domain.UserID, d domain.RecipeDraft) domain.Recipe {
	t.Helper()
	r, err := e.svc.Recipes.Create(context.Background(), actor, d)
	if err != nil {
		t.Fatalf("Create recipe: %v", err)
	}
	return r
}

// couple starts chats for Dima and Anya so that every event has a recipient.
func (e *env) couple(t *testing.T) {
	t.Helper()
	e.touch(t, dima, "Дима", true)
	e.touch(t, anya, "Аня", true)
}

func rub(minor int64) domain.Money { return domain.Money{Minor: minor, Currency: "RUB"} }

func quantity(t *testing.T, amount, unit string) *domain.Quantity {
	t.Helper()
	q, err := domain.ParseQuantity(amount, unit)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

// plain renders a quantity with ordinary spaces ("750 мл").
func plain(q *domain.Quantity) string {
	if q == nil {
		return ""
	}
	return strings.TrimSpace(q.Amount() + " " + string(q.Unit))
}

func wantValidation(t *testing.T, what string, err error, field string) {
	t.Helper()
	if v, ok := domain.AsValidation(err); !ok || v.Field != field {
		t.Errorf("%s = %v, want a %q validation error", what, err, field)
	}
}

// ---- recipe tags

func TestRecipeTagsService(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	cuisines, courses := e.tags(t)
	if len(cuisines)+len(courses) != len(domain.DefaultRecipeTags()) || cuisines[0].Name != "Русская" || courses[0].Name != "Завтрак" {
		t.Fatalf("default tags = %+v / %+v", cuisines, courses)
	}

	_, err := e.svc.RecipeTags.Create(ctx, dima, "colour", "Красное", "🔴")
	wantValidation(t, "unknown kind", err, "kind")
	_, err = e.svc.RecipeTags.Create(ctx, dima, domain.TagCuisine, "", "🍢")
	wantValidation(t, "empty name", err, "name")
	_, err = e.svc.RecipeTags.Create(ctx, dima, domain.TagCuisine, "Грузинская", "ab")
	wantValidation(t, "letters as emoji", err, "emoji")

	georgian, err := e.svc.RecipeTags.Create(ctx, dima, domain.TagCuisine, " Грузинская ", "🍢")
	if err != nil || georgian.Name != "Грузинская" || georgian.Kind != domain.TagCuisine || georgian.Position != len(cuisines) {
		t.Fatalf("Create = %+v, %v", georgian, err)
	}
	if _, err := e.svc.RecipeTags.Create(ctx, anya, domain.TagCuisine, "ГРУЗИНСКАЯ", "🥙"); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("duplicate cuisine = %v, want ErrConflict", err)
	}
	renamed, err := e.svc.RecipeTags.Update(ctx, anya, georgian.ID, domain.RecipeTagPatch{Emoji: domain.Some("🥙")})
	if err != nil || renamed.Name != "Грузинская" || renamed.Emoji != "🥙" {
		t.Fatalf("emoji-only Update = %+v, %v", renamed, err)
	}
	if _, err := e.svc.RecipeTags.Update(ctx, anya, georgian.ID, domain.RecipeTagPatch{Name: domain.Some("русская")}); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("rename onto an existing cuisine = %v, want ErrConflict", err)
	}
	if _, err := e.svc.RecipeTags.Update(ctx, anya, 9999, domain.RecipeTagPatch{}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Update(missing) = %v, want ErrNotFound", err)
	}

	r := e.createRecipe(t, dima, domain.RecipeDraft{Title: "Хачапури", CuisineID: &georgian.ID, CourseIDs: []domain.RecipeTagID{courses[0].ID}})
	if err := e.svc.RecipeTags.Delete(ctx, dima, georgian.ID); err != nil {
		t.Fatal(err)
	}
	got, err := e.svc.Recipes.Get(ctx, r.ID)
	if err != nil || got.CuisineID != nil || len(got.CourseIDs) != 1 {
		t.Errorf("recipe after deleting its cuisine = %+v, %v", got, err)
	}
	if err := e.svc.RecipeTags.Delete(ctx, dima, georgian.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second Delete = %v, want ErrNotFound", err)
	}
}

func TestRecipeTagReferencesAreValidated(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	cuisines, courses := e.tags(t)
	missing := domain.RecipeTagID(9999)
	course := courses[1].ID

	_, err := e.svc.Recipes.Create(ctx, dima, domain.RecipeDraft{Title: "x", CuisineID: &course})
	wantValidation(t, "a course as the cuisine", err, "cuisine_id")
	_, err = e.svc.Recipes.Create(ctx, dima, domain.RecipeDraft{Title: "x", CuisineID: &missing})
	wantValidation(t, "a missing cuisine", err, "cuisine_id")
	_, err = e.svc.Recipes.Create(ctx, dima, domain.RecipeDraft{Title: "x", CourseIDs: []domain.RecipeTagID{course, cuisines[0].ID}})
	wantValidation(t, "a cuisine among the courses", err, "course_ids")
	_, err = e.svc.Recipes.Create(ctx, dima, domain.RecipeDraft{Title: "x", CourseIDs: []domain.RecipeTagID{missing}})
	wantValidation(t, "a missing course", err, "course_ids")
	if list, _ := e.svc.Recipes.List(ctx, domain.RecipeFilter{}); len(list) != 0 {
		t.Fatalf("rejected recipes were stored: %+v", list)
	}

	r := e.createRecipe(t, dima, domain.RecipeDraft{
		Title:     "Борщ",
		CuisineID: &cuisines[0].ID,
		CourseIDs: []domain.RecipeTagID{courses[3].ID, courses[2].ID},
	})
	if *r.CuisineID != cuisines[0].ID || !slices.Equal(r.CourseIDs, []domain.RecipeTagID{courses[3].ID, courses[2].ID}) {
		t.Errorf("Create = %+v", r)
	}

	_, err = e.svc.Recipes.Update(ctx, anya, r.ID, domain.RecipePatch{CuisineID: domain.Some(&course)})
	wantValidation(t, "Update to a course as the cuisine", err, "cuisine_id")
	_, err = e.svc.Recipes.Update(ctx, anya, r.ID, domain.RecipePatch{CourseIDs: domain.Some([]domain.RecipeTagID{cuisines[1].ID})})
	wantValidation(t, "Update to a cuisine as a course", err, "course_ids")
	if got, _ := e.svc.Recipes.Get(ctx, r.ID); *got.CuisineID != cuisines[0].ID || len(got.CourseIDs) != 2 {
		t.Errorf("rejected updates changed the recipe: %+v", got)
	}

	updated, err := e.svc.Recipes.Update(ctx, anya, r.ID, domain.RecipePatch{
		CuisineID: domain.Some[*domain.RecipeTagID](nil),
		CourseIDs: domain.Some([]domain.RecipeTagID{courses[0].ID}),
	})
	if err != nil || updated.CuisineID != nil || !slices.Equal(updated.CourseIDs, []domain.RecipeTagID{courses[0].ID}) {
		t.Fatalf("valid Update = %+v, %v", updated, err)
	}

	byCourse, err := e.svc.Recipes.List(ctx, domain.RecipeFilter{CourseID: courses[0].ID})
	if err != nil || len(byCourse) != 1 || byCourse[0].ID != r.ID {
		t.Errorf("List by course = %+v, %v", byCourse, err)
	}
}

// ---- recipe notifications, cooking and rating

func TestRecipeUpdatedIsNotifiedForContentChangesOnly(t *testing.T) {
	e := newEnv(t)
	e.couple(t)
	ctx := context.Background()
	r := e.createRecipe(t, dima, domain.RecipeDraft{Title: "Паста"})

	updated, err := e.svc.Recipes.Update(ctx, dima, r.ID, domain.RecipePatch{Body: domain.Some("Сварить")})
	if err != nil {
		t.Fatal(err)
	}
	img, err := e.svc.Recipes.AddImage(ctx, dima, r.ID, bytes.NewReader([]byte("photo")))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Recipes.RemoveImage(ctx, dima, r.ID, img.ID); err != nil {
		t.Fatal(err)
	}
	cook, err := e.svc.Recipes.Cook(ctx, dima, r.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Recipes.Rate(ctx, anya, r.ID, cook.ID, service.RatingInput{Stars: 5}); err != nil {
		t.Fatal(err)
	}
	// Failures notify nobody.
	_, _ = e.svc.Recipes.Update(ctx, dima, r.ID, domain.RecipePatch{Title: domain.Some("")})
	_, _ = e.svc.Recipes.Update(ctx, dima, 9999, domain.RecipePatch{Body: domain.Some("x")})
	_ = e.svc.Recipes.RemoveImage(ctx, dima, r.ID, img.ID)
	_, _ = e.svc.Recipes.AddImage(ctx, dima, 9999, bytes.NewReader(nil))

	want := []string{"recipe_created", "recipe_updated", "recipe_updated", "recipe_updated", "recipe_cooked", "recipe_rated"}
	if got := e.notifier.kinds(); !slices.Equal(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	events := e.notifier.all()
	if ev := events[1]; ev.recipe.ID != r.ID || ev.recipe.Body != "Сварить" || !ev.recipe.UpdatedAt.Equal(updated.UpdatedAt) ||
		ev.r.Actor.ID != dima || !slices.Equal(ids(ev.r.To), []domain.UserID{anya}) {
		t.Errorf("update event = %+v", ev)
	}
	if ev := events[2]; len(ev.recipe.Images) != 1 || ev.recipe.Images[0].ID != img.ID {
		t.Errorf("image-added event carries images %+v", ev.recipe.Images)
	}
	if ev := events[3]; len(ev.recipe.Images) != 0 {
		t.Errorf("image-removed event carries images %+v", ev.recipe.Images)
	}
	if ev := events[3]; ev.ctx.Err() != nil {
		t.Error("notifier context must not be cancellable")
	}
}

func TestRecipeUpdateWithoutChangesIsSilent(t *testing.T) {
	e := newEnv(t)
	e.couple(t)
	ctx := context.Background()
	cuisines, courses := e.tags(t)
	link := "https://example.com/pasta"
	cuisine := cuisines[0].ID
	nutrition := domain.Nutrition{KcalPer100: 1500, ProteinPer100: 125, FatPer100: 60, CarbsPer100: 104, WeightGrams: 800, Servings: 4}
	r := e.createRecipe(t, dima, domain.RecipeDraft{
		Title: "Паста", Link: &link, Body: "Сварить", CuisineID: &cuisine,
		CourseIDs:   []domain.RecipeTagID{courses[0].ID, courses[1].ID},
		Ingredients: []domain.Ingredient{{Name: "Спагетти", Quantity: quantity(t, "320", "г")}, {Name: "Соль", Quantity: quantity(t, "", "по вкусу")}},
		Nutrition:   &nutrition,
	})

	// The whole form, as the Mini App sends it: fresh values equal to the
	// stored ones, the title differing only in whitespace.
	form := func() domain.RecipePatch {
		sameLink, sameCuisine, sameNutrition := link, cuisine, nutrition
		return domain.RecipePatch{
			Title:     domain.Some("  Паста "),
			Link:      domain.Some(&sameLink),
			Body:      domain.Some("Сварить"),
			CuisineID: domain.Some(&sameCuisine),
			CourseIDs: domain.Some([]domain.RecipeTagID{courses[0].ID, courses[1].ID}),
			Ingredients: domain.Some([]domain.Ingredient{
				{Name: "Спагетти", Quantity: quantity(t, "320", "г")}, {Name: "Соль", Quantity: quantity(t, "", "по вкусу")},
			}),
			Nutrition: domain.Some(&sameNutrition),
		}
	}
	e.clock.Set(start.Add(time.Hour))
	for _, p := range []domain.RecipePatch{form(), {}} {
		got, err := e.svc.Recipes.Update(ctx, anya, r.ID, p)
		if err != nil {
			t.Fatal(err)
		}
		if !got.UpdatedAt.Equal(r.UpdatedAt) || got.Title != "Паста" || len(got.Ingredients) != 2 || got.CourseIDs == nil {
			t.Errorf("unchanged update returned %+v", got)
		}
	}
	if stored, err := e.svc.Recipes.Get(ctx, r.ID); err != nil || !stored.UpdatedAt.Equal(r.UpdatedAt) {
		t.Errorf("unchanged update wrote the recipe: %v, %v", stored.UpdatedAt, err)
	}
	if got := e.notifier.kinds(); !slices.Equal(got, []string{"recipe_created"}) {
		t.Fatalf("events = %v, want no update notice", got)
	}

	// Every real change is written and notified once.
	changes := map[string]func(p *domain.RecipePatch){
		"title":           func(p *domain.RecipePatch) { p.Title = domain.Some("Паста болоньезе") },
		"link cleared":    func(p *domain.RecipePatch) { p.Link = domain.Some[*string](nil) },
		"body":            func(p *domain.RecipePatch) { p.Body = domain.Some("Сварить аль денте") },
		"cuisine cleared": func(p *domain.RecipePatch) { p.CuisineID = domain.Some[*domain.RecipeTagID](nil) },
		"course order": func(p *domain.RecipePatch) {
			p.CourseIDs = domain.Some([]domain.RecipeTagID{courses[1].ID, courses[0].ID})
		},
		"ingredient amount": func(p *domain.RecipePatch) { p.Ingredients.Value[0].Quantity = quantity(t, "350", "г") },
		"ingredient added": func(p *domain.RecipePatch) {
			p.Ingredients.Value = append(p.Ingredients.Value, domain.Ingredient{Name: "Бекон"})
		},
		"servings":          func(p *domain.RecipePatch) { p.Nutrition.Value.Servings = 2 },
		"nutrition cleared": func(p *domain.RecipePatch) { p.Nutrition = domain.Some[*domain.Nutrition](nil) },
	}
	for name, change := range changes {
		// Restore the original content first; that is a change of its own.
		if _, err := e.svc.Recipes.Update(ctx, anya, r.ID, form()); err != nil {
			t.Fatal(err)
		}
		before := len(e.notifier.all())
		e.clock.Set(e.clock.Now().Add(time.Minute))
		p := form()
		change(&p)
		got, err := e.svc.Recipes.Update(ctx, anya, r.ID, p)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		events := e.notifier.all()[before:]
		if len(events) != 1 || events[0].kind != "recipe_updated" || !got.UpdatedAt.Equal(e.clock.Now()) {
			t.Errorf("%s: events %v, updated_at %v", name, events, got.UpdatedAt)
		}
	}
}

func TestCookAndRate(t *testing.T) {
	e := newEnv(t)
	e.couple(t)
	ctx := context.Background()
	pasta := e.createRecipe(t, dima, domain.RecipeDraft{Title: "Паста"})
	soup := e.createRecipe(t, dima, domain.RecipeDraft{Title: "Суп"})

	for _, bad := range []service.RatingInput{{Stars: 0}, {Stars: 6}, {Stars: 5, Comment: strings.Repeat("я", domain.MaxRatingCommentLen+1)}} {
		_, err := e.svc.Recipes.Cook(ctx, dima, pasta.ID, &bad)
		if _, ok := domain.AsValidation(err); !ok {
			t.Errorf("Cook with %+v = %v, want a validation error", bad, err)
		}
	}
	if cooks, _ := e.svc.Recipes.ListCooks(ctx, pasta.ID); len(cooks) != 0 {
		t.Fatalf("rejected cooks were stored: %+v", cooks)
	}
	if _, err := e.svc.Recipes.Cook(ctx, dima, 9999, nil); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Cook(missing) = %v, want ErrNotFound", err)
	}

	e.clock.Set(start.Add(time.Hour))
	first, err := e.svc.Recipes.Cook(ctx, dima, pasta.ID, &service.RatingInput{Stars: 5, Comment: " Идеально "})
	if err != nil {
		t.Fatal(err)
	}
	if first.CookedBy != dima || !first.CookedAt.Equal(start.Add(time.Hour)) || len(first.Ratings) != 1 ||
		first.Ratings[0].UserID != dima || first.Ratings[0].Stars != 5 || first.Ratings[0].Comment != "Идеально" {
		t.Fatalf("Cook = %+v", first)
	}
	ev := e.notifier.all()[len(e.notifier.all())-1]
	if ev.kind != "recipe_cooked" || ev.cook.ID != first.ID || ev.recipe.ID != pasta.ID || ev.recipe.Cooking.Count != 1 ||
		!slices.Equal(ids(ev.r.To), []domain.UserID{anya}) {
		t.Errorf("cooked event = %+v", ev)
	}

	// Anya rates, then changes her mind: the rating is replaced.
	e.clock.Set(start.Add(2 * time.Hour))
	if _, err := e.svc.Recipes.Rate(ctx, anya, pasta.ID, first.ID, service.RatingInput{Stars: 2}); err != nil {
		t.Fatal(err)
	}
	c, err := e.svc.Recipes.Rate(ctx, anya, pasta.ID, first.ID, service.RatingInput{Stars: 4, Comment: "Солоновато"})
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Ratings) != 2 || c.Ratings[1].UserID != anya || c.Ratings[1].Stars != 4 || c.Ratings[1].Comment != "Солоновато" {
		t.Fatalf("ratings = %+v", c.Ratings)
	}
	ev = e.notifier.all()[len(e.notifier.all())-1]
	if ev.kind != "recipe_rated" || ev.rating.Stars != 4 || ev.rating.UserID != anya || ev.cook.ID != first.ID ||
		!slices.Equal(ids(ev.r.To), []domain.UserID{dima}) || ev.recipe.Cooking.RatingCount != 2 {
		t.Errorf("rated event = %+v", ev)
	}
	_, err = e.svc.Recipes.Rate(ctx, anya, pasta.ID, first.ID, service.RatingInput{Stars: 9})
	wantValidation(t, "Rate with 9 stars", err, "stars")

	// A cook of another recipe (or a missing one) cannot be touched.
	soupCook, err := e.svc.Recipes.Cook(ctx, anya, soup.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Recipes.Rate(ctx, anya, pasta.ID, soupCook.ID, service.RatingInput{Stars: 1}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Rate of another recipe's cook = %v, want ErrNotFound", err)
	}
	if _, err := e.svc.Recipes.Rate(ctx, anya, 9999, first.ID, service.RatingInput{Stars: 1}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Rate via a missing recipe = %v, want ErrNotFound", err)
	}
	if err := e.svc.Recipes.RemoveCook(ctx, anya, pasta.ID, soupCook.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("RemoveCook of another recipe's cook = %v, want ErrNotFound", err)
	}
	if soupCooks, _ := e.svc.Recipes.ListCooks(ctx, soup.ID); len(soupCooks) != 1 || len(soupCooks[0].Ratings) != 0 {
		t.Errorf("soup history was touched: %+v", soupCooks)
	}

	// Cooking again keeps the recipe in the list and grows its history.
	e.clock.Set(start.Add(3 * time.Hour))
	second, err := e.svc.Recipes.Cook(ctx, anya, pasta.ID, &service.RatingInput{Stars: 3})
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.svc.Recipes.Get(ctx, pasta.ID)
	if err != nil {
		t.Fatal(err)
	}
	if s := got.Cooking; s.Count != 2 || s.RatingCount != 3 || s.RatingSum != 12 || !s.LastCookedAt.Equal(start.Add(3*time.Hour)) {
		t.Errorf("summary = %+v", s)
	}
	if avg, _ := got.Cooking.AverageTenths(); avg != 40 {
		t.Errorf("average = %d tenths, want 40", avg)
	}
	cooks, err := e.svc.Recipes.ListCooks(ctx, pasta.ID)
	if err != nil || len(cooks) != 2 || cooks[0].ID != second.ID {
		t.Fatalf("ListCooks = %+v, %v", cooks, err)
	}
	if _, err := e.svc.Recipes.ListCooks(ctx, 9999); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("ListCooks(missing) = %v, want ErrNotFound", err)
	}

	if err := e.svc.Recipes.RemoveCook(ctx, dima, pasta.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.svc.Recipes.Get(ctx, pasta.ID); got.Cooking.Count != 1 || got.Cooking.RatingCount != 1 || got.Cooking.RatingSum != 3 {
		t.Errorf("summary after RemoveCook = %+v", got.Cooking)
	}
	for _, kind := range e.notifier.kinds() {
		if kind == "recipe_updated" {
			t.Error("cooking or rating fired RecipeUpdated")
		}
	}
}

// ---- savings

func TestAddSaving(t *testing.T) {
	e := newEnv(t)
	e.couple(t)
	ctx := context.Background()
	price := rub(4_500_000)
	sofa := e.createWish(t, dima, domain.WishDraft{Title: "Диван", Price: &price})

	_, err := e.svc.Wishes.AddSaving(ctx, anya, sofa.ID, domain.Money{Minor: 100, Currency: "EUR"}, "")
	wantValidation(t, "saving in another currency", err, "currency")
	_, err = e.svc.Wishes.AddSaving(ctx, anya, sofa.ID, rub(0), "")
	if _, ok := domain.AsValidation(err); !ok {
		t.Errorf("zero saving = %v, want a validation error", err)
	}
	_, err = e.svc.Wishes.AddSaving(ctx, anya, sofa.ID, rub(100), strings.Repeat("я", domain.MaxSavingNoteLen+1))
	wantValidation(t, "long note", err, "note")
	if got, _ := e.svc.Wishes.Get(ctx, sofa.ID); got.Status != domain.StatusWant || got.Saved != nil {
		t.Fatalf("rejected savings changed the wish: %+v", got)
	}
	if _, err := e.svc.Wishes.AddSaving(ctx, anya, 9999, rub(100), ""); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("AddSaving(missing) = %v, want ErrNotFound", err)
	}

	before := len(e.notifier.all())
	e.clock.Set(start.Add(time.Hour))
	s1, err := e.svc.Wishes.AddSaving(ctx, anya, sofa.ID, rub(500_000), " с зарплаты ")
	if err != nil {
		t.Fatal(err)
	}
	if s1.ID == 0 || s1.WishID != sofa.ID || s1.UserID != anya || s1.Note != "с зарплаты" || !s1.CreatedAt.Equal(start.Add(time.Hour)) {
		t.Errorf("AddSaving = %+v", s1)
	}
	got, _ := e.svc.Wishes.Get(ctx, sofa.ID)
	if got.Status != domain.StatusProgress || got.Saved == nil || *got.Saved != rub(500_000) || !got.UpdatedAt.Equal(start.Add(time.Hour)) {
		t.Errorf("wish after the first saving = %+v", got)
	}
	events := e.notifier.all()[before:]
	if len(events) != 1 || events[0].kind != "wish_saved" {
		t.Fatalf("events = %+v, want one wish_saved", events)
	}
	if ev := events[0]; ev.saving.ID != s1.ID || ev.wish.Status != domain.StatusProgress || ev.wish.Saved == nil ||
		*ev.wish.Saved != rub(500_000) || ev.r.Actor.ID != anya || !slices.Equal(ids(ev.r.To), []domain.UserID{dima}) {
		t.Errorf("wish_saved event = %+v", ev)
	}

	e.clock.Set(start.Add(2 * time.Hour))
	s2, err := e.svc.Wishes.AddSaving(ctx, dima, sofa.ID, rub(1_200_000), "")
	if err != nil {
		t.Fatal(err)
	}
	if ev := e.notifier.all()[len(e.notifier.all())-1]; *ev.wish.Saved != rub(1_700_000) {
		t.Errorf("second event Saved = %v", ev.wish.Saved)
	}
	if p, ok := (func() (int, bool) { w, _ := e.svc.Wishes.Get(ctx, sofa.ID); return w.SavedPercent() })(); !ok || p != 37 {
		t.Errorf("SavedPercent = %d, %v; want 37", p, ok)
	}
	savings, err := e.svc.Wishes.ListSavings(ctx, sofa.ID)
	if err != nil || len(savings) != 2 || savings[0].ID != s2.ID || savings[1].ID != s1.ID {
		t.Fatalf("ListSavings = %+v, %v", savings, err)
	}

	// Update sees the savings: the price currency cannot drift away.
	eur := domain.Money{Minor: 100_000, Currency: "EUR"}
	_, err = e.svc.Wishes.Update(ctx, dima, sofa.ID, domain.WishPatch{Price: domain.Some(&eur)})
	wantValidation(t, "price in another currency than the savings", err, "price")
	if _, err := e.svc.Wishes.Update(ctx, dima, sofa.ID, domain.WishPatch{Price: domain.Some[*domain.Money](nil)}); err != nil {
		t.Errorf("removing the price: %v", err)
	}
	// Without a price the savings currency still holds.
	_, err = e.svc.Wishes.AddSaving(ctx, dima, sofa.ID, domain.Money{Minor: 100, Currency: "USD"}, "")
	wantValidation(t, "saving in another currency without a price", err, "currency")

	// A saving can only be removed through its wish.
	other := e.createWish(t, dima, domain.WishDraft{Title: "Другое"})
	if err := e.svc.Wishes.RemoveSaving(ctx, dima, other.ID, s1.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("RemoveSaving via another wish = %v, want ErrNotFound", err)
	}
	if err := e.svc.Wishes.RemoveSaving(ctx, dima, sofa.ID, s1.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.svc.Wishes.Get(ctx, sofa.ID); *got.Saved != rub(1_200_000) || got.Status != domain.StatusProgress {
		t.Errorf("after RemoveSaving = %+v", got)
	}
	if _, err := e.svc.Wishes.ListSavings(ctx, 9999); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("ListSavings(missing) = %v, want ErrNotFound", err)
	}
}

// A fulfilled wish takes no more savings: the saving is refused and the wish
// stays exactly as it was.
func TestAddSavingRefusedForFulfilledWish(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	w := e.createWish(t, dima, domain.WishDraft{Title: "Уже сбылось"})
	if _, err := e.svc.Wishes.SetStatus(ctx, dima, w.ID, domain.StatusDone); err != nil {
		t.Fatal(err)
	}
	_, err := e.svc.Wishes.AddSaving(ctx, dima, w.ID, domain.Money{Minor: 100, Currency: "GBP"}, "")
	wantValidation(t, "AddSaving(done wish)", err, "status")
	got, _ := e.svc.Wishes.Get(ctx, w.ID)
	if got.Status != domain.StatusDone || got.FulfilledAt == nil || got.Saved != nil {
		t.Errorf("a refused saving changed the wish: %+v", got)
	}
}

// ---- shopping

func TestShoppingAdd(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	_, err := e.svc.Shopping.Add(ctx, dima, nil)
	wantValidation(t, "Add(nothing)", err, "items")
	_, err = e.svc.Shopping.Add(ctx, dima, []domain.ShoppingDraft{{Name: "Хлеб"}, {Name: "  "}})
	wantValidation(t, "a nameless item", err, "name")
	if list, _ := e.svc.Shopping.List(ctx); len(list) != 0 {
		t.Fatalf("a rejected batch was stored partly: %+v", list)
	}

	items, err := e.svc.Shopping.Add(ctx, dima, []domain.ShoppingDraft{
		{Name: "Молоко", Quantity: quantity(t, "500", "мл")},
		{Name: " молоко ", Quantity: quantity(t, "250", "мл")},
		{Name: "Хлеб"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Name != "Молоко" || plain(items[0].Quantity) != "750 мл" || items[0].AddedBy != dima || items[1].Name != "Хлеб" {
		t.Fatalf("Add = %+v", items)
	}

	e.clock.Set(start.Add(time.Minute))
	checked, err := e.svc.Shopping.Update(ctx, anya, items[1].ID, domain.ShoppingPatch{Checked: domain.Some(true)})
	if err != nil || !checked.Checked || !checked.UpdatedAt.Equal(start.Add(time.Minute)) {
		t.Fatalf("check = %+v, %v", checked, err)
	}
	_, err = e.svc.Shopping.Update(ctx, anya, items[0].ID, domain.ShoppingPatch{Name: domain.Some("")})
	wantValidation(t, "renaming to nothing", err, "name")
	if _, err := e.svc.Shopping.Update(ctx, anya, 9999, domain.ShoppingPatch{}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Update(missing) = %v, want ErrNotFound", err)
	}
	// A bought item does not absorb a new one.
	again, err := e.svc.Shopping.Add(ctx, anya, []domain.ShoppingDraft{{Name: "Хлеб"}})
	if err != nil || again[0].ID == items[1].ID {
		t.Fatalf("checked item absorbed a new one: %+v, %v", again, err)
	}
	list, _ := e.svc.Shopping.List(ctx)
	if len(list) != 3 || list[0].ID != items[0].ID || list[1].ID != again[0].ID || list[2].ID != items[1].ID {
		t.Errorf("List order = %+v; want unchecked first, oldest first", list)
	}

	n, err := e.svc.Shopping.ClearChecked(ctx, anya)
	if err != nil || n != 1 {
		t.Errorf("ClearChecked = %d, %v; want 1", n, err)
	}
	if err := e.svc.Shopping.Delete(ctx, anya, items[1].ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Delete(cleared) = %v, want ErrNotFound", err)
	}
	if err := e.svc.Shopping.Delete(ctx, anya, items[0].ID); err != nil {
		t.Fatal(err)
	}
}

func TestShoppingLimit(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	drafts := make([]domain.ShoppingDraft, domain.MaxShoppingItems)
	for i := range drafts {
		drafts[i] = domain.ShoppingDraft{Name: "Позиция " + strings.Repeat("я", i%40) + string(rune('A'+i%26)) + string(rune('a'+i/26))}
	}
	if _, err := e.svc.Shopping.Add(ctx, dima, drafts); err != nil {
		t.Fatalf("filling the list: %v", err)
	}
	if _, err := e.svc.Shopping.Add(ctx, dima, []domain.ShoppingDraft{{Name: "Лишнее"}}); !errors.Is(err, domain.ErrLimitExceeded) {
		t.Errorf("item over the limit = %v, want ErrLimitExceeded", err)
	}
	// Merging into an existing line needs no room.
	if _, err := e.svc.Shopping.Add(ctx, dima, []domain.ShoppingDraft{{Name: drafts[0].Name}}); err != nil {
		t.Errorf("merge into a full list: %v", err)
	}
	if _, err := e.svc.Shopping.Add(ctx, dima, make([]domain.ShoppingDraft, domain.MaxShoppingItems+1)); !errors.Is(err, domain.ErrLimitExceeded) {
		t.Errorf("oversized batch = %v, want ErrLimitExceeded", err)
	}
	if list, _ := e.svc.Shopping.List(ctx); len(list) != domain.MaxShoppingItems {
		t.Errorf("%d items, want %d", len(list), domain.MaxShoppingItems)
	}
}

func TestShoppingAddFromRecipe(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	r := e.createRecipe(t, dima, domain.RecipeDraft{Title: "Карбонара", Ingredients: []domain.Ingredient{
		{Name: "Спагетти", Quantity: quantity(t, "320", "г")},
		{Name: "Соль", Quantity: quantity(t, "", "по вкусу")},
		{Name: "Яйца", Quantity: quantity(t, "4", "шт")},
	}})
	empty := e.createRecipe(t, dima, domain.RecipeDraft{Title: "Без ингредиентов"})

	for _, bad := range [][]int{{3}, {-1}, {0, 7}, {}} {
		_, err := e.svc.Shopping.AddFromRecipe(ctx, anya, r.ID, bad)
		wantValidation(t, "positions", err, "positions")
	}
	_, err := e.svc.Shopping.AddFromRecipe(ctx, anya, empty.ID, nil)
	wantValidation(t, "a recipe without ingredients", err, "positions")
	if _, err := e.svc.Shopping.AddFromRecipe(ctx, anya, 9999, nil); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("AddFromRecipe(missing) = %v, want ErrNotFound", err)
	}
	if list, _ := e.svc.Shopping.List(ctx); len(list) != 0 {
		t.Fatalf("rejected calls added items: %+v", list)
	}

	picked, err := e.svc.Shopping.AddFromRecipe(ctx, anya, r.ID, []int{2, 0, 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(picked) != 2 || picked[0].Name != "Яйца" || plain(picked[0].Quantity) != "4 шт" || picked[1].Name != "Спагетти" ||
		picked[0].RecipeID == nil || *picked[0].RecipeID != r.ID || picked[0].AddedBy != anya {
		t.Fatalf("AddFromRecipe(positions) = %+v", picked)
	}

	all, err := e.svc.Shopping.AddFromRecipe(ctx, dima, r.ID, nil)
	if err != nil || len(all) != 3 {
		t.Fatalf("AddFromRecipe(all) = %+v, %v", all, err)
	}
	got := map[string]string{}
	for _, it := range all {
		got[it.Name] = plain(it.Quantity)
	}
	if got["Спагетти"] != "640 г" || got["Яйца"] != "8 шт" || got["Соль"] != "по вкусу" {
		t.Errorf("quantities after adding the recipe twice = %v", got)
	}
	if list, _ := e.svc.Shopping.List(ctx); len(list) != 3 {
		t.Errorf("%d lines, want 3 (merged)", len(list))
	}

	// The recipe itself is untouched by the merges.
	stored, _ := e.svc.Recipes.Get(ctx, r.ID)
	if plain(stored.Ingredients[0].Quantity) != "320 г" {
		t.Errorf("recipe ingredient changed to %s", plain(stored.Ingredients[0].Quantity))
	}
}

// ---- stats

func TestStatsSavedAndCooked(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	s, err := e.svc.Stats.Compute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.RecipesCooked != 0 || s.Saved == nil || len(s.Saved) != 0 {
		t.Errorf("empty stats: cooked %d, saved %#v (want 0 and an empty, non-nil slice)", s.RecipesCooked, s.Saved)
	}

	price := rub(4_500_000)
	sofa := e.createWish(t, dima, domain.WishDraft{Title: "Диван", Price: &price})
	trip := e.createWish(t, dima, domain.WishDraft{Title: "Поездка"})
	done := e.createWish(t, dima, domain.WishDraft{Title: "Сбылось"})
	for _, x := range []struct {
		id     domain.WishID
		amount domain.Money
	}{
		{sofa.ID, rub(500_000)},
		{sofa.ID, rub(1_200_000)},
		{trip.ID, domain.Money{Minor: 30_000, Currency: "EUR"}},
		{done.ID, rub(9_900_000)},
	} {
		if _, err := e.svc.Wishes.AddSaving(ctx, anya, x.id, x.amount, ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.svc.Wishes.SetStatus(ctx, dima, done.ID, domain.StatusDone); err != nil {
		t.Fatal(err)
	}

	pasta := e.createRecipe(t, dima, domain.RecipeDraft{Title: "Паста"})
	soup := e.createRecipe(t, dima, domain.RecipeDraft{Title: "Суп"})
	for _, id := range []domain.RecipeID{pasta.ID, pasta.ID, soup.ID} {
		if _, err := e.svc.Recipes.Cook(ctx, dima, id, nil); err != nil {
			t.Fatal(err)
		}
	}

	s, err = e.svc.Stats.Compute(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if s.RecipesCooked != 3 || s.Recipes != 2 {
		t.Errorf("RecipesCooked = %d, Recipes = %d; want 3, 2", s.RecipesCooked, s.Recipes)
	}
	want := []domain.Money{{Minor: 30_000, Currency: "EUR"}, rub(1_700_000)}
	if !slices.Equal(s.Saved, want) {
		t.Errorf("Saved = %v, want %v (done wishes excluded, currencies apart)", s.Saved, want)
	}
	if s.Overall[domain.StatusProgress].Count != 2 {
		t.Errorf("savings must have moved both wishes to «Копим»: %+v", s.Overall)
	}
}
