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
	"testing"

	"google.golang.org/grpc"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
)

// nameRecordingIAMServer hands the ValidatePolicy request it receives to the
// test, so the test can read the name that crossed the wire.
type nameRecordingIAMServer struct {
	iampb.UnimplementedIAMServiceServer
	got chan *iampb.ValidatePolicyRequest
}

func (s *nameRecordingIAMServer) ValidatePolicy(_ context.Context, req *iampb.ValidatePolicyRequest) (*iampb.ValidatePolicyResponse, error) {
	s.got <- req
	return &iampb.ValidatePolicyResponse{Valid: true}, nil
}

func validatePolicyOverWire(t *testing.T, req map[string]interface{}) *iampb.ValidatePolicyRequest {
	t.Helper()
	rec := &nameRecordingIAMServer{got: make(chan *iampb.ValidatePolicyRequest, 1)}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		iampb.RegisterIAMServiceServer(s, rec)
	})
	h := newIAMHandler(srv.dial(t))
	if _, err := h.handleRequest(context.Background(), "quicbaas.IAM.Policy", "Validate", req); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
	return <-rec.got
}

// TestHandleValidatePolicy_SendsName pins that the name the server checks
// against metadata.name reaches ValidatePolicyRequest.name.
func TestHandleValidatePolicy_SendsName(t *testing.T) {
	got := validatePolicyOverWire(t, map[string]interface{}{
		"tenant_id": "t1",
		"policy":    map[string]interface{}{"name": "p1", "spec_yaml": []byte(sampleSpecYAML)},
		"name":      "p1",
	})
	if got.GetName() != "p1" {
		t.Errorf("name = %q, want %q", got.GetName(), "p1")
	}
	if got.GetTenantId() != "t1" {
		t.Errorf("tenant_id = %q, want %q", got.GetTenantId(), "t1")
	}
}

// TestHandleValidatePolicy_NoNameLeavesFieldEmpty pins that a request without
// a name stays unchecked rather than inheriting one from the policy body.
func TestHandleValidatePolicy_NoNameLeavesFieldEmpty(t *testing.T) {
	got := validatePolicyOverWire(t, map[string]interface{}{
		"tenant_id": "t1",
		"policy":    map[string]interface{}{"spec_yaml": []byte(sampleSpecYAML)},
	})
	if got.GetName() != "" {
		t.Errorf("name = %q, want empty", got.GetName())
	}
}
