package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/recipeimport"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

// recipeImporter adapts internal/recipeimport to service.RecipeImporter.
// Every user gets an own daily quota of videos read by the model.
type recipeImporter struct {
	base     recipeimport.Importer
	log      *slog.Logger
	perDay   int
	mu       sync.Mutex
	quotaFor map[domain.UserID]*recipeimport.VideoQuota
}

var _ service.RecipeImporter = (*recipeImporter)(nil)

// newRecipeImporter builds the importer; llm may be nil (rules only).
func newRecipeImporter(llm recipeimport.LLM, log *slog.Logger) *recipeImporter {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &recipeImporter{
		base:     recipeimport.Importer{LLM: llm},
		log:      log,
		perDay:   recipeimport.DefaultVideoDailyLimit,
		quotaFor: make(map[domain.UserID]*recipeimport.VideoQuota),
	}
}

func (a *recipeImporter) Canonical(raw string) (string, bool) {
	ref, ok := recipeimport.ParseURL(raw)
	if !ok {
		return "", false
	}
	return ref.URL(), true
}

func (a *recipeImporter) PostKey(raw string) (string, bool) {
	ref, ok := recipeimport.ParseURL(raw)
	if !ok {
		return "", false
	}
	return ref.Shortcode, true
}

func (a *recipeImporter) FromURL(ctx context.Context, actor domain.UserID, raw string) (service.ImportResult, error) {
	im := a.base
	im.Videos = a.videos(actor)
	res, err := im.FromURL(ctx, raw)
	a.diagnose(res.Report, err)
	return importResult(res), importError(err)
}

func (a *recipeImporter) FromText(ctx context.Context, text string) (service.ImportResult, error) {
	res, err := a.base.FromText(ctx, text)
	a.diagnose(res.Report, err)
	return importResult(res), importError(err)
}

// diagnose logs why a model could not help (a revoked key, an exhausted
// quota, a timeout): the user only sees a degraded import. Diagnostics are
// built from fixed words and status codes, never keys, URLs or bodies.
func (a *recipeImporter) diagnose(r recipeimport.Report, err error) {
	if len(r.Diagnostics) > 0 {
		a.log.Warn("recipe import: model trouble", slog.Any("diagnostics", r.Diagnostics))
	}
	if errors.Is(err, recipeimport.ErrRecipeInVideo) {
		a.log.Info("recipe import: recipe only in the video", slog.String("hint", recipeimport.OperatorHintVideo))
	}
}

// videos returns the user's quota. Only whitelisted users reach the
// importer, so the map stays as small as the whitelist.
func (a *recipeImporter) videos(id domain.UserID) *recipeimport.VideoQuota {
	a.mu.Lock()
	defer a.mu.Unlock()
	q, ok := a.quotaFor[id]
	if !ok {
		q = recipeimport.NewVideoQuota(a.perDay)
		a.quotaFor[id] = q
	}
	return q
}

func importResult(r recipeimport.Result) service.ImportResult {
	return service.ImportResult{
		Draft: r.Draft,
		Image: r.Image,
		Report: service.ImportReport{
			Source:     r.Report.Source,
			Parser:     r.Report.Parser,
			Confidence: r.Report.Confidence,
			Image:      r.Report.Image,
			Warnings:   r.Report.Warnings,
		},
	}
}

// importError maps the package's errors to the service's. Unavailability
// already wraps domain.ErrExternalUnavailable.
func importError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, recipeimport.ErrRecipeInVideo):
		return fmt.Errorf("%w: %w", service.ErrRecipeInVideo, err)
	case errors.Is(err, recipeimport.ErrNotARecipe):
		return fmt.Errorf("%w: %w", service.ErrNotARecipe, err)
	case errors.Is(err, recipeimport.ErrBadURL):
		// The service checks the link first; this is a fallback.
		return &domain.ValidationError{Field: "url", Message: "это не ссылка на пост или рилс в Instagram"}
	default:
		return err
	}
}
