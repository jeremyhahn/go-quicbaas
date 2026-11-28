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
	"strings"
	"testing"
)

// yamlBlocks returns the body of every fenced block in doc that opens with a
// line "```yaml" and closes with a line "```".
func yamlBlocks(doc string) []string {
	var blocks []string
	var body []string
	in := false
	for _, line := range strings.Split(doc, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case !in && trimmed == "```yaml":
			in, body = true, nil
		case in && trimmed == "```":
			blocks = append(blocks, strings.Join(body, "\n"))
			in = false
		case in:
			body = append(body, line)
		}
	}
	return blocks
}

// TestProductionChecklistFoundersCarryMemberIdentities keeps every founding
// example in the production checklist bootstrappable (go-quicbaas#1696): a
// founder that lists the members in join_addresses but not the other
// founders' identities in member_identities is refused bootstrap.
func TestProductionChecklistFoundersCarryMemberIdentities(t *testing.T) {
	path := filepath.Join(repoRoot, "docs", "deployment", "production-checklist.md")
	doc := readFile(t, path)
	founders := 0
	for i, block := range yamlBlocks(doc) {
		if !strings.Contains(block, "join_addresses:") {
			continue
		}
		founders++
		if !strings.Contains(block, "member_identities:") {
			t.Errorf("%s: yaml block %d lists join_addresses but no member_identities:\n%s", path, i+1, block)
		}
		if n := strings.Count(block, "=spki:"); n < 2 {
			t.Errorf("%s: yaml block %d carries %d =spki: identities, want the other two founders:\n%s", path, i+1, n, block)
		}
	}
	if founders < 2 {
		t.Errorf("%s: found %d founding yaml blocks (with join_addresses:), want at least 2", path, founders)
	}
	if !strings.Contains(doc, "openssl pkey -pubin -outform DER") {
		t.Errorf("%s does not say how to derive the spki identities (openssl pkey -pubin -outform DER)", path)
	}
}
