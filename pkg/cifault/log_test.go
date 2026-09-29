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

package cifault

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestStripTimestamps_RemovesTheRunnerPrefix covers the ordinary case.
func TestStripTimestamps_RemovesTheRunnerPrefix(t *testing.T) {
	raw := "2026-09-12T17:45:25.7019041Z ci-runner-02(version:v3.1.0)\n" +
		"2026-09-12T17:45:25.7019657Z ::group::Runner Information\r\n" +
		"2026-09-12T17:45:25.7019672Z Runner labels: ubuntu-latest\n"
	want := "ci-runner-02(version:v3.1.0)\n::group::Runner Information\nRunner labels: ubuntu-latest"
	if got := StripTimestamps(raw); got != want {
		t.Fatalf("StripTimestamps =\n%q\nwant\n%q", got, want)
	}
}

// TestStripTimestamps_BadInput covers what a byte-bounded tail hands it: a
// first line cut mid-sentence, an empty log, and a log with no stamps at all.
func TestStripTimestamps_BadInput(t *testing.T) {
	cases := map[string]struct{ raw, want string }{
		"a partial first line is dropped": {
			"ing the suite\n2026-09-12T17:45:25.7019657Z Job failed\n", "Job failed",
		},
		"a single unstamped line is kept": {"Job failed", "Job failed"},
		"an empty log":                    {"", ""},
		"whitespace only":                 {"   \n\t\n", ""},
		"no stamps at all": {
			"first\nsecond\nthird", "second\nthird",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := StripTimestamps(tc.raw); got != tc.want {
				t.Fatalf("StripTimestamps(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

// TestTerminalMarker_ClassifiesRunnerLines covers every marker act_runner
// writes, taken verbatim from the corpus.
func TestTerminalMarker_ClassifiesRunnerLines(t *testing.T) {
	cases := map[string]marker{
		"job succeeded":                       markerSucceeded,
		"job failed":                          markerFailed,
		"job 'lint' failed":                   markerFailed,
		"job 'race detector' failed":          markerFailed,
		"cleaning up container for job lint":  markerCleanup,
		"cleaning up container for job build": markerCleanup,
	}
	for line, want := range cases {
		if got := terminalMarker(line); got != want {
			t.Errorf("terminalMarker(%q) = %d, want %d", line, got, want)
		}
	}
}

// TestTerminalMarker_RefusesLookalikes is the bad-input half, and it is the
// one that protects a real failure: application output that MENTIONS a marker
// is not a marker.
func TestTerminalMarker_RefusesLookalikes(t *testing.T) {
	for _, line := range []string{
		"",
		"    job succeeded",
		"the log said job succeeded",
		"job succeeded (probably)",
		"=== run   testjobsucceeded",
		"job_failed",
		"cleaning up network for job lint, and network name is: gitea-actions",
		"ok  \tgithub.com/jeremyhahn/go-quicbaas/pkg/api/websocket\t35.451s",
	} {
		if got := terminalMarker(line); got != markerNone {
			t.Errorf("terminalMarker(%q) = %d, want markerNone", line, got)
		}
	}
}

// TestFailureLine_RecognisesTheChangesOwnFailure covers the veto's evidence.
func TestFailureLine_RecognisesTheChangesOwnFailure(t *testing.T) {
	for _, line := range []string{
		"--- fail: testaccountpool_concurrentspawnsproduceonerelease (0.01s)",
		"fail",
		"fail\tgithub.com/jeremyhahn/go-quicbaas/pkg/fleet\t1.161s",
		"fail github.com/jeremyhahn/go-quicbaas/pkg/fleet 1.161s",
		"fail: testthing",
		"panic: runtime error: invalid memory address",
		"fatal error: concurrent map writes",
		"##[error]process completed with exit code 1",
		"::error::the build did not compile",
		"make: *** [makefile:516: vet] error 1",
	} {
		if !failureLine(line) {
			t.Errorf("failureLine(%q) = false, want true", line)
		}
	}
}

// TestFailureLine_RefusesOrdinaryOutput is the bad-input half. A false
// positive here only costs an agent attempt, but a rule that fired on every
// passing line would make the veto meaningless.
func TestFailureLine_RefusesOrdinaryOutput(t *testing.T) {
	for _, line := range []string{
		"",
		"=== run   testfailfast",
		"--- pass: testnoopauthenticator (0.00s)",
		"ok  \tgithub.com/jeremyhahn/go-quicbaas/pkg/api/websocket\t35.451s",
		"failing over to the secondary",
		"failures: 0",
		"make: entering directory '/workspace'",
		"[gosec] 2026/09/12 17:46:58 including rules: default",
	} {
		if failureLine(line) {
			t.Errorf("failureLine(%q) = true, want false", line)
		}
	}
}

// TestLogView_ReadsLines covers the view's accessors over a real excerpt.
func TestLogView_ReadsLines(t *testing.T) {
	view := newLogView(fixture(t, "registry-unreachable.log"))
	if view.empty() {
		t.Fatal("a 20-line log read as empty")
	}
	if !strings.Contains(view.last(), "failed to pull image") {
		t.Errorf("last() = %q, want the pull failure", view.last())
	}
	if line, found := view.findLine("failed to pull image"); !found ||
		!strings.Contains(line, "ci-runner:latest") {
		t.Errorf("findLine = %q, %v", line, found)
	}
	if from := view.tailFrom(5); from != len(view.lines)-5 {
		t.Errorf("tailFrom(5) = %d, want %d", from, len(view.lines)-5)
	}
}

// TestLogView_BadInput covers the empty and degenerate views every void rule
// has to refuse.
func TestLogView_BadInput(t *testing.T) {
	view := newLogView("")
	if !view.empty() {
		t.Error("an empty log did not read as empty")
	}
	if got := view.last(); got != "" {
		t.Errorf("last() on an empty view = %q", got)
	}
	if _, found := view.findLine("anything"); found {
		t.Error("findLine found something in an empty view")
	}
	if _, found := view.findLine(); found {
		t.Error("findLine with no needles matched")
	}
	if got := view.tailFrom(10); got != 0 {
		t.Errorf("tailFrom(10) on an empty view = %d, want 0", got)
	}
	if _, found := view.markerIn(0, markerFailed); found {
		t.Error("markerIn found a marker in an empty view")
	}
	if _, found := view.failureEvidence(); found {
		t.Error("failureEvidence found evidence in an empty view")
	}

	populated := newLogView("2026-09-12T17:50:34.0000000Z Job failed\n")
	if _, found := populated.markerIn(0); found {
		t.Error("markerIn with no wanted markers matched")
	}
	if got := populated.tailFrom(99); got != 0 {
		t.Errorf("tailFrom past the start = %d, want 0", got)
	}
}

// TestBoundEvidence_KeepsQuotedLinesReadable covers the bound and the rune
// boundary a naive slice would break.
func TestBoundEvidence_KeepsQuotedLinesReadable(t *testing.T) {
	short := "failed to pull image"
	if got := boundEvidence("  " + short + "  "); got != short {
		t.Errorf("boundEvidence trimmed to %q", got)
	}

	long := strings.Repeat("a", evidenceLimit*2)
	bounded := boundEvidence(long)
	if len(bounded) > evidenceLimit+len("…") {
		t.Errorf("boundEvidence returned %d bytes, want at most %d", len(bounded), evidenceLimit+len("…"))
	}
	if !strings.HasSuffix(bounded, "…") {
		t.Errorf("a truncated line must say so: %q", bounded)
	}

	// Multi-byte runes straddling the cut must not become replacement
	// characters: evidence an operator cannot read is evidence nobody checks.
	runes := boundEvidence(strings.Repeat("héllo wörld ", evidenceLimit))
	if !utf8.ValidString(runes) {
		t.Errorf("boundEvidence produced invalid UTF-8: %q", runes)
	}
	if strings.ContainsRune(runes, '�') {
		t.Errorf("boundEvidence cut inside a rune: %q", runes)
	}
}

// TestBoundEvidence_BadInput covers the empty and whitespace-only line.
func TestBoundEvidence_BadInput(t *testing.T) {
	if got := boundEvidence(""); got != "" {
		t.Errorf("boundEvidence(\"\") = %q", got)
	}
	if got := boundEvidence("   \t  "); got != "" {
		t.Errorf("boundEvidence(whitespace) = %q", got)
	}
}
