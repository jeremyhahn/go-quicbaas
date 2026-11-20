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

//go:build minio

package backup

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNewStorageBackend_MinIO proves the minio build registers the MinIO
// backup target: NewStorageBackend builds a "minio" backend from its config.
func TestNewStorageBackend_MinIO(t *testing.T) {
	backend, err := NewStorageBackend(&Config{
		StorageBackend: "minio",
		StorageConfig: map[string]string{
			"endpoint":   "127.0.0.1:1",
			"bucket":     "backups",
			"access_key": "minioadmin",
			"secret_key": "minioadmin",
			"use_ssl":    "false",
		},
	})
	require.NoError(t, err)
	require.NotNil(t, backend)
}

// TestNewStorageBackend_MinIOUseSSLFalseUploadsOverHTTP drives an upload
// through a bare host:port endpoint with use_ssl=false against a plain-HTTP
// server: the request reaches it only when the backend talks HTTP.
func TestNewStorageBackend_MinIOUseSSLFalseUploadsOverHTTP(t *testing.T) {
	var mu sync.Mutex
	var puts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		puts = append(puts, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	backend, err := NewStorageBackend(&Config{
		StorageBackend: "minio",
		StorageConfig: map[string]string{
			"endpoint":   strings.TrimPrefix(srv.URL, "http://"),
			"bucket":     "backups",
			"access_key": "minioadmin",
			"secret_key": "minioadmin",
			"use_ssl":    "false",
		},
	})
	require.NoError(t, err)
	require.NoError(t, backend.Put("snap/1", strings.NewReader("payload")))

	mu.Lock()
	defer mu.Unlock()
	require.Contains(t, puts, "PUT /backups/snap/1")
}
