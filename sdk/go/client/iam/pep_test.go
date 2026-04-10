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

package iam

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// mockPEPTransport implements transport.Transport for testing.
type mockPEPTransport struct {
	requestFunc func(ctx context.Context, service, method string, req interface{}) (interface{}, error)
}

func (m *mockPEPTransport) Request(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	if m.requestFunc != nil {
		return m.requestFunc(ctx, service, method, req)
	}
	return nil, nil
}

func (m *mockPEPTransport) RequestStream(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
	return nil, nil
}

func (m *mockPEPTransport) Conn() interface{} {
	return nil
}

func (m *mockPEPTransport) Close() error {
	return nil
}

func (m *mockPEPTransport) Healthy() bool {
	return true
}

func TestNewPEPClient(t *testing.T) {
	t.Run("returns nil for nil transport", func(t *testing.T) {
		client := NewPEPClient(nil)
		if client != nil {
			t.Error("expected nil client for nil transport")
		}
	})

	t.Run("returns client for valid transport", func(t *testing.T) {
		tr := &mockPEPTransport{}
		client := NewPEPClient(tr)
		if client == nil {
			t.Error("expected non-nil client")
		}
	})
}

func TestPEPClient_Check(t *testing.T) {
	t.Run("success - allowed", func(t *testing.T) {
		expectedResp := &CheckResponse{
			Allowed:          true,
			Effect:           EffectAllow,
			MatchedPolicy:    "admin-policy",
			MatchedRule:      "allow-all",
			EvaluationTimeNs: 1500,
			Reason:           "User is admin",
			CacheHit:         false,
			Timestamp:        time.Now(),
		}

		tr := &mockPEPTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if service != pepServiceName {
					t.Errorf("expected service %s, got %s", pepServiceName, service)
				}
				if method != "Check" {
					t.Errorf("expected method Check, got %s", method)
				}
				return expectedResp, nil
			},
		}

		client := NewPEPClient(tr)
		req := &CheckRequest{
			Subject: CheckSubject{
				ID:       "user-1",
				TenantID: "tenant-1",
				Type:     SubjectTypeUser,
				Roles:    []string{"admin"},
			},
			Action:   "data:read",
			Resource: "orders://123",
			TenantID: "tenant-1",
		}

		resp, err := client.Check(context.Background(), req)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
		if !resp.Allowed {
			t.Error("expected allowed result")
		}
		if resp.MatchedPolicy != "admin-policy" {
			t.Errorf("expected policy admin-policy, got %s", resp.MatchedPolicy)
		}
	})

	t.Run("success - denied", func(t *testing.T) {
		expectedResp := &CheckResponse{
			Allowed:          false,
			Effect:           EffectDeny,
			MatchedPolicy:    "restrict-policy",
			MatchedRule:      "deny-delete",
			EvaluationTimeNs: 1000,
			Reason:           "User not authorized for delete",
		}

		tr := &mockPEPTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return expectedResp, nil
			},
		}

		client := NewPEPClient(tr)
		req := &CheckRequest{
			Subject: CheckSubject{
				ID:   "user-2",
				Type: SubjectTypeUser,
			},
			Action:   "data:delete",
			Resource: "orders://456",
		}

		resp, err := client.Check(context.Background(), req)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
		if resp.Allowed {
			t.Error("expected denied result")
		}
	})

	t.Run("nil context", func(t *testing.T) {
		tr := &mockPEPTransport{}
		client := NewPEPClient(tr)
		req := &CheckRequest{
			Subject:  CheckSubject{ID: "user-1"},
			Action:   "data:read",
			Resource: "orders://123",
		}

		_, err := client.Check(nil, req)
		if err == nil {
			t.Error("expected error for nil context")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("nil request", func(t *testing.T) {
		tr := &mockPEPTransport{}
		client := NewPEPClient(tr)

		_, err := client.Check(context.Background(), nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty subject ID", func(t *testing.T) {
		tr := &mockPEPTransport{}
		client := NewPEPClient(tr)
		req := &CheckRequest{
			Subject:  CheckSubject{ID: ""},
			Action:   "data:read",
			Resource: "orders://123",
		}

		_, err := client.Check(context.Background(), req)
		if err == nil {
			t.Error("expected error for empty subject ID")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty action", func(t *testing.T) {
		tr := &mockPEPTransport{}
		client := NewPEPClient(tr)
		req := &CheckRequest{
			Subject:  CheckSubject{ID: "user-1"},
			Action:   "",
			Resource: "orders://123",
		}

		_, err := client.Check(context.Background(), req)
		if err == nil {
			t.Error("expected error for empty action")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("empty resource", func(t *testing.T) {
		tr := &mockPEPTransport{}
		client := NewPEPClient(tr)
		req := &CheckRequest{
			Subject:  CheckSubject{ID: "user-1"},
			Action:   "data:read",
			Resource: "",
		}

		_, err := client.Check(context.Background(), req)
		if err == nil {
			t.Error("expected error for empty resource")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected invalid input error, got %v", err)
		}
	})

	t.Run("nil response from server", func(t *testing.T) {
		tr := &mockPEPTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, nil
			},
		}
		client := NewPEPClient(tr)
		req := &CheckRequest{
			Subject:  CheckSubject{ID: "user-1"},
			Action:   "data:read",
			Resource: "orders://123",
		}

		_, err := client.Check(context.Background(), req)
		if err == nil {
			t.Error("expected error for nil response")
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		tr := &mockPEPTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return "invalid", nil
			},
		}
		client := NewPEPClient(tr)
		req := &CheckRequest{
			Subject:  CheckSubject{ID: "user-1"},
			Action:   "data:read",
			Resource: "orders://123",
		}

		_, err := client.Check(context.Background(), req)
		if err == nil {
			t.Error("expected error for invalid response type")
		}
	})

	t.Run("with context attributes", func(t *testing.T) {
		expectedResp := &CheckResponse{
			Allowed: true,
			Effect:  EffectAllow,
		}

		tr := &mockPEPTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return expectedResp, nil
			},
		}

		client := NewPEPClient(tr)
		req := &CheckRequest{
			Subject: CheckSubject{
				ID:       "user-1",
				TenantID: "tenant-1",
				Type:     SubjectTypeUser,
				Roles:    []string{"reader"},
				Groups:   []string{"team-a"},
				Attributes: map[string]interface{}{
					"department": "engineering",
					"level":      3,
				},
				ClientIP:  "192.168.1.100",
				UserAgent: "QuicBaaS-SDK/1.0",
			},
			Action:   "data:read",
			Resource: "orders://123",
			Context: map[string]interface{}{
				"request_id": "req-123",
				"timestamp":  time.Now().Unix(),
			},
			TenantID:    "tenant-1",
			CacheKey:    "check:user-1:data:read:orders://123",
			BypassCache: false,
		}

		resp, err := client.Check(context.Background(), req)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
		if !resp.Allowed {
			t.Error("expected allowed result")
		}
	})
}

func TestPEPClient_CheckBatch(t *testing.T) {
	t.Run("success - all allowed", func(t *testing.T) {
		expectedResp := &CheckBatchResponse{
			Results: []CheckResponse{
				{Allowed: true, Effect: EffectAllow, MatchedPolicy: "policy-1"},
				{Allowed: true, Effect: EffectAllow, MatchedPolicy: "policy-1"},
			},
			AllAllowed:            true,
			AllowedCount:          2,
			DeniedCount:           0,
			TotalEvaluationTimeNs: 3000,
		}

		tr := &mockPEPTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				if method != "CheckBatch" {
					t.Errorf("expected method CheckBatch, got %s", method)
				}
				return expectedResp, nil
			},
		}

		client := NewPEPClient(tr)
		req := &CheckBatchRequest{
			Requests: []CheckRequest{
				{
					Subject:  CheckSubject{ID: "user-1"},
					Action:   "data:read",
					Resource: "orders://123",
				},
				{
					Subject:  CheckSubject{ID: "user-1"},
					Action:   "data:read",
					Resource: "orders://456",
				},
			},
			Parallel: true,
		}

		resp, err := client.CheckBatch(context.Background(), req)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
		if !resp.AllAllowed {
			t.Error("expected all allowed")
		}
		if resp.AllowedCount != 2 {
			t.Errorf("expected 2 allowed, got %d", resp.AllowedCount)
		}
	})

	t.Run("success - mixed results", func(t *testing.T) {
		expectedResp := &CheckBatchResponse{
			Results: []CheckResponse{
				{Allowed: true, Effect: EffectAllow},
				{Allowed: false, Effect: EffectDeny, Reason: "Insufficient permissions"},
			},
			AllAllowed:   false,
			AllowedCount: 1,
			DeniedCount:  1,
		}

		tr := &mockPEPTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return expectedResp, nil
			},
		}

		client := NewPEPClient(tr)
		req := &CheckBatchRequest{
			Requests: []CheckRequest{
				{
					Subject:  CheckSubject{ID: "user-1"},
					Action:   "data:read",
					Resource: "orders://123",
				},
				{
					Subject:  CheckSubject{ID: "user-1"},
					Action:   "data:delete",
					Resource: "orders://123",
				},
			},
			StopOnDeny: false,
		}

		resp, err := client.CheckBatch(context.Background(), req)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
		if resp.AllAllowed {
			t.Error("expected not all allowed")
		}
		if resp.DeniedCount != 1 {
			t.Errorf("expected 1 denied, got %d", resp.DeniedCount)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		tr := &mockPEPTransport{}
		client := NewPEPClient(tr)
		req := &CheckBatchRequest{
			Requests: []CheckRequest{
				{
					Subject:  CheckSubject{ID: "user-1"},
					Action:   "data:read",
					Resource: "orders://123",
				},
			},
		}

		_, err := client.CheckBatch(nil, req)
		if err == nil {
			t.Error("expected error for nil context")
		}
	})

	t.Run("nil request", func(t *testing.T) {
		tr := &mockPEPTransport{}
		client := NewPEPClient(tr)

		_, err := client.CheckBatch(context.Background(), nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
	})

	t.Run("empty requests", func(t *testing.T) {
		tr := &mockPEPTransport{}
		client := NewPEPClient(tr)
		req := &CheckBatchRequest{
			Requests: []CheckRequest{},
		}

		_, err := client.CheckBatch(context.Background(), req)
		if err == nil {
			t.Error("expected error for empty requests")
		}
	})

	t.Run("invalid request in batch - empty subject", func(t *testing.T) {
		tr := &mockPEPTransport{}
		client := NewPEPClient(tr)
		req := &CheckBatchRequest{
			Requests: []CheckRequest{
				{
					Subject:  CheckSubject{ID: "user-1"},
					Action:   "data:read",
					Resource: "orders://123",
				},
				{
					Subject:  CheckSubject{ID: ""}, // invalid
					Action:   "data:read",
					Resource: "orders://456",
				},
			},
		}

		_, err := client.CheckBatch(context.Background(), req)
		if err == nil {
			t.Error("expected error for invalid request")
		}
	})

	t.Run("invalid request in batch - empty action", func(t *testing.T) {
		tr := &mockPEPTransport{}
		client := NewPEPClient(tr)
		req := &CheckBatchRequest{
			Requests: []CheckRequest{
				{
					Subject:  CheckSubject{ID: "user-1"},
					Action:   "", // invalid
					Resource: "orders://123",
				},
			},
		}

		_, err := client.CheckBatch(context.Background(), req)
		if err == nil {
			t.Error("expected error for invalid request")
		}
	})

	t.Run("invalid request in batch - empty resource", func(t *testing.T) {
		tr := &mockPEPTransport{}
		client := NewPEPClient(tr)
		req := &CheckBatchRequest{
			Requests: []CheckRequest{
				{
					Subject:  CheckSubject{ID: "user-1"},
					Action:   "data:read",
					Resource: "", // invalid
				},
			},
		}

		_, err := client.CheckBatch(context.Background(), req)
		if err == nil {
			t.Error("expected error for invalid request")
		}
	})

	t.Run("nil response from server", func(t *testing.T) {
		tr := &mockPEPTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, nil
			},
		}
		client := NewPEPClient(tr)
		req := &CheckBatchRequest{
			Requests: []CheckRequest{
				{
					Subject:  CheckSubject{ID: "user-1"},
					Action:   "data:read",
					Resource: "orders://123",
				},
			},
		}

		_, err := client.CheckBatch(context.Background(), req)
		if err == nil {
			t.Error("expected error for nil response")
		}
	})

	t.Run("with stop on deny", func(t *testing.T) {
		expectedResp := &CheckBatchResponse{
			Results: []CheckResponse{
				{Allowed: false, Effect: EffectDeny},
			},
			AllAllowed:   false,
			AllowedCount: 0,
			DeniedCount:  1,
		}

		tr := &mockPEPTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return expectedResp, nil
			},
		}

		client := NewPEPClient(tr)
		req := &CheckBatchRequest{
			Requests: []CheckRequest{
				{
					Subject:  CheckSubject{ID: "user-1"},
					Action:   "data:delete",
					Resource: "orders://123",
				},
				{
					Subject:  CheckSubject{ID: "user-1"},
					Action:   "data:read",
					Resource: "orders://456",
				},
			},
			StopOnDeny:     true,
			MaxConcurrency: 4,
		}

		resp, err := client.CheckBatch(context.Background(), req)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if resp == nil {
			t.Fatal("expected non-nil response")
		}
	})
}

func TestCheckResponse_IsAllowed(t *testing.T) {
	t.Run("allowed", func(t *testing.T) {
		resp := &CheckResponse{Allowed: true}
		if !resp.IsAllowed() {
			t.Error("expected IsAllowed to return true")
		}
	})

	t.Run("denied", func(t *testing.T) {
		resp := &CheckResponse{Allowed: false}
		if resp.IsAllowed() {
			t.Error("expected IsAllowed to return false")
		}
	})

	t.Run("nil response", func(t *testing.T) {
		var resp *CheckResponse
		if resp.IsAllowed() {
			t.Error("expected IsAllowed to return false for nil")
		}
	})
}

func TestCheckResponse_IsDenied(t *testing.T) {
	t.Run("denied", func(t *testing.T) {
		resp := &CheckResponse{Allowed: false}
		if !resp.IsDenied() {
			t.Error("expected IsDenied to return true")
		}
	})

	t.Run("allowed", func(t *testing.T) {
		resp := &CheckResponse{Allowed: true}
		if resp.IsDenied() {
			t.Error("expected IsDenied to return false")
		}
	})

	t.Run("nil response", func(t *testing.T) {
		var resp *CheckResponse
		if !resp.IsDenied() {
			t.Error("expected IsDenied to return true for nil")
		}
	})
}

func TestCheckResponse_HasObligations(t *testing.T) {
	t.Run("has obligations", func(t *testing.T) {
		resp := &CheckResponse{
			Obligations: []Obligation{
				{ID: "log-access", Type: ObligationTypeLog},
			},
		}
		if !resp.HasObligations() {
			t.Error("expected HasObligations to return true")
		}
	})

	t.Run("no obligations", func(t *testing.T) {
		resp := &CheckResponse{
			Obligations: nil,
		}
		if resp.HasObligations() {
			t.Error("expected HasObligations to return false")
		}
	})

	t.Run("empty obligations", func(t *testing.T) {
		resp := &CheckResponse{
			Obligations: []Obligation{},
		}
		if resp.HasObligations() {
			t.Error("expected HasObligations to return false for empty slice")
		}
	})

	t.Run("nil response", func(t *testing.T) {
		var resp *CheckResponse
		if resp.HasObligations() {
			t.Error("expected HasObligations to return false for nil")
		}
	})
}

func TestCheckResponse_GetObligationsByType(t *testing.T) {
	t.Run("finds obligations by type", func(t *testing.T) {
		resp := &CheckResponse{
			Obligations: []Obligation{
				{ID: "log-1", Type: ObligationTypeLog},
				{ID: "audit-1", Type: ObligationTypeAudit},
				{ID: "log-2", Type: ObligationTypeLog},
				{ID: "mask-1", Type: ObligationTypeMask},
			},
		}

		logs := resp.GetObligationsByType(ObligationTypeLog)
		if len(logs) != 2 {
			t.Errorf("expected 2 log obligations, got %d", len(logs))
		}

		audits := resp.GetObligationsByType(ObligationTypeAudit)
		if len(audits) != 1 {
			t.Errorf("expected 1 audit obligation, got %d", len(audits))
		}

		notifies := resp.GetObligationsByType(ObligationTypeNotify)
		if len(notifies) != 0 {
			t.Errorf("expected 0 notify obligations, got %d", len(notifies))
		}
	})

	t.Run("nil response", func(t *testing.T) {
		var resp *CheckResponse
		result := resp.GetObligationsByType(ObligationTypeLog)
		if result != nil {
			t.Error("expected nil for nil response")
		}
	})
}

func TestCheckBatchResponse_FirstDenied(t *testing.T) {
	t.Run("finds first denied", func(t *testing.T) {
		resp := &CheckBatchResponse{
			Results: []CheckResponse{
				{Allowed: true},
				{Allowed: false, Reason: "first denied"},
				{Allowed: false, Reason: "second denied"},
			},
		}

		first := resp.FirstDenied()
		if first == nil {
			t.Fatal("expected non-nil first denied")
		}
		if first.Reason != "first denied" {
			t.Errorf("expected reason 'first denied', got %s", first.Reason)
		}
	})

	t.Run("no denied", func(t *testing.T) {
		resp := &CheckBatchResponse{
			Results: []CheckResponse{
				{Allowed: true},
				{Allowed: true},
			},
		}

		first := resp.FirstDenied()
		if first != nil {
			t.Error("expected nil for no denied")
		}
	})

	t.Run("nil response", func(t *testing.T) {
		var resp *CheckBatchResponse
		result := resp.FirstDenied()
		if result != nil {
			t.Error("expected nil for nil response")
		}
	})
}

func TestCheckBatchResponse_GetDeniedIndices(t *testing.T) {
	t.Run("finds denied indices", func(t *testing.T) {
		resp := &CheckBatchResponse{
			Results: []CheckResponse{
				{Allowed: true},
				{Allowed: false},
				{Allowed: true},
				{Allowed: false},
			},
		}

		indices := resp.GetDeniedIndices()
		if len(indices) != 2 {
			t.Errorf("expected 2 indices, got %d", len(indices))
		}
		if indices[0] != 1 || indices[1] != 3 {
			t.Errorf("expected indices [1, 3], got %v", indices)
		}
	})

	t.Run("no denied", func(t *testing.T) {
		resp := &CheckBatchResponse{
			Results: []CheckResponse{
				{Allowed: true},
			},
		}

		indices := resp.GetDeniedIndices()
		if len(indices) != 0 {
			t.Errorf("expected empty slice, got %v", indices)
		}
	})

	t.Run("nil response", func(t *testing.T) {
		var resp *CheckBatchResponse
		result := resp.GetDeniedIndices()
		if result != nil {
			t.Error("expected nil for nil response")
		}
	})
}

func TestCheckBatchResponse_GetAllowedIndices(t *testing.T) {
	t.Run("finds allowed indices", func(t *testing.T) {
		resp := &CheckBatchResponse{
			Results: []CheckResponse{
				{Allowed: true},
				{Allowed: false},
				{Allowed: true},
				{Allowed: false},
			},
		}

		indices := resp.GetAllowedIndices()
		if len(indices) != 2 {
			t.Errorf("expected 2 indices, got %d", len(indices))
		}
		if indices[0] != 0 || indices[1] != 2 {
			t.Errorf("expected indices [0, 2], got %v", indices)
		}
	})

	t.Run("all allowed", func(t *testing.T) {
		resp := &CheckBatchResponse{
			Results: []CheckResponse{
				{Allowed: true},
				{Allowed: true},
			},
		}

		indices := resp.GetAllowedIndices()
		if len(indices) != 2 {
			t.Errorf("expected 2 indices, got %d", len(indices))
		}
	})

	t.Run("nil response", func(t *testing.T) {
		var resp *CheckBatchResponse
		result := resp.GetAllowedIndices()
		if result != nil {
			t.Error("expected nil for nil response")
		}
	})
}

func TestObligationType_String(t *testing.T) {
	tests := []struct {
		obligationType ObligationType
		expected       string
	}{
		{ObligationTypeLog, "LOG"},
		{ObligationTypeAudit, "AUDIT"},
		{ObligationTypeNotify, "NOTIFY"},
		{ObligationTypeEncrypt, "ENCRYPT"},
		{ObligationTypeMask, "MASK"},
		{ObligationTypeRateLimit, "RATE_LIMIT"},
		{ObligationTypeCustom, "CUSTOM"},
		{ObligationType(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.obligationType.String(); got != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, got)
			}
		})
	}
}

func TestTransportError(t *testing.T) {
	t.Run("check with transport error", func(t *testing.T) {
		expectedErr := errors.NewUnavailable("transport", "connection refused")

		tr := &mockPEPTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, expectedErr
			},
		}

		client := NewPEPClient(tr)
		req := &CheckRequest{
			Subject:  CheckSubject{ID: "user-1"},
			Action:   "data:read",
			Resource: "orders://123",
		}

		_, err := client.Check(context.Background(), req)
		if err == nil {
			t.Error("expected error from transport")
		}
		if !errors.IsUnavailable(err) {
			t.Errorf("expected unavailable error, got %v", err)
		}
	})

	t.Run("checkbatch with transport error", func(t *testing.T) {
		expectedErr := errors.NewTimeout("transport", "request timed out")

		tr := &mockPEPTransport{
			requestFunc: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
				return nil, expectedErr
			},
		}

		client := NewPEPClient(tr)
		req := &CheckBatchRequest{
			Requests: []CheckRequest{
				{
					Subject:  CheckSubject{ID: "user-1"},
					Action:   "data:read",
					Resource: "orders://123",
				},
			},
		}

		_, err := client.CheckBatch(context.Background(), req)
		if err == nil {
			t.Error("expected error from transport")
		}
		if !errors.IsTimeout(err) {
			t.Errorf("expected timeout error, got %v", err)
		}
	})
}
