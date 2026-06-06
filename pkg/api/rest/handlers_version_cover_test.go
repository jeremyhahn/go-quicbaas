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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHandleVersion_ReturnsBuildInfo verifies that the open version endpoint
// responds 200 with a JSON body that always carries the daemon version string,
// without requiring authentication.
func TestHandleVersion_ReturnsBuildInfo(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/version", nil)
	rec := httptest.NewRecorder()

	server.HandleVersion(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("HandleVersion status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp VersionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode version response: %v", err)
	}

	if resp.Version == "" {
		t.Error("VersionResponse.Version is empty; expected the build version string")
	}
}
