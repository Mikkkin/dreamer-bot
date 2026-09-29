package bot

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

func (e *testEnv) recipeDraftCard(t *testing.T, title string) {
	t.Helper()
	e.handle(textUpdate(alice, title))
	e.handle(e.draftCallback(t, callback{op: opDraftKind, kind: kindRecipe}))
}

func TestRecipeDraftCuisinePicker(t *testing.T) {
	e := newTestEnv(t)
	e.recipeDraftCard(t, "Паста карбонара")
	if texts := buttonTexts(e.api.lastEdit(t).ReplyMarkup); !slices.Contains(texts, "🍜 Кухня") || !slices.Contains(texts, "🍽 Тип") {
		t.Fatalf("recipe draft buttons %v", texts)
	}

	e.handle(e.draftCallback(t, callback{op: opDraftCuisines}))
	edit := e.api.lastEdit(t)
	texts := buttonTexts(edit.ReplyMarkup)
	for _, want := range []string{"🍝 Итальянская", "🥟 Русская", selectedMark + "Без кухни", "← Назад"} {
		if !slices.Contains(texts, want) {
			t.Errorf("cuisine picker lacks %q: %v", want, texts)
		}
	}
	if slices.Contains(texts, "🌙 Ужин") {
		t.Error("course tags in the cuisine picker")
	}
	if !strings.Contains(edit.Text, "Выберите кухню") {
		t.Errorf("card text %q", edit.Text)
	}

	e.handle(e.draftCallback(t, callback{op: opDraftCuisine, id: int64(tagItalian)}))
	d := e.draft(t)
	if d.cuisine == nil || d.cuisine.id != tagItalian || d.view != viewMain {
		t.Fatalf("cuisine not chosen: %+v", d)
	}
	edit = e.api.lastEdit(t)
	if !strings.Contains(edit.Text, "🍝 Итальянская") || !slices.Contains(buttonTexts(edit.ReplyMarkup), "🍝 Итальянская") {
		t.Errorf("card %q, buttons %v", edit.Text, buttonTexts(edit.ReplyMarkup))
	}

	// Another kind of tag, or an unknown one, is refused.
	for _, id := range []domain.RecipeTagID{tagDinner, 404} {
		e.api.reset()
		e.handle(e.draftCallback(t, callback{op: opDraftCuisine, id: int64(id)}))
		if got := e.api.answers(); len(got) != 1 || !strings.Contains(got[0], "удалили") {
			t.Errorf("tag %d answered %v", id, got)
		}
		if d := e.draft(t); d.cuisine == nil || d.cuisine.id != tagItalian {
			t.Errorf("cuisine changed by tag %d", id)
		}
	}

	// «Без кухни» clears it.
	e.handle(e.draftCallback(t, callback{op: opDraftCuisines}))
	e.handle(e.draftCallback(t, callback{op: opDraftCuisine}))
	if d := e.draft(t); d.cuisine != nil || d.view != viewMain {
		t.Errorf("cuisine not cleared: %+v", d)
	}
}

func TestRecipeDraftCoursePicker(t *testing.T) {
	e := newTestEnv(t)
	e.recipeDraftCard(t, "Паста карбонара")
	e.handle(e.draftCallback(t, callback{op: opDraftCourses}))
	if text := e.api.lastEdit(t).Text; !strings.Contains(text, "можно несколько") {
		t.Errorf("card text %q", text)
	}

	e.handle(e.draftCallback(t, callback{op: opDraftCourse, id: int64(tagDinner)}))
	e.handle(e.draftCallback(t, callback{op: opDraftCourse, id: int64(tagFirst)}))
	d := e.draft(t)
	if d.view != viewCourses || len(d.courses) != 2 {
		t.Fatalf("courses %+v, view %v (the picker must stay open)", d.courses, d.view)
	}
	texts := buttonTexts(e.api.lastEdit(t).ReplyMarkup)
	for _, want := range []string{selectedMark + "🌙 Ужин", selectedMark + "🍲 Первое", "🍳 Завтрак", "Без типа", "✅ Готово"} {
		if !slices.Contains(texts, want) {
			t.Errorf("course picker lacks %q: %v", want, texts)
		}
	}

	// A second tap removes the course.
	e.handle(e.draftCallback(t, callback{op: opDraftCourse, id: int64(tagDinner)}))
	if d := e.draft(t); len(d.courses) != 1 || d.courses[0].id != tagFirst {
		t.Fatalf("courses after toggling off %+v", d.courses)
	}

	e.handle(e.draftCallback(t, callback{op: opDraftBack}))
	edit := e.api.lastEdit(t)
	if !slices.Contains(buttonTexts(edit.ReplyMarkup), "🍲 Первое") || !strings.Contains(edit.Text, "🍲 Первое") {
		t.Errorf("card %q, buttons %v", edit.Text, buttonTexts(edit.ReplyMarkup))
	}

	e.handle(e.draftCallback(t, callback{op: opDraftCuisines}))
	e.handle(e.draftCallback(t, callback{op: opDraftCuisine, id: int64(tagItalian)}))
	e.handle(e.draftCallback(t, callback{op: opDraftSave}))
	if len(e.svc.recipes.created) != 1 {
		t.Fatal("recipe not saved")
	}
	got := e.svc.recipes.created[0]
	if got.CuisineID == nil || *got.CuisineID != tagItalian || !slices.Equal(got.CourseIDs, []domain.RecipeTagID{tagFirst}) {
		t.Errorf("saved draft %+v", got)
	}
	if text := e.api.lastEdit(t).Text; !strings.Contains(text, "🍝 Итальянская · 🍲 Первое") {
		t.Errorf("saved card %q", text)
	}
}

func TestRecipeDraftCourseLimit(t *testing.T) {
	e := newTestEnv(t)
	e.recipeDraftCard(t, "Всё сразу")
	e.handle(e.draftCallback(t, callback{op: opDraftCourses}))
	for id := domain.RecipeTagID(7); id <= 15; id++ { // all nine default courses
		e.api.reset()
		e.handle(e.draftCallback(t, callback{op: opDraftCourse, id: int64(id)}))
	}
	if got := e.api.answers(); !slices.Equal(got, []string{"Не больше 8 типов блюда"}) {
		t.Errorf("ninth course answered %v", got)
	}
	d := e.draft(t)
	if len(d.courses) != domain.MaxCoursesPerRecipe {
		t.Fatalf("%d courses", len(d.courses))
	}
	e.handle(e.draftCallback(t, callback{op: opDraftBack}))
	if texts := buttonTexts(e.api.lastEdit(t).ReplyMarkup); !slices.Contains(texts, "🍳 Завтрак +7") {
		t.Errorf("main buttons %v", texts)
	}

	// «Без типа» clears every course; a deleted tag can still be removed.
	e.handle(e.draftCallback(t, callback{op: opDraftCourses}))
	e.handle(e.draftCallback(t, callback{op: opDraftCourse}))
	if d := e.draft(t); len(d.courses) != 0 {
		t.Errorf("courses not cleared: %+v", d.courses)
	}
	e.handle(e.draftCallback(t, callback{op: opDraftCourse, id: int64(tagDinner)}))
	if err := e.svc.tags.Delete(context.Background(), alice, tagDinner); err != nil {
		t.Fatal(err)
	}
	e.handle(e.draftCallback(t, callback{op: opDraftCourse, id: int64(tagDinner)}))
	if d := e.draft(t); len(d.courses) != 0 {
		t.Errorf("a deleted course could not be removed: %+v", d.courses)
	}
}

func TestRecipeDraftMainButtonsSummarizeCourses(t *testing.T) {
	d := draft{id: 1, kind: kindRecipe, courses: []tagRef{{9, "🌙 Ужин"}, {10, "🍲 Первое"}, {11, "🍛 Второе"}}}
	if texts := buttonTexts(draftKeyboard(d, nil, nil)); !slices.Contains(texts, "🌙 Ужин +2") {
		t.Errorf("buttons %v", texts)
	}
}

func TestTagPickersIgnoredForWishes(t *testing.T) {
	e := newTestEnv(t)
	e.handle(textUpdate(alice, "Лампа"))
	e.handle(e.draftCallback(t, callback{op: opDraftCuisines}))
	e.handle(e.draftCallback(t, callback{op: opDraftCourse, id: int64(tagDinner)}))
	d := e.draft(t)
	if d.view != viewMain || len(d.courses) != 0 || d.cuisine != nil {
		t.Errorf("wish draft changed by recipe pickers: %+v", d)
	}
	if texts := buttonTexts(e.api.lastEdit(t).ReplyMarkup); slices.Contains(texts, "🍜 Кухня") {
		t.Errorf("wish draft offers a cuisine: %v", texts)
	}
}

func TestTagPickerSurvivesTagOutage(t *testing.T) {
	e := newTestEnv(t)
	e.recipeDraftCard(t, "Суп")
	e.svc.tags.fail = domain.ErrConflict
	e.handle(e.draftCallback(t, callback{op: opDraftCuisines}))
	if d := e.draft(t); d.view != viewMain {
		t.Errorf("picker opened without tags: view %v", d.view)
	}
	if !strings.Contains(e.logs.String(), "list recipe tags failed") {
		t.Error("outage not logged")
	}
}
