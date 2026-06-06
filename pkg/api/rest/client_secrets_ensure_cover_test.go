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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func TestEnsureSecret(t *testing.T) {
	t.Run("nil request", func(t *testing.T) {
		c := newKMSTestClient(t, kmsTestServer(t, http.StatusOK, nil, nil))
		_, err := c.EnsureSecret(context.Background(), nil)
		assert.Error(t, err)
	})
	t.Run("missing secret id", func(t *testing.T) {
		c := newKMSTestClient(t, kmsTestServer(t, http.StatusOK, nil, nil))
		_, err := c.EnsureSecret(context.Background(), &service.EnsureSecretRequest{})
		assert.Error(t, err)
	})
	t.Run("success", func(t *testing.T) {
		var got http.Request
		srv := kmsTestServer(t, http.StatusOK,
			&service.EnsureSecretResponse{SecretID: "db/pw", Data: []byte("abc")}, &got)
		defer srv.Close()
		c := newKMSTestClient(t, srv)
		resp, err := c.EnsureSecret(context.Background(), &service.EnsureSecretRequest{
			SecretID: "db/pw", Length: 16,
		})
		require.NoError(t, err)
		assert.Equal(t, "db/pw", resp.SecretID)
		assert.Contains(t, got.URL.Path, "/api/v1/secrets/db/pw/ensure")
	})
	t.Run("server error", func(t *testing.T) {
		srv := kmsTestServer(t, http.StatusInternalServerError, nil, nil)
		defer srv.Close()
		c := newKMSTestClient(t, srv)
		_, err := c.EnsureSecret(context.Background(), &service.EnsureSecretRequest{SecretID: "x", Length: 8})
		assert.Error(t, err)
	})
}
