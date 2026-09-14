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

package gate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeMakefile drops a Makefile carrying the `print-%` rule the gates read
// variables through.
func writeMakefile(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	content := body + "\nprint-%:\n\t@echo \"$($*)\"\n"
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(content), 0o644); err != nil {
		t.Fatalf("writing the fixture Makefile: %v", err)
	}
	return dir
}

func TestMakeVarReadsTheValue(t *testing.T) {
	dir := writeMakefile(t, "DOCKER_IMAGE := quicbaas\nDOCKER_TAG := 9.9.9\nCOMPOSED := $(DOCKER_IMAGE):$(DOCKER_TAG)")

	cases := []struct {
		name string
		want string
	}{
		{name: "DOCKER_IMAGE", want: "quicbaas"},
		{name: "DOCKER_TAG", want: "9.9.9"},
		// The composed value is the reason a gate asks make rather than
		// parsing: only make knows what the expansion produces.
		{name: "COMPOSED", want: "quicbaas:9.9.9"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := MakeVar(dir, tc.name)
			if err != nil {
				t.Fatalf("MakeVar(%q): %v", tc.name, err)
			}
			if got != tc.want {
				t.Fatalf("MakeVar(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

func TestMakeVarRefusesAnUnusableAnswer(t *testing.T) {
	t.Run("an empty value is not an answer", func(t *testing.T) {
		dir := writeMakefile(t, "EMPTY :=")
		_, err := MakeVar(dir, "EMPTY")
		var empty *EmptyMakeVarError
		if !errors.As(err, &empty) {
			t.Fatalf("MakeVar on an empty variable = %v, want *EmptyMakeVarError", err)
		}
		if empty.Name != "EMPTY" || empty.Dir != dir {
			t.Fatalf("the error does not name what is missing: %+v", empty)
		}
	})

	t.Run("an undefined variable expands to nothing and is refused", func(t *testing.T) {
		dir := writeMakefile(t, "DEFINED := yes")
		_, err := MakeVar(dir, "NEVER_DEFINED")
		var empty *EmptyMakeVarError
		if !errors.As(err, &empty) {
			t.Fatalf("MakeVar on an undefined variable = %v, want *EmptyMakeVarError", err)
		}
	})

	t.Run("a directory with no Makefile fails rather than answering empty", func(t *testing.T) {
		_, err := MakeVar(t.TempDir(), "DOCKER_IMAGE")
		var varErr *MakeVarError
		if !errors.As(err, &varErr) {
			t.Fatalf("MakeVar without a Makefile = %v, want *MakeVarError", err)
		}
		if varErr.Unwrap() == nil {
			t.Fatal("MakeVarError must carry the underlying failure")
		}
	})
}

func TestTypedErrorsNameWhatIsMissing(t *testing.T) {
	// A gate's error text is read by the engineer who tripped it, so the name of
	// the missing input has to be in it. "skipping" is the message this whole
	// package exists to make impossible.
	missing := &MissingInputError{Input: "-qrdb-dir", Why: "the canonical types cannot be read"}
	if got := missing.Error(); got != "required input -qrdb-dir is missing: the canonical types cannot be read" {
		t.Fatalf("MissingInputError.Error() = %q", got)
	}

	wrapped := errors.New("exit status 2")
	varErr := &MakeVarError{Name: "DOCKER_IMAGE", Dir: "/repo", Err: wrapped}
	if !errors.Is(varErr, wrapped) {
		t.Fatal("MakeVarError must unwrap to the underlying failure")
	}
}

func TestExitCodesAreDistinct(t *testing.T) {
	// Collapsing "a violation" into "could not run" is the defect the whole
	// contract exists to prevent, so the three values must differ.
	if ExitClean == ExitFindings || ExitFindings == ExitToolError || ExitClean == ExitToolError {
		t.Fatalf("exit codes must be distinct: clean=%d findings=%d tool=%d",
			ExitClean, ExitFindings, ExitToolError)
	}
	if ExitClean != 0 {
		t.Fatalf("a clean run must exit 0, got %d", ExitClean)
	}
}

func TestErrorMessagesAreReadable(t *testing.T) {
	// These strings are the whole product when a gate cannot run, so they are
	// asserted rather than assumed.
	varErr := &MakeVarError{Name: "DOCKER_IMAGE", Dir: "/repo", Output: "make: *** No rule to make target\n", Err: errors.New("exit status 2")}
	got := varErr.Error()
	for _, want := range []string{"DOCKER_IMAGE", "/repo", "exit status 2", "No rule to make target"} {
		if !strings.Contains(got, want) {
			t.Fatalf("MakeVarError.Error() omits %q: %s", want, got)
		}
	}
	// With nothing from make, the message carries no empty parenthesis.
	quiet := (&MakeVarError{Name: "X", Dir: "/d", Err: errors.New("boom")}).Error()
	if strings.Contains(quiet, "()") {
		t.Fatalf("MakeVarError.Error() has an empty aside: %s", quiet)
	}

	empty := (&EmptyMakeVarError{Name: "DOCKER_TAG", Dir: "/repo"}).Error()
	for _, want := range []string{"DOCKER_TAG", "/repo", "expanded to nothing"} {
		if !strings.Contains(empty, want) {
			t.Fatalf("EmptyMakeVarError.Error() omits %q: %s", want, empty)
		}
	}
}
