//go:build rest

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

// Package ui embeds the pre-built Svelte UI bundle and registers it on the
// REST server under /ui/*. The embedded file system is read-only; no writes
// are ever performed against it.
//
// Build tag: this file is only compiled when the 'rest' build tag is active so
// that non-REST builds (e.g. Wails desktop) do not embed the bundle at all.
package ui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

//go:embed dist
var bundleFS embed.FS

// RegisterUI attaches the embedded Svelte UI under /ui/* on the given router.
// The default file is index.html; unknown-path requests fall through to
// index.html so client-side routing works.
func RegisterUI(r chi.Router) {
	sub, err := fs.Sub(bundleFS, "dist")
	if err != nil {
		return
	}
	fileServer := http.FileServer(http.FS(sub))
	r.Handle("/ui/*", http.StripPrefix("/ui", spaHandler(sub, fileServer)))
	r.Get("/ui", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/ui/", http.StatusFound)
	})
}

// spaHandler wraps a static file server so that any path that does not match a
// real file in the embedded FS is served as index.html. This allows the
// Svelte hash-based router to handle all client-side routes.
func spaHandler(fsys fs.FS, files http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Trim the leading slash so fs.Stat can resolve relative paths.
		clean := strings.TrimPrefix(r.URL.Path, "/")
		if clean == "" {
			clean = "index.html"
		}
		if _, err := fs.Stat(fsys, clean); err == nil {
			files.ServeHTTP(w, r)
			return
		}
		// Unknown path — serve index.html and let the Svelte router decide.
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/index.html"
		files.ServeHTTP(w, r2)
	})
}
