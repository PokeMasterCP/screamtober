// movie-info fetches TMDB metadata without starting the app or opening SQLite.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"

	"github.com/pokemastercp/screamtober/internal/tmdb"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: movie-info <TMDB movie ID>")
		os.Exit(1)
	}
	id, err := strconv.ParseInt(os.Args[1], 10, 32)
	if err != nil || id <= 0 {
		fmt.Fprintln(os.Stderr, tmdb.ErrInvalidID)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	movie, err := tmdb.New(os.Getenv("TMDB_API_KEY")).GetMovie(ctx, id)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(movie); err != nil {
		fmt.Fprintln(os.Stderr, "could not write movie information")
		os.Exit(1)
	}
}
