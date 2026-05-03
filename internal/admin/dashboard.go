package admin

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static/*
var staticFiles embed.FS

// DashboardHandler returns an http.Handler that serves the admin dashboard SPA.
// The dashboard is served from embedded static files (embed.FS).
func DashboardHandler() http.Handler {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic("admin: failed to create sub filesystem for dashboard: " + err.Error())
	}
	return http.FileServer(http.FS(sub))
}
