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
	"bytes"
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

// TestMinIOStorage_PutOverPlainHTTPWithUseSSLFalse proves use_ssl=false is
// honoured for a bare endpoint: the Put reaches a plain-HTTP server.
func TestMinIOStorage_PutOverPlainHTTPWithUseSSLFalse(t *testing.T) {
	var (
		mu   sync.Mutex
		seen []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path)
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

	require.NoError(t, backend.Put("k", bytes.NewReader([]byte("x"))))

	mu.Lock()
	defer mu.Unlock()
	require.Contains(t, seen, "PUT /backups/k")
}

// TestMinIOStorage_BareEndpointWithoutUseSSLStaysHTTPS proves a bare minio
// endpoint without use_ssl is dialled over HTTPS, as the storage library does
// on its own: the plain-HTTP server never serves the object.
func TestMinIOStorage_BareEndpointWithoutUseSSLStaysHTTPS(t *testing.T) {
	var (
		mu   sync.Mutex
		seen []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path)
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
		},
	})
	require.NoError(t, err)

	require.Error(t, backend.Put("k", bytes.NewReader([]byte("x"))))

	mu.Lock()
	defer mu.Unlock()
	require.NotContains(t, seen, "PUT /backups/k")
}
