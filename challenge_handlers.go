package main

import (
	"database/sql"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"
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
	// Today is the October day of the selected year's challenge, or 0 outside it.
	Today int64
	// DaysUntil counts down to October 1 of the current year; 0 once it arrives.
	DaysUntil int
	Movies    []challengeMovieView
	Lineup    []challengeMovieView
	Watched   int
	User      *store.User
	// Year-wide summaries of submitted ratings, derived for display only.
	RatingCount int
	Average     string
	Critics     []critic
	TopMovie    *challengeMovieView
	// ToRate lists watched entries the signed-in person has not rated yet.
	ToRate []challengeMovieView
}

func (m challengeMovieView) Service() viewingService { return findViewingService(m.ViewingService) }

type challengeMovieView struct {
	store.ListChallengeMoviesRow
	PosterURL  string
	ThumbURL   string
	Ratings    []store.ListChallengeRatingsRow
	Average    string
	YourRating int64
	score      float64
}

// A critic summarizes one person's submitted ratings for the selected year.
type critic struct {
	Name    string
	Count   int
	Average string
	total   int64
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
	addEventAttrs(r, "year", year)
	challenge, err := h.queries.GetChallengeByYear(r.Context(), year)
	if errors.Is(err, sql.ErrNoRows) {
		eventRejected(r, "not_found")
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
	now := time.Now()
	data := challengePage{Title: "Screamtober", SignedIn: user != nil, User: user, Challenges: challenges, Challenge: selected}
	data.Year = int64(now.Year())
	data.DaysUntil = daysUntilOctober(now)
	if selected != nil {
		data.Year = selected.Year
		if selected.Year != int64(now.Year()) {
			data.DaysUntil = 0
		}
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
		critics := make(map[int64]*critic)
		var total int64
		for _, rating := range ratings {
			byEntry[rating.ChallengeMovieID] = append(byEntry[rating.ChallengeMovieID], rating)
			c := critics[rating.UserID]
			if c == nil {
				c = &critic{Name: rating.DisplayName}
				critics[rating.UserID] = c
			}
			c.Count++
			c.total += rating.Score
			total += rating.Score
		}
		if len(ratings) > 0 {
			data.RatingCount = len(ratings)
			data.Average = fmt.Sprintf("%.1f", float64(total)/float64(len(ratings)))
		}
		for _, c := range critics {
			c.Average = fmt.Sprintf("%.1f", float64(c.total)/float64(c.Count))
			data.Critics = append(data.Critics, *c)
		}
		sort.Slice(data.Critics, func(i, j int) bool {
			if data.Critics[i].Count != data.Critics[j].Count {
				return data.Critics[i].Count > data.Critics[j].Count
			}
			return data.Critics[i].Name < data.Critics[j].Name
		})
		for _, movie := range movies {
			entry := challengeMovieView{ListChallengeMoviesRow: movie, PosterURL: moviePosterURL(movie.PosterPath), ThumbURL: moviePosterThumbURL(movie.PosterPath), Ratings: byEntry[movie.ID]}
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
				entry.score = float64(total) / float64(len(entry.Ratings))
				entry.Average = fmt.Sprintf("%.1f", entry.score)
			}
			if user != nil && movie.WatchedAt.Valid && entry.YourRating == 0 {
				data.ToRate = append(data.ToRate, entry)
			}
			data.Movies = append(data.Movies, entry)
			if !movie.WatchedAt.Valid && data.NextMovie == nil {
				data.NextMovie = &entry
			} else {
				data.Lineup = append(data.Lineup, entry)
			}
		}
		// Ties favor the more-rated entry, then the earlier night.
		for i := range data.Movies {
			m := &data.Movies[i]
			if m.Average == "" {
				continue
			}
			if top := data.TopMovie; top == nil || m.score > top.score || (m.score == top.score && len(m.Ratings) > len(top.Ratings)) {
				data.TopMovie = m
			}
		}
		if now.Month() == time.October && int64(now.Year()) == selected.Year {
			data.Today = int64(now.Day())
		}
		if data.NextMovie != nil && data.Today != 0 && data.NextMovie.Position.Valid && data.NextMovie.Position.Int64 == data.Today {
			data.Tonight = true
		}
	}
	renderPage(w, r, h.pages, "home.html", http.StatusOK, data)
}

// daysUntilOctober counts calendar days from now until October 1 of the same year.
func daysUntilOctober(now time.Time) int {
	today := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, time.UTC)
	october := time.Date(now.Year(), time.October, 1, 12, 0, 0, 0, time.UTC)
	if !today.Before(october) {
		return 0
	}
	return int(october.Sub(today).Hours() / 24)
}

func (h *challengeHandler) fail(w http.ResponseWriter, r *http.Request, step string, err error) {
	eventFailed(r, step, err)
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

type challengeNight struct {
	Day     int64
	Weekday string
	Movie   *challengeMovieView
}

// Nights places scheduled entries on the challenge year's 31 October dates.
func (p challengePage) Nights() []challengeNight {
	nights := make([]challengeNight, 31)
	for i := range nights {
		day := time.Date(int(p.Year), time.October, i+1, 0, 0, 0, 0, time.UTC)
		nights[i] = challengeNight{Day: int64(i + 1), Weekday: day.Weekday().String()[:3]}
	}
	for i := range p.Movies {
		if position := p.Movies[i].Position; position.Valid && position.Int64 >= 1 && position.Int64 <= 31 {
			nights[position.Int64-1].Movie = &p.Movies[i]
		}
	}
	return nights
}

// FirstColumn is October 1's column in a Sunday-first month grid, from 1 to 7.
func (p challengePage) FirstColumn() int {
	return int(time.Date(int(p.Year), time.October, 1, 0, 0, 0, 0, time.UTC).Weekday()) + 1
}

// NightLabel names a scheduled entry's date, such as "Wednesday, October 5".
func (p challengePage) NightLabel(movie challengeMovieView) string {
	if !movie.Position.Valid {
		return ""
	}
	return time.Date(int(p.Year), time.October, int(movie.Position.Int64), 0, 0, 0, 0, time.UTC).Format("Monday, January 2")
}

// NightsLeft counts tonight and the remaining October nights during the challenge.
func (p challengePage) NightsLeft() int64 {
	if p.Today == 0 {
		return 0
	}
	return 32 - p.Today
}

// Unscheduled lists entries without an October night.
func (p challengePage) Unscheduled() []challengeMovieView {
	var movies []challengeMovieView
	for _, movie := range p.Movies {
		if !movie.Position.Valid {
			movies = append(movies, movie)
		}
	}
	return movies
}

// ReleaseYear keeps the year from TMDB's YYYY-MM-DD release dates.
func (m challengeMovieView) ReleaseYear() string {
	if !m.ReleaseDate.Valid || len(m.ReleaseDate.String) < 4 {
		return ""
	}
	return m.ReleaseDate.String[:4]
}

// Initial is the first letter of a display name, for avatar badges.
func (challengePage) Initial(name string) string {
	for _, r := range strings.TrimSpace(name) {
		return strings.ToUpper(string(r))
	}
	return ""
}
