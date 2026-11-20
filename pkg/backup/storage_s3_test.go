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
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

// TestNewStorageBackend_S3SessionTokenSignsEveryRequest configures temporary
// credentials (access key, secret key and session token) and drives a put,
// a get, an exists check, a list and a delete against a fake S3: every
// request must carry the session token.
func TestNewStorageBackend_S3SessionTokenSignsEveryRequest(t *testing.T) {
	var mu sync.Mutex
	tokens := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		mu.Lock()
		tokens[r.Method] = r.Header.Get("X-Amz-Security-Token")
		mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2":
			w.Header().Set("Content-Type", "application/xml")
			_, _ = io.WriteString(w, `<?xml version="1.0" encoding="UTF-8"?>`+
				`<ListBucketResult><Name>backups</Name><IsTruncated>false</IsTruncated>`+
				`<Contents><Key>snap/1</Key><Size>7</Size></Contents></ListBucketResult>`)
		case r.Method == http.MethodGet:
			_, _ = io.WriteString(w, "payload")
		case r.Method == http.MethodHead:
			w.Header().Set("Content-Length", "7")
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	backend, err := NewStorageBackend(&Config{
		StorageBackend: "s3",
		StorageConfig: map[string]string{
			"endpoint":       srv.URL,
			"bucket":         "backups",
			"region":         "us-east-1",
			"access_key":     "ASIATEMP",
			"secret_key":     "secret",
			"session_token":  "temp-session-token",
			"use_path_style": "true",
		},
	})
	require.NoError(t, err)

	require.NoError(t, backend.Put("snap/1", strings.NewReader("payload")))
	rc, err := backend.Get("snap/1")
	require.NoError(t, err)
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	require.Equal(t, "payload", string(got))
	ok, err := backend.Exists(context.Background(), "snap/1")
	require.NoError(t, err)
	require.True(t, ok)
	keys, err := backend.List("snap/")
	require.NoError(t, err)
	require.Equal(t, []string{"snap/1"}, keys)
	require.NoError(t, backend.Delete("snap/1"))

	mu.Lock()
	defer mu.Unlock()
	for _, m := range []string{http.MethodPut, http.MethodGet, http.MethodHead, http.MethodDelete} {
		require.Equal(t, "temp-session-token", tokens[m], "%s request", m)
	}
}

// TestNewStorageBackend_S3SessionTokenNeedsKeys refuses a session token
// without the access key it belongs to.
func TestNewStorageBackend_S3SessionTokenNeedsKeys(t *testing.T) {
	_, err := NewStorageBackend(&Config{
		StorageBackend: "s3",
		StorageConfig: map[string]string{
			"bucket":        "backups",
			"region":        "us-east-1",
			"session_token": "temp-session-token",
		},
	})
	require.Error(t, err)
}
