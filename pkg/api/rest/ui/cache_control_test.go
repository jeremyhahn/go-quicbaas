//go:build rest

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

// firstHashedAsset returns the first content-hashed script index.html
// references, so the test exercises a real asset path.
func firstHashedAsset(t *testing.T) string {
	t.Helper()
	sub, err := fs.Sub(bundleFS, "dist")
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	m := regexp.MustCompile(`assets/index-[A-Za-z0-9_-]+\.js`).Find(index)
	if m == nil {
		t.Fatal("index.html names no hashed index asset")
	}
	return string(m)
}

func serveUI(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ui"+path, nil))
	return rec
}

// TestUI_TheEntryPageIsNeverCachedStale: index.html names the current
// asset hashes, so a browser must revalidate it on every load — the
// 2026-09-22 stale-bundle report was a browser heuristically caching it in
// the absence of any directive.
func TestUI_TheEntryPageIsNeverCachedStale(t *testing.T) {
	// http.FileServer canonicalises "/index.html" to "./" with a redirect, so
	// the entry page is reached as "/" (200) and an unknown client route
	// reaches the SPA fallback (a 301 to the canonical entry, as the existing
	// handler test pins). Both carry the directive: the redirect must not be
	// cached either, or a stale entry could be revisited by URL.
	for path, wantCode := range map[string]int{"/": http.StatusOK, "/some/client/route": http.StatusMovedPermanently} {
		rec := serveUI(t, path)
		if rec.Code != wantCode {
			t.Fatalf("%s: status %d, want %d", path, rec.Code, wantCode)
		}
		if got := rec.Header().Get("Cache-Control"); got != cacheControlEntry {
			t.Errorf("%s: Cache-Control = %q, want %q", path, got, cacheControlEntry)
		}
	}
}

// TestUI_HashedAssetsAreImmutable: an asset URL changes with its content,
// so the one it names may be cached indefinitely.
func TestUI_HashedAssetsAreImmutable(t *testing.T) {
	asset := firstHashedAsset(t)
	rec := serveUI(t, "/"+asset)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: status %d", asset, rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != cacheControlAsset {
		t.Errorf("%s: Cache-Control = %q, want %q", asset, got, cacheControlAsset)
	}
}

// TestCacheControlFor_OnlyTheAssetsDirectoryIsImmutable pins the rule for
// paths that are not assets: anything else under the bundle revalidates.
func TestCacheControlFor_OnlyTheAssetsDirectoryIsImmutable(t *testing.T) {
	cases := map[string]string{
		"assets/index-abc123.js":  cacheControlAsset,
		"assets/style-def456.css": cacheControlAsset,
		"index.html":              cacheControlEntry,
		"favicon.svg":             cacheControlEntry,
		"assetsindex.js":          cacheControlEntry,
	}
	for path, want := range cases {
		if got := cacheControlFor(path); got != want {
			t.Errorf("%s: %q, want %q", path, got, want)
		}
	}
}
