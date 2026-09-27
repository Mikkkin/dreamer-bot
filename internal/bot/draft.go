package bot

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-telegram/bot/models"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// entityKind is what a draft will become when saved.
type entityKind uint8

const (
	kindWish entityKind = iota
	kindRecipe
)

// draftField is a draft field the user is asked to type in.
type draftField uint8

const (
	fieldNone draftField = iota
	fieldTitle
	fieldPrice
	fieldLink
	fieldText
)

// draftView selects what the draft card currently shows.
type draftView uint8

const (
	viewMain draftView = iota
	viewCategories
)

const (
	draftTTL = 30 * time.Minute
	// photoJoinWindow is how long after the last activity a photo without a
	// caption still joins the current draft instead of starting a new one.
	photoJoinWindow = 10 * time.Minute
	// maxDraftPhotos is the larger of the per-entity image limits; the
	// entity-specific cap is applied when saving.
	maxDraftPhotos = max(domain.MaxImagesPerWish, domain.MaxImagesPerRecipe)
	// clearToken is what the user sends to clear an optional field.
	clearToken = "-"
)

// draft is the in-progress quick-add of one user. It is a value: handlers
// take a copy from the store, change it and put it back, so nothing is
// shared between goroutines.
type draft struct {
	id     uint32
	chatID int64
	cardID int // message ID of the draft card; 0 until it is sent

	kind          entityKind
	title         string
	text          string // the note of a wish or the body of a recipe
	link          *string
	price         *domain.Money
	category      *domain.CategoryID
	categoryLabel string
	hot           bool
	photos        []string // Telegram file IDs, largest size
	albumID       string   // media_group_id of the album being collected

	awaiting draftField
	promptID int // message ID of the ForceReply prompt
	view     draftView

	created time.Time
	touched time.Time
}

// applyParsed fills the draft from a freshly parsed message.
func (d *draft) applyParsed(p parsedInput) {
	d.title, d.text, d.link, d.price = p.Title, p.Text, p.Link, p.Price
}

// mergeParsed folds the caption of a later album photo into the draft:
// fields that are already set win, extra text is appended to the note.
func (d *draft) mergeParsed(p parsedInput) {
	extra := p.Text
	if d.title == "" {
		d.title = p.Title
	} else if p.Title != "" {
		extra = joinLines(p.Title, p.Text)
	}
	d.text = joinLines(d.text, extra)
	if d.link == nil {
		d.link = p.Link
	}
	if d.price == nil {
		d.price = p.Price
	}
}

func joinLines(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + "\n" + b
}

// addPhoto appends a photo unless the draft is full.
func (d *draft) addPhoto(fileID string) bool {
	if fileID == "" || len(d.photos) >= maxDraftPhotos || slices.Contains(d.photos, fileID) {
		return false
	}
	d.photos = append(d.photos, fileID)
	return true
}

// acceptsPhoto reports whether a photo from album (may be "") joins this
// draft: photos of the same album always do, other photos only while the
// draft is recent.
func (d *draft) acceptsPhoto(album string, now time.Time) bool {
	if album != "" && album == d.albumID {
		return true
	}
	return now.Sub(d.touched) < photoJoinWindow
}

func (d *draft) setKind(k entityKind) {
	d.kind = k
	d.view = viewMain
	if !d.fieldApplies(d.awaiting) {
		d.awaiting = fieldNone
	}
}

// fieldApplies reports whether the field exists for the current kind.
func (d *draft) fieldApplies(f draftField) bool {
	switch f {
	case fieldTitle, fieldLink, fieldText:
		return true
	case fieldPrice:
		return d.kind == kindWish
	}
	return false
}

func (d *draft) setCategory(id *domain.CategoryID, label string) {
	d.category, d.categoryLabel = id, label
	d.view = viewMain
}

// fill validates input for the awaited field and stores it. On error the
// draft is unchanged and still awaiting, so the user can simply retry.
func (d *draft) fill(input string, entities []models.MessageEntity, fallback domain.Currency) error {
	value := strings.TrimSpace(input)
	clearing := value == clearToken
	switch d.awaiting {
	case fieldTitle:
		title, err := domain.NormalizeTitle(value)
		if err != nil {
			return err
		}
		d.title = title
	case fieldPrice:
		if clearing {
			d.price = nil
			break
		}
		if d.price != nil {
			fallback = d.price.Currency
		}
		m, err := parseMoneyInput(value, fallback)
		if err != nil {
			return err
		}
		d.price = &m
	case fieldLink:
		if clearing {
			d.link = nil
			break
		}
		link, err := linkInput(value, entities)
		if err != nil {
			return err
		}
		d.link = &link
	case fieldText:
		if clearing {
			d.text = ""
			break
		}
		text, err := d.normalizeText(input)
		if err != nil {
			return err
		}
		d.text = text
	default:
		return nil
	}
	d.awaiting, d.promptID = fieldNone, 0
	return nil
}

func (d *draft) normalizeText(s string) (string, error) {
	if d.kind == kindRecipe {
		return domain.NormalizeRecipeBody(s)
	}
	return domain.NormalizeNote(s)
}

func (d *draft) wishDraft() domain.WishDraft {
	return domain.WishDraft{
		Title:      d.title,
		Note:       d.text,
		CategoryID: d.category,
		Link:       d.link,
		Price:      d.price,
		Hot:        d.hot,
	}
}

func (d *draft) recipeDraft() domain.RecipeDraft {
	return domain.RecipeDraft{Title: d.title, Link: d.link, Body: d.text}
}

// photoLimit is the image cap of the entity the draft becomes.
func (d *draft) photoLimit() int {
	if d.kind == kindRecipe {
		return domain.MaxImagesPerRecipe
	}
	return domain.MaxImagesPerWish
}

func (d *draft) clone() draft {
	c := *d
	c.photos = slices.Clone(d.photos)
	return c
}

// draftStore keeps at most one draft per user in memory. Drafts expire after
// ttl without activity; get treats expired drafts as absent, and the janitor
// only reclaims memory.
type draftStore struct {
	mu     sync.Mutex
	drafts map[domain.UserID]draft
	seq    uint32
	ttl    time.Duration
	now    func() time.Time
}

func newDraftStore(ttl time.Duration, now func() time.Time) *draftStore {
	return &draftStore{drafts: make(map[domain.UserID]draft), ttl: ttl, now: now}
}

// begin returns a new, not yet stored draft with a fresh ID.
func (s *draftStore) begin(chatID int64) draft {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	if s.seq == 0 { // wrapped around; 0 is never a valid draft ID
		s.seq = 1
	}
	now := s.now()
	return draft{id: s.seq, chatID: chatID, created: now, touched: now}
}

// get returns a copy of the user's live draft.
func (s *draftStore) get(user domain.UserID) (draft, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.drafts[user]
	if !ok {
		return draft{}, false
	}
	if s.expired(d) {
		delete(s.drafts, user)
		return draft{}, false
	}
	return d.clone(), true
}

// lookup returns the user's live draft only if it has the given ID, so that
// buttons of replaced or expired drafts are recognised as stale.
func (s *draftStore) lookup(user domain.UserID, id uint32) (draft, bool) {
	d, ok := s.get(user)
	if !ok || d.id != id {
		return draft{}, false
	}
	return d, true
}

// put stores d as the user's draft and marks it active.
func (s *draftStore) put(user domain.UserID, d draft) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d.touched = s.now()
	s.drafts[user] = d.clone()
}

// remove deletes the user's draft if it is still the one with the given ID.
func (s *draftStore) remove(user domain.UserID, id uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.drafts[user]; ok && d.id == id {
		delete(s.drafts, user)
	}
}

// sweep drops expired drafts and reports how many were removed.
func (s *draftStore) sweep() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for user, d := range s.drafts {
		if s.expired(d) {
			delete(s.drafts, user)
			n++
		}
	}
	return n
}

func (s *draftStore) expired(d draft) bool {
	return s.now().Sub(d.touched) >= s.ttl
}

// janitor sweeps periodically until ctx is done.
func (s *draftStore) janitor(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.sweep()
		}
	}
}
