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
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The UI bundle is compiled build output that is tracked in git, because
// go:embed needs the files present and `go build` does not run npm. That makes
// it possible for the committed bundle to fall behind the source that produced
// it, and when it does, every ordinary test still passes: the tests exercise
// the TypeScript source or the server's routing table, neither of which is what
// the binary actually serves.
//
// That is not hypothetical. The P3.3 de-collision moved PIV login to
// /api/v1/iam/login/piv and updated sdk/typescript/src/rest.ts, but the
// compiled bundles kept posting to the retired /api/v1/auth/login/piv, so the
// shipped UI's PIV login was broken while CI stayed green.
//
// These tests read the embedded payload itself — what the binary serves, not
// what produced it.

// retiredPIVLoginRoute is the endpoint the P3.3 de-collision gave up; xkms owns
// that path on a composed node.
const retiredPIVLoginRoute = "api/v1/auth/login/piv"

// currentPIVLoginRoute is the endpoint this server actually registers.
const currentPIVLoginRoute = "api/v1/iam/login/piv"

// desktopBundleDir is the second embed location. The Makefile builds the
// frontend once and copies it into both, so the two must agree.
const desktopBundleDir = "../../../../cmd/quicbaas/frontend/dist"

// readEmbeddedBundle returns every embedded file keyed by its path relative to
// dist/.
func readEmbeddedBundle(t *testing.T) map[string][]byte {
	t.Helper()

	sub, err := fs.Sub(bundleFS, "dist")
	if err != nil {
		t.Fatalf("the embedded bundle has no dist/ root: %v", err)
	}

	files := make(map[string][]byte)
	err = fs.WalkDir(sub, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		content, readErr := fs.ReadFile(sub, path)
		if readErr != nil {
			return readErr
		}
		files[path] = content
		return nil
	})
	if err != nil {
		t.Fatalf("walking the embedded bundle: %v", err)
	}

	if len(files) == 0 {
		t.Fatal("the embedded bundle is empty; the binary would serve nothing")
	}
	return files
}

func TestEmbeddedBundle_TargetsTheCurrentPIVLoginRoute(t *testing.T) {
	files := readEmbeddedBundle(t)

	found := false
	for path, content := range files {
		body := string(content)

		if strings.Contains(body, retiredPIVLoginRoute) {
			t.Errorf("%s targets the retired route %q; the bundle is stale against sdk/typescript/src/rest.ts",
				path, retiredPIVLoginRoute)
		}
		if strings.Contains(body, currentPIVLoginRoute) {
			found = true
		}
	}

	if !found {
		t.Errorf("no embedded asset references %q — PIV login cannot reach the server",
			currentPIVLoginRoute)
	}
}

// assetRefPattern matches the hashed asset paths index.html points at.
var assetRefPattern = regexp.MustCompile(`(?:src|href)="\./(assets/[^"]+)"`)

func TestEmbeddedBundle_IndexReferencesOnlyEmbeddedAssets(t *testing.T) {
	// A vite rebuild renames every bundle by content hash. If index.html is
	// committed without the assets it names — or the assets are committed
	// without the index — the UI loads to a blank page. This catches that
	// whole class of drift, not just the route it was written for.
	files := readEmbeddedBundle(t)

	index, ok := files["index.html"]
	if !ok {
		t.Fatal("the embedded bundle has no index.html")
	}

	refs := assetRefPattern.FindAllStringSubmatch(string(index), -1)
	if len(refs) == 0 {
		t.Fatal("index.html references no assets; the pattern or the build output changed shape")
	}

	for _, ref := range refs {
		if _, ok := files[ref[1]]; !ok {
			t.Errorf("index.html references %q, which is not in the embedded bundle", ref[1])
		}
	}
}

func TestEmbeddedBundle_MatchesTheDesktopCopy(t *testing.T) {
	// cmd/quicbaas embeds its own copy for the Wails build. The Makefile
	// produces one build and copies it to both, so a difference means one of
	// them was regenerated and the other was not — which is exactly how the
	// PIV route went stale in only some of the shipped artifacts.
	files := readEmbeddedBundle(t)

	for path, embedded := range files {
		desktop, err := os.ReadFile(filepath.Join(desktopBundleDir, filepath.FromSlash(path)))
		if err != nil {
			t.Errorf("%s is embedded here but missing from the desktop bundle: %v", path, err)
			continue
		}
		if string(desktop) != string(embedded) {
			t.Errorf("%s differs between the two embed locations; rebuild so both copies come from one build", path)
		}
	}

	// The reverse direction: a file the desktop bundle carries and this one
	// does not is drift just the same.
	err := filepath.WalkDir(desktopBundleDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(desktopBundleDir, path)
		if relErr != nil {
			return relErr
		}
		if _, ok := files[filepath.ToSlash(rel)]; !ok {
			t.Errorf("%s is in the desktop bundle but not embedded here", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the desktop bundle: %v", err)
	}
}
