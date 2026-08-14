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

package orchestration

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMaterializeAccountFiles_EmptyReturnsNoDir(t *testing.T) {
	dir, cleanup, err := materializeAccountFiles(nil)
	if err != nil {
		t.Fatalf("materializeAccountFiles(nil) error = %v", err)
	}
	if dir != "" || cleanup != nil {
		t.Fatalf("materializeAccountFiles(nil) = (%q, cleanup non-nil=%v), want (\"\", nil)", dir, cleanup != nil)
	}
}

func TestMaterializeAccountFiles_WritesNestedFile(t *testing.T) {
	dir, cleanup, err := materializeAccountFiles(map[string][]byte{
		".config/opencode/auth.json": []byte(`{"anthropic":{"apiKey":"x"}}`),
	})
	if err != nil {
		t.Fatalf("materializeAccountFiles() error = %v", err)
	}
	defer cleanup()

	content, readErr := os.ReadFile(filepath.Join(dir, ".config/opencode/auth.json"))
	if readErr != nil {
		t.Fatalf("ReadFile() error = %v", readErr)
	}
	if string(content) != `{"anthropic":{"apiKey":"x"}}` {
		t.Fatalf("content = %s, want the seeded auth.json body", content)
	}
}

// TestMaterializeAccountFiles_TwoSessionsAreIsolated proves two concurrent
// materializations never share a directory, so one identity's credential
// file can never be visible to a session that resolved a different account.
func TestMaterializeAccountFiles_TwoSessionsAreIsolated(t *testing.T) {
	dirA, cleanupA, err := materializeAccountFiles(map[string][]byte{"auth.json": []byte("account-a-secret")})
	if err != nil {
		t.Fatalf("materializeAccountFiles(A) error = %v", err)
	}
	defer cleanupA()

	dirB, cleanupB, err := materializeAccountFiles(map[string][]byte{"auth.json": []byte("account-b-secret")})
	if err != nil {
		t.Fatalf("materializeAccountFiles(B) error = %v", err)
	}
	defer cleanupB()

	if dirA == dirB {
		t.Fatalf("two materializations produced the same directory: %s", dirA)
	}

	contentA, _ := os.ReadFile(filepath.Join(dirA, "auth.json"))
	contentB, _ := os.ReadFile(filepath.Join(dirB, "auth.json"))
	if string(contentA) == string(contentB) {
		t.Fatalf("both directories contain the same content — isolation broken")
	}
	if string(contentA) != "account-a-secret" || string(contentB) != "account-b-secret" {
		t.Fatalf("contentA=%s contentB=%s, want each session to see only its own credential", contentA, contentB)
	}
}

func TestMaterializeAccountFiles_CleanupRemovesDirectory(t *testing.T) {
	dir, cleanup, err := materializeAccountFiles(map[string][]byte{"x": []byte("y")})
	if err != nil {
		t.Fatalf("materializeAccountFiles() error = %v", err)
	}
	cleanup()
	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Fatalf("directory %s still exists after cleanup()", dir)
	}
}

func TestMaterializeAccountFiles_RejectsPathEscape(t *testing.T) {
	_, _, err := materializeAccountFiles(map[string][]byte{"../../etc/passwd": []byte("x")})
	if err == nil {
		t.Fatalf("materializeAccountFiles() with a path-escaping key succeeded, want an error")
	}
}
