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

//go:build azureblob

package backup

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNewStorageBackend_Azure proves the azureblob build registers the Azure
// Blob backup target: NewStorageBackend builds an "azure" backend from its
// config (the Azurite development account).
func TestNewStorageBackend_Azure(t *testing.T) {
	backend, err := NewStorageBackend(&Config{
		StorageBackend: "azure",
		StorageConfig: map[string]string{
			"endpoint":     "http://127.0.0.1:1/devstoreaccount1",
			"container":    "backups",
			"account_name": "devstoreaccount1",
			"account_key":  "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw==",
			"use_emulator": "true",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, backend)
}
