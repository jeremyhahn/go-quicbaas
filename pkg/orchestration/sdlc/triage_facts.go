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
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// Repository facts are the deterministic half of triage's first two checks,
// measured by the runner and handed to the model as evidence.
//
// WHY THE RUNNER MEASURES THEM. The charter's first check is "is it already
// done?", and the honest way to answer it is to look at the checkout. A
// frontier model does; a small one does not. Measured on the NPU
// (2026-09-21): on a short prompt qwen3-8b called the run_command tool, on the
// real ~7k-token triage prompt it never once did — tool_uses=0 on every turn
// of the day — and wrote "I have examined the code" regardless. Its verdicts
// were ungrounded by construction. Asking harder does not change that; the
// owner's standing rule does: work that is deterministic is done in Go, not
// bought from a model. So the runner extracts what the issue names — the
// file paths and the code identifiers — asks git whether each exists in the
// checkout at the head being judged, and puts the answers in the prompt.
// Every rung benefits: a frontier model spends fewer tool rounds, and a small
// one gets the evidence it would not have fetched.
//
// WHAT IS NOT MEASURED. Whether the work is done is a judgement; whether the
// files and symbols it talks about exist is a fact. The facts stop at the
// fact.

const (
	// triageFactMaxPaths bounds how many paths one issue is probed for.
	triageFactMaxPaths = 10
	// triageFactMaxIdentifiers bounds how many identifiers one issue is
	// probed for.
	triageFactMaxIdentifiers = 8
	// triageFactMaxMatches is how many grep lines one identifier reports.
	triageFactMaxMatches = 3
	// triageFactMaxLine bounds one reported grep line.
	triageFactMaxLine = 160
	// triageFactMaxBytes bounds the whole rendered section, so the facts
	// cannot crowd the issue out of a small model's prompt.
	triageFactMaxBytes = 4000
)

var (
	// triagePathPattern matches the file paths an issue names in prose or
	// code spans: a slash-or-dot path ending in a source, doc, build or
	// config extension, optionally followed by a :line suffix.
	triagePathPattern = regexp.MustCompile(
		`(?:^|[\s(\x60"'])((?:[A-Za-z0-9_.-]+/)*[A-Za-z0-9_.-]+\.(?:go|md|ya?ml|sh|toml|json|proto|txt|mod|sum|pbtxt|yaml)|Makefile|Dockerfile(?:\.[A-Za-z0-9_.-]+)?)(?::\d+)?(?:[\s)\x60"',.:;]|$)`)
	// triageIdentifierPattern matches a code identifier inside backticks:
	// Go-shaped, dotted allowed, no path separators.
	triageIdentifierPattern = regexp.MustCompile("`([A-Za-z_][A-Za-z0-9_]*(?:\\.[A-Za-z_][A-Za-z0-9_]*)*)`")
)

// TriageProbes is what an issue names that the checkout can be asked about.
type TriageProbes struct {
	// Paths are the file paths the issue mentions, in order of first
	// appearance, deduplicated and bounded.
	Paths []string
	// Identifiers are the backticked code identifiers, likewise.
	Identifiers []string
}

// ExtractTriageProbes reads the paths and identifiers an issue names.
//
// A backticked token that is also a path is a path, not an identifier, and an
// identifier shorter than four characters is too common a word to grep for
// (`err`, `ctx`, `ok`).
func ExtractTriageProbes(title, body string) TriageProbes {
	text := title + "\n" + body
	var probes TriageProbes
	seen := map[string]bool{}
	for _, m := range triagePathPattern.FindAllStringSubmatch(text, -1) {
		p := m[1]
		if seen[p] || len(probes.Paths) >= triageFactMaxPaths {
			continue
		}
		seen[p] = true
		probes.Paths = append(probes.Paths, p)
	}
	for _, m := range triageIdentifierPattern.FindAllStringSubmatch(text, -1) {
		id := m[1]
		if len(id) < 4 || seen[id] || strings.Contains(id, "/") || len(probes.Identifiers) >= triageFactMaxIdentifiers {
			continue
		}
		seen[id] = true
		probes.Identifiers = append(probes.Identifiers, id)
	}
	return probes
}

// TriageFact is one measured answer.
type TriageFact struct {
	// Subject is the path or identifier that was probed.
	Subject string
	// Present says whether the checkout has it.
	Present bool
	// Matches are the grep lines an identifier was found on, or the tracked
	// paths a path was found at, bounded.
	Matches []string
}

// TriageFacts is what the checkout said about an issue's probes.
type TriageFacts struct {
	// Head is the commit the facts were measured at.
	Head string
	// Paths is one fact per probed path.
	Paths []TriageFact
	// Identifiers is one fact per probed identifier.
	Identifiers []TriageFact
}

// MeasureTriageFacts asks the checkout about every probe through exec, which
// runs a command in the session's container.
//
// A PROBE THAT CANNOT BE RUN FAILS THE MEASUREMENT: a fact half-measured is a
// fact wrong, and the caller renders no section rather than a misleading one.
// `git ls-files` exits 0 whether or not the path is tracked and prints it
// only when it is; `git grep` exits 1 for no match, which is an answer, and
// higher for an error, which is not.
func MeasureTriageFacts(
	exec func(*orchestration.ExecSpec) (*orchestration.ExecResult, error),
	workDir, head string,
	probes TriageProbes,
) (TriageFacts, error) {
	facts := TriageFacts{Head: head}
	for _, p := range probes.Paths {
		// THE PATH AS WRITTEN, AND THE SAME PATH AT ANY DEPTH. A pathspec is
		// read from the repository root, so an issue that names a file by its
		// base name — "canonicalconformance_integration_test.go", which lives
		// in pkg/luks2 — was reported ABSENT, and triage parked go-luks2#209
		// as needs-info on that false fact every pass (2026-09-22, 09-25).
		// The extracted path holds no glob characters (triagePathPattern), so
		// the glob form matches only the path itself, under any directory.
		result, err := exec(&orchestration.ExecSpec{
			Cmd:     []string{gitCommand, "ls-files", "--", p, triageAnyDepth(p)},
			WorkDir: workDir,
		})
		if err != nil {
			return TriageFacts{}, err
		}
		if !result.Succeeded() {
			return TriageFacts{}, &CheckoutCommandError{Command: "ls-files -- " + p, ExitCode: result.ExitCode, Output: strings.TrimSpace(result.Stderr)}
		}
		fact := TriageFact{Subject: p}
		for _, found := range strings.Split(strings.TrimSpace(result.Stdout), "\n") {
			found = strings.TrimSpace(found)
			if found == "" || slices.Contains(fact.Matches, found) || len(fact.Matches) >= triageFactMaxMatches {
				continue
			}
			fact.Matches = append(fact.Matches, found)
		}
		fact.Present = len(fact.Matches) > 0
		facts.Paths = append(facts.Paths, fact)
	}
	for _, id := range probes.Identifiers {
		result, err := exec(&orchestration.ExecSpec{
			Cmd:     []string{gitCommand, "grep", "-n", "-I", "--fixed-strings", "--max-count", strconv.Itoa(triageFactMaxMatches), "--", id},
			WorkDir: workDir,
		})
		if err != nil {
			return TriageFacts{}, err
		}
		if result.ExitCode > 1 {
			return TriageFacts{}, &CheckoutCommandError{Command: "grep -- " + id, ExitCode: result.ExitCode, Output: strings.TrimSpace(result.Stderr)}
		}
		fact := TriageFact{Subject: id}
		for _, line := range strings.Split(strings.TrimSpace(result.Stdout), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || len(fact.Matches) >= triageFactMaxMatches {
				continue
			}
			if len(line) > triageFactMaxLine {
				line = line[:triageFactMaxLine] + "…"
			}
			fact.Matches = append(fact.Matches, line)
		}
		fact.Present = len(fact.Matches) > 0
		facts.Identifiers = append(facts.Identifiers, fact)
	}
	return facts, nil
}

// triageAnyDepth is the pathspec matching path under any directory of the
// checkout, the root included.
func triageAnyDepth(path string) string {
	return ":(glob)**/" + strings.TrimPrefix(path, "/")
}

// Render writes the facts as the prompt section the model reads, bounded to
// triageFactMaxBytes.
func (f TriageFacts) Render() string {
	if len(f.Paths) == 0 && len(f.Identifiers) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Repository facts, measured by the factory at ")
	if f.Head != "" {
		b.WriteString(shortSHA(f.Head))
	} else {
		b.WriteString("the default branch")
	}
	b.WriteString("\n\nThese were measured with git in the checkout, not by you. Trust them over " +
		"what you remember, and cite them.\n\n")
	if len(f.Paths) > 0 {
		b.WriteString("Files the issue names:\n\n")
		for _, p := range f.Paths {
			if p.Present {
				b.WriteString("- `" + p.Subject + "` — present")
				if len(p.Matches) > 0 && (len(p.Matches) > 1 || p.Matches[0] != p.Subject) {
					b.WriteString(" at `" + strings.Join(p.Matches, "`, `") + "`")
				}
				b.WriteString("\n")
			} else {
				b.WriteString("- `" + p.Subject + "` — ABSENT from the checkout\n")
			}
		}
		b.WriteString("\n")
	}
	if len(f.Identifiers) > 0 {
		b.WriteString("Identifiers the issue names:\n\n")
		for _, id := range f.Identifiers {
			if !id.Present {
				b.WriteString("- `" + id.Subject + "` — no match anywhere in the tree\n")
				continue
			}
			b.WriteString("- `" + id.Subject + "` — found; first " + strconv.Itoa(len(id.Matches)) + " match(es):\n")
			for _, m := range id.Matches {
				b.WriteString("    " + m + "\n")
			}
		}
		b.WriteString("\n")
	}
	out := b.String()
	if len(out) > triageFactMaxBytes {
		out = out[:triageFactMaxBytes] + "\n… (facts truncated)\n"
	}
	return out
}
