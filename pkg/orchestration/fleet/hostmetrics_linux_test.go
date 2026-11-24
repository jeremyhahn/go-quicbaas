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

//go:build linux

package fleet

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatDisk_ReportsTheWorkDirFilesystem(t *testing.T) {
	t.Parallel()
	usage, err := statDisk(t.TempDir())
	require.NoError(t, err)
	assert.Positive(t, usage.total)
	assert.LessOrEqual(t, usage.available, usage.total)
}

func TestStatDisk_RefusesAMissingPath(t *testing.T) {
	t.Parallel()
	_, err := statDisk(filepath.Join(t.TempDir(), "missing"))
	assert.Error(t, err)
}
