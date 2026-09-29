package vkusvill

import (
	"sync"
	"time"

	"github.com/Mikkkin/dreamer-bot/internal/domain"
)

// cache keeps search results per normalized, lower-cased query for a fixed
// time. When full, it drops expired entries and then the oldest one.
type cache struct {
	ttl   time.Duration
	limit int
	now   func() time.Time

	mu      sync.Mutex
	entries map[string]cacheEntry
}

type cacheEntry struct {
	products []Product
	expires  time.Time
}

func newCache(ttl time.Duration, limit int, now func() time.Time) *cache {
	return &cache{ttl: ttl, limit: limit, now: now, entries: make(map[string]cacheEntry)}
}

func (c *cache) get(key string) ([]Product, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if !c.now().Before(e.expires) {
		delete(c.entries, key)
		return nil, false
	}
	return cloneProducts(e.products), true
}

func (c *cache) put(key string, products []Product) {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.entries[key]; !ok && len(c.entries) >= c.limit {
		c.evict(now)
	}
	c.entries[key] = cacheEntry{products: cloneProducts(products), expires: now.Add(c.ttl)}
}

// evict makes room for one entry. Every entry lives for the same ttl, so
// the one expiring first is the oldest.
func (c *cache) evict(now time.Time) {
	var (
		oldest    string
		oldestExp time.Time
	)
	for key, e := range c.entries {
		if !now.Before(e.expires) {
			delete(c.entries, key)
			continue
		}
		if oldest == "" || e.expires.Before(oldestExp) {
			oldest, oldestExp = key, e.expires
		}
	}
	if len(c.entries) >= c.limit {
		delete(c.entries, oldest)
	}
}

// price returns the price of a product from the freshest search that
// offered it.
func (c *cache) price(xmlID int) (domain.Money, bool) {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	var (
		found  domain.Money
		ok     bool
		newest time.Time
	)
	for _, e := range c.entries {
		if !now.Before(e.expires) || (ok && !e.expires.After(newest)) {
			continue
		}
		for _, p := range e.products {
			if p.XMLID == xmlID && p.Price != nil {
				found, ok, newest = *p.Price, true, e.expires
				break
			}
		}
	}
	return found, ok
}
