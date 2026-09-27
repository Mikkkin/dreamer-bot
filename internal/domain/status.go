package domain

// Status is the lifecycle stage of a wish.
type Status string

const (
	StatusWant     Status = "want"     // «Хотим»
	StatusProgress Status = "progress" // «Копим» / в процессе
	StatusDone     Status = "done"     // «Сбылось ✨»
)

// Statuses lists every status in display order.
var Statuses = [...]Status{StatusWant, StatusProgress, StatusDone}

// ParseStatus converts a raw string into a Status.
func ParseStatus(raw string) (Status, error) {
	switch s := Status(raw); s {
	case StatusWant, StatusProgress, StatusDone:
		return s, nil
	default:
		return "", invalid("status", "неизвестный статус")
	}
}

// Label is the human-readable Russian name of the status.
func (s Status) Label() string {
	switch s {
	case StatusWant:
		return "Хотим"
	case StatusProgress:
		return "Копим"
	case StatusDone:
		return "Сбылось"
	default:
		return string(s)
	}
}

// Emoji is the icon used for the status in chat messages.
func (s Status) Emoji() string {
	switch s {
	case StatusWant:
		return "💭"
	case StatusProgress:
		return "⏳"
	case StatusDone:
		return "✨"
	default:
		return ""
	}
}
