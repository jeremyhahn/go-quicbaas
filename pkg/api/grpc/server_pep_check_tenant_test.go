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

//go:build !integration

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// TestPEPServer_CheckTenant proves Check acts only in the caller's bound
// tenant: the request tenant, falling back to the subject's, must be empty or
// the bound tenant, and the engine is given the bound tenant.
func TestPEPServer_CheckTenant(t *testing.T) {
	bound := tenantctx.WithTenant(context.Background(), "t1")

	tests := []struct {
		name          string
		ctx           context.Context
		tenantID      string
		subjectTenant string
		wantCode      codes.Code
		wantTenant    string
	}{
		{name: "same tenant reaches the service", ctx: bound, tenantID: "t1", wantCode: codes.OK, wantTenant: "t1"},
		{name: "empty tenant means the bound tenant", ctx: bound, wantCode: codes.OK, wantTenant: "t1"},
		{name: "other request tenant is refused", ctx: bound, tenantID: "t2", wantCode: codes.PermissionDenied},
		{name: "other subject tenant is refused", ctx: bound, subjectTenant: "t2", wantCode: codes.PermissionDenied},
		{name: "no bound tenant is refused", ctx: context.Background(), tenantID: "t1", wantCode: codes.PermissionDenied},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, mockSvc := createTestPEPServer(t)
			var got *iam.CheckRequest
			mockSvc.checkFunc = func(_ context.Context, req *iam.CheckRequest) (*iam.CheckResponse, error) {
				got = req
				return &iam.CheckResponse{Allowed: true}, nil
			}

			resp, err := srv.Check(tt.ctx, &iampb.CheckRequest{
				Subject:  &iampb.Subject{Id: "user1", TenantId: tt.subjectTenant},
				Action:   "read",
				Resource: "document:123",
				TenantId: tt.tenantID,
			})

			assert.Equal(t, tt.wantCode, status.Code(err))
			if tt.wantCode != codes.OK {
				assert.Nil(t, resp)
				assert.Nil(t, got, "the service must not be called")
				return
			}
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, tt.wantTenant, got.Subject.TenantID)
		})
	}
}
