package main

import (
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"time"

	"github.com/pokemastercp/screamtober/internal/store"
)

type challengeHandler struct {
	db      *sql.DB
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
	// Tonight is true when the next movie is scheduled for today's October date.
	Tonight bool
	Movies  []challengeMovieView
	Lineup  []challengeMovieView
	Watched int
	User    *store.User
}

func (m challengeMovieView) Service() viewingService { return findViewingService(m.ViewingService) }

type challengeMovieView struct {
	store.ListChallengeMoviesRow
	PosterURL  string
	Ratings    []store.ListChallengeRatingsRow
	Average    string
	YourRating int64
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
	year, ok := pathYear(w, r)
	if !ok {
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
			entry := challengeMovieView{ListChallengeMoviesRow: movie, PosterURL: moviePosterURL(movie.PosterPath), Ratings: byEntry[movie.ID]}
			if movie.WatchedAt.Valid {
				data.Watched++
			}
			if len(entry.Ratings) > 0 {
				var total int64
				for _, rating := range entry.Ratings {
					total += rating.Score
					if user != nil && rating.UserID == user.ID {
						entry.YourRating = rating.Score
					}
				}
				entry.Average = fmt.Sprintf("%.1f", float64(total)/float64(len(entry.Ratings)))
			}
			data.Movies = append(data.Movies, entry)
			if !movie.WatchedAt.Valid && data.NextMovie == nil {
				data.NextMovie = &entry
			} else {
				data.Lineup = append(data.Lineup, entry)
			}
		}
		now := time.Now()
		if data.NextMovie != nil && now.Month() == time.October && int64(now.Year()) == selected.Year && data.NextMovie.Position.Valid && data.NextMovie.Position.Int64 == int64(now.Day()) {
			data.Tonight = true
		}
	}
	renderPage(w, r, h.pages, "home.html", http.StatusOK, data)
}

func (h *challengeHandler) fail(w http.ResponseWriter, r *http.Request, operation string, err error) {
	setRequestEvent(r, slog.LevelError, "load challenge failed", "operation", operation, "error", err)
	http.Error(w, "Unable to load challenge. Please try again later.", http.StatusInternalServerError)
}

type ratingPanel struct {
	Movie              challengeMovieView
	Year               int64
	SignedIn, Featured bool
}

func (p challengePage) RatingPanel(movie challengeMovieView, featured bool) ratingPanel {
	return ratingPanel{Movie: movie, Year: p.Year, SignedIn: p.SignedIn, Featured: featured}
}

func (ratingPanel) Scores() []int64 { return []int64{1, 2, 3, 4, 5} }
