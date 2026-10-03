package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/auth"
	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

// importConnSlack keeps the connection open a little longer than the
// import (tuning.importDeadline), so the answer to a timed-out import still
// gets written.
const importConnSlack = 10 * time.Second

var (
	errNotARecipe = apiError{status: http.StatusUnprocessableEntity, code: "not_a_recipe", message: "Не нашли в тексте рецепт — вставьте текст с ингредиентами"}
	// A reel without a recipe in its caption, and no model that watches
	// videos configured.
	errRecipeInVideo  = apiError{status: http.StatusUnprocessableEntity, code: "not_a_recipe", message: "Рецепт, похоже, только в видео — вставьте текст рецепта или подписи"}
	errTooManyImports = apiError{status: http.StatusTooManyRequests, code: "rate_limited", message: "Слишком много импортов подряд — подождите минуту."}
	// The user can always fall back to pasting the caption.
	errInstagramDown = apiError{status: http.StatusServiceUnavailable, code: "unavailable", message: "Instagram не отдал пост. Скопируйте текст подписи и вставьте его сюда."}
)

type importInput struct {
	URL  *string `json:"url"`
	Text *string `json:"text"`
}

type importReportJSON struct {
	Source     string   `json:"source"`
	Parser     string   `json:"parser"`
	Confidence float64  `json:"confidence"`
	Image      bool     `json:"image"`
	Warnings   []string `json:"warnings"`
	Duplicate  bool     `json:"duplicate"`
}

// importRecipe creates a recipe from an Instagram link or a pasted caption
// (service.Recipes.Import): 201 with the new recipe, or 200 with the
// existing one when the post was imported before.
func (s *server) importRecipe(w http.ResponseWriter, r *http.Request, u auth.WebAppUser) error {
	// The server's read and write timeouts are shorter than an import; a
	// passed read deadline would also cancel the request context. Extend
	// both for this request only.
	var in importInput
	if err := decodeJSON(w, r, &in); err != nil {
		return err
	}
	// Only after the (small) body is read, so a slow body cannot hold the
	// connection for the length of an import.
	extendDeadlines(w, time.Now().Add(s.importDeadline+importConnSlack))
	if in.URL != nil && in.Text != nil {
		return badRequest("text", "Нужно что-то одно: ссылка или текст.")
	}
	var input service.ImportInput
	if in.URL != nil {
		input.URL = *in.URL
	}
	if in.Text != nil {
		input.Text = *in.Text
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.importDeadline)
	defer cancel()
	recipe, report, err := s.svc.Recipes.Import(ctx, u.ID, input)
	switch {
	case err == nil:
	case errors.Is(err, service.ErrTooManyImports):
		return errTooManyImports
	case errors.Is(err, service.ErrRecipeInVideo):
		return errRecipeInVideo
	case errors.Is(err, service.ErrNotARecipe):
		return errNotARecipe
	case errors.Is(err, domain.ErrExternalUnavailable),
		errors.Is(err, context.DeadlineExceeded) && ctx.Err() != nil && r.Context().Err() == nil:
		return fmt.Errorf("%w: %w", errInstagramDown, err)
	default:
		return err
	}

	p, err := s.presenter(r.Context())
	if err != nil {
		return err
	}
	status := http.StatusCreated
	if report.Duplicate {
		status = http.StatusOK
	}
	warnings := report.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	writeJSON(w, status, struct {
		Recipe recipeJSON       `json:"recipe"`
		Import importReportJSON `json:"import"`
	}{p.recipe(recipe), importReportJSON{
		Source:     report.Source,
		Parser:     report.Parser,
		Confidence: report.Confidence,
		Image:      report.Image,
		Warnings:   warnings,
		Duplicate:  report.Duplicate,
	}})
	return nil
}

// extendDeadlines moves the connection's read and write deadlines. Writers
// that cannot (a test recorder) keep the server's.
func extendDeadlines(w http.ResponseWriter, until time.Time) {
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(until)
	_ = rc.SetWriteDeadline(until)
}
