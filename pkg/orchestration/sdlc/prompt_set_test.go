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

package sdlc

import (
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/prompts"
)

// =============================================================================
// The set's own contract
// =============================================================================

// TestWorkerCharterMarkers_ComeFromTheParsersOwnConstants is the guard the
// whole extraction rests on.
//
// The charter used to be assembled by concatenating turn.go's constants, so
// it could not name a code the parser did not know. The charter is now a
// file a person edits, and what replaces that guarantee is this: the
// specification declares its markers FROM stopCodeOutcomes, and registration
// refuses a charter that dropped one. Change a constant's value and the
// markdown stops matching; add a stop code and the charter must teach it.
func TestWorkerCharterMarkers_ComeFromTheParsersOwnConstants(t *testing.T) {
	spec, declared := sdlcPrompts.Spec().Prompt("worker_charter")
	if !declared {
		t.Fatal("the set declares no worker_charter")
	}
	literals := make(map[string]bool, len(spec.Markers))
	for _, marker := range spec.Markers {
		if marker.Reason == "" {
			t.Fatalf("marker %q must say what reads it", marker.Literal)
		}
		literals[marker.Literal] = true
	}
	if !literals[TurnStopMarker] {
		t.Fatalf("the charter must be held to %q, the marker ParseTurnStop reads", TurnStopMarker)
	}
	for code := range stopCodeOutcomes {
		if !literals[code] {
			t.Fatalf("ClassifyTurn acts on stop code %q and the charter is not held to naming it", code)
		}
	}
	if len(literals) != len(stopCodeOutcomes)+1 {
		t.Fatalf("the charter declares %d markers for %d codes plus the marker; a marker "+
			"nothing parses is a clause an editor cannot remove for no reason",
			len(literals), len(stopCodeOutcomes))
	}
	body, err := sdlcPrompts.Body("worker_charter")
	if err != nil {
		t.Fatalf("read the charter: %v", err)
	}
	for literal := range literals {
		if !strings.Contains(body, literal) {
			t.Fatalf("the shipped charter does not name %q", literal)
		}
	}
}

// TestWorkerCharter_RefusesABodyThatDroppedAStopCode proves the declaration
// is enforced rather than decorative: a charter edited to forget a code the
// runner acts on is refused, by name.
func TestWorkerCharter_RefusesABodyThatDroppedAStopCode(t *testing.T) {
	spec, _ := sdlcPrompts.Spec().Prompt("worker_charter")
	body, err := sdlcPrompts.Body("worker_charter")
	if err != nil {
		t.Fatalf("read the charter: %v", err)
	}
	withoutMarker := strings.ReplaceAll(body, TurnStopMarker, "STOP-PLEASE:")
	err = prompts.Validate(SetSDLC, spec, withoutMarker)
	var missing *prompts.MissingMarkerError
	if !errors.As(err, &missing) {
		t.Fatalf("a charter without the stop marker must be refused, got %v", err)
	}
	if missing.Marker != TurnStopMarker {
		t.Fatalf("the refusal must name the marker, got %q", missing.Marker)
	}
	withoutCode := strings.ReplaceAll(body, TurnStopOwnerDecision, "owner-says-so")
	if err := prompts.Validate(SetSDLC, spec, withoutCode); !errors.Is(err, prompts.ErrMissingMarker) {
		t.Fatalf("a charter that stopped naming %q must be refused, got %v", TurnStopOwnerDecision, err)
	}
	if err := prompts.Validate(SetSDLC, spec, body); err != nil {
		t.Fatalf("the shipped charter must pass its own contract: %v", err)
	}
}

// TestSDLCPromptDefaults_EveryFileIsDeclared refuses dead markdown: a file a
// person can edit and nothing will ever send.
func TestSDLCPromptDefaults_EveryFileIsDeclared(t *testing.T) {
	if unused := prompts.UnusedDefaults(sdlcPrompts.Spec(), sdlcDefaults); len(unused) > 0 {
		t.Fatalf("no specification reads these files: %v", unused)
	}
	for _, name := range sdlcPrompts.Names() {
		if _, shipped := sdlcDefaults[name]; !shipped {
			t.Fatalf("prompt %q is declared with no markdown behind it", name)
		}
	}
}

// TestSDLCPromptDefaults_EveryPromptSaysWhatItIsFor keeps the editing surface
// usable: a prompt with no description is a text box with no label.
func TestSDLCPromptDefaults_EveryPromptSaysWhatItIsFor(t *testing.T) {
	for _, prompt := range sdlcPrompts.Spec().Prompts {
		if strings.TrimSpace(prompt.Description) == "" {
			t.Errorf("prompt %q has no description", prompt.Name)
		}
		for _, variable := range prompt.Variables {
			if strings.TrimSpace(variable.Description) == "" {
				t.Errorf("prompt %q variable %q has no description", prompt.Name, variable.Name)
			}
		}
	}
}

// TestSDLCPromptDefaults_EveryFileEndsWithOneNewline holds the files to the
// rule the loader depends on. A file saved without its final newline, or
// with two where one was meant, changes what an agent reads by a byte
// nobody intended.
func TestSDLCPromptDefaults_EveryFileEndsWithOneNewline(t *testing.T) {
	assertPromptFilesAreWellFormed(t, sdlcDefaultsFS, "promptdefaults/sdlc")
}

// assertPromptFilesAreWellFormed checks the file hygiene both sets keep.
func assertPromptFilesAreWellFormed(t *testing.T, fsys fs.FS, dir string) {
	t.Helper()
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, prompts.FileExtension) {
			t.Fatalf("%s/%s is not markdown; the loader ignores it and nobody will notice", dir, name)
		}
		raw, readErr := fs.ReadFile(fsys, dir+"/"+name)
		if readErr != nil {
			t.Fatalf("read %s/%s: %v", dir, name, readErr)
		}
		content := string(raw)
		if !strings.HasSuffix(content, "\n") {
			t.Errorf("%s/%s does not end with a newline", dir, name)
		}
		if strings.Contains(content, "\r") {
			t.Errorf("%s/%s carries a carriage return", dir, name)
		}
		if strings.TrimSpace(content) == "" {
			t.Errorf("%s/%s is blank", dir, name)
		}
	}
}
