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

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/kmstenant"
)

// kmsTenantRequest is one KMS verb sent to REST with a key name or DEK path.
type kmsTenantRequest struct {
	name   string
	method string
	id     string
	path   func(id string) string
	body   string
}

func kmsTenantRequests() []kmsTenantRequest {
	key := func(suffix string) func(string) string {
		return func(id string) string { return "/api/v1/kms/keys/" + id + suffix }
	}
	dek := func(suffix string) func(string) string {
		return func(id string) string { return "/api/v1/kms/deks/" + id + suffix }
	}
	k, signing, d := kmstenant.KeyName, kmstenant.SigningKeyName, kmstenant.DEKPath
	return []kmsTenantRequest{
		{"GetKey", http.MethodGet, k, key(""), ""},
		{"GetPublicKey", http.MethodGet, signing, key("/public"), ""},
		{"RotateKey", http.MethodPost, k, key("/rotate"), ""},
		{"RevokeKey", http.MethodPost, k, key("/revoke"), ""},
		{"DeleteKey", http.MethodDelete, k, key(""), `{"force":true}`},
		{"SetDefaultKey", http.MethodPut, k, key("/default"), ""},
		{"GetKeyMetadata", http.MethodGet, k, key("/metadata"), ""},
		{"UpdateKeyMetadata", http.MethodPut, k, key("/metadata"), `{"tags":{"env":"prod"}}`},
		{"UpdateKeyStatus", http.MethodPut, k, key("/status"), `{"status":"disabled"}`},
		{"ListKeyVersions", http.MethodGet, k, key("/versions"), ""},
		{"SetKeyPolicy", http.MethodPut, k, key("/policy"), `{"policy":{"allowed_operations":["encrypt"]}}`},
		{"GetKeyPolicy", http.MethodGet, k, key("/policy"), ""},
		{"SetKeyRotationPolicy", http.MethodPut, k, key("/rotation-policy"), `{"enabled":true,"auto_rotate":true,"interval_seconds":3600}`},
		{"GetKeyRotationPolicy", http.MethodGet, k, key("/rotation-policy"), ""},
		{"DeleteKeyRotationPolicy", http.MethodDelete, k, key("/rotation-policy"), ""},
		{"SetKeySafety", http.MethodPut, k, key("/safety"), `{"max_operations":1000}`},
		{"ClearKeySafety", http.MethodDelete, k, key("/safety"), ""},
		{"GetDEK", http.MethodGet, d, dek(""), ""},
		{"RotateDEK", http.MethodPost, d, dek("/rotate"), ""},
		{"DeleteDEK", http.MethodDelete, d, dek(""), `{"force":true}`},
		{"GetDEKStatus", http.MethodGet, d, dek("/status"), ""},
		{"SetDefaultDEK", http.MethodPost, d, dek("/set-default"), ""},
		{"SetDEKSafety", http.MethodPut, d, dek("/safety"), `{"max_operations":1000}`},
		{"ClearDEKSafety", http.MethodDelete, d, dek("/safety"), ""},
	}
}

// kmsTenantRouter mounts the KMS routes of the route table on a router that
// stamps the caller's subject the way the REST auth middleware does.
func kmsTenantRouter(s *Server, tenant string) http.Handler {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := injectPrincipal(req.Context(), interfaces.Subject{ID: "caller", TenantID: tenant}, nil)
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	for _, spec := range Routes() {
		if strings.HasPrefix(spec.Pattern, "/api/v1/kms/") {
			r.Method(spec.Method, spec.Pattern, spec.Handler(s))
		}
	}
	return r
}

// TestKMSHandlers_TenantBGetsNotFoundForTenantAKey pins #1476 on REST: the
// tenant the auth middleware stamps from the caller's subject reaches the
// KMS service, so tenant B naming tenant A's key or DEK gets the same 404 a
// missing id gets and leaves tenant A's state unchanged, while tenant A
// still reaches its own.
func TestKMSHandlers_TenantBGetsNotFoundForTenantAKey(t *testing.T) {
	cases := []struct {
		name      string
		tenant    string
		missingID bool
		want      int
	}{
		{name: "tenant A reaches its own", tenant: kmstenant.OwnerTenant, want: http.StatusOK},
		{name: "tenant B gets not found for tenant A's id", tenant: kmstenant.OtherTenant, want: http.StatusNotFound},
		{name: "tenant A gets not found for a missing id", tenant: kmstenant.OwnerTenant, missingID: true, want: http.StatusNotFound},
	}

	for _, verb := range kmsTenantRequests() {
		for _, tc := range cases {
			t.Run(verb.name+"/"+tc.name, func(t *testing.T) {
				server, mockService, cleanup := setupMockServer(t)
				defer cleanup()
				svc := kmstenant.NewService(t)
				mockService.KMSService = svc
				id := verb.id
				if tc.missingID {
					id = kmstenant.MissingID
				}
				before := kmstenant.ReadOwnerState(t, svc)

				req := httptest.NewRequest(verb.method, verb.path(id), strings.NewReader(verb.body))
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				kmsTenantRouter(server, tc.tenant).ServeHTTP(w, req)

				if tc.want == http.StatusOK {
					require.Less(t, w.Code, http.StatusMultipleChoices, "body: %s", w.Body.String())
					return
				}
				require.Equal(t, tc.want, w.Code, "body: %s", w.Body.String())
				require.Equal(t, before, kmstenant.ReadOwnerState(t, svc), "a refused call changed tenant A's state")
			})
		}
	}
}
