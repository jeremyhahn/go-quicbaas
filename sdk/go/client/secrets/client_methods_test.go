// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package secrets

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// errReq returns a mockTransport whose Request always fails with the given error.
func errReq(err error) *mockTransport {
	return &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, err
		},
	}
}

// okReq returns a mockTransport whose Request returns the given response.
func okReq(resp interface{}) *mockTransport {
	return &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return resp, nil
		},
	}
}

// =============================================================================
// Ensure
// =============================================================================

func TestClient_Ensure_Validation(t *testing.T) {
	client := NewClient(okReq(&EnsureSecretResponse{Path: "p", Version: 1}))

	tests := []struct {
		name string
		ctx  context.Context
		req  *EnsureSecretRequest
	}{
		{"nil request", context.Background(), nil},
		{"empty path", context.Background(), &EnsureSecretRequest{Length: 16, Encoding: "hex"}},
		{"zero length", context.Background(), &EnsureSecretRequest{Path: "p", Length: 0, Encoding: "hex"}},
		{"negative length", context.Background(), &EnsureSecretRequest{Path: "p", Length: -1, Encoding: "hex"}},
		{"empty encoding", context.Background(), &EnsureSecretRequest{Path: "p", Length: 16}},
		{"nil context", nil, &EnsureSecretRequest{Path: "p", Length: 16, Encoding: "hex"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Ensure(tt.ctx, tt.req)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput, got %v", err)
			}
		})
	}
}

func TestClient_Ensure(t *testing.T) {
	validReq := &EnsureSecretRequest{Path: "tokens/api", Length: 32, Encoding: "base64", TenantID: "t1"}

	t.Run("success", func(t *testing.T) {
		mockT := &mockTransport{
			requestFn: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
				if svc != serviceName || method != "Ensure" {
					t.Errorf("unexpected svc/method %q/%q", svc, method)
				}
				if r := req.(*EnsureSecretRequest); r.Path != "tokens/api" {
					t.Errorf("unexpected path %q", r.Path)
				}
				return &EnsureSecretResponse{Path: "tokens/api", Data: []byte("abc"), Generated: true, Version: 1}, nil
			},
		}
		resp, err := NewClient(mockT).Ensure(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !resp.Generated || resp.Version != 1 {
			t.Errorf("unexpected response: %+v", resp)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		_, err := NewClient(errReq(errors.NewUnavailable("test", "down"))).Ensure(context.Background(), validReq)
		if errors.Code(err) != errors.CodeUnavailable {
			t.Errorf("expected Unavailable, got %v", err)
		}
	})

	t.Run("nil response", func(t *testing.T) {
		_, err := NewClient(okReq(nil)).Ensure(context.Background(), validReq)
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", err)
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		_, err := NewClient(okReq("nope")).Ensure(context.Background(), validReq)
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", err)
		}
	})
}

// =============================================================================
// GetVersion
// =============================================================================

func TestClient_GetVersion_Validation(t *testing.T) {
	client := NewClient(okReq(&SecretVersionInfo{}))

	tests := []struct {
		name string
		ctx  context.Context
		req  *GetSecretVersionRequest
	}{
		{"nil request", context.Background(), nil},
		{"empty secret ID", context.Background(), &GetSecretVersionRequest{Version: 1}},
		{"zero version", context.Background(), &GetSecretVersionRequest{SecretID: "s1"}},
		{"negative version", context.Background(), &GetSecretVersionRequest{SecretID: "s1", Version: -2}},
		{"nil context", nil, &GetSecretVersionRequest{SecretID: "s1", Version: 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.GetVersion(tt.ctx, tt.req)
			if !errors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput, got %v", err)
			}
		})
	}
}

func TestClient_GetVersion(t *testing.T) {
	validReq := &GetSecretVersionRequest{SecretID: "s1", Version: 2}
	now := time.Now()

	t.Run("native response", func(t *testing.T) {
		resp, err := NewClient(okReq(&SecretVersionInfo{SecretID: "s1", Version: 2, Data: []byte("d")})).GetVersion(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Version != 2 {
			t.Errorf("expected version 2, got %d", resp.Version)
		}
	})

	t.Run("service response conversion", func(t *testing.T) {
		svcResp := &service.GetSecretVersionResponse{
			Version: &service.SecretVersion{
				SecretID:  "s1",
				Version:   2,
				Data:      []byte("data"),
				Metadata:  map[string]string{"k": "v"},
				CreatedAt: now,
				CreatedBy: "alice",
			},
		}
		resp, err := NewClient(okReq(svcResp)).GetVersion(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.SecretID != "s1" || resp.Version != 2 || resp.CreatedBy != "alice" {
			t.Errorf("unexpected conversion: %+v", resp)
		}
	})

	t.Run("service response nil version", func(t *testing.T) {
		_, err := NewClient(okReq(&service.GetSecretVersionResponse{Version: nil})).GetVersion(context.Background(), validReq)
		if errors.Code(err) != errors.CodeNotFound {
			t.Errorf("expected NotFound, got %v", err)
		}
	})

	t.Run("nil response not found", func(t *testing.T) {
		_, err := NewClient(okReq(nil)).GetVersion(context.Background(), validReq)
		if errors.Code(err) != errors.CodeNotFound {
			t.Errorf("expected NotFound, got %v", err)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		_, err := NewClient(errReq(errors.NewUnavailable("test", "down"))).GetVersion(context.Background(), validReq)
		if errors.Code(err) != errors.CodeUnavailable {
			t.Errorf("expected Unavailable, got %v", err)
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		_, err := NewClient(okReq(42)).GetVersion(context.Background(), validReq)
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", err)
		}
	})
}

// =============================================================================
// ListVersions
// =============================================================================

func TestClient_ListVersions_Validation(t *testing.T) {
	client := NewClient(okReq(&ListSecretVersionsResponse{}))

	t.Run("empty secret ID", func(t *testing.T) {
		_, err := client.ListVersions(context.Background(), "")
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected InvalidInput, got %v", err)
		}
	})
	t.Run("nil context", func(t *testing.T) {
		_, err := client.ListVersions(nil, "s1")
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected InvalidInput, got %v", err)
		}
	})
}

func TestClient_ListVersions(t *testing.T) {
	now := time.Now()

	t.Run("native response", func(t *testing.T) {
		resp, err := NewClient(okReq(&ListSecretVersionsResponse{
			Versions: []SecretVersionInfo{{SecretID: "s1", Version: 1}, {SecretID: "s1", Version: 2}},
		})).ListVersions(context.Background(), "s1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Versions) != 2 {
			t.Errorf("expected 2 versions, got %d", len(resp.Versions))
		}
	})

	t.Run("service response conversion", func(t *testing.T) {
		svcResp := &service.ListSecretVersionsResponse{
			Versions: []*service.SecretVersion{
				{SecretID: "s1", Version: 1, Data: []byte("a"), Metadata: map[string]string{"x": "1"}, CreatedAt: now, CreatedBy: "bob"},
				{SecretID: "s1", Version: 2},
			},
		}
		resp, err := NewClient(okReq(svcResp)).ListVersions(context.Background(), "s1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Versions) != 2 || resp.Versions[0].CreatedBy != "bob" || resp.Versions[1].Version != 2 {
			t.Errorf("unexpected conversion: %+v", resp.Versions)
		}
	})

	t.Run("nil response returns empty", func(t *testing.T) {
		resp, err := NewClient(okReq(nil)).ListVersions(context.Background(), "s1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Versions) != 0 {
			t.Errorf("expected 0 versions, got %d", len(resp.Versions))
		}
	})

	t.Run("transport error", func(t *testing.T) {
		_, err := NewClient(errReq(errors.NewUnavailable("test", "down"))).ListVersions(context.Background(), "s1")
		if errors.Code(err) != errors.CodeUnavailable {
			t.Errorf("expected Unavailable, got %v", err)
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		_, err := NewClient(okReq("bad")).ListVersions(context.Background(), "s1")
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", err)
		}
	})
}

// =============================================================================
// Rotate
// =============================================================================

func TestClient_Rotate_Validation(t *testing.T) {
	client := NewClient(okReq(&RotateSecretResponse{}))

	tests := []struct {
		name string
		ctx  context.Context
		req  *RotateSecretRequest
	}{
		{"nil request", context.Background(), nil},
		{"empty secret ID", context.Background(), &RotateSecretRequest{NewData: []byte("x")}},
		{"empty data", context.Background(), &RotateSecretRequest{SecretID: "s1"}},
		{"nil context", nil, &RotateSecretRequest{SecretID: "s1", NewData: []byte("x")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Rotate(tt.ctx, tt.req)
			if !errors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput, got %v", err)
			}
		})
	}
}

func TestClient_Rotate(t *testing.T) {
	validReq := &RotateSecretRequest{SecretID: "s1", NewData: []byte("newval")}

	t.Run("native response", func(t *testing.T) {
		resp, err := NewClient(okReq(&RotateSecretResponse{SecretID: "s1", OldVersion: 1, NewVersion: 2})).Rotate(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.NewVersion != 2 {
			t.Errorf("expected new version 2, got %d", resp.NewVersion)
		}
	})

	t.Run("service response conversion", func(t *testing.T) {
		resp, err := NewClient(okReq(&service.RotateSecretResponse{SecretID: "s1", OldVersion: 3, NewVersion: 4})).Rotate(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.OldVersion != 3 || resp.NewVersion != 4 || resp.SecretID != "s1" {
			t.Errorf("unexpected conversion: %+v", resp)
		}
	})

	t.Run("nil response", func(t *testing.T) {
		_, err := NewClient(okReq(nil)).Rotate(context.Background(), validReq)
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", err)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		_, err := NewClient(errReq(errors.NewUnavailable("test", "down"))).Rotate(context.Background(), validReq)
		if errors.Code(err) != errors.CodeUnavailable {
			t.Errorf("expected Unavailable, got %v", err)
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		_, err := NewClient(okReq("bad")).Rotate(context.Background(), validReq)
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", err)
		}
	})
}

// =============================================================================
// Rollback
// =============================================================================

func TestClient_Rollback_Validation(t *testing.T) {
	client := NewClient(okReq(&RollbackSecretResponse{}))

	tests := []struct {
		name string
		ctx  context.Context
		req  *RollbackSecretRequest
	}{
		{"nil request", context.Background(), nil},
		{"empty secret ID", context.Background(), &RollbackSecretRequest{TargetVersion: 1}},
		{"zero target version", context.Background(), &RollbackSecretRequest{SecretID: "s1"}},
		{"negative target version", context.Background(), &RollbackSecretRequest{SecretID: "s1", TargetVersion: -1}},
		{"nil context", nil, &RollbackSecretRequest{SecretID: "s1", TargetVersion: 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Rollback(tt.ctx, tt.req)
			if !errors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput, got %v", err)
			}
		})
	}
}

func TestClient_Rollback(t *testing.T) {
	validReq := &RollbackSecretRequest{SecretID: "s1", TargetVersion: 1}

	t.Run("native response", func(t *testing.T) {
		resp, err := NewClient(okReq(&RollbackSecretResponse{SecretID: "s1", NewVersion: 5})).Rollback(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.NewVersion != 5 {
			t.Errorf("expected new version 5, got %d", resp.NewVersion)
		}
	})

	t.Run("service response conversion", func(t *testing.T) {
		resp, err := NewClient(okReq(&service.RollbackSecretResponse{SecretID: "s1", NewVersion: 6})).Rollback(context.Background(), validReq)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.SecretID != "s1" || resp.NewVersion != 6 {
			t.Errorf("unexpected conversion: %+v", resp)
		}
	})

	t.Run("nil response", func(t *testing.T) {
		_, err := NewClient(okReq(nil)).Rollback(context.Background(), validReq)
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", err)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		_, err := NewClient(errReq(errors.NewUnavailable("test", "down"))).Rollback(context.Background(), validReq)
		if errors.Code(err) != errors.CodeUnavailable {
			t.Errorf("expected Unavailable, got %v", err)
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		_, err := NewClient(okReq(99)).Rollback(context.Background(), validReq)
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", err)
		}
	})
}

// =============================================================================
// UpdateStatus
// =============================================================================

func TestClient_UpdateStatus_Validation(t *testing.T) {
	client := NewClient(okReq(&updateSecretStatusResponse{}))

	tests := []struct {
		name string
		ctx  context.Context
		req  *UpdateSecretStatusRequest
	}{
		{"nil request", context.Background(), nil},
		{"empty secret ID", context.Background(), &UpdateSecretStatusRequest{Status: SecretStatusEnabled}},
		{"nil context", nil, &UpdateSecretStatusRequest{SecretID: "s1", Status: SecretStatusEnabled}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.UpdateStatus(tt.ctx, tt.req)
			if !errors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput, got %v", err)
			}
		})
	}
}

func TestClient_UpdateStatus(t *testing.T) {
	validReq := &UpdateSecretStatusRequest{SecretID: "s1", Status: SecretStatusDisabled}

	t.Run("native response", func(t *testing.T) {
		if err := NewClient(okReq(&updateSecretStatusResponse{})).UpdateStatus(context.Background(), validReq); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("service response", func(t *testing.T) {
		if err := NewClient(okReq(&service.UpdateSecretStatusResponse{SecretID: "s1"})).UpdateStatus(context.Background(), validReq); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("nil response", func(t *testing.T) {
		if err := NewClient(okReq(nil)).UpdateStatus(context.Background(), validReq); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		err := NewClient(errReq(errors.NewUnavailable("test", "down"))).UpdateStatus(context.Background(), validReq)
		if errors.Code(err) != errors.CodeUnavailable {
			t.Errorf("expected Unavailable, got %v", err)
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		err := NewClient(okReq("bad")).UpdateStatus(context.Background(), validReq)
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", err)
		}
	})
}

// =============================================================================
// SetRotationPolicy
// =============================================================================

func TestClient_SetRotationPolicy_Validation(t *testing.T) {
	client := NewClient(okReq(&setRotationPolicyResponse{}))
	policy := &RotationPolicy{Enabled: true, Interval: time.Hour}

	tests := []struct {
		name string
		ctx  context.Context
		req  *SetRotationPolicyRequest
	}{
		{"nil request", context.Background(), nil},
		{"empty secret ID", context.Background(), &SetRotationPolicyRequest{Policy: policy}},
		{"nil policy", context.Background(), &SetRotationPolicyRequest{SecretID: "s1"}},
		{"nil context", nil, &SetRotationPolicyRequest{SecretID: "s1", Policy: policy}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.SetRotationPolicy(tt.ctx, tt.req)
			if !errors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput, got %v", err)
			}
		})
	}
}

func TestClient_SetRotationPolicy(t *testing.T) {
	validReq := &SetRotationPolicyRequest{
		SecretID: "s1",
		Policy:   &RotationPolicy{Enabled: true, Interval: 24 * time.Hour, RetainVersions: 5},
	}

	t.Run("native response", func(t *testing.T) {
		if err := NewClient(okReq(&setRotationPolicyResponse{})).SetRotationPolicy(context.Background(), validReq); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("service response", func(t *testing.T) {
		if err := NewClient(okReq(&service.SetRotationPolicyResponse{SecretID: "s1"})).SetRotationPolicy(context.Background(), validReq); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("nil response", func(t *testing.T) {
		if err := NewClient(okReq(nil)).SetRotationPolicy(context.Background(), validReq); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		err := NewClient(errReq(errors.NewUnavailable("test", "down"))).SetRotationPolicy(context.Background(), validReq)
		if errors.Code(err) != errors.CodeUnavailable {
			t.Errorf("expected Unavailable, got %v", err)
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		err := NewClient(okReq(7)).SetRotationPolicy(context.Background(), validReq)
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", err)
		}
	})
}

// =============================================================================
// GetRotationPolicy
// =============================================================================

func TestClient_GetRotationPolicy_Validation(t *testing.T) {
	client := NewClient(okReq(&RotationPolicy{}))

	t.Run("empty secret ID", func(t *testing.T) {
		_, err := client.GetRotationPolicy(context.Background(), "")
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected InvalidInput, got %v", err)
		}
	})
	t.Run("nil context", func(t *testing.T) {
		_, err := client.GetRotationPolicy(nil, "s1")
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected InvalidInput, got %v", err)
		}
	})
}

func TestClient_GetRotationPolicy(t *testing.T) {
	now := time.Now()

	t.Run("native response", func(t *testing.T) {
		resp, err := NewClient(okReq(&RotationPolicy{Enabled: true, RetainVersions: 3})).GetRotationPolicy(context.Background(), "s1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !resp.Enabled || resp.RetainVersions != 3 {
			t.Errorf("unexpected response: %+v", resp)
		}
	})

	t.Run("service response conversion", func(t *testing.T) {
		svcResp := &service.GetRotationPolicyResponse{
			Policy: &service.SecretRotationPolicy{
				Enabled:        true,
				Interval:       2 * time.Hour,
				NextRotation:   now,
				RetainVersions: 7,
				RotationLambda: "arn:fn",
			},
		}
		resp, err := NewClient(okReq(svcResp)).GetRotationPolicy(context.Background(), "s1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Interval != 2*time.Hour || resp.RetainVersions != 7 || resp.RotationLambda != "arn:fn" {
			t.Errorf("unexpected conversion: %+v", resp)
		}
	})

	t.Run("service response nil policy", func(t *testing.T) {
		_, err := NewClient(okReq(&service.GetRotationPolicyResponse{Policy: nil})).GetRotationPolicy(context.Background(), "s1")
		if errors.Code(err) != errors.CodeNotFound {
			t.Errorf("expected NotFound, got %v", err)
		}
	})

	t.Run("nil response not found", func(t *testing.T) {
		_, err := NewClient(okReq(nil)).GetRotationPolicy(context.Background(), "s1")
		if errors.Code(err) != errors.CodeNotFound {
			t.Errorf("expected NotFound, got %v", err)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		_, err := NewClient(errReq(errors.NewUnavailable("test", "down"))).GetRotationPolicy(context.Background(), "s1")
		if errors.Code(err) != errors.CodeUnavailable {
			t.Errorf("expected Unavailable, got %v", err)
		}
	})

	t.Run("invalid response type", func(t *testing.T) {
		_, err := NewClient(okReq("bad")).GetRotationPolicy(context.Background(), "s1")
		if errors.Code(err) != errors.CodeInternal {
			t.Errorf("expected Internal, got %v", err)
		}
	})
}

// =============================================================================
// Transport-error paths for Store/Get/List (previously uncovered)
// =============================================================================

func TestClient_Store_TransportError(t *testing.T) {
	req := &StoreSecretRequest{Path: "p", Data: map[string]interface{}{"k": "v"}}
	_, err := NewClient(errReq(errors.NewUnavailable("test", "down"))).Store(context.Background(), req)
	if errors.Code(err) != errors.CodeUnavailable {
		t.Errorf("expected Unavailable, got %v", err)
	}
}

func TestClient_Get_TransportError(t *testing.T) {
	_, err := NewClient(errReq(errors.NewUnavailable("test", "down"))).Get(context.Background(), "p")
	if errors.Code(err) != errors.CodeUnavailable {
		t.Errorf("expected Unavailable, got %v", err)
	}
}

func TestClient_List_TransportError(t *testing.T) {
	_, err := NewClient(errReq(errors.NewUnavailable("test", "down"))).List(context.Background(), "p/")
	if errors.Code(err) != errors.CodeUnavailable {
		t.Errorf("expected Unavailable, got %v", err)
	}
}
