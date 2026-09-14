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

package prompts

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"
)

// defaultsFS is a shipped set as it looks on disk: markdown files, a
// subdirectory, and something that is not markdown at all.
func defaultsFS() fstest.MapFS {
	return fstest.MapFS{
		"promptdefaults/sdlc/charter.md":       {Data: []byte("Be careful.\n")},
		"promptdefaults/sdlc/ending.md":        {Data: []byte("Stop here.\n\n")},
		"promptdefaults/sdlc/no_newline.md":    {Data: []byte("Terse.")},
		"promptdefaults/sdlc/windows.md":       {Data: []byte("Carriage.\r\n")},
		"promptdefaults/sdlc/README.txt":       {Data: []byte("not a prompt")},
		"promptdefaults/sdlc/nested/deeper.md": {Data: []byte("not read\n")},
		"promptdefaults/workflow/unrelated.md": {Data: []byte("elsewhere\n")},
	}
}

func TestDefaults_ReadsEveryMarkdownFileAndDropsOnlyTheFinalNewline(t *testing.T) {
	bodies, err := Defaults(defaultsFS(), "promptdefaults/sdlc")
	if err != nil {
		t.Fatalf("read defaults: %v", err)
	}
	want := map[string]string{
		"charter":    "Be careful.",
		"ending":     "Stop here.\n",
		"no_newline": "Terse.",
		"windows":    "Carriage.",
	}
	if len(bodies) != len(want) {
		t.Fatalf("read %d bodies, want %d: %v", len(bodies), len(want), bodies)
	}
	for name, body := range want {
		if bodies[name] != body {
			t.Fatalf("%s = %q, want %q", name, bodies[name], body)
		}
	}
	if _, read := bodies["deeper"]; read {
		t.Fatal("a nested directory is not part of the set")
	}
	if _, read := bodies["README"]; read {
		t.Fatal("a file that is not markdown is not a prompt")
	}
}

func TestDefaults_RefusesADirectoryWithNothingInIt(t *testing.T) {
	_, err := Defaults(defaultsFS(), "promptdefaults/absent")
	if !errors.Is(err, ErrDefaultsUnreadable) {
		t.Fatalf("a missing directory must be refused, got %v", err)
	}
	var typed *DefaultsUnreadableError
	if !errors.As(err, &typed) || typed.Dir != "promptdefaults/absent" {
		t.Fatalf("the refusal must name the directory, got %v", err)
	}
	empty := fstest.MapFS{"promptdefaults/sdlc/README.txt": {Data: []byte("nothing")}}
	if _, err := Defaults(empty, "promptdefaults/sdlc"); !errors.Is(err, ErrDefaultsUnreadable) {
		t.Fatalf("a directory with no markdown is a set that ships nothing, got %v", err)
	}
}

func TestMustDefaults_ReadsOrPanics(t *testing.T) {
	if bodies := MustDefaults(defaultsFS(), "promptdefaults/workflow"); bodies["unrelated"] != "elsewhere" {
		t.Fatalf("MustDefaults read %v", bodies)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("markdown the binary was built without must panic rather than prompting nobody")
		}
	}()
	MustDefaults(defaultsFS(), "promptdefaults/absent")
}

func TestUnusedDefaults_NamesEveryFileNoSpecificationReads(t *testing.T) {
	spec := SetSpec{Kind: "sdlc", Prompts: []PromptSpec{{Name: "charter", Default: "Be careful."}}}
	bodies := map[string]string{"charter": "Be careful.", "ending": "Stop here.", "aardvark": "x"}
	unused := UnusedDefaults(spec, bodies)
	if len(unused) != 2 || unused[0] != "aardvark" || unused[1] != "ending" {
		t.Fatalf("unused = %v, want [aardvark ending]", unused)
	}
	if extra := UnusedDefaults(spec, map[string]string{"charter": "Be careful."}); len(extra) != 0 {
		t.Fatalf("a set that reads every file has nothing unused, got %v", extra)
	}
}

// unreadableFS lists a file the reader then refuses, which is what a corrupt
// embedded blob looks like from here.
type unreadableFS struct{ fstest.MapFS }

// ReadFile refuses the markdown the directory listing advertises.
func (f unreadableFS) ReadFile(name string) ([]byte, error) {
	if strings.HasSuffix(name, FileExtension) {
		return nil, errors.New("the embedded blob is damaged")
	}
	return f.MapFS.ReadFile(name)
}

func TestDefaults_ReportsAFileItCannotRead(t *testing.T) {
	fsys := unreadableFS{fstest.MapFS{"promptdefaults/sdlc/charter.md": {Data: []byte("Be careful.\n")}}}
	_, err := Defaults(fsys, "promptdefaults/sdlc")
	var typed *DefaultsUnreadableError
	if !errors.As(err, &typed) {
		t.Fatalf("an unreadable file must be reported, got %v", err)
	}
	if typed.Dir != "promptdefaults/sdlc/charter.md" {
		t.Fatalf("the refusal must name the file, got %q", typed.Dir)
	}
}
