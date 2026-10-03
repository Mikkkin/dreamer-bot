package httpapi

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// An external service that fails is a 503 unavailable; a handler may give
// its own message, which wins even around a wrapped domain error.
func TestExternalUnavailableIs503(t *testing.T) {
	e := classify(fmt.Errorf("fetch post: %w", domain.ErrExternalUnavailable))
	if e.status != http.StatusServiceUnavailable || e.code != "unavailable" || e.message == "" || e.field != "" {
		t.Fatalf("classify = %+v", e)
	}
	own := apiError{status: http.StatusServiceUnavailable, code: "unavailable", message: "Свой текст."}
	if got := classify(fmt.Errorf("%w: %w", own, domain.ErrExternalUnavailable)); got != own {
		t.Errorf("a handler's own 503 must win, got %+v", got)
	}
}
