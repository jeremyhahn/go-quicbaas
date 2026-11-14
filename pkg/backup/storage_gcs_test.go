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

//go:build gcpstorage

package backup

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNewStorageBackend_GCS proves the gcpstorage build registers the GCS
// backup target: NewStorageBackend builds a "gcs" backend from its config.
func TestNewStorageBackend_GCS(t *testing.T) {
	// The GCS client skips Application Default Credentials for an emulator.
	t.Setenv("STORAGE_EMULATOR_HOST", "127.0.0.1:1")
	backend, err := NewStorageBackend(&Config{
		StorageBackend: "gcs",
		StorageConfig: map[string]string{
			"endpoint":     "http://127.0.0.1:1/storage/v1/",
			"bucket":       "backups",
			"project_id":   "test-project",
			"use_emulator": "true",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, backend)
}
