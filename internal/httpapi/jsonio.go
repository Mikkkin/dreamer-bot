package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"mime"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

const maxJSONBytes = 64 << 10

// decodeJSON reads exactly one JSON value of at most 64 KiB into dst and
// rejects unknown object fields.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		return errNotJSON
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return jsonError(err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		var maxBytes *http.MaxBytesError
		if errors.As(err, &maxBytes) {
			return err
		}
		return badRequest("", "В запросе должен быть ровно один JSON-объект.")
	}
	return nil
}

// jsonError turns a decoding failure into a validation error. Unknown-field
// errors have no type in encoding/json, so their text is inspected; if the
// wording ever changes the result degrades to a generic message.
func jsonError(err error) error {
	var (
		maxBytes *http.MaxBytesError
		typeErr  *json.UnmarshalTypeError
	)
	switch {
	case errors.As(err, &maxBytes):
		return err
	case errors.As(err, &typeErr):
		return badRequest(typeErr.Field, "Неверный тип значения.")
	case errors.Is(err, io.EOF):
		return badRequest("", "Пустой запрос.")
	}
	if field, ok := strings.CutPrefix(err.Error(), "json: unknown field "); ok {
		if unquoted, err := strconv.Unquote(field); err == nil {
			field = unquoted
		}
		return badRequest(field, "Неизвестное поле.")
	}
	return badRequest("", "Некорректный JSON.")
}

// patchFields is a partial update: absent keys are missing from the map and
// explicit nulls are kept as the literal null.
type patchFields map[string]json.RawMessage

// decodePatch reads a JSON object for PATCH and rejects keys not in allowed.
func decodePatch(w http.ResponseWriter, r *http.Request, allowed ...string) (patchFields, error) {
	var fields patchFields
	if err := decodeJSON(w, r, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return nil, badRequest("", "Ожидается JSON-объект.")
	}
	for _, key := range fields.keys() {
		if !slices.Contains(allowed, key) {
			return nil, badRequest(key, "Неизвестное поле.")
		}
	}
	return fields, nil
}

// keys returns the present keys in a stable order, so that the first
// reported error does not depend on map iteration.
func (p patchFields) keys() []string { return slices.Sorted(maps.Keys(p)) }

func isNull(raw json.RawMessage) bool { return bytes.Equal(bytes.TrimSpace(raw), []byte("null")) }

// decodeValue strictly decodes one field value; nested objects reject
// unknown keys as well.
func decodeValue(raw json.RawMessage, field string, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return badRequest(field, "Неверное значение поля.")
	}
	return nil
}

// patchValue decodes a non-nullable field. null resets it to the zero value
// ("cleared"); the domain then decides whether that is allowed, e.g. an
// empty title is rejected with the usual validation message.
func patchValue[T any](raw json.RawMessage, field string) (domain.Optional[T], error) {
	var v T
	if !isNull(raw) {
		if err := decodeValue(raw, field, &v); err != nil {
			return domain.Optional[T]{}, err
		}
	}
	return domain.Some(v), nil
}

// patchNullable decodes a nullable field; null clears it.
func patchNullable[T any](raw json.RawMessage, field string) (domain.Optional[*T], error) {
	if isNull(raw) {
		return domain.Some[*T](nil), nil
	}
	v := new(T)
	if err := decodeValue(raw, field, v); err != nil {
		return domain.Optional[*T]{}, err
	}
	return domain.Some(v), nil
}

// parseID accepts only canonical positive decimal IDs.
func parseID(raw string) (int64, bool) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != raw {
		return 0, false
	}
	return id, true
}

// pathID reads a path parameter as an entity ID. A malformed ID cannot name
// an existing entity, so it is reported as not found.
func pathID[T ~int64](r *http.Request, name string) (T, error) {
	id, ok := parseID(r.PathValue(name))
	if !ok {
		return 0, errNotFound
	}
	return T(id), nil
}
