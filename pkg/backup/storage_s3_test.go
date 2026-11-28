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

//go:build awss3

package backup

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNewStorageBackend_S3 proves the awss3 build registers the S3 backup
// target: NewStorageBackend builds an "s3" backend from its config.
func TestNewStorageBackend_S3(t *testing.T) {
	backend, err := NewStorageBackend(&Config{
		StorageBackend: "s3",
		StorageConfig: map[string]string{
			"endpoint":       "http://127.0.0.1:1",
			"bucket":         "backups",
			"region":         "us-east-1",
			"access_key":     "test",
			"secret_key":     "test",
			"use_path_style": "true",
			"disable_ssl":    "true",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, backend)
}
