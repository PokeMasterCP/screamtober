// movie-search searches TMDB without starting the web app or opening SQLite.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/pokemastercp/screamtober/internal/tmdb"
)

func main() {
	year := flag.Int("year", 0, "primary release year (optional)")
	page := flag.Int("page", 1, "result page (1–500)")
	flag.Usage = func() { fmt.Fprintln(os.Stderr, "usage: movie-search [-year 1978] [-page 1] <title>") }
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	results, err := tmdb.New(os.Getenv("TMDB_API_KEY")).SearchMovies(ctx, flag.Arg(0), tmdb.SearchOptions{Year: *year, Page: *page})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(results); err != nil {
		fmt.Fprintln(os.Stderr, "could not write movie search results")
		os.Exit(1)
	}
}
