package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/time/rate"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

var (
	// ErrNotARecipe means the imported post or text holds no recipe: no
	// ingredients and no steps, or too little to trust.
	ErrNotARecipe = errors.New("not a recipe")
	// ErrRecipeInVideo is the ErrNotARecipe of a reel whose caption holds
	// no recipe while no model that watches videos is configured: the
	// recipe is probably only in the video.
	ErrRecipeInVideo = fmt.Errorf("%w: the recipe is probably only in the video", ErrNotARecipe)
	// ErrTooManyImports means the user started too many imports in a row.
	ErrTooManyImports = errors.New("too many imports")
)

// Import throttling shared by the bot and the Mini App: a burst of
// importBurst per user, then one per importEvery; at most importsAtOnce
// imports run at a time (each may hold a downloaded video).
const (
	importBurst   = 5
	importEvery   = 20 * time.Second
	importsAtOnce = 2
)

// Import sources and parsers, as reported in ImportReport.
const (
	ImportSourceInstagram = "instagram"
	ImportSourceText      = "text"
	ImportParserRules     = "rules"
	ImportParserLLM       = "llm"
	ImportParserVideo     = "video"
)

// MaxImportTextLen caps a pasted text, like the body of a recipe.
const MaxImportTextLen = domain.MaxRecipeBodyLen

// coverFailed is the warning for a cover that was downloaded but could not
// be stored.
const coverFailed = "Обложку не удалось сохранить"

func (s recipeService) Import(ctx context.Context, actor domain.UserID, in ImportInput) (domain.Recipe, ImportReport, error) {
	if s.importer == nil {
		return domain.Recipe{}, ImportReport{}, fmt.Errorf("recipe import is not configured: %w", domain.ErrExternalUnavailable)
	}
	link, text, err := s.importInput(in)
	if err != nil {
		return domain.Recipe{}, ImportReport{}, err
	}

	var res ImportResult
	if link != "" {
		// The same post sent twice at once (a double tap, or both partners)
		// is imported once; the second request then finds it below.
		unlock, err := s.importing.lock(ctx, link)
		if err != nil {
			return domain.Recipe{}, ImportReport{}, err
		}
		defer unlock()
		existing, found, err := s.importedBefore(ctx, link)
		if err != nil {
			return domain.Recipe{}, ImportReport{}, err
		}
		if found {
			s.log.Info("recipe import skipped: post imported before",
				slog.Int64("recipe_id", int64(existing.ID)), slog.Int64("user_id", int64(actor)))
			return existing, ImportReport{
				Source:    ImportSourceInstagram,
				Image:     len(existing.Images) > 0,
				Warnings:  []string{},
				Duplicate: true,
			}, nil
		}
	}
	if !s.imports.allow(actor) {
		return domain.Recipe{}, ImportReport{}, ErrTooManyImports
	}
	release, err := s.imports.acquire(ctx)
	if err != nil {
		return domain.Recipe{}, ImportReport{}, err
	}
	defer release()
	switch {
	case text == "":
		res, err = s.importer.FromURL(ctx, actor, link)
	default:
		res, err = s.importer.FromText(ctx, text)
		if err == nil && link != "" {
			res.Draft.Link = &link
		}
	}
	if err != nil {
		return domain.Recipe{}, ImportReport{}, err
	}

	// The import is paid for (a model may have read the post): store it
	// even if the client stops waiting now.
	ctx = context.WithoutCancel(ctx)
	r, err := s.insert(ctx, actor, res.Draft)
	if err != nil {
		return domain.Recipe{}, ImportReport{}, err
	}
	report := res.Report
	report.Warnings = append([]string{}, report.Warnings...)
	report.Image, report.Duplicate = false, false
	if len(res.Image) > 0 {
		if r, err = s.attachCover(ctx, r, res.Image); err != nil {
			s.log.Warn("store imported cover", slog.Int64("recipe_id", int64(r.ID)), slog.Any("error", err))
			report.Warnings = append(report.Warnings, coverFailed)
		} else {
			report.Image = true
		}
	}
	s.log.Info("recipe imported",
		slog.Int64("recipe_id", int64(r.ID)),
		slog.Int64("user_id", int64(actor)),
		slog.String("source", report.Source),
		slog.String("parser", report.Parser),
		slog.String("confidence", strconv.FormatFloat(report.Confidence, 'f', 2, 64)),
		slog.Int("ingredients", len(r.Ingredients)),
		slog.Bool("image", report.Image),
		slog.Int("warnings", len(report.Warnings)))
	s.notify(ctx, "recipe_imported", actor, func(ctx context.Context, rc Recipients) {
		s.notifier.RecipeImported(ctx, rc, r)
	})
	return r, report, nil
}

// importInput checks that exactly one of the link and the text is given
// and returns the post's canonical link or the trimmed text.
func (s recipeService) importInput(in ImportInput) (link, text string, err error) {
	raw, text := strings.TrimSpace(in.URL), strings.TrimSpace(in.Text)
	switch {
	case raw == "" && text == "":
		return "", "", &domain.ValidationError{Field: "url", Message: "вставьте ссылку на пост в Instagram или текст рецепта"}
	case raw != "" && text != "":
		return "", "", &domain.ValidationError{Field: "text", Message: "нужно что-то одно: ссылка или текст"}
	case len(raw) > domain.MaxLinkLen:
		return "", "", &domain.ValidationError{Field: "url", Message: "ссылка слишком длинная (максимум " + strconv.Itoa(domain.MaxLinkLen) + " символов)"}
	case utf8.RuneCountInString(text) > MaxImportTextLen:
		return "", "", &domain.ValidationError{Field: "text", Message: "текст слишком длинный (максимум " + strconv.Itoa(MaxImportTextLen) + " символов)"}
	case text != "":
		// A caption may name the post it came from; a bad link is ignored.
		link, _ := s.importer.Canonical(strings.TrimSpace(in.Link))
		return link, text, nil
	}
	link, ok := s.importer.Canonical(raw)
	if !ok {
		return "", "", &domain.ValidationError{Field: "url", Message: "это не ссылка на пост или рилс в Instagram"}
	}
	return link, "", nil
}

// importedBefore finds a recipe whose link names the same post, whether it
// was imported or typed in by hand.
func (s recipeService) importedBefore(ctx context.Context, link string) (domain.Recipe, bool, error) {
	recipes, err := s.repos.ListRecipes(ctx, domain.RecipeFilter{})
	if err != nil {
		return domain.Recipe{}, false, err
	}
	want, ok := s.importer.PostKey(link)
	if !ok {
		return domain.Recipe{}, false, nil
	}
	for _, r := range recipes {
		if r.Link == nil {
			continue
		}
		if key, ok := s.importer.PostKey(*r.Link); ok && key == want {
			return r, true, nil
		}
	}
	return domain.Recipe{}, false, nil
}

// importLimits throttles imports: a token bucket per user and a semaphore
// for the imports running at once.
type importLimits struct {
	mu      sync.Mutex
	perUser map[domain.UserID]*rate.Limiter
	running chan struct{}
}

func newImportLimits() *importLimits {
	return &importLimits{perUser: make(map[domain.UserID]*rate.Limiter), running: make(chan struct{}, importsAtOnce)}
}

// allow takes one import from the user's bucket. Only whitelisted users
// import, so the map stays as small as the whitelist.
func (l *importLimits) allow(user domain.UserID) bool {
	l.mu.Lock()
	lim, ok := l.perUser[user]
	if !ok {
		lim = rate.NewLimiter(rate.Every(importEvery), importBurst)
		l.perUser[user] = lim
	}
	l.mu.Unlock()
	return lim.Allow()
}

// acquire waits for a free import slot or the end of ctx.
func (l *importLimits) acquire(ctx context.Context) (release func(), err error) {
	select {
	case l.running <- struct{}{}:
		return func() { <-l.running }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// attachCover stores the post's cover as the recipe's first photo through
// the same media pipeline as an upload (re-encoded, metadata stripped) and
// returns the recipe with it. Unlike AddImage it tells nobody: the import
// notice shows the cover.
func (s recipeService) attachCover(ctx context.Context, r domain.Recipe, cover []byte) (domain.Recipe, error) {
	if _, err := s.storeImage(ctx, RecipeImages(r.ID), domain.MaxImagesPerRecipe, bytes.NewReader(cover)); err != nil {
		return r, err
	}
	stored, err := s.repos.GetRecipe(ctx, r.ID)
	if err != nil {
		return r, err
	}
	return stored, nil
}

// keyedLocks is a set of mutexes keyed by string, created on demand.
type keyedLocks struct {
	mu   sync.Mutex
	held map[string]chan struct{}
}

func newKeyedLocks() *keyedLocks { return &keyedLocks{held: make(map[string]chan struct{})} }

// lock waits until key is free or ctx ends, then holds key until unlock is
// called.
func (k *keyedLocks) lock(ctx context.Context, key string) (unlock func(), err error) {
	for {
		k.mu.Lock()
		busy, ok := k.held[key]
		if !ok {
			done := make(chan struct{})
			k.held[key] = done
			k.mu.Unlock()
			return func() {
				k.mu.Lock()
				delete(k.held, key)
				k.mu.Unlock()
				close(done)
			}, nil
		}
		k.mu.Unlock()
		select {
		case <-busy:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
