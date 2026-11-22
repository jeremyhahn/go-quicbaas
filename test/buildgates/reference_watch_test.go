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

package buildgates

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// draftCopyName splits a pinned draft copy's file name, such as
// draft-ietf-webtrans-http3-16.txt, into its draft name and revision.
var draftCopyName = regexp.MustCompile(`^(draft-.+)-([0-9]{2})\.txt$`)

// TestReferenceDraftsHaveAStandardsWatchLine enforces the rule of
// docs/reference/README.md:3 (and docs/SPEC.md): every adopted draft copied
// into docs/reference/ has a standards-watch line in ROADMAP.md Dependencies
// that names the draft and the revision pinned, so later revisions are tracked.
func TestReferenceDraftsHaveAStandardsWatchLine(t *testing.T) {
	copies, err := filepath.Glob(filepath.Join(repoRoot, "docs", "reference", "draft-*-[0-9][0-9].txt"))
	if err != nil {
		t.Fatalf("glob docs/reference: %v", err)
	}
	if len(copies) == 0 {
		t.Fatal("docs/reference holds no draft copy; the gate would pass vacuously")
	}
	lines := strings.Split(readFile(t, filepath.Join(repoRoot, "ROADMAP.md")), "\n")
	for _, path := range copies {
		m := draftCopyName.FindStringSubmatch(filepath.Base(path))
		if m == nil {
			t.Errorf("%s: not a draft-<name>-NN.txt copy", filepath.Base(path))
			continue
		}
		name, rev := m[1], m[2]
		prefix := "- Standards watch: " + name + " "
		found := false
		for _, line := range lines {
			if strings.HasPrefix(line, prefix) && strings.Contains(line, "revision -"+rev) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: ROADMAP.md has no line starting %q and pinning revision -%s", name, prefix, rev)
		}
	}
}
