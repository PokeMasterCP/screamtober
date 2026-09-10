package main

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/pokemastercp/screamtober/internal/store"
)

type challengeHandler struct {
	queries *store.Queries
	auth    *auth
	pages   *template.Template
}

type challengePage struct {
	Title      string
	SignedIn   bool
	Challenges []store.Challenge
	Challenge  *store.Challenge
	Year       int64
	NextMovie  *challengeMovieView
	Movies     []challengeMovieView
	Watched    int
	User       *store.User
}

type challengeMovieView struct {
	store.ListChallengeMoviesRow
	Ratings []store.ListChallengeRatingsRow
	Average string
}

func (h *challengeHandler) home(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	challenges, err := h.queries.ListChallenges(r.Context())
	if err != nil {
		h.fail(w, r, "list challenges", err)
		return
	}
	var selected *store.Challenge
	for i := range challenges {
		if challenges[i].Year == int64(time.Now().Year()) {
			selected = &challenges[i]
			break
		}
	}
	h.render(w, r, challenges, selected)
}

func (h *challengeHandler) byYear(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	value := r.PathValue("year")
	year, err := strconv.ParseInt(value, 10, 64)
	if err != nil || strconv.FormatInt(year, 10) != value {
		http.NotFound(w, r)
		return
	}
	challenge, err := h.queries.GetChallengeByYear(r.Context(), year)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.fail(w, r, "get challenge", err)
		return
	}
	challenges, err := h.queries.ListChallenges(r.Context())
	if err != nil {
		h.fail(w, r, "list challenges", err)
		return
	}
	h.render(w, r, challenges, &challenge)
}

func (h *challengeHandler) render(w http.ResponseWriter, r *http.Request, challenges []store.Challenge, selected *store.Challenge) {
	user, err := h.auth.currentUser(r)
	if err != nil {
		h.fail(w, r, "check user session", err)
		return
	}
	data := challengePage{Title: "Screamtober", SignedIn: user != nil, User: user, Challenges: challenges, Challenge: selected}
	data.Year = int64(time.Now().Year())
	if selected != nil {
		data.Year = selected.Year
		movies, err := h.queries.ListChallengeMovies(r.Context(), selected.ID)
		if err != nil {
			h.fail(w, r, "list challenge movies", err)
			return
		}
		ratings, err := h.queries.ListChallengeRatings(r.Context(), selected.ID)
		if err != nil {
			h.fail(w, r, "list challenge ratings", err)
			return
		}
		byEntry := make(map[int64][]store.ListChallengeRatingsRow)
		for _, rating := range ratings {
			byEntry[rating.ChallengeMovieID] = append(byEntry[rating.ChallengeMovieID], rating)
		}
		for _, movie := range movies {
			entry := challengeMovieView{ListChallengeMoviesRow: movie, Ratings: byEntry[movie.ID]}
			if movie.WatchedAt.Valid {
				data.Watched++
			}
			if len(entry.Ratings) > 0 {
				var total int64
				for _, rating := range entry.Ratings {
					total += rating.Score
				}
				entry.Average = fmt.Sprintf("%.1f", float64(total)/float64(len(entry.Ratings)))
			}
			data.Movies = append(data.Movies, entry)
			if !movie.WatchedAt.Valid && data.NextMovie == nil {
				data.NextMovie = &entry
			}
		}
	}
	var body bytes.Buffer
	if err := h.pages.ExecuteTemplate(&body, "home.html", data); err != nil {
		h.fail(w, r, "render challenge", err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = body.WriteTo(w)
}

func (h *challengeHandler) fail(w http.ResponseWriter, r *http.Request, operation string, err error) {
	setRequestEvent(r, slog.LevelError, "load challenge failed", "operation", operation, "error", err)
	http.Error(w, "Unable to load challenge. Please try again later.", http.StatusInternalServerError)
}
