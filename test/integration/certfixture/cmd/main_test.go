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
)

// declaration is a two-identity topology written to a temp file, so the command
// is exercised against a real declaration on disk rather than the committed one.
func declaration(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "topology.json")
	body := `{
  "spiffe_trust_domain": "cmd.certfixture.test",
  "nodes": [
    {"file": "node1", "common_name": "test-node1", "dns_names": ["localhost"], "ip_addresses": ["127.0.0.1"]}
  ],
  "clients": [
    {"file": "admin", "common_name": "admin@cmd.test", "email": "admin@cmd.test", "tenant_id": "system"}
  ]
}
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// The command writes the declared set and reports every file it stands behind.
func TestRun_WritesTheDeclaredSet(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "certs")
	var stdout, stderr strings.Builder

	if code := run([]string{"-topology", declaration(t), dir}, &stdout, &stderr); code != exitOK {
		t.Fatalf("exit code = %d, want %d (stderr %q)", code, exitOK, stderr.String())
	}
	for _, stem := range []string{"ca.crt", "node1.crt", "node1.key", "admin.crt", "admin.key"} {
		path := filepath.Join(dir, stem)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s was not written: %v", stem, err)
		}
		if !strings.Contains(stdout.String(), path) {
			t.Errorf("stdout does not name %s: %q", path, stdout.String())
		}
	}
	if !strings.Contains(stdout.String(), "1 node(s), 1 client(s)") {
		t.Errorf("stdout does not summarise the set: %q", stdout.String())
	}
}

// A second invocation over a complete set leaves the trust anchor alone. Compose
// runs this service twice, and a second root under the same subject name would
// make every already-issued leaf unverifiable.
func TestRun_IsANoOpOverACompleteSet(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "certs")
	topology := declaration(t)
	var stdout, stderr strings.Builder

	if code := run([]string{"-topology", topology, dir}, &stdout, &stderr); code != exitOK {
		t.Fatalf("first run: exit %d (stderr %q)", code, stderr.String())
	}
	before, err := os.ReadFile(filepath.Join(dir, "ca.crt"))
	if err != nil {
		t.Fatalf("read ca.crt: %v", err)
	}

	if code := run([]string{"-topology", topology, dir}, &stdout, &stderr); code != exitOK {
		t.Fatalf("second run: exit %d (stderr %q)", code, stderr.String())
	}
	after, err := os.ReadFile(filepath.Join(dir, "ca.crt"))
	if err != nil {
		t.Fatalf("re-read ca.crt: %v", err)
	}
	if string(before) != string(after) {
		t.Fatal("the second run replaced the trust anchor")
	}
}

// -force reissues over a complete set, which is how an operator rotates the
// fixture material deliberately rather than by deleting files.
func TestRun_ForceReissues(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "certs")
	topology := declaration(t)
	var stdout, stderr strings.Builder

	if code := run([]string{"-topology", topology, dir}, &stdout, &stderr); code != exitOK {
		t.Fatalf("first run: exit %d (stderr %q)", code, stderr.String())
	}
	before, err := os.ReadFile(filepath.Join(dir, "node1.crt"))
	if err != nil {
		t.Fatalf("read node1.crt: %v", err)
	}

	if code := run([]string{"-topology", topology, "-force", dir}, &stdout, &stderr); code != exitOK {
		t.Fatalf("forced run: exit %d (stderr %q)", code, stderr.String())
	}
	after, err := os.ReadFile(filepath.Join(dir, "node1.crt"))
	if err != nil {
		t.Fatalf("re-read node1.crt: %v", err)
	}
	if string(before) == string(after) {
		t.Fatal("-force did not reissue the serving leaf")
	}
}

// A missing declaration fails and says which path it could not read, rather than
// writing a partial set.
func TestRun_ReportsAMissingDeclaration(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "certs")
	absent := filepath.Join(t.TempDir(), "absent.json")
	var stdout, stderr strings.Builder

	if code := run([]string{"-topology", absent, dir}, &stdout, &stderr); code != exitFailure {
		t.Fatalf("exit code = %d, want %d", code, exitFailure)
	}
	if !strings.Contains(stderr.String(), absent) {
		t.Errorf("stderr does not name the declaration: %q", stderr.String())
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("a failed run created the output directory")
	}
}

// An unknown flag and a second positional argument are usage failures, distinct
// from a generation failure, so a harness can tell the two apart.
func TestRun_RejectsBadInvocations(t *testing.T) {
	var stdout, stderr strings.Builder
	if code := run([]string{"-nope"}, &stdout, &stderr); code != exitUsage {
		t.Errorf("unknown flag: exit %d, want %d", code, exitUsage)
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"one", "two"}, &stdout, &stderr); code != exitUsage {
		t.Errorf("two directories: exit %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "at most one output directory") {
		t.Errorf("stderr does not explain the usage failure: %q", stderr.String())
	}
}

// A valid declaration whose output directory cannot be written fails, on both
// the default and the -force path, and says so rather than reporting success
// over a set that was never written.
func TestRun_ReportsAGenerationFailure(t *testing.T) {
	topology := declaration(t)
	for _, args := range [][]string{
		{"-topology", topology},
		{"-topology", topology, "-force"},
	} {
		blocker := filepath.Join(t.TempDir(), "not-a-directory")
		if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
			t.Fatalf("write %s: %v", blocker, err)
		}
		dir := filepath.Join(blocker, "certs")

		var stdout, stderr strings.Builder
		if code := run(append(args, dir), &stdout, &stderr); code != exitFailure {
			t.Fatalf("%v: exit %d, want %d (stdout %q)", args, code, exitFailure, stdout.String())
		}
		if !strings.Contains(stderr.String(), "certfixture:") {
			t.Errorf("%v: stderr does not report the failure: %q", args, stderr.String())
		}
		if stdout.String() != "" {
			t.Errorf("%v: a failed run wrote a summary: %q", args, stdout.String())
		}
	}
}
