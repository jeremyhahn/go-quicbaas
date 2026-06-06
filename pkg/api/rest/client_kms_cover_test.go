//go:build rest

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

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// kmsTestServer returns an httptest server that records the last request and
// replies with the supplied JSON body and status.
func kmsTestServer(t *testing.T, status int, body interface{}, capture *http.Request) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			*capture = *r
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if body != nil {
			_ = json.NewEncoder(w).Encode(body)
		}
	}))
}

func newKMSTestClient(t *testing.T, srv *httptest.Server) *RESTClient {
	t.Helper()
	client, err := NewRESTClient(srv.URL, testLogger(t))
	require.NoError(t, err)
	return client
}

func TestKMSExportKey(t *testing.T) {
	t.Run("nil request", func(t *testing.T) {
		c := newKMSTestClient(t, kmsTestServer(t, http.StatusOK, nil, nil))
		_, err := c.KMSExportKey(context.Background(), nil)
		assert.Error(t, err)
	})
	t.Run("missing key id", func(t *testing.T) {
		c := newKMSTestClient(t, kmsTestServer(t, http.StatusOK, nil, nil))
		_, err := c.KMSExportKey(context.Background(), &service.ExportKeyRequest{})
		assert.Error(t, err)
	})
	t.Run("success", func(t *testing.T) {
		var got http.Request
		srv := kmsTestServer(t, http.StatusOK, &service.ExportKeyResponse{KeyID: "k1", Version: 2}, &got)
		defer srv.Close()
		c := newKMSTestClient(t, srv)
		resp, err := c.KMSExportKey(context.Background(), &service.ExportKeyRequest{KeyID: "k1", Version: 2})
		require.NoError(t, err)
		assert.Equal(t, "k1", resp.KeyID)
		assert.True(t, strings.Contains(got.URL.Path, "/api/v1/kms/keys/k1/export"))
	})
}

func TestKMSUnwrapKey(t *testing.T) {
	t.Run("nil request", func(t *testing.T) {
		c := newKMSTestClient(t, kmsTestServer(t, http.StatusOK, nil, nil))
		_, err := c.KMSUnwrapKey(context.Background(), nil)
		assert.Error(t, err)
	})
	t.Run("success", func(t *testing.T) {
		srv := kmsTestServer(t, http.StatusOK, &service.UnwrapKeyResponse{KeyID: "new", Version: 1}, nil)
		defer srv.Close()
		c := newKMSTestClient(t, srv)
		resp, err := c.KMSUnwrapKey(context.Background(), &service.UnwrapKeyRequest{
			KeyID: "new", WrappedKey: []byte("wrapped"),
		})
		require.NoError(t, err)
		assert.Equal(t, "new", resp.KeyID)
	})
	t.Run("server error", func(t *testing.T) {
		srv := kmsTestServer(t, http.StatusInternalServerError, nil, nil)
		defer srv.Close()
		c := newKMSTestClient(t, srv)
		_, err := c.KMSUnwrapKey(context.Background(), &service.UnwrapKeyRequest{KeyID: "x"})
		assert.Error(t, err)
	})
}

func TestKMSSetKeySafety(t *testing.T) {
	t.Run("nil request", func(t *testing.T) {
		c := newKMSTestClient(t, kmsTestServer(t, http.StatusOK, nil, nil))
		_, err := c.KMSSetKeySafety(context.Background(), nil)
		assert.Error(t, err)
	})
	t.Run("missing key id", func(t *testing.T) {
		c := newKMSTestClient(t, kmsTestServer(t, http.StatusOK, nil, nil))
		_, err := c.KMSSetKeySafety(context.Background(), &service.SetKeySafetyRequest{})
		assert.Error(t, err)
	})
	t.Run("success", func(t *testing.T) {
		srv := kmsTestServer(t, http.StatusOK, &service.SetKeySafetyResponse{KeyID: "k1"}, nil)
		defer srv.Close()
		c := newKMSTestClient(t, srv)
		resp, err := c.KMSSetKeySafety(context.Background(), &service.SetKeySafetyRequest{
			KeyID: "k1", MaxOperations: 1000,
		})
		require.NoError(t, err)
		assert.Equal(t, "k1", resp.KeyID)
	})
}

func TestKMSSafetyCheck(t *testing.T) {
	var got http.Request
	srv := kmsTestServer(t, http.StatusOK, &service.SafetyCheckResponse{ThresholdPercent: 80}, &got)
	defer srv.Close()
	c := newKMSTestClient(t, srv)

	resp, err := c.KMSSafetyCheck(context.Background(), &service.SafetyCheckRequest{ThresholdPercent: 80})
	require.NoError(t, err)
	assert.Equal(t, float64(80), resp.ThresholdPercent)
	assert.Contains(t, got.URL.RawQuery, "threshold_percent")

	// nil request is tolerated (defaults applied).
	resp, err = c.KMSSafetyCheck(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestKMSSafetyReport(t *testing.T) {
	var got http.Request
	srv := kmsTestServer(t, http.StatusOK, &service.SafetyReportResponse{TotalOperations: 5}, &got)
	defer srv.Close()
	c := newKMSTestClient(t, srv)

	resp, err := c.KMSSafetyReport(context.Background(), &service.SafetyReportRequest{IncludeDetails: true})
	require.NoError(t, err)
	assert.Equal(t, uint64(5), resp.TotalOperations)
	assert.Contains(t, got.URL.RawQuery, "include_details")

	// nil request is tolerated.
	_, err = c.KMSSafetyReport(context.Background(), nil)
	require.NoError(t, err)
}

func TestKMSDeleteDEK(t *testing.T) {
	t.Run("nil request", func(t *testing.T) {
		c := newKMSTestClient(t, kmsTestServer(t, http.StatusOK, nil, nil))
		_, err := c.KMSDeleteDEK(context.Background(), nil)
		assert.Error(t, err)
	})
	t.Run("success with force and migrate", func(t *testing.T) {
		var got http.Request
		srv := kmsTestServer(t, http.StatusOK, &service.DeleteDEKResponse{Path: "p", Deleted: true}, &got)
		defer srv.Close()
		c := newKMSTestClient(t, srv)
		resp, err := c.KMSDeleteDEK(context.Background(), &service.DeleteDEKRequest{
			Path: "p", Force: true, MigrateTo: "q",
		})
		require.NoError(t, err)
		assert.True(t, resp.Deleted)
		assert.Contains(t, got.URL.RawQuery, "force=true")
		assert.Contains(t, got.URL.RawQuery, "migrate_to=q")
	})
}
