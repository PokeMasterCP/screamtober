package app

import (
	"embed"
	"net/http"
	"strings"
)

//go:embed static/fonts/*.ttf static/fonts/OFL.txt static/services/*
var assetFiles embed.FS

// The site is for a household, so ask well-behaved crawlers not to index it.
// robots.txt is advisory; it does not replace authentication.
const robotsTxt = "User-agent: *\nDisallow: /\n"

func serveRobots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write([]byte(robotsTxt))
}

func registerAssetRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /robots.txt", serveRobots)
	assets, _ := assetFiles.ReadDir("static/services")
	for _, asset := range assets {
		name := asset.Name()
		if !strings.HasSuffix(name, ".svg") && !strings.HasSuffix(name, ".png") {
			continue
		}
		mux.HandleFunc("GET /assets/services/"+name, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "public, max-age=86400")
			if strings.HasSuffix(name, ".svg") {
				w.Header().Set("Content-Type", "image/svg+xml")
			} else {
				w.Header().Set("Content-Type", "image/png")
			}
			http.ServeFileFS(w, r, assetFiles, "static/services/"+name)
		})
	}
	// Exact routes expose only the bundled assets, without a directory listing.
	for _, name := range []string{
		"Barlow-Regular.ttf",
		"Barlow-SemiBold.ttf",
		"BarlowCondensed-Bold.ttf",
		"BarlowCondensed-ExtraBold.ttf",
		"OFL.txt",
	} {
		mux.HandleFunc("GET /assets/fonts/"+name, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "public, max-age=86400")
			if name != "OFL.txt" {
				w.Header().Set("Content-Type", "font/ttf")
			}
			http.ServeFileFS(w, r, assetFiles, "static/fonts/"+name)
		})
	}
}
