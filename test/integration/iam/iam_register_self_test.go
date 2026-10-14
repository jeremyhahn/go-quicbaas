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

//go:build integration
// +build integration

package iam_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSelfRegistration_DisabledByDefault is the "off" leg of the #358
// registration matrix. The integration daemon leaves
// iam.self_service_registration.enabled at its default, false, so the
// anonymous registration ceremony must be refused at both steps with the
// typed self_registration_disabled code. The other half of the off leg, the
// system admin provisioning a tenant and its first admin, runs against this
// same daemon in tenant_provision_test.go.
func TestSelfRegistration_DisabledByDefault(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	for _, path := range []string{"/api/v1/auth/register/begin", "/api/v1/auth/register/finish"} {
		t.Run(path, func(t *testing.T) {
			status, body := doAnonymousPost(t, cluster, path, map[string]interface{}{
				"tenant_id": "selfreg-off",
				"username":  "founder",
				"email":     "founder@selfreg-off.example",
			})
			assert.Equal(t, http.StatusForbidden, status, "%s: %s", path, body)

			var refusal struct {
				Error string `json:"error"`
			}
			require.NoError(t, json.Unmarshal(body, &refusal), "%s: decode refusal %s", path, body)
			assert.Equal(t, "self_registration_disabled", refusal.Error, "%s: %s", path, body)
		})
	}
}

// doAnonymousPost sends body to path with no credential, as a prospective
// self-registrant would, and returns the status and response body.
func doAnonymousPost(t *testing.T, cluster *testCluster, path string, body interface{}) (int, []byte) {
	t.Helper()
	data, err := json.Marshal(body)
	require.NoError(t, err, "marshal request body")

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cluster.httpEndpoint+path, bytes.NewReader(data))
	require.NoError(t, err, "build POST %s", path)
	req.Header.Set("Content-Type", "application/json")

	resp, err := cluster.client.Do(req)
	require.NoError(t, err, "POST %s", path)
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "read POST %s response", path)
	return resp.StatusCode, respBody
}
