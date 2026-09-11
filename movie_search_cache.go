package main

import (
	"crypto/rand"
	"sync"
	"time"

	"github.com/pokemastercp/screamtober/internal/tmdb"
)

const searchCacheTTL = 10 * time.Minute
const maxCachedSearches = 128

type cachedMovieSearch struct {
	session [32]byte
	query   string
	results tmdb.SearchResults
	expires time.Time
}

// Entries are immutable after insertion. Expired entries are removed lazily;
// the capacity bound also limits memory when no cleanup requests arrive.
type movieSearchCache struct {
	mu      sync.Mutex
	entries map[string]cachedMovieSearch
}

func (c *movieSearchCache) put(entry cachedMovieSearch) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if c.entries == nil {
		c.entries = make(map[string]cachedMovieSearch)
	}
	for key, entry := range c.entries {
		if !now.Before(entry.expires) {
			delete(c.entries, key)
		}
	}
	if len(c.entries) >= maxCachedSearches {
		var oldest string
		var expiry time.Time
		for key, entry := range c.entries {
			if oldest == "" || entry.expires.Before(expiry) {
				oldest, expiry = key, entry.expires
			}
		}
		delete(c.entries, oldest)
	}
	key := rand.Text()
	c.entries[key] = entry
	return key
}

// find reuses a full upstream page without extending its original lifetime.
func (c *movieSearchCache) find(session [32]byte, query string, page int) (cachedMovieSearch, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for key, entry := range c.entries {
		if !now.Before(entry.expires) {
			delete(c.entries, key)
			continue
		}
		if entry.session == session && entry.query == query && entry.results.Page == page {
			return entry, true
		}
	}
	return cachedMovieSearch{}, false
}

func (c *movieSearchCache) get(key string, session [32]byte) (cachedMovieSearch, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return cachedMovieSearch{}, false
	}
	if !time.Now().Before(entry.expires) {
		delete(c.entries, key)
		return cachedMovieSearch{}, false
	}
	if entry.session != session {
		return cachedMovieSearch{}, false
	}
	return entry, true
}
