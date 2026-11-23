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
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// noSyncVar is the qrdb opt-in that skips fsync on every write
// (pkg/server/qrdb_node.go). It stays for local benchmarks, but an
// integration compose file that sets it makes the SIGKILL durability tests
// (#2516) pass without proving anything.
const noSyncVar = "QRDB_NOSYNC_WRITES"

// noSyncLines returns "file:line: text" for every non-comment line of a
// compose file that sets QRDB_NOSYNC_WRITES.
func noSyncLines(name, content string) []string {
	var found []string
	for i, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || !strings.Contains(trimmed, noSyncVar) {
			continue
		}
		found = append(found, fmt.Sprintf("%s:%d: %s", name, i+1, trimmed))
	}
	return found
}

// integrationComposeFiles lists every docker-compose*.yml under
// test/integration (recursively) and at the repository root.
func integrationComposeFiles(t *testing.T) []string {
	t.Helper()
	isCompose := func(name string) bool {
		return strings.HasPrefix(name, "docker-compose") &&
			(strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml"))
	}
	var files []string
	root := filepath.Join(repoRoot, "test", "integration")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && isCompose(d.Name()) {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	entries, err := os.ReadDir(repoRoot)
	if err != nil {
		t.Fatalf("read %s: %v", repoRoot, err)
	}
	for _, e := range entries {
		if !e.IsDir() && isCompose(e.Name()) {
			files = append(files, filepath.Join(repoRoot, e.Name()))
		}
	}
	return files
}

// TestIntegrationComposeFilesKeepFsync fails when any integration compose
// file turns qrdb fsync off again (#2516).
func TestIntegrationComposeFilesKeepFsync(t *testing.T) {
	files := integrationComposeFiles(t)
	if len(files) == 0 {
		t.Fatal("found no docker-compose*.yml under test/integration or the repository root")
	}
	for _, path := range files {
		for _, hit := range noSyncLines(path, readFile(t, path)) {
			t.Errorf("compose file turns fsync off (%s): %s", noSyncVar, hit)
		}
	}
}

// TestNoSyncLinesFindsTheOptIn proves the gate detects the variable in both
// compose environment forms and ignores it in a comment.
func TestNoSyncLinesFindsTheOptIn(t *testing.T) {
	fixture := strings.Join([]string{
		"services:",
		"  node1:",
		"    environment:",
		"      - QRDB_NOSYNC_WRITES=1",
		"      # QRDB_NOSYNC_WRITES=1 was removed for #2516",
		"  node2:",
		"    environment:",
		"      QRDB_NOSYNC_WRITES: \"true\"",
	}, "\n")
	got := noSyncLines("fixture.yml", fixture)
	want := []string{
		"fixture.yml:4: - QRDB_NOSYNC_WRITES=1",
		"fixture.yml:8: QRDB_NOSYNC_WRITES: \"true\"",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("noSyncLines = %q, want %q", got, want)
	}
	if hits := noSyncLines("clean.yml", "services:\n  node1:\n    image: x\n"); len(hits) != 0 {
		t.Fatalf("noSyncLines on a clean file = %q, want none", hits)
	}
}
