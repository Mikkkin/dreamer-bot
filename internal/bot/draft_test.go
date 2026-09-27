package bot

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

func TestDraftStoreLifecycle(t *testing.T) {
	clk := newClock()
	s := newDraftStore(draftTTL, clk.Now)

	d := s.begin(int64(alice))
	d.title = "Лампа"
	s.put(alice, d)

	got, ok := s.get(alice)
	if !ok || got.title != "Лампа" || got.id != d.id {
		t.Fatalf("get = %+v, %v", got, ok)
	}
	if _, ok := s.lookup(alice, d.id+1); ok {
		t.Error("lookup accepted a stale draft ID")
	}
	if _, ok := s.get(bob); ok {
		t.Error("drafts leak between users")
	}

	// Activity keeps the draft alive.
	clk.Advance(draftTTL - time.Minute)
	s.put(alice, got)
	clk.Advance(draftTTL - time.Minute)
	if _, ok := s.get(alice); !ok {
		t.Fatal("draft expired despite activity")
	}

	clk.Advance(draftTTL)
	if _, ok := s.lookup(alice, d.id); ok {
		t.Fatal("draft outlived its TTL")
	}
}

func TestDraftStoreIDsAndRemove(t *testing.T) {
	clk := newClock()
	s := newDraftStore(draftTTL, clk.Now)
	first := s.begin(1)
	second := s.begin(1)
	if first.id == 0 || second.id == first.id {
		t.Fatalf("draft IDs %d, %d", first.id, second.id)
	}
	s.put(alice, second)
	s.remove(alice, first.id) // a stale ID must not remove the live draft
	if _, ok := s.get(alice); !ok {
		t.Fatal("stale remove deleted the live draft")
	}
	s.remove(alice, second.id)
	if _, ok := s.get(alice); ok {
		t.Fatal("draft not removed")
	}
}

func TestDraftStoreGetReturnsCopy(t *testing.T) {
	s := newDraftStore(draftTTL, newClock().Now)
	d := s.begin(1)
	d.addPhoto("a")
	s.put(alice, d)
	got, _ := s.get(alice)
	got.addPhoto("b")
	again, _ := s.get(alice)
	if len(again.photos) != 1 {
		t.Fatalf("stored draft changed through a copy: %v", again.photos)
	}
}

func TestDraftStoreSweepAndJanitor(t *testing.T) {
	clk := newClock()
	s := newDraftStore(draftTTL, clk.Now)
	s.put(alice, s.begin(1))
	s.put(bob, s.begin(2))
	clk.Advance(draftTTL / 2)
	s.put(bob, s.begin(2))
	clk.Advance(draftTTL/2 + time.Second)
	if n := s.sweep(); n != 1 {
		t.Fatalf("sweep removed %d drafts, want 1", n)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.janitor(ctx, time.Millisecond)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("janitor did not stop with its context")
	}
}

func TestDraftKindToggle(t *testing.T) {
	d := draft{kind: kindWish, awaiting: fieldPrice, view: viewCategories}
	d.setKind(kindRecipe)
	if d.kind != kindRecipe || d.awaiting != fieldNone || d.view != viewMain {
		t.Fatalf("after switch to recipe: %+v", d)
	}
	d.awaiting = fieldLink
	d.setKind(kindWish)
	if d.awaiting != fieldLink {
		t.Error("switching kind dropped a field that applies to both kinds")
	}
	if !d.fieldApplies(fieldPrice) {
		t.Error("wish must accept a price")
	}
}

func TestDraftFill(t *testing.T) {
	link := "https://old.example/"
	price := domain.Money{Minor: 100, Currency: "USD"}
	tests := []struct {
		name    string
		kind    entityKind
		field   draftField
		input   string
		check   func(draft) bool
		wantErr bool
	}{
		{"title", kindWish, fieldTitle, "  Новая лампа ", func(d draft) bool { return d.title == "Новая лампа" }, false},
		{"empty title", kindWish, fieldTitle, "  ", nil, true},
		{"price keeps the current currency", kindWish, fieldPrice, "15 000", func(d draft) bool {
			return d.price != nil && *d.price == domain.Money{Minor: 1500000, Currency: "USD"}
		}, false},
		{"price with explicit currency", kindWish, fieldPrice, "15 000 ₽", func(d draft) bool {
			return d.price != nil && d.price.Currency == "RUB" && d.price.Minor == 1500000
		}, false},
		{"bad price", kindWish, fieldPrice, "дорого", nil, true},
		{"clear price", kindWish, fieldPrice, "-", func(d draft) bool { return d.price == nil }, false},
		{"link", kindWish, fieldLink, "ozon.ru/item", func(d draft) bool {
			return d.link != nil && *d.link == "https://ozon.ru/item"
		}, false},
		{"bad link", kindWish, fieldLink, "ftp://x.example", nil, true},
		{"clear link", kindWish, fieldLink, " - ", func(d draft) bool { return d.link == nil }, false},
		{"recipe body", kindRecipe, fieldText, "1. Сварить", func(d draft) bool { return d.text == "1. Сварить" }, false},
		{"clear body", kindRecipe, fieldText, "-", func(d draft) bool { return d.text == "" }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := draft{kind: tt.kind, title: "Лампа", text: "старое", link: &link, price: &price, awaiting: tt.field, promptID: 5}
			before := d.clone()
			err := d.fill(tt.input, nil, "EUR")
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				if _, ok := domain.AsValidation(err); !ok {
					t.Errorf("error %v is not a validation error", err)
				}
				if d.awaiting != tt.field || d.promptID != 5 || d.title != before.title || d.link != before.link || d.price != before.price {
					t.Errorf("failed fill changed the draft: %+v", d)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !tt.check(d) {
				t.Errorf("unexpected draft %+v", d)
			}
			if d.awaiting != fieldNone || d.promptID != 0 {
				t.Error("draft still awaiting after a successful fill")
			}
		})
	}
}

func TestDraftPhotosAndMerge(t *testing.T) {
	clk := newClock()
	d := draft{touched: clk.Now()}
	for i := range maxDraftPhotos + 3 {
		d.addPhoto(fmt.Sprintf("f%d", i))
	}
	if len(d.photos) != maxDraftPhotos {
		t.Fatalf("draft holds %d photos, cap is %d", len(d.photos), maxDraftPhotos)
	}
	if d.addPhoto("f0") {
		t.Error("duplicate photo accepted")
	}

	d.albumID = "album"
	clk.Advance(photoJoinWindow + time.Minute)
	if !d.acceptsPhoto("album", clk.Now()) {
		t.Error("photos of the same album must always join")
	}
	if d.acceptsPhoto("", clk.Now()) || d.acceptsPhoto("other", clk.Now()) {
		t.Error("an old draft accepted an unrelated photo")
	}

	m := draft{title: "Лампа", text: "в спальню"}
	p := parseInput("Настольная 30€\nтёплый свет", nil)
	m.mergeParsed(p)
	if m.title != "Лампа" || m.text != "в спальню\nНастольная\nтёплый свет" || m.price == nil {
		t.Errorf("merge result %+v", m)
	}
	empty := draft{}
	empty.mergeParsed(p)
	if empty.title != "Настольная" || empty.text != "тёплый свет" {
		t.Errorf("merge into empty draft: %+v", empty)
	}
}
