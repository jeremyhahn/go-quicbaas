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

//go:build linux

package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/fleet"
)

// serveWorkDirCommand parses a fleet serve command line that passes Validate
// and names workDir.
func serveWorkDirCommand(t *testing.T, workDir string) *cobra.Command {
	t.Helper()
	return parsedServeCommand(t,
		"--server", "https://127.0.0.1:1",
		"--tenant", "t1",
		"--work-dir", workDir,
		"--node-cert", "node.crt",
		"--node-key", "node.key",
		"--server-ca", "ca.crt",
		"--container-runtime", "none",
	)
}

func TestRunFleetServe_RefusesAHeldWorkDir(t *testing.T) {
	workDir := t.TempDir()
	held, err := fleet.LockWorkDir(workDir)
	if err != nil {
		t.Fatalf("LockWorkDir: %v", err)
	}
	t.Cleanup(func() {
		if err := held.Release(); err != nil {
			t.Errorf("Release: %v", err)
		}
	})

	err = runFleetServe(serveWorkDirCommand(t, workDir), nil)
	var locked *fleet.WorkDirLockedError
	if !errors.As(err, &locked) {
		t.Fatalf("err = %v; want *fleet.WorkDirLockedError", err)
	}
	if locked.Dir != workDir {
		t.Fatalf("locked dir = %q; want %q", locked.Dir, workDir)
	}
}

func TestRunFleetServe_RefusesAWorkDirItCannotCreate(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cmd := serveWorkDirCommand(t, filepath.Join(file, "work"))
	var opErr *fleet.OpError
	if err := runFleetServe(cmd, nil); !errors.As(err, &opErr) {
		t.Fatalf("err = %v; want *fleet.OpError", err)
	}
}
