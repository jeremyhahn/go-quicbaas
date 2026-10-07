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

package softwarefactoryworker

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/fstest"
)

// untar reads an archive into name -> contents, failing on a malformed one.
func untar(t *testing.T, archive []byte) map[string]string {
	t.Helper()
	files := map[string]string{}
	tr := tar.NewReader(bytes.NewReader(archive))
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return files
		}
		if err != nil {
			t.Fatalf("archive is not a tar stream: %v", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("reading %s: %v", header.Name, err)
		}
		files[header.Name] = string(data)
	}
}

func TestFixtureArchive_RelativeNamesAndTheGoModRestored(t *testing.T) {
	fsys := fstest.MapFS{
		"fixtures/go/go.mod.fixture":  {Data: []byte("module example.com/fixture\n")},
		"fixtures/go/greeter.go":      {Data: []byte("package fixture\n")},
		"fixtures/yaml/greeting.yaml": {Data: []byte("greeting: hello\n")},
	}
	archive, err := fixtureArchive(fsys, "fixtures")
	if err != nil {
		t.Fatalf("fixtureArchive: %v", err)
	}
	files := untar(t, archive)
	if files["go/go.mod"] != "module example.com/fixture\n" || files["go/greeter.go"] != "package fixture\n" ||
		files["yaml/greeting.yaml"] != "greeting: hello\n" || len(files) != 3 {
		t.Fatalf("archive = %v, want the three fixtures relative to the root with go.mod.fixture as go.mod", files)
	}
	again, err := fixtureArchive(fsys, "fixtures")
	if err != nil || !bytes.Equal(archive, again) {
		t.Fatal("the same fixtures must archive to the same bytes")
	}
}

func TestFixtureArchive_RefusesAMissingRoot(t *testing.T) {
	var archiveErr *FixtureArchiveError
	_, err := fixtureArchive(fstest.MapFS{}, "no-such-root")
	if !errors.As(err, &archiveErr) || archiveErr.Root != "no-such-root" || !strings.Contains(err.Error(), "no-such-root") {
		t.Fatalf("fixtureArchive(missing root) = %v, want a FixtureArchiveError naming the root", err)
	}
}

// TestSerenaLanguageServersProbe_EveryCheckedSymbolIsInItsFixture keeps the
// probe and its fixtures in step: a fixture file renamed or a function edited
// without the probe would fail the image for a reason that is not the image's.
func TestSerenaLanguageServersProbe_EveryCheckedSymbolIsInItsFixture(t *testing.T) {
	files := untar(t, serenaFixtureArchive)
	languages := map[string]bool{}
	for _, line := range strings.Split(serenaLanguageServersProbe, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[0] != "symbol_from" {
			continue
		}
		lang, file, symbol := fields[1], fields[2], fields[3]
		languages[lang] = true
		text, ok := files[lang+"/"+file]
		if !ok {
			t.Fatalf("the probe reads %s/%s, which is not a fixture", lang, file)
		}
		if !strings.Contains(text, symbol) {
			t.Fatalf("the probe expects %s from %s/%s, which does not declare it", symbol, lang, file)
		}
	}
	for _, lang := range []string{"go", "python", "typescript", "rust", "cpp", "ruby", "bash", "markdown", "yaml"} {
		if !languages[lang] {
			t.Errorf("the probe does not check %s", lang)
		}
	}
	if _, ok := files["go/go.mod"]; !ok {
		t.Fatal("the Go fixture must be a module: go/go.mod is missing from the archive")
	}
}

func TestProbes_SerenaLanguageServersRunAsALaneDoes(t *testing.T) {
	for _, p := range Probes() {
		if p.Script != serenaLanguageServersProbe {
			continue
		}
		if !p.Sandboxed || !p.NetworkNone || p.PidsLimit != 0 || !bytes.Equal(p.Stdin, serenaFixtureArchive) {
			t.Fatalf("the serena language-server probe must run sandboxed, offline, fed its fixtures, got %+v", p)
		}
		return
	}
	t.Fatal("no probe runs serena against the language fixtures")
}
