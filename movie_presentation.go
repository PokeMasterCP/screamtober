package main

import (
	"database/sql"
	"regexp"
)

var posterPathPattern = regexp.MustCompile(`^/[A-Za-z0-9_-]+\.(jpg|jpeg|png|webp)$`)

// Only cached TMDB-relative image paths may become browser image URLs.
func moviePosterURL(path sql.NullString) string {
	return tmdbImageURL(path, "w500")
}

// Calendar nights show small posters, so they request a lighter TMDB size.
func moviePosterThumbURL(path sql.NullString) string {
	return tmdbImageURL(path, "w185")
}

func tmdbImageURL(path sql.NullString, size string) string {
	if !path.Valid || !posterPathPattern.MatchString(path.String) {
		return ""
	}
	return "https://image.tmdb.org/t/p/" + size + path.String
}
