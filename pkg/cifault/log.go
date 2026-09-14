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
	"time"
)

// =============================================================================
// Reading a job log
// =============================================================================
//
// Every rule in this package reads LINES, never the log as one string, and
// that is not a style choice. A test whose own output contains the words "Job
// succeeded" must not be able to void the run that failed it, so the
// end-of-job markers are matched whole-line. A path that happens to contain a
// project owner's name must not stand in for a module the toolchain could not
// resolve, so that rule needs both facts on the SAME line.

// StripTimestamps drops the RFC3339 prefix runners put on every log line.
//
// A BYTE-BOUNDED TAIL CUTS MID-LINE, so an unstamped first line is a partial
// line rather than a line, and is dropped: quoting half a sentence as evidence
// invites a reader to draw a conclusion from text nobody wrote.
func StripTimestamps(raw string) string {
	return strings.TrimSpace(strings.Join(stripTimestampLines(raw), "\n"))
}

// stripTimestampLines is StripTimestamps as the lines it produced.
func stripTimestampLines(raw string) []string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	for i, line := range lines {
		stamped := false
		if space := strings.IndexByte(line, ' '); space > 0 {
			if _, err := time.Parse(time.RFC3339Nano, line[:space]); err == nil {
				line = line[space+1:]
				stamped = true
			}
		}
		if i == 0 && !stamped && len(lines) > 1 {
			continue
		}
		out = append(out, strings.TrimRight(line, " \t"))
	}
	return out
}

// logView is a job log prepared for the rules: its non-blank lines with the
// runner's timestamp stripped, and the same lines lower-cased for the
// case-insensitive rules.
type logView struct {
	lines []string
	lower []string
}

// newLogView prepares raw for the rules.
func newLogView(raw string) logView {
	stripped := stripTimestampLines(raw)
	view := logView{lines: make([]string, 0, len(stripped)), lower: make([]string, 0, len(stripped))}
	for _, line := range stripped {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		view.lines = append(view.lines, trimmed)
		view.lower = append(view.lower, strings.ToLower(trimmed))
	}
	return view
}

// empty reports whether the log carried no line at all. An empty log is no
// evidence of anything and every rule that would void a check refuses it.
func (v logView) empty() bool { return len(v.lines) == 0 }

// last returns the log's final line, bounded for quoting.
func (v logView) last() string {
	if v.empty() {
		return ""
	}
	return boundEvidence(v.lines[len(v.lines)-1])
}

// findLine returns the newest line containing every needle. It searches from
// the end because the decisive line of a CI log is near its close.
func (v logView) findLine(needles ...string) (string, bool) {
	if len(needles) == 0 {
		return "", false
	}
	for i := len(v.lower) - 1; i >= 0; i-- {
		matched := true
		for _, needle := range needles {
			if !strings.Contains(v.lower[i], needle) {
				matched = false
				break
			}
		}
		if matched {
			return boundEvidence(v.lines[i]), true
		}
	}
	return "", false
}

// tailFrom returns the index the last n lines start at.
func (v logView) tailFrom(n int) int {
	if n >= len(v.lower) {
		return 0
	}
	return len(v.lower) - n
}

// =============================================================================
// End-of-job markers
// =============================================================================

// marker is what a runner writes when it reaches the END of a job.
type marker int

const (
	// markerNone is a line that is not an end-of-job marker.
	markerNone marker = iota

	// markerSucceeded is the runner's own statement that the job passed.
	markerSucceeded

	// markerFailed is the runner's own statement that the job failed.
	markerFailed

	// markerCleanup is the teardown that follows either answer. It says the
	// runner reached the end without saying which end.
	markerCleanup
)

// terminalMarker classifies one log line as an end-of-job marker.
//
// WHOLE-LINE, NEVER A SUBSTRING SEARCH OVER THE LOG. The words "Job
// succeeded" appear in plenty of application output, and this package's most
// expensive possible mistake is reading one of them as a runner's verdict and
// voiding a change's real failure.
func terminalMarker(lower string) marker {
	if lower == "job succeeded" {
		return markerSucceeded
	}
	if lower == "job failed" || (strings.HasPrefix(lower, "job '") && strings.HasSuffix(lower, "' failed")) {
		return markerFailed
	}
	if strings.HasPrefix(lower, "cleaning up container for job ") {
		return markerCleanup
	}
	return markerNone
}

// markerIn reports whether any of the lines from index start carries any of
// the wanted markers.
func (v logView) markerIn(start int, wanted ...marker) (string, bool) {
	for i := start; i < len(v.lower); i++ {
		found := terminalMarker(v.lower[i])
		if found == markerNone {
			continue
		}
		for _, want := range wanted {
			if found == want {
				return boundEvidence(v.lines[i]), true
			}
		}
	}
	return "", false
}

// =============================================================================
// The change's own failure, visible in its own log
// =============================================================================

// failureEvidence reports whether the log SHOWS the change failing: a Go test
// that failed, a panic, a make recipe that returned non-zero, a workflow
// error annotation.
//
// IT IS A VETO ON EVERY VOID VERDICT, which is the only reason it exists. A
// log that already contains what broke is not a log to re-run: whatever else
// went wrong on that host, there is a failure in it for its author to read.
// Being liberal here is the safe direction — a false positive costs one agent
// attempt, a false negative costs an unbounded re-run of a broken change.
func (v logView) failureEvidence() (string, bool) {
	for i := len(v.lower) - 1; i >= 0; i-- {
		if failureLine(v.lower[i]) {
			return boundEvidence(v.lines[i]), true
		}
	}
	return "", false
}

// failureLine reports whether one line is the change's own failure.
func failureLine(lower string) bool {
	switch {
	case strings.HasPrefix(lower, "--- fail:"):
		return true
	case lower == "fail" || strings.HasPrefix(lower, "fail\t") ||
		strings.HasPrefix(lower, "fail ") || strings.HasPrefix(lower, "fail:"):
		return true
	case strings.HasPrefix(lower, "panic:"):
		return true
	case strings.HasPrefix(lower, "fatal error:"):
		return true
	case strings.HasPrefix(lower, "##[error]") || strings.HasPrefix(lower, "::error"):
		return true
	case strings.Contains(lower, "make: *** ") && strings.Contains(lower, "error "):
		return true
	}
	return false
}

// evidenceLimit bounds a quoted log line. A CI log can carry a single line of
// megabytes — a base64 blob, a minified bundle — and evidence is something a
// person reads.
const evidenceLimit = 300

// boundEvidence trims a quoted line to evidenceLimit, on a rune boundary.
func boundEvidence(line string) string {
	line = strings.TrimSpace(line)
	if len(line) <= evidenceLimit {
		return line
	}
	cut := evidenceLimit
	for cut > 0 && !isRuneStart(line[cut]) {
		cut--
	}
	return strings.TrimSpace(line[:cut]) + "…"
}

// isRuneStart reports whether b begins a UTF-8 rune.
func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
