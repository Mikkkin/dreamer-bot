package bot

import (
	"context"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
	"github.com/Mikkkin/dreamer-bot/internal/service"
)

// notifyImportedQuiet is the review window of an imported recipe: the
// importer checks what the parser made of the post right away, and the
// partner hears of the recipe once it has stayed unchanged this long.
const notifyImportedQuiet = 3 * time.Minute

// pendingImport is the notice of an imported recipe waiting for its quiet
// window; guarded by notifier.mu. bump wakes the waiting goroutine when due
// moves or the notice is dropped.
type pendingImport struct {
	r       service.Recipients
	rec     domain.Recipe
	due     time.Time
	bump    chan struct{}
	dropped bool
}

// RecipeImported announces a recipe made by an import once it has stayed
// unchanged for the review window. Edits by the importer meanwhile restart
// the window and are not announced on their own; a recipe deleted meanwhile
// (forgetImport, or found gone when the notice reloads it) is never
// announced. On shutdown a pending notice is delivered right away.
func (n *notifier) RecipeImported(ctx context.Context, r service.Recipients, rec domain.Recipe) {
	n.mu.Lock()
	if _, ok := n.imports[rec.ID]; ok {
		n.mu.Unlock()
		n.log.Debug("bot: recipe imported twice, one notice kept", "recipe_id", rec.ID)
		return
	}
	p := &pendingImport{r: r, rec: rec, due: n.now().Add(n.importQuiet), bump: make(chan struct{}, 1)}
	n.imports[rec.ID] = p
	n.mu.Unlock()
	if !n.spawn(ctx, func(ctx context.Context) { n.announceImport(ctx, rec.ID, p) }) {
		n.mu.Lock()
		delete(n.imports, rec.ID)
		n.mu.Unlock()
	}
}

// foldIntoImport restarts the review window of an imported recipe when its
// importer edits it, and reports whether the edit was folded into that
// notice. Edits by someone else are announced as usual.
func (n *notifier) foldIntoImport(r service.Recipients, rec domain.Recipe) bool {
	n.mu.Lock()
	p, ok := n.imports[rec.ID]
	if !ok || p.r.Actor.ID != r.Actor.ID {
		n.mu.Unlock()
		return false
	}
	p.r, p.rec, p.due = r, rec, n.now().Add(n.importQuiet)
	n.mu.Unlock()
	wake(p.bump)
	n.log.Debug("bot: recipe edit folded into its import notice", "recipe_id", rec.ID)
	return true
}

// forgetImport drops the pending notice of an imported recipe (it was
// deleted from the chat) and reports whether there was one.
func (n *notifier) forgetImport(id domain.RecipeID) bool {
	n.mu.Lock()
	p, ok := n.imports[id]
	if ok {
		p.dropped = true
		delete(n.imports, id)
	}
	n.mu.Unlock()
	if ok {
		wake(p.bump)
		n.log.Debug("bot: import notice dropped, recipe deleted", "recipe_id", id)
	}
	return ok
}

func (n *notifier) announceImport(ctx context.Context, id domain.RecipeID, p *pendingImport) {
	r, rec, ok := n.awaitImport(id, p)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, n.timeout)
	defer cancel()
	rec, ok = n.reloadRecipe(ctx, rec)
	if !ok {
		return
	}
	actor := r.Actor.DisplayName()
	n.deliver(ctx, r.To, notice{
		cover:   coverOf(rec.Images),
		caption: renderRecipeImported(actor, rec, maxCaptionLen),
		text:    renderRecipeImported(actor, rec, maxMessageLen),
		kb:      openKeyboard("Открыть ✨", n.web.recipeLink(rec.ID)),
	})
}

// awaitImport waits until the imported recipe has stayed unchanged for the
// review window (or the notifier shuts down), then removes the notice from
// the pending set and returns its latest state. ok is false when the notice
// was dropped.
func (n *notifier) awaitImport(id domain.RecipeID, p *pendingImport) (service.Recipients, domain.Recipe, bool) {
	for {
		n.mu.Lock()
		if p.dropped {
			n.mu.Unlock()
			return service.Recipients{}, domain.Recipe{}, false
		}
		wait := p.due.Sub(n.now())
		if wait <= 0 || n.closed {
			if n.imports[id] == p {
				delete(n.imports, id)
			}
			r, rec := p.r, p.rec
			n.mu.Unlock()
			return r, rec, true
		}
		n.mu.Unlock()
		t := time.NewTimer(wait)
		select {
		case <-t.C:
		case <-p.bump:
		case <-n.quit:
		}
		t.Stop()
	}
}

// wake signals a waiting goroutine without blocking; one pending wake-up
// is enough.
func wake(bump chan struct{}) {
	select {
	case bump <- struct{}{}:
	default:
	}
}
