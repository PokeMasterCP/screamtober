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
	movies  []tmdb.MovieSummary
	expires time.Time
}

// Entries are immutable after insertion. Expired entries are removed lazily;
// the capacity bound also limits memory when no cleanup requests arrive.
type movieSearchCache struct {
	mu      sync.Mutex
	entries map[string]cachedMovieSearch
}

func (c *movieSearchCache) put(session [32]byte, query string, movies []tmdb.MovieSummary) string {
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
	c.entries[key] = cachedMovieSearch{session: session, query: query, movies: movies, expires: now.Add(searchCacheTTL)}
	return key
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
