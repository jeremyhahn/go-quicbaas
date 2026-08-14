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

package integration

import (
	"bytes"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaultDenyRejectsAnonymous proves the server enforces the default-deny
// scope -> RBAC gate (QUICBAAS_SECURITY_REQUIRE_AUTHZ=true): a request to a
// protected data-plane path WITHOUT an Authorization header is rejected with 401
// (no token) or 403 (no scope), never served. Public paths (health) remain open,
// and the same path WITH the bootstrap token succeeds — proving authed clients
// still work. Together these are the E2E proof that enforcement is on AND that
// the suite's authentication is what unlocks the data plane.
func TestDefaultDenyRejectsAnonymous(t *testing.T) {
	SkipIfNoDocker(t)
	SkipIfNoServer(t)

	httpClient := newHTTPClient(10 * time.Second)

	t.Run("anonymous request to protected entities path is denied", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		entityType := "default_deny_probe"
		id := nextID()
		url := GetRESTAddress() + "/api/v1/entities/" + entityType + "/" +
			uintToStr(id)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		require.NoError(t, err)
		// Intentionally NO Authorization header.

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Contains(t, []int{http.StatusUnauthorized, http.StatusForbidden},
			resp.StatusCode,
			"protected path must reject anonymous request under default-deny, got %d",
			resp.StatusCode)
	})

	t.Run("anonymous write to protected entities path is denied", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		entityType := "default_deny_probe"
		id := nextID()
		url := GetRESTAddress() + "/api/v1/entities/" + entityType + "/" +
			uintToStr(id)

		req, err := http.NewRequestWithContext(ctx, http.MethodPut, url,
			bytes.NewReader([]byte(`{"data":{"v":1}}`)))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		// Intentionally NO Authorization header.

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Contains(t, []int{http.StatusUnauthorized, http.StatusForbidden},
			resp.StatusCode,
			"protected write must reject anonymous request under default-deny, got %d",
			resp.StatusCode)
	})

	t.Run("public health path remains open without a token", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet,
			GetRESTAddress()+"/api/v1/health/live", nil)
		require.NoError(t, err)

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode,
			"public health path must remain open under default-deny")
	})

	t.Run("authenticated write+read to protected path succeeds", func(t *testing.T) {
		ctx, cancel := DefaultContext(t)
		defer cancel()

		require.NotEmpty(t, bootstrapToken,
			"suite must have acquired a platform token for authed access")

		entityType := "default_deny_authed"
		id := nextID()
		data := map[string]interface{}{"value": "authed", "n": 7}

		// entityPut / entityGet attach the bootstrap token via authorize().
		putResp, err := entityPut(ctx, entityType, id, data)
		require.NoError(t, err, "authed PUT must succeed under default-deny")
		require.NotNil(t, putResp)

		getResp, err := entityGet(ctx, entityType, id)
		require.NoError(t, err, "authed GET must succeed under default-deny")
		require.NotNil(t, getResp)
		assert.Equal(t, id, getResp.ID)
	})
}

// uintToStr renders a uint64 id for URL construction.
func uintToStr(v uint64) string {
	return strconv.FormatUint(v, 10)
}
