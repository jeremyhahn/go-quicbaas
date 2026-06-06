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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// SSH handlers return 503 when no SSH client is configured.
func TestSSHHandlers_ServiceUnavailable(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	handlers := map[string]http.HandlerFunc{
		"list_keys":      server.HandleListSSHKeys,
		"export_public":  server.HandleExportSSHKeyPublic,
		"emit_auth":      server.HandleEmitAuthorized,
		"verify_auth":    server.HandleVerifyAuthorized,
		"import_key":     server.HandleImportSSHKey,
		"add_known_host": server.HandleAddKnownHost,
		"list_hosts":     server.HandleListKnownHosts,
	}
	for name, h := range handlers {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/x", strings.NewReader(`{}`))
			w := httptest.NewRecorder()
			h(w, req)
			assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		})
	}
}

// SSH write handlers return 400 on malformed JSON bodies.
func TestSSHHandlers_BadBody(t *testing.T) {
	server, _, cleanup := newSSHTestServer(t)
	defer cleanup()

	handlers := map[string]http.HandlerFunc{
		"import_key":     server.HandleImportSSHKey,
		"add_known_host": server.HandleAddKnownHost,
		"emit_auth":      server.HandleEmitAuthorized,
		"verify_auth":    server.HandleVerifyAuthorized,
	}
	for name, h := range handlers {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/x", strings.NewReader(`{not-json`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
}
