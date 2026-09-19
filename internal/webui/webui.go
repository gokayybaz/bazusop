package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
)

// assets contains the production React build.
//
//go:embed dist
var assets embed.FS

func Handler() http.Handler {
	dist, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(dist))

	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requestedPath := path.Clean(request.URL.Path)
		if requestedPath == "." || requestedPath == "/" {
			serveIndex(response, request, dist)
			return
		}

		if _, err := fs.Stat(dist, requestedPath[1:]); err == nil {
			files.ServeHTTP(response, request)
			return
		}

		serveIndex(response, request, dist)
	})
}

func serveIndex(response http.ResponseWriter, request *http.Request, dist fs.FS) {
	index, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		http.Error(response, "web application is not built", http.StatusServiceUnavailable)
		return
	}
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = response.Write(index)
}
