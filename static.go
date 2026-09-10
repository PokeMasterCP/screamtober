package main

import (
	"embed"
	"net/http"
)

//go:embed static/fonts/*.ttf static/fonts/OFL.txt
var fontFiles embed.FS

func registerFontRoutes(mux *http.ServeMux) {
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
			http.ServeFileFS(w, r, fontFiles, "static/fonts/"+name)
		})
	}
}
