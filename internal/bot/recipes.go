package bot

import (
	"context"
	"errors"
	"strconv"

	"github.com/go-telegram/bot/models"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

// rerollAttempts bounds how often /cook retries to avoid showing the same
// recipe twice in a row.
const rerollAttempts = 3

func (a *app) sendRecipeList(ctx context.Context, chatID int64) {
	text, kb, err := a.recipeListView(ctx, 0)
	if err != nil {
		a.say(ctx, chatID, a.userError(err, "list recipes"))
		return
	}
	if _, err := sendHTML(ctx, a.api, chatID, text, kb); err != nil {
		a.log.Warn("bot: send recipe list failed", "err", err)
	}
}

func (a *app) showRecipeList(ctx context.Context, r *cbReply, msg *models.Message, page int) {
	text, kb, err := a.recipeListView(ctx, page)
	if err != nil {
		r.answer(ctx, a.userError(err, "list recipes"))
		return
	}
	a.replaceMessage(ctx, msg, text, kb)
}

func (a *app) recipeListView(ctx context.Context, page int) (string, *models.InlineKeyboardMarkup, error) {
	all, err := a.svc.Recipes.List(ctx, domain.RecipeFilter{})
	if err != nil {
		return "", nil, err
	}
	page, pages, from, to := pageBounds(len(all), page)
	return renderRecipeList(len(all)), recipeListKeyboard(all[from:to], page, pages), nil
}

func (a *app) recipeCard(ctx context.Context, r domain.Recipe, reroll bool) card {
	meta := a.meta(ctx, r.AuthorID, nil)
	meta.tags = a.tagLine(ctx, r)
	return recipeCard(r, meta, a.loc, a.web.recipeLink(r.ID), reroll)
}

// tagLine is the cuisine and course labels of a recipe. Tags decorate the
// card, so a lookup failure shows none.
func (a *app) tagLine(ctx context.Context, r domain.Recipe) string {
	if r.CuisineID == nil && len(r.CourseIDs) == 0 {
		return ""
	}
	tags, err := a.svc.RecipeTags.List(ctx)
	if err != nil {
		a.log.Warn("bot: list recipe tags failed", "err", err)
		return ""
	}
	return recipeTagLine(r, tags)
}

func (a *app) openRecipe(ctx context.Context, r *cbReply, chatID int64, id domain.RecipeID) {
	rec, err := a.svc.Recipes.Get(ctx, id)
	if err != nil {
		r.answer(ctx, a.userError(err, "get recipe"))
		return
	}
	r.answer(ctx, "")
	a.sendCard(ctx, chatID, a.recipeCard(ctx, rec, false))
}

// sendRandomRecipe answers /cook with a random recipe other than previous
// (when there is more than one).
func (a *app) sendRandomRecipe(ctx context.Context, chatID int64, previous domain.RecipeID) {
	var (
		rec domain.Recipe
		err error
	)
	for range rerollAttempts {
		rec, err = a.svc.Recipes.Random(ctx)
		if err != nil || rec.ID != previous {
			break
		}
	}
	switch {
	case errors.Is(err, domain.ErrNotFound):
		a.say(ctx, chatID, noRecipesHint)
		return
	case err != nil:
		a.say(ctx, chatID, a.userError(err, "random recipe"))
		return
	}
	a.sendCard(ctx, chatID, a.recipeCard(ctx, rec, true))
}

// cookAgain shows another random recipe; the previous card keeps its own
// buttons but loses «Ещё вариант».
func (a *app) cookAgain(ctx context.Context, r *cbReply, msg *models.Message, previous domain.RecipeID) {
	r.answer(ctx, "🎲")
	if prev, err := a.svc.Recipes.Get(ctx, previous); err == nil {
		a.setKeyboard(ctx, msg, recipeCardKeyboard(prev, a.web.recipeLink(prev.ID), false))
	} else {
		a.setKeyboard(ctx, msg, nil)
	}
	a.sendRandomRecipe(ctx, msg.Chat.ID, previous)
}

func (a *app) deleteRecipe(ctx context.Context, r *cbReply, msg *models.Message, user domain.UserID, id domain.RecipeID) {
	if err := a.svc.Recipes.Delete(ctx, user, id); err != nil && !errors.Is(err, domain.ErrNotFound) {
		r.answer(ctx, a.userError(err, "delete recipe"))
		return
	}
	a.removeCard(ctx, msg)
	// A recipe imported a moment ago has not been announced yet; the
	// partner never hears of it.
	if a.notices != nil && a.notices.forgetImport(id) {
		r.answer(ctx, "Удалено 🗑 Партнёр о нём не узнает")
		return
	}
	r.answer(ctx, "Удалено 🗑")
}

// restoreRecipeButtons puts the card buttons back after «Отмена» on a
// delete confirmation or «← Назад» on the stars row.
func (a *app) restoreRecipeButtons(ctx context.Context, r *cbReply, msg *models.Message, id domain.RecipeID, toast string) {
	rec, err := a.svc.Recipes.Get(ctx, id)
	if err != nil {
		r.answer(ctx, a.userError(err, "get recipe"))
		a.setKeyboard(ctx, msg, nil)
		return
	}
	a.setKeyboard(ctx, msg, recipeCardKeyboard(rec, a.web.recipeLink(rec.ID), false))
	r.answer(ctx, toast)
}

// askCook swaps the card buttons for the stars row of «Приготовили».
func (a *app) askCook(ctx context.Context, r *cbReply, msg *models.Message, id domain.RecipeID) {
	if _, err := a.svc.Recipes.Get(ctx, id); err != nil {
		r.answer(ctx, a.userError(err, "get recipe"))
		if errors.Is(err, domain.ErrNotFound) {
			a.setKeyboard(ctx, msg, nil)
		}
		return
	}
	a.setKeyboard(ctx, msg, cookKeyboard(id))
	r.answer(ctx, "Как получилось? Оцените 🙂")
}

// cookRecipe records a cooking (stars 0 = without a rating) and refreshes
// the card, whose rating and cooking count change.
func (a *app) cookRecipe(ctx context.Context, r *cbReply, msg *models.Message, user domain.UserID, id domain.RecipeID, stars int) {
	key := "cook:" + messageKey(msg)
	if !a.recent.first(key) {
		r.answer(ctx, "Уже отмечено ✓")
		return
	}
	var rating *service.RatingInput
	if stars > 0 {
		rating = &service.RatingInput{Stars: stars}
	}
	if _, err := a.svc.Recipes.Cook(ctx, user, id, rating); err != nil {
		a.recent.forget(key)
		r.answer(ctx, a.userError(err, "cook recipe"))
		if errors.Is(err, domain.ErrNotFound) {
			a.setKeyboard(ctx, msg, nil)
		}
		return
	}
	toast := "🍳 Отмечено!"
	if stars > 0 {
		toast += " ⭐" + strconv.Itoa(stars)
	}
	r.answer(ctx, toast)
	rec, err := a.svc.Recipes.Get(ctx, id)
	if err != nil {
		a.log.Warn("bot: reload cooked recipe failed", "err", err)
		a.setKeyboard(ctx, msg, nil)
		return
	}
	a.editCard(ctx, msg, a.recipeCard(ctx, rec, false))
}

// addRecipeToShopping puts every ingredient of the recipe on the shopping
// list (amounts merge into matching unchecked items).
func (a *app) addRecipeToShopping(ctx context.Context, r *cbReply, msg *models.Message, user domain.UserID, id domain.RecipeID) {
	key := "shop:" + messageKey(msg)
	if !a.recent.first(key) {
		r.answer(ctx, "Уже в списке ✓")
		return
	}
	items, err := a.svc.Shopping.AddFromRecipe(ctx, user, id, nil)
	switch {
	case err != nil:
		a.recent.forget(key)
		r.answer(ctx, a.userError(err, "add recipe to shopping list"))
	case len(items) == 0:
		a.recent.forget(key)
		r.answer(ctx, "В рецепте нет ингредиентов")
	default:
		r.answer(ctx, "Добавлено в список: "+strconv.Itoa(len(items)))
	}
}

// rateCook sets the partner's rating of a cooking from its notice and marks
// the choice on the notice (msg may be nil when it is inaccessible).
func (a *app) rateCook(ctx context.Context, r *cbReply, msg *models.Message, user domain.UserID, id domain.RecipeID, cookID domain.CookID, stars int) {
	cook, err := a.svc.Recipes.Rate(ctx, user, id, cookID, service.RatingInput{Stars: stars})
	if err != nil {
		r.answer(ctx, a.userError(err, "rate cooking"))
		if msg != nil && errors.Is(err, domain.ErrNotFound) {
			a.setKeyboard(ctx, msg, nil)
		}
		return
	}
	r.answer(ctx, "Спасибо! ⭐"+strconv.Itoa(stars))
	if msg == nil {
		return
	}
	rec, err := a.svc.Recipes.Get(ctx, id)
	if err != nil {
		a.log.Warn("bot: reload rated recipe failed", "err", err)
		return
	}
	isPhoto := len(msg.Photo) > 0
	limit := maxMessageLen
	if isPhoto {
		limit = maxCaptionLen
	}
	author := a.meta(ctx, cook.CookedBy, nil).author
	text := renderRecipeCooked(author, rec.Title, starsBy(cook, cook.CookedBy), stars, limit)
	kb := rateKeyboard(rec.ID, cook.ID, a.web.recipeLink(rec.ID), stars)
	if err := editHTML(ctx, a.api, msg.Chat.ID, msg.ID, isPhoto, text, kb); err != nil {
		a.log.Warn("bot: update cooked notice failed", "err", err)
	}
}

// messageKey identifies a message for recentActions.
func messageKey(msg *models.Message) string {
	return strconv.FormatInt(msg.Chat.ID, 10) + ":" + strconv.Itoa(msg.ID)
}
