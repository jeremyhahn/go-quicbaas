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

package grpc

import (
	"context"
	"testing"

	"google.golang.org/grpc"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// tenantCheckPEPServer records the last CheckRequest it receives.
type tenantCheckPEPServer struct {
	iampb.UnimplementedPEPServiceServer
	last *iampb.CheckRequest
}

func (s *tenantCheckPEPServer) Check(_ context.Context, req *iampb.CheckRequest) (*iampb.CheckResponse, error) {
	s.last = req
	return &iampb.CheckResponse{Allowed: true}, nil
}

// TestIAMHandler_PEPCheck_Tenant proves the gRPC transport sends the SDK
// CheckRequest's tenant on the wire.
func TestIAMHandler_PEPCheck_Tenant(t *testing.T) {
	tests := []struct {
		name     string
		tenantID string
	}{
		{name: "tenant is sent", tenantID: "t1"},
		{name: "empty tenant is sent empty", tenantID: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := &tenantCheckPEPServer{}
			srv := newHandlerServer(t, func(s *grpc.Server) {
				iampb.RegisterPEPServiceServer(s, fake)
			})
			h := newIAMHandler(srv.dial(t))

			req := &struct {
				Request *iam.CheckRequest `json:"request"`
			}{Request: &iam.CheckRequest{
				Subject:  iam.CheckSubject{ID: "user-1"},
				Action:   "read",
				Resource: "doc-1",
				TenantID: tc.tenantID,
			}}
			if _, err := h.handlePEPRequest(context.Background(), "Check", req); err != nil {
				t.Fatalf("Check: unexpected error: %v", err)
			}
			if fake.last == nil {
				t.Fatal("server received no request")
			}
			if got := fake.last.GetTenantId(); got != tc.tenantID {
				t.Errorf("tenant_id = %q, want %q", got, tc.tenantID)
			}
			if got := fake.last.GetSubject().GetId(); got != "user-1" {
				t.Errorf("subject id = %q, want user-1", got)
			}
		})
	}
}
