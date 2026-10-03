package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"unicode"
	"unicode/utf8"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// apiError is a response-ready error. Message is Russian and safe to show to
// users; it never contains internal details.
type apiError struct {
	status  int
	code    string
	message string
	field   string
}

func (e apiError) Error() string { return e.code + ": " + e.message }

var (
	errUnauthorized     = apiError{status: http.StatusUnauthorized, code: "unauthorized", message: "Не удалось подтвердить вход через Telegram. Откройте приложение заново."}
	errForbidden        = apiError{status: http.StatusForbidden, code: "forbidden", message: "Это приватный список, доступ только для своих."}
	errMediaForbidden   = apiError{status: http.StatusForbidden, code: "forbidden", message: "Ссылка на фото устарела. Обновите страницу."}
	errNotFound         = apiError{status: http.StatusNotFound, code: "not_found", message: "Ничего не найдено."}
	errConflict         = apiError{status: http.StatusConflict, code: "conflict", message: "Такая запись уже есть."}
	errBodyTooLarge     = apiError{status: http.StatusRequestEntityTooLarge, code: "too_large", message: "Слишком большой запрос."}
	errImageTooLarge    = apiError{status: http.StatusRequestEntityTooLarge, code: "too_large", message: "Фото слишком большое."}
	errNotJSON          = apiError{status: http.StatusUnsupportedMediaType, code: "unsupported_media", message: "Запрос должен быть в формате JSON."}
	errNotMultipart     = apiError{status: http.StatusUnsupportedMediaType, code: "unsupported_media", message: "Фото нужно отправить как multipart/form-data."}
	errImageUnsupported = apiError{status: http.StatusUnsupportedMediaType, code: "unsupported_media", message: "Поддерживаются только фото в форматах JPEG, PNG и WebP."}
	errLimit            = apiError{status: http.StatusUnprocessableEntity, code: "limit", message: "Достигнут лимит."}
	errRateLimited      = apiError{status: http.StatusTooManyRequests, code: "rate_limited", message: "Слишком много запросов. Подождите немного."}
	errInternal         = apiError{status: http.StatusInternalServerError, code: "internal", message: "Что-то пошло не так. Попробуйте ещё раз."}
	// An external service failed (domain.ErrExternalUnavailable). A handler
	// that knows a better fallback returns its own 503 apiError instead.
	errExternalDown = apiError{status: http.StatusServiceUnavailable, code: "unavailable", message: "Сервис сейчас не отвечает. Попробуйте чуть позже."}
)

// badRequest is a 400 validation error. field may be empty when the whole
// request is malformed.
func badRequest(field, message string) apiError {
	return apiError{status: http.StatusBadRequest, code: "validation", message: message, field: field}
}

// classify maps any error returned by a handler to its HTTP response. It is
// the only place where domain errors meet HTTP status codes.
func classify(err error) apiError {
	if v, ok := domain.AsValidation(err); ok {
		return badRequest(v.Field, capitalize(v.Message))
	}
	var (
		ae       apiError
		maxBytes *http.MaxBytesError
	)
	switch {
	case errors.As(err, &ae):
		return ae
	case errors.Is(err, domain.ErrNotFound):
		return errNotFound
	case errors.Is(err, domain.ErrConflict):
		return errConflict
	case errors.Is(err, domain.ErrLimitExceeded):
		return errLimit
	case errors.Is(err, domain.ErrImageTooLarge):
		return errImageTooLarge
	case errors.As(err, &maxBytes):
		return errBodyTooLarge
	case errors.Is(err, domain.ErrImageUnsupported):
		return errImageUnsupported
	case errors.Is(err, domain.ErrExternalUnavailable):
		return errExternalDown
	default:
		return errInternal
	}
}

// fail writes the response for err and logs it when it is unexpected.
func (s *server) fail(w http.ResponseWriter, r *http.Request, err error) {
	e := classify(err)
	switch {
	case e.status < http.StatusInternalServerError:
	case errors.Is(err, context.Canceled) && r.Context().Err() != nil:
		s.log.DebugContext(r.Context(), "client went away", "route", r.Pattern)
	case e.status == http.StatusServiceUnavailable:
		// An outage of an external service is expected now and then.
		s.log.WarnContext(r.Context(), "external service unavailable", "route", r.Pattern, "err", err)
	default:
		s.log.ErrorContext(r.Context(), "request failed", "route", r.Pattern, "err", err)
	}
	writeError(w, e)
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

func writeError(w http.ResponseWriter, e apiError) {
	writeJSON(w, e.status, struct {
		Error errorBody `json:"error"`
	}{errorBody{Code: e.code, Message: e.message, Field: e.field}})
}

// fallbackBody is sent if a response cannot be encoded, which only a
// programming error in a DTO can cause.
const fallbackBody = `{"error":{"code":"internal","message":"Что-то пошло не так. Попробуйте ещё раз."}}`

func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		status, body = http.StatusInternalServerError, []byte(fallbackBody)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}

// capitalize upper-cases the first letter: domain messages are lower-case so
// that the bot can embed them into sentences.
func capitalize(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}
