package domain

import "time"

// Identifier types keep IDs of different entities from being mixed up.
type (
	UserID     int64
	WishID     int64
	CategoryID int64
	ImageID    int64
)

// MaxImagesPerWish caps how many photos a single wish can hold.
const MaxImagesPerWish = 10

// Wish is a single shared dream. Link and Price are optional by design:
// the author decides whether to specify them.
type Wish struct {
	ID          WishID
	Title       string
	Note        string
	CategoryID  *CategoryID
	Link        *string
	Price       *Money
	Status      Status
	Hot         bool // «очень хочу 🔥»
	AuthorID    UserID
	Images      []Image
	CreatedAt   time.Time
	UpdatedAt   time.Time
	FulfilledAt *time.Time
}

// WishDraft is the raw input for a new wish; NewWish validates it.
type WishDraft struct {
	Title      string
	Note       string
	CategoryID *CategoryID
	Link       *string
	Price      *Money
	Hot        bool
}

// NewWish validates the draft and builds a wish authored by author.
// Price must already be a validated Money (see NewMoney / ParseAmount).
func NewWish(d WishDraft, author UserID, now time.Time) (Wish, error) {
	title, err := NormalizeTitle(d.Title)
	if err != nil {
		return Wish{}, err
	}
	note, err := NormalizeNote(d.Note)
	if err != nil {
		return Wish{}, err
	}
	link, err := normalizeOptionalLink(d.Link)
	if err != nil {
		return Wish{}, err
	}
	price, err := validateOptionalPrice(d.Price)
	if err != nil {
		return Wish{}, err
	}
	now = now.UTC()
	return Wish{
		Title:      title,
		Note:       note,
		CategoryID: d.CategoryID,
		Link:       link,
		Price:      price,
		Status:     StatusWant,
		Hot:        d.Hot,
		AuthorID:   author,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

// Optional marks a field of a partial update. Set=false leaves the field
// untouched; Set=true with a nil pointer value clears a nullable field.
type Optional[T any] struct {
	Set   bool
	Value T
}

// Some returns an Optional that is set to v.
func Some[T any](v T) Optional[T] { return Optional[T]{Set: true, Value: v} }

// WishPatch is a partial update; only fields with Set=true change.
type WishPatch struct {
	Title      Optional[string]
	Note       Optional[string]
	CategoryID Optional[*CategoryID]
	Link       Optional[*string]
	Price      Optional[*Money]
	Hot        Optional[bool]
}

// Apply validates every set field first and only then mutates the wish, so a
// failed patch leaves the wish unchanged.
func (w *Wish) Apply(p WishPatch, now time.Time) error {
	next := *w
	if p.Title.Set {
		title, err := NormalizeTitle(p.Title.Value)
		if err != nil {
			return err
		}
		next.Title = title
	}
	if p.Note.Set {
		note, err := NormalizeNote(p.Note.Value)
		if err != nil {
			return err
		}
		next.Note = note
	}
	if p.CategoryID.Set {
		next.CategoryID = p.CategoryID.Value
	}
	if p.Link.Set {
		link, err := normalizeOptionalLink(p.Link.Value)
		if err != nil {
			return err
		}
		next.Link = link
	}
	if p.Price.Set {
		price, err := validateOptionalPrice(p.Price.Value)
		if err != nil {
			return err
		}
		next.Price = price
	}
	if p.Hot.Set {
		next.Hot = p.Hot.Value
	}
	next.UpdatedAt = now.UTC()
	*w = next
	return nil
}

// SetStatus moves the wish to status s. Entering StatusDone stamps
// FulfilledAt; leaving it clears the stamp. It reports whether the wish has
// just been fulfilled (a transition into StatusDone).
func (w *Wish) SetStatus(s Status, now time.Time) (fulfilled bool, err error) {
	if _, err := ParseStatus(string(s)); err != nil {
		return false, err
	}
	if w.Status == s {
		return false, nil
	}
	now = now.UTC()
	fulfilled = s == StatusDone
	if fulfilled {
		w.FulfilledAt = &now
	} else {
		w.FulfilledAt = nil
	}
	w.Status = s
	w.UpdatedAt = now
	return fulfilled, nil
}

// CanAddImage reports whether one more image fits into the wish.
func (w *Wish) CanAddImage() bool { return len(w.Images) < MaxImagesPerWish }

// Cover returns the first image of the wish, if any.
func (w *Wish) Cover() (Image, bool) {
	if len(w.Images) == 0 {
		return Image{}, false
	}
	return w.Images[0], true
}

func normalizeOptionalLink(link *string) (*string, error) {
	if link == nil {
		return nil, nil
	}
	s, err := NormalizeLink(*link)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func validateOptionalPrice(price *Money) (*Money, error) {
	if price == nil {
		return nil, nil
	}
	m, err := NewMoney(price.Minor, price.Currency)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// WishFilter narrows a wish listing. Zero values mean "no filter".
type WishFilter struct {
	Status *Status
	// CategoryID filters by category. Use Uncategorized to select wishes
	// without a category.
	CategoryID *CategoryID
	// Query is a case-insensitive substring of the title.
	Query string
}

// Uncategorized is the sentinel CategoryID used in filters to select wishes
// that have no category.
const Uncategorized CategoryID = 0
