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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestGetOAuthASMetadata tests the GetOAuthASMetadata handler.
func TestGetOAuthASMetadata(t *testing.T) {
	t.Run("OIDC provider not available", func(t *testing.T) {
		handler, _, cleanup := setupIAMHandler(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
		w := httptest.NewRecorder()

		handler.GetOAuthASMetadata(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "OIDC provider not available")
	})
}
