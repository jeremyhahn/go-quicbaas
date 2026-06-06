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
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestListSecrets_QueryBranches exercises the cursor and prefix query-building
// branches plus the nil-request default and the server-error path.
func TestListSecrets_QueryBranches(t *testing.T) {
	t.Run("nil request defaults to empty filter", func(t *testing.T) {
		var got http.Request
		srv := kmsTestServer(t, http.StatusOK, &service.ListSecretsResponse{}, &got)
		defer srv.Close()
		c := newKMSTestClient(t, srv)

		_, err := c.ListSecrets(context.Background(), nil)
		require.NoError(t, err)
		assert.Contains(t, got.URL.RawQuery, "limit=0")
	})

	t.Run("cursor and prefix are appended to the query", func(t *testing.T) {
		var got http.Request
		srv := kmsTestServer(t, http.StatusOK, &service.ListSecretsResponse{}, &got)
		defer srv.Close()
		c := newKMSTestClient(t, srv)

		_, err := c.ListSecrets(context.Background(), &service.ListSecretsRequest{
			Limit:  25,
			Cursor: "page-2",
			Prefix: "/apps/myapp/",
		})
		require.NoError(t, err)
		assert.Contains(t, got.URL.RawQuery, "limit=25")
		assert.Contains(t, got.URL.RawQuery, "cursor=page-2")
		assert.Contains(t, got.URL.RawQuery, "prefix=")
	})

	t.Run("server error is propagated", func(t *testing.T) {
		srv := kmsTestServer(t, http.StatusInternalServerError, nil, nil)
		defer srv.Close()
		c := newKMSTestClient(t, srv)

		_, err := c.ListSecrets(context.Background(), &service.ListSecretsRequest{Limit: 10})
		assert.Error(t, err)
	})
}

// TestGetSecretsByPrefix_Branches covers nil-request, empty-prefix validation,
// the optional query-parameter branches, and the server-error path.
func TestGetSecretsByPrefix_Branches(t *testing.T) {
	t.Run("nil request", func(t *testing.T) {
		c := newKMSTestClient(t, kmsTestServer(t, http.StatusOK, nil, nil))
		_, err := c.GetSecretsByPrefix(context.Background(), nil)
		assert.Error(t, err)
	})

	t.Run("empty prefix is rejected", func(t *testing.T) {
		c := newKMSTestClient(t, kmsTestServer(t, http.StatusOK, nil, nil))
		_, err := c.GetSecretsByPrefix(context.Background(), &service.GetSecretsByPrefixRequest{})
		assert.Error(t, err)
	})

	t.Run("all optional params are encoded", func(t *testing.T) {
		var got http.Request
		srv := kmsTestServer(t, http.StatusOK, &service.GetSecretsByPrefixResponse{}, &got)
		defer srv.Close()
		c := newKMSTestClient(t, srv)

		_, err := c.GetSecretsByPrefix(context.Background(), &service.GetSecretsByPrefixRequest{
			Prefix:      "/apps/myapp/",
			IncludeData: false,
			Limit:       50,
			Cursor:      "next-cursor",
		})
		require.NoError(t, err)
		assert.True(t, strings.Contains(got.URL.RawQuery, "include_data=false"))
		assert.True(t, strings.Contains(got.URL.RawQuery, "limit=50"))
		assert.True(t, strings.Contains(got.URL.RawQuery, "cursor=next-cursor"))
	})

	t.Run("server error is propagated", func(t *testing.T) {
		srv := kmsTestServer(t, http.StatusInternalServerError, nil, nil)
		defer srv.Close()
		c := newKMSTestClient(t, srv)

		_, err := c.GetSecretsByPrefix(context.Background(), &service.GetSecretsByPrefixRequest{Prefix: "/x/"})
		assert.Error(t, err)
	})
}

// TestWatchSecretsByPrefix_ErrorBranches covers the validation and non-200
// status branches of the SSE watch client without requiring a live stream.
func TestWatchSecretsByPrefix_ErrorBranches(t *testing.T) {
	t.Run("nil request", func(t *testing.T) {
		c := newKMSTestClient(t, kmsTestServer(t, http.StatusOK, nil, nil))
		_, err := c.WatchSecretsByPrefix(context.Background(), nil)
		assert.Error(t, err)
	})

	t.Run("empty prefix is rejected", func(t *testing.T) {
		c := newKMSTestClient(t, kmsTestServer(t, http.StatusOK, nil, nil))
		_, err := c.WatchSecretsByPrefix(context.Background(), &service.WatchSecretsRequest{})
		assert.Error(t, err)
	})

	t.Run("non-200 status returns an HTTP status error", func(t *testing.T) {
		var got http.Request
		srv := kmsTestServer(t, http.StatusForbidden, nil, &got)
		defer srv.Close()
		c := newKMSTestClient(t, srv)

		ch, err := c.WatchSecretsByPrefix(context.Background(), &service.WatchSecretsRequest{
			Prefix:       "/apps/myapp/",
			StartVersion: 7,
		})
		assert.Error(t, err)
		assert.Nil(t, ch)
		assert.Contains(t, got.URL.RawQuery, "start_version=7")
	})
}
