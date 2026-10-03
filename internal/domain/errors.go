// Package domain holds the core business types of the wishlist: wishes,
// categories, money, images and users. It has no dependencies on transport
// (Telegram, HTTP) or storage, and every constructor validates its input so
// that an invalid entity can never be created.
package domain

import (
	"errors"
	"fmt"
)

var (
	// ErrNotFound is returned when a requested entity does not exist.
	ErrNotFound = errors.New("not found")
	// ErrConflict is returned when an operation violates a uniqueness rule.
	ErrConflict = errors.New("conflict")
	// ErrLimitExceeded is returned when a per-entity quota would be exceeded.
	ErrLimitExceeded = errors.New("limit exceeded")
	// ErrImageTooLarge is returned for uploads above the size or pixel limit.
	ErrImageTooLarge = errors.New("image too large")
	// ErrImageUnsupported is returned for payloads that are not JPEG, PNG or WebP.
	ErrImageUnsupported = errors.New("unsupported image format")
	// ErrExternalUnavailable is returned when an external service the server
	// calls on a user's request (e.g. Instagram for a recipe import) fails,
	// refuses or does not answer in time.
	ErrExternalUnavailable = errors.New("external service unavailable")
)

// ValidationError describes invalid user input. Message is written in
// Russian because it is shown to end users verbatim (bot and Mini App).
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("invalid %s: %s", e.Field, e.Message)
}

func invalid(field, message string) error {
	return &ValidationError{Field: field, Message: message}
}

// AsValidation reports whether err is a *ValidationError and returns it.
func AsValidation(err error) (*ValidationError, bool) {
	var v *ValidationError
	if errors.As(err, &v) {
		return v, true
	}
	return nil, false
}
