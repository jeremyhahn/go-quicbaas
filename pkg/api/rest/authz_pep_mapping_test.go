//go:build rest

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

package rest

import (
	"net/http"
	"testing"
)

func TestMapRequestToPermission(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		method       string
		wantResource string
		wantAction   string
		wantOK       bool
	}{
		// KV data plane: collection vs item.
		{"keys collection list", "/api/v1/keys", http.MethodGet, "kv", "list", true},
		{"keys item get", "/api/v1/keys/foo", http.MethodGet, "kv", "get", true},
		{"keys put", "/api/v1/keys/foo", http.MethodPut, "kv", "put", true},
		{"keys post", "/api/v1/keys", http.MethodPost, "kv", "put", true},
		{"keys delete", "/api/v1/keys/foo", http.MethodDelete, "kv", "delete", true},
		{"kv alias get", "/api/v1/kv/foo", http.MethodGet, "kv", "get", true},

		// Cryptographic key operations.
		{"kms get", "/api/v1/kms/key1", http.MethodGet, "keys", "get", true},
		{"kms list", "/api/v1/kms", http.MethodGet, "keys", "list", true},
		{"kms create", "/api/v1/kms", http.MethodPost, "keys", "create", true},
		{"kms delete", "/api/v1/kms/key1", http.MethodDelete, "keys", "delete", true},
		{"crypto create", "/api/v1/crypto/sign", http.MethodPost, "keys", "create", true},

		// Certificates.
		{"cert read", "/api/v1/certificate/abc", http.MethodGet, "certs", "read", true},
		{"cert generate", "/api/v1/certificate", http.MethodPost, "certs", "generate", true},
		{"cert revoke", "/api/v1/certificate/abc", http.MethodDelete, "certs", "revoke", true},
		{"certs plural list", "/api/v1/certificates", http.MethodGet, "certs", "list", true},

		// Secrets -> credentials.
		{"secrets retrieve", "/api/v1/secrets/db", http.MethodGet, "credentials", "retrieve", true},
		{"secrets store", "/api/v1/secrets", http.MethodPost, "credentials", "store", true},
		{"secrets list", "/api/v1/secrets", http.MethodGet, "credentials", "list", true},
		{"secrets delete", "/api/v1/secrets/db", http.MethodDelete, "credentials", "delete", true},

		// Tenant barrier.
		{"barrier status", "/api/v1/barrier", http.MethodGet, "tenant_barrier", "status", true},
		{"barrier init", "/api/v1/barrier/init", http.MethodPost, "tenant_barrier", "init", true},
		{"barrier seal", "/api/v1/barrier/seal", http.MethodDelete, "tenant_barrier", "seal", true},

		// IAM management.
		{"users read", "/api/v1/users/u1", http.MethodGet, "users", "read", true},
		{"users list", "/api/v1/users", http.MethodGet, "users", "list", true},
		{"users create", "/api/v1/users", http.MethodPost, "users", "create", true},
		{"users delete", "/api/v1/users/u1", http.MethodDelete, "users", "delete", true},
		{"roles list", "/api/v1/roles", http.MethodGet, "roles", "list", true},
		{"tenants create", "/api/v1/tenants", http.MethodPost, "tenants", "create", true},

		// Data services.
		{"timeseries query", "/api/v1/timeseries", http.MethodGet, "timeseries", "query", true},
		{"timeseries write", "/api/v1/timeseries", http.MethodPost, "timeseries", "write", true},
		{"watch", "/api/v1/watch/key", http.MethodGet, "watch", "watch", true},
		{"transaction execute", "/api/v1/transactions", http.MethodPost, "transaction", "execute", true},
		{"backups create", "/api/v1/backups", http.MethodPost, "backup", "create", true},

		// Trailing slash normalization (collection).
		{"keys trailing slash", "/api/v1/keys/", http.MethodGet, "kv", "list", true},

		// Unmapped surfaces.
		{"unmapped path", "/api/v1/orchestrator/spawn", http.MethodPost, "", "", false},
		{"unknown path", "/api/v1/totally-unknown", http.MethodGet, "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action, ok := mapRequestToPermission(tt.path, tt.method)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if resource != tt.wantResource {
				t.Errorf("resource = %q, want %q", resource, tt.wantResource)
			}
			if action != tt.wantAction {
				t.Errorf("action = %q, want %q", action, tt.wantAction)
			}
		})
	}
}

// TestActionForMethod_DefaultFallsBackToGet asserts an unrecognized method maps
// to the resource's get verb rather than panicking or returning empty.
func TestActionForMethod_DefaultFallsBackToGet(t *testing.T) {
	verbs := crudVerbs{get: "get", list: "list", create: "put", del: "delete"}
	if got := actionForMethod("PROPFIND", verbs, false); got != "get" {
		t.Errorf("actionForMethod(PROPFIND) = %q, want get", got)
	}
}
