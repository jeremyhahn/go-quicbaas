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

// Package ui embed regression tests verify the built Svelte bundle uses
// RELATIVE asset paths (./assets/foo) so it works in two contexts without
// tweaking:
//   - Daemon: index.html served at /ui/, relative path resolves to /ui/assets/
//   - Wails desktop: index.html served at /, relative path resolves to /assets/
//
// An absolute /ui/ prefix was Bug #7 — it broke the Wails desktop window
// because Wails serves embedded assets at root, not under /ui/.
package ui

import (
	"io/fs"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// isContextNeutralAssetPath reports whether an asset reference works in
// both the daemon's /ui/ mount and the Wails desktop's / mount. Relative
// paths (./foo) and schemeless/path-scheme URLs (data:, blob:) pass; any
// leading-slash absolute path fails because it hardcodes one context.
func isContextNeutralAssetPath(v string) bool {
	if v == "" || v == "#" {
		return true
	}
	if strings.HasPrefix(v, "data:") || strings.HasPrefix(v, "blob:") ||
		strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://") ||
		strings.HasPrefix(v, "wails://") {
		return true
	}
	// Leading slash means absolute — context-specific, rejected.
	if strings.HasPrefix(v, "/") {
		return false
	}
	// Everything else (./foo, foo, ../foo) is relative — acceptable.
	return true
}

// TestEmbedIndexHTMLAssetPaths parses the embedded index.html and asserts
// every <script src> and <link href> uses a context-neutral path.
// Guards Bug #7 — where absolute /ui/ paths blanked the Wails window.
func TestEmbedIndexHTMLAssetPaths(t *testing.T) {
	t.Parallel()

	data, err := fs.ReadFile(bundleFS, "dist/index.html")
	if err != nil {
		t.Fatalf("read embedded index.html: %v", err)
	}

	doc, err := html.Parse(strings.NewReader(string(data)))
	if err != nil {
		t.Fatalf("parse index.html: %v", err)
	}

	var violations []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script":
				for _, a := range n.Attr {
					if a.Key == "src" && a.Val != "" {
						if !isContextNeutralAssetPath(a.Val) {
							violations = append(violations,
								"<script src=\""+a.Val+"\"> must be a relative path (./…), not absolute (/…)")
						}
					}
				}
			case "link":
				for _, a := range n.Attr {
					if a.Key == "href" && a.Val != "" && a.Val != "#" {
						rel := ""
						for _, ra := range n.Attr {
							if ra.Key == "rel" {
								rel = ra.Val
								break
							}
						}
						if rel == "stylesheet" || rel == "modulepreload" || rel == "icon" {
							if !isContextNeutralAssetPath(a.Val) {
								violations = append(violations,
									"<link href=\""+a.Val+"\"> (rel="+rel+") must be a relative path (./…), not absolute (/…)")
							}
						}
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	if len(violations) > 0 {
		t.Errorf("index.html has absolute asset paths (rebuild frontend with base: \"./\" in vite.config.ts — Bug #7):\n  %s",
			strings.Join(violations, "\n  "))
	}
}

// TestEmbedBundleServesAtRoot simulates the Wails desktop path by serving
// the embedded bundle as an `http.FileServer` mounted at `/` (no prefix
// stripping). Every asset referenced by index.html must be reachable.
// This is the direct Bug #7 regression — the Wails desktop window was
// blank because assets referenced /ui/assets/* but Wails serves them
// at /assets/*.
func TestEmbedBundleServesAtRoot(t *testing.T) {
	t.Parallel()

	sub, err := fs.Sub(bundleFS, "dist")
	if err != nil {
		t.Fatalf("fs.Sub(dist): %v", err)
	}

	indexData, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}

	// Pull every src/href from index.html and verify each is reachable
	// by name within the embed. Relative paths (./assets/foo) are
	// normalised to just the path component; bare absolute paths (/…)
	// are rejected as Bug #7.
	refs := extractAssetRefs(t, string(indexData))
	if len(refs) == 0 {
		t.Fatal("no asset refs found in index.html — unexpected structure")
	}

	for _, ref := range refs {
		// Strip leading "./" if present — relative path into embed root.
		cleaned := strings.TrimPrefix(ref, "./")
		if strings.HasPrefix(cleaned, "/") {
			t.Errorf("asset %q is absolute; must be relative so Wails can serve it at root", ref)
			continue
		}
		if _, err := fs.Stat(sub, cleaned); err != nil {
			t.Errorf("asset %q (resolved to %q) not found in embedded FS: %v", ref, cleaned, err)
		}
	}
}

// extractAssetRefs pulls src/href values for asset references (anything
// under /assets/ plus the icon) from an index.html string.
func extractAssetRefs(t *testing.T, htmlStr string) []string {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		t.Fatalf("parse index.html: %v", err)
	}
	var refs []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			for _, a := range n.Attr {
				if (a.Key == "src" || a.Key == "href") && a.Val != "" && a.Val != "#" {
					if strings.Contains(a.Val, "/assets/") || strings.HasSuffix(a.Val, ".svg") {
						refs = append(refs, a.Val)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return refs
}

// TestEmbedIndexHTMLExists verifies the embedded FS contains index.html at
// the expected path so RegisterUI has a file to serve.
func TestEmbedIndexHTMLExists(t *testing.T) {
	t.Parallel()

	if _, err := fs.Stat(bundleFS, "dist/index.html"); err != nil {
		t.Fatalf("dist/index.html not found in embedded FS: %v", err)
	}
}
