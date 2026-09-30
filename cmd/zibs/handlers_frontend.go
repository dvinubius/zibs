package main

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed web
var webFS embed.FS

func frontendPage(w http.ResponseWriter, req *http.Request) {
	http.ServeFileFS(w, req, webFS, "web/index.html")
}

func staticAssets() http.Handler {
	assets, err := fs.Sub(webFS, "web/static")
	if err != nil {
		panic(err)
	}

	fileServer := http.StripPrefix("/static/", http.FileServerFS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// Serve files only; a trailing slash would render a directory listing.
		if strings.HasSuffix(req.URL.Path, "/") {
			http.NotFound(w, req)
			return
		}
		fileServer.ServeHTTP(w, req)
	})
}
