package main

import (
	"database/sql"
	"regexp"
)

var posterPathPattern = regexp.MustCompile(`^/[A-Za-z0-9_-]+\.(jpg|jpeg|png|webp)$`)

// Only cached TMDB-relative image paths may become browser image URLs.
func moviePosterURL(path sql.NullString) string {
	if !path.Valid || !posterPathPattern.MatchString(path.String) {
		return ""
	}
	return "https://image.tmdb.org/t/p/w500" + path.String
}
