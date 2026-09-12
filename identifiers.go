package main

import (
	"net/http"
	"strconv"
)

func parseYear(value string) (int64, bool) {
	year, err := strconv.ParseInt(value, 10, 64)
	return year, err == nil && year >= 1 && year <= 9999
}

func pathYear(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.PathValue("year")
	year, ok := parseYear(raw)
	if !ok || strconv.FormatInt(year, 10) != raw {
		http.NotFound(w, r)
		return 0, false
	}
	return year, true
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.PathValue("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 || strconv.FormatInt(id, 10) != raw {
		http.NotFound(w, r)
		return 0, false
	}
	return id, true
}

func challengeMovieIDs(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	year, ok := pathYear(w, r)
	if !ok {
		return 0, 0, false
	}
	id, ok := pathID(w, r)
	return year, id, ok
}
