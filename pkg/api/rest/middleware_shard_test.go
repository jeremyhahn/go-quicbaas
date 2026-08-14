//go:build rest

// Copyright 2026 Jeremy Hahn
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func testShardLogger() *logger.Logger {
	l, _ := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	return l
}

// okHandler is a simple handler that writes a 200 response.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func TestPlatformShardGuardMiddleware_NoHeader_Allows(t *testing.T) {
	middleware := PlatformShardGuardMiddleware(testShardLogger())
	handler := middleware(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 when no shard header, got: %d", rec.Code)
	}
}

func TestPlatformShardGuardMiddleware_TenantShard_Allows(t *testing.T) {
	middleware := PlatformShardGuardMiddleware(testShardLogger())
	handler := middleware(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys", nil)
	req.Header.Set(ShardIDHeader, "100")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for tenant shard, got: %d", rec.Code)
	}
}

func TestPlatformShardGuardMiddleware_PlatformShard_AdminAllowed(t *testing.T) {
	middleware := PlatformShardGuardMiddleware(testShardLogger())
	handler := middleware(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys", nil)
	req.Header.Set(ShardIDHeader, "1")

	// Set admin role in context (as the authz middleware would).
	ctx := context.WithValue(req.Context(), UserRolesKey, []string{qrdbsdk.RoleNameAdmin})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin on platform shard, got: %d", rec.Code)
	}
}

func TestPlatformShardGuardMiddleware_PlatformShard_NonAdminDenied(t *testing.T) {
	middleware := PlatformShardGuardMiddleware(testShardLogger())
	handler := middleware(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys", nil)
	req.Header.Set(ShardIDHeader, "1")

	// Set non-admin role in context.
	ctx := context.WithValue(req.Context(), UserRolesKey, []string{"readwrite"})
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin on platform shard, got: %d", rec.Code)
	}
}

func TestPlatformShardGuardMiddleware_PlatformShard_NoRoles_Denied(t *testing.T) {
	middleware := PlatformShardGuardMiddleware(testShardLogger())
	handler := middleware(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys", nil)
	req.Header.Set(ShardIDHeader, "1")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for platform shard with no roles, got: %d", rec.Code)
	}
}

func TestPlatformShardGuardMiddleware_InvalidShardID_Allows(t *testing.T) {
	middleware := PlatformShardGuardMiddleware(testShardLogger())
	handler := middleware(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys", nil)
	req.Header.Set(ShardIDHeader, "not-a-number")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for invalid shard ID header, got: %d", rec.Code)
	}
}

func TestExtractShardIDFromHeader_Valid(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(ShardIDHeader, "42")

	id, ok := extractShardIDFromHeader(req)
	if !ok {
		t.Fatal("expected ok=true for valid shard ID header")
	}
	if id != 42 {
		t.Fatalf("expected shard ID 42, got: %d", id)
	}
}

func TestExtractShardIDFromHeader_Missing(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	_, ok := extractShardIDFromHeader(req)
	if ok {
		t.Fatal("expected ok=false for missing shard ID header")
	}
}

func TestExtractShardIDFromHeader_Invalid(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(ShardIDHeader, "xyz")

	_, ok := extractShardIDFromHeader(req)
	if ok {
		t.Fatal("expected ok=false for invalid shard ID header value")
	}
}

func TestHasAdminRoleFromContext_AdminPresent(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.WithValue(req.Context(), UserRolesKey, []string{qrdbsdk.RoleNameAdmin, "readwrite"})
	req = req.WithContext(ctx)

	if !hasAdminRoleFromContext(req) {
		t.Fatal("expected true when admin role is in context")
	}
}

func TestHasAdminRoleFromContext_NoAdminRole(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ctx := context.WithValue(req.Context(), UserRolesKey, []string{"readwrite", "readonly"})
	req = req.WithContext(ctx)

	if hasAdminRoleFromContext(req) {
		t.Fatal("expected false when admin role is not in context")
	}
}

func TestHasAdminRoleFromContext_NoRolesInContext(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	if hasAdminRoleFromContext(req) {
		t.Fatal("expected false when no roles in context")
	}
}

func TestPlatformShardForbiddenError_Error(t *testing.T) {
	err := &PlatformShardForbiddenError{ShardID: 1}
	expected := "platform shard access denied for shard 1"
	if err.Error() != expected {
		t.Fatalf("expected %q, got: %q", expected, err.Error())
	}
}

func TestPlatformShardForbiddenError_DifferentShardID(t *testing.T) {
	err := &PlatformShardForbiddenError{ShardID: 999}
	expected := "platform shard access denied for shard 999"
	if err.Error() != expected {
		t.Fatalf("expected %q, got: %q", expected, err.Error())
	}
}
