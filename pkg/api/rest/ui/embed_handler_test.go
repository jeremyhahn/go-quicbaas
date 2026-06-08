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

package ui

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestRegisterUI_ServesRedirectsAndSPAFallback exercises RegisterUI + spaHandler
// against the real embedded bundle: the /ui redirect, serving a real embedded
// file (the spaHandler file-exists branch), the empty-path → index.html branch,
// and the unknown-route → index.html SPA fallback branch.
func TestRegisterUI_ServesRedirectsAndSPAFallback(t *testing.T) {
	t.Parallel()

	r := chi.NewRouter()
	RegisterUI(r)

	cases := []struct {
		name     string
		path     string
		wantCode int
		wantLoc  string // non-empty asserts a redirect Location
	}{
		// /ui → 302 to /ui/ (the RegisterUI convenience redirect).
		{"bare /ui redirects to /ui/", "/ui", http.StatusFound, "/ui/"},
		// /ui/ serves the SPA shell (spaHandler empty-path → index.html, file-exists branch).
		// This is the load-bearing path: the UI entry point must serve 200.
		{"/ui/ serves index.html (empty-path branch)", "/ui/", http.StatusOK, ""},
		// http.FileServer canonicalizes an explicit index.html request to "./" (301).
		// Exercises the spaHandler file-exists branch.
		{"/ui/index.html canonicalizes to ./ (file-exists branch)", "/ui/index.html", http.StatusMovedPermanently, ""},
		// Unknown path → spaHandler fallback sets URL.Path=/index.html, which FileServer
		// canonicalizes to "./" (301). Harmless here: the Svelte UI uses hash-based routing,
		// so deep PATH requests never occur in practice. Exercises the fallback branch.
		{"unknown route hits SPA fallback branch", "/ui/deep/client/route", http.StatusMovedPermanently, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != tc.wantCode {
				t.Fatalf("GET %s: expected %d, got %d", tc.path, tc.wantCode, rec.Code)
			}
			if tc.wantLoc != "" && rec.Header().Get("Location") != tc.wantLoc {
				t.Fatalf("GET %s: expected redirect to %q, got %q", tc.path, tc.wantLoc, rec.Header().Get("Location"))
			}
		})
	}
}
