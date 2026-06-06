//go:build !integration

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

// lifecycle_helpers_test.go covers the package-private helpers in
// lifecycle.go that are not exercised by integration tests because they
// gate on filesystem-level conditions rather than running subsystems.

package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileExists_BranchCoverage(t *testing.T) {
	t.Run("returns false for nonexistent path", func(t *testing.T) {
		assert.False(t, fileExists(filepath.Join(t.TempDir(), "no-such-file")))
	})

	t.Run("returns true for existing regular file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "x")
		require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
		assert.True(t, fileExists(path))
	})

	t.Run("returns false for directories", func(t *testing.T) {
		// Directories must not satisfy "file exists" so callers checking
		// for cert/key paths don't accidentally treat a directory as a
		// readable certificate.
		assert.False(t, fileExists(t.TempDir()))
	})
}
