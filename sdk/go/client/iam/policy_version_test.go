package iam

import (
	"context"
	stderrors "errors"
	"testing"
)

// TestPolicyClient_Update_NonZeroVersionRefused proves the refusal sits in
// the shared client path: every transport is reached only through
// transport.Request, and a non-zero version never gets there.
func TestPolicyClient_Update_NonZeroVersionRefused(t *testing.T) {
	tr := &mockPolicyTransport{
		requestFunc: func(context.Context, string, string, interface{}) (interface{}, error) {
			t.Error("a refused version must not reach the transport")
			return nil, nil
		},
	}
	client := NewPolicyClient(tr)

	_, err := client.Update(context.Background(), "tenant-1", "test-policy", &UpdatePolicyRequest{Version: 4})
	var versionErr *PolicyVersionUnsupportedError
	if !stderrors.As(err, &versionErr) {
		t.Fatalf("expected *PolicyVersionUnsupportedError, got %v", err)
	}
	if versionErr.Version != "4" {
		t.Errorf("expected version 4 in the error, got %q", versionErr.Version)
	}
}

func TestPolicyClient_Update_ZeroVersionIsSent(t *testing.T) {
	sent := false
	tr := &mockPolicyTransport{
		requestFunc: func(context.Context, string, string, interface{}) (interface{}, error) {
			sent = true
			return &Policy{Name: "test-policy", TenantID: "tenant-1"}, nil
		},
	}
	client := NewPolicyClient(tr)

	desc := "updated"
	if _, err := client.Update(context.Background(), "tenant-1", "test-policy", &UpdatePolicyRequest{Description: &desc}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sent {
		t.Error("a zero version must be sent")
	}
}
