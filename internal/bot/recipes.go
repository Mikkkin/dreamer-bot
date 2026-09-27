package bot

import (
	"context"
	"errors"

	"github.com/go-telegram/bot/models"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
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
	return recipeCard(r, a.meta(ctx, r.AuthorID, nil), a.loc, a.web.recipeLink(r.ID), reroll)
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
	r.answer(ctx, "Удалено 🗑")
}

func (a *app) keepRecipe(ctx context.Context, r *cbReply, msg *models.Message, id domain.RecipeID) {
	rec, err := a.svc.Recipes.Get(ctx, id)
	if err != nil {
		r.answer(ctx, a.userError(err, "get recipe"))
		a.setKeyboard(ctx, msg, nil)
		return
	}
	a.setKeyboard(ctx, msg, recipeCardKeyboard(rec, a.web.recipeLink(rec.ID), false))
	r.answer(ctx, "Оставили 👌")
}
