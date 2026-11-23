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

package grpc

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

func TestRBACGRPCStatus(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want codes.Code
	}{
		{"rbac denial", &qrdbsdk.PermissionDeniedError{}, codes.PermissionDenied},
		{"wrapped rbac denial", fmt.Errorf("x: %w", &qrdbsdk.PermissionDeniedError{}), codes.PermissionDenied},
		{"abac denial", &qrdbsdk.ABACPolicyDeniedError{}, codes.PermissionDenied},
		{"wrapped abac denial", fmt.Errorf("x: %w", &qrdbsdk.ABACPolicyDeniedError{}), codes.PermissionDenied},
		{"node down", errors.New("node down"), codes.Unavailable},
		{"cancelled", context.Canceled, codes.Unavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rbacGRPCStatus(tt.err)
			if got == nil {
				t.Fatal("rbacGRPCStatus returned nil: there is no allow path")
			}
			if code := status.Code(got); code != tt.want {
				t.Fatalf("code = %v, want %v", code, tt.want)
			}
		})
	}
}

func TestAuthorizedContextToQRDBIdentity(t *testing.T) {
	got := authorizedContextToQRDBIdentity(&platformtoken.AuthorizedContext{
		Subject:  "alice",
		TenantID: "t1",
		Roles:    []string{"r1"},
	})
	if got == nil {
		t.Fatal("identity is nil")
	}
	if got.Subject != "alice" || got.TenantID != "t1" || !reflect.DeepEqual(got.Roles, []string{"r1"}) {
		t.Fatalf("identity = %+v", got)
	}

	empty := authorizedContextToQRDBIdentity(nil)
	if empty == nil {
		t.Fatal("nil AuthorizedContext projected to a nil identity")
	}
	if empty.Subject != "" || empty.TenantID != "" || len(empty.Roles) != 0 {
		t.Fatalf("nil AuthorizedContext projected to %+v, want empty", empty)
	}
}
