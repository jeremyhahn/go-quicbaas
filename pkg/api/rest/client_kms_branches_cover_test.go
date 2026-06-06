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

// TestKMSListDEKs_QueryBranches covers the prefix and recursive query-building
// branches plus the server-error propagation path.
func TestKMSListDEKs_QueryBranches(t *testing.T) {
	t.Run("prefix and recursive are encoded into the query", func(t *testing.T) {
		var got http.Request
		srv := kmsTestServer(t, http.StatusOK, &service.ListDEKsResponse{DEKs: []*service.DEKMetadata{}}, &got)
		defer srv.Close()
		c := newKMSTestClient(t, srv)

		_, err := c.KMSListDEKs(context.Background(), &service.ListDEKsRequest{
			Prefix:    "/apps/",
			Recursive: true,
		})
		require.NoError(t, err)
		assert.True(t, strings.Contains(got.URL.RawQuery, "prefix="))
		assert.True(t, strings.Contains(got.URL.RawQuery, "recursive=true"))
	})

	t.Run("server error is propagated", func(t *testing.T) {
		srv := kmsTestServer(t, http.StatusInternalServerError, nil, nil)
		defer srv.Close()
		c := newKMSTestClient(t, srv)

		_, err := c.KMSListDEKs(context.Background(), &service.ListDEKsRequest{Prefix: "/apps/"})
		assert.Error(t, err)
	})
}

// TestKMSExportKey_ServerError covers the doRequest error branch of KMSExportKey.
func TestKMSExportKey_ServerError(t *testing.T) {
	srv := kmsTestServer(t, http.StatusInternalServerError, nil, nil)
	defer srv.Close()
	c := newKMSTestClient(t, srv)

	_, err := c.KMSExportKey(context.Background(), &service.ExportKeyRequest{KeyID: "k1"})
	assert.Error(t, err)
}

// TestKMSSetKeySafety_ServerError covers the doRequest error branch of
// KMSSetKeySafety.
func TestKMSSetKeySafety_ServerError(t *testing.T) {
	srv := kmsTestServer(t, http.StatusInternalServerError, nil, nil)
	defer srv.Close()
	c := newKMSTestClient(t, srv)

	_, err := c.KMSSetKeySafety(context.Background(), &service.SetKeySafetyRequest{KeyID: "k1", MaxOperations: 10})
	assert.Error(t, err)
}
