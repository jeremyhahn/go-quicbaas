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

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/scripts/internal/gate"
)

// writeRepo builds a fixture repository with one build target.
func writeRepo(t *testing.T, recipe string) string {
	t.Helper()
	dir := t.TempDir()
	body := "DOCKER_IMAGE := quicbaas\nDOCKER_TAG := 9.9.9\nDOCKER_REGISTRY := registry.invalid\n\n" +
		"print-%:\n\t@echo \"$($*)\"\n\n" +
		".PHONY: build\nbuild:\n" + recipe
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(body), 0o644); err != nil {
		t.Fatalf("writing the fixture Makefile: %v", err)
	}
	return dir
}

func TestRunPrintsTheInventory(t *testing.T) {
	repo := writeRepo(t, "\tdocker build -t b:2 .\n\tdocker build -t a:1 .\n")

	var stdout, stderr strings.Builder
	if status := run([]string{"-root", repo}, &stdout, &stderr); status != gate.ExitClean {
		t.Fatalf("run exited %d, want %d; stderr: %s", status, gate.ExitClean, stderr.String())
	}
	// Sorted, so the report does not reshuffle between runs.
	if stdout.String() != "a:1\nb:2\n" {
		t.Fatalf("run printed %q, want the sorted inventory", stdout.String())
	}
}

func TestRunFailsRatherThanPrintingAPartialInventory(t *testing.T) {
	// THE CASE THAT MATTERS. A producer named at run time cannot be cleaned up
	// by name. Printing the rest and exiting 0 would report the escape as
	// coverage downstream, so nothing is printed and the status is non-zero.
	repo := writeRepo(t, "\tdocker build -t a:1 .\n\tdocker build -t \"$$RUNTIME_TAG\" .\n")

	var stdout, stderr strings.Builder
	status := run([]string{"-root", repo}, &stdout, &stderr)
	if status != gate.ExitFindings {
		t.Fatalf("run exited %d, want %d", status, gate.ExitFindings)
	}
	if stdout.String() != "" {
		t.Fatalf("a failed derivation printed an inventory: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "cannot resolve the image built by") {
		t.Fatalf("the failure does not say what could not be resolved: %q", stderr.String())
	}
}

func TestRunRejectsBadUsage(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want int
	}{
		{name: "an unknown flag", args: []string{"-nonesuch"}, want: gate.ExitToolError},
		{name: "a root that is not a repository", args: []string{"-root", t.TempDir()}, want: gate.ExitFindings},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr strings.Builder
			if status := run(tc.args, &stdout, &stderr); status != tc.want {
				t.Fatalf("run(%v) exited %d, want %d", tc.args, status, tc.want)
			}
			if stdout.String() != "" {
				t.Fatalf("a failed run printed an inventory: %q", stdout.String())
			}
		})
	}
}
