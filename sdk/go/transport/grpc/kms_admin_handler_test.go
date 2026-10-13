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

// kms_admin_handler_test.go covers the gRPC transport handlers for the KMS
// key-administration verbs (GetPublicKey, RevokeKey, metadata, access policy,
// rotation policy) wired for full transport parity.

package grpc

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	kmsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// fakeKMSAdminServer implements the key-administration RPCs on the
// KeyManagement proto service and records the most recent request so tests can
// assert the SDK struct converted correctly.
type fakeKMSAdminServer struct {
	pb.UnimplementedKeyManagementServer
	fail bool

	lastRevokeKeyID  string
	lastRevokeReason string
	lastPublicKeyID  string
	lastMetadataName string
	lastPolicyKeyID  string
	lastRotationReq  *pb.SetKeyRotationPolicyRequest
}

func (f *fakeKMSAdminServer) GetPublicKey(_ context.Context, req *pb.GetPublicKeyRequest) (*pb.GetPublicKeyResponse, error) {
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	f.lastPublicKeyID = req.KeyId
	return &pb.GetPublicKeyResponse{
		KeyId:     req.KeyId,
		PublicKey: []byte{0x30, 0x01},
		Algorithm: pb.KeyAlgorithm_KEY_ALG_RSA_2048,
		Version:   1,
		Timestamp: 1,
	}, nil
}

func (f *fakeKMSAdminServer) RevokeKey(_ context.Context, req *pb.RevokeKeyRequest) (*pb.RevokeKeyResponse, error) {
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	f.lastRevokeKeyID = req.KeyId
	f.lastRevokeReason = req.Reason
	return &pb.RevokeKeyResponse{
		KeyId:     req.KeyId,
		Status:    pb.KeyStatus_KEY_STATUS_COMPROMISED,
		Timestamp: 1,
	}, nil
}

func (f *fakeKMSAdminServer) GetKeyMetadata(_ context.Context, req *pb.GetKeyMetadataRequest) (*pb.GetKeyMetadataResponse, error) {
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	return &pb.GetKeyMetadataResponse{
		Metadata:  &pb.KeyMetadata{KeyId: req.KeyId, Name: "meta-key", Version: 1},
		Timestamp: 1,
	}, nil
}

func (f *fakeKMSAdminServer) UpdateKeyMetadata(_ context.Context, req *pb.UpdateKeyMetadataRequest) (*pb.UpdateKeyMetadataResponse, error) {
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	f.lastMetadataName = req.Name
	return &pb.UpdateKeyMetadataResponse{
		Metadata:  &pb.KeyMetadata{KeyId: req.KeyId, Name: req.Name, Version: 1},
		Timestamp: 1,
	}, nil
}

func (f *fakeKMSAdminServer) SetKeyPolicy(_ context.Context, req *pb.SetKeyPolicyRequest) (*pb.SetKeyPolicyResponse, error) {
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	f.lastPolicyKeyID = req.KeyId
	return &pb.SetKeyPolicyResponse{KeyId: req.KeyId, PolicyVersion: 2, Timestamp: 1}, nil
}

func (f *fakeKMSAdminServer) GetKeyPolicy(_ context.Context, req *pb.GetKeyPolicyRequest) (*pb.GetKeyPolicyResponse, error) {
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	return &pb.GetKeyPolicyResponse{
		Policy:    &pb.KeyPolicy{KeyId: req.KeyId, Version: 1},
		Timestamp: 1,
	}, nil
}

func (f *fakeKMSAdminServer) SetKeyRotationPolicy(_ context.Context, req *pb.SetKeyRotationPolicyRequest) (*pb.SetKeyRotationPolicyResponse, error) {
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	f.lastRotationReq = req
	return &pb.SetKeyRotationPolicyResponse{KeyId: req.KeyId, Policy: req.Policy, Timestamp: 1}, nil
}

func (f *fakeKMSAdminServer) GetKeyRotationPolicy(_ context.Context, req *pb.GetKeyRotationPolicyRequest) (*pb.GetKeyRotationPolicyResponse, error) {
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	return &pb.GetKeyRotationPolicyResponse{
		KeyId:     req.KeyId,
		Policy:    &pb.RotationPolicy{Enabled: true, IntervalSeconds: 3600},
		Timestamp: 1,
	}, nil
}

func (f *fakeKMSAdminServer) DeleteKeyRotationPolicy(_ context.Context, req *pb.DeleteKeyRotationPolicyRequest) (*pb.DeleteKeyRotationPolicyResponse, error) {
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	return &pb.DeleteKeyRotationPolicyResponse{KeyId: req.KeyId, Timestamp: 1}, nil
}

func newKMSAdminHandlerWithServer(t *testing.T, fail bool) (*kmsHandler, *fakeKMSAdminServer) {
	t.Helper()
	fake := &fakeKMSAdminServer{fail: fail}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterKeyManagementServer(s, fake)
	})
	h, err := newKMSHandler(srv.dial(t))
	if err != nil {
		t.Fatalf("newKMSHandler: %v", err)
	}
	return h, fake
}

// =============================================================================
// GetPublicKey
// =============================================================================

func TestKMSHandler_GetPublicKey_Success(t *testing.T) {
	h, fake := newKMSAdminHandlerWithServer(t, false)

	// The SDK client sends an unexported struct carrying only KeyID; a local
	// struct with the same shape exercises the reflective path.
	resp, err := h.handleRequest(context.Background(), "quicbaas.KMS", "GetPublicKey",
		&struct{ KeyID string }{KeyID: "k1"})
	if err != nil {
		t.Fatalf("GetPublicKey: %v", err)
	}
	if fake.lastPublicKeyID != "k1" {
		t.Errorf("server saw key %q, want k1", fake.lastPublicKeyID)
	}
	out, ok := resp.(*service.GetPublicKeyResponse)
	if !ok {
		t.Fatalf("unexpected response type %T", resp)
	}
	if out.KeyID != "k1" || len(out.PublicKey) == 0 {
		t.Errorf("unexpected response %+v", out)
	}
}

func TestKMSHandler_GetPublicKey_InvalidRequestType(t *testing.T) {
	h, _ := newKMSAdminHandlerWithServer(t, false)
	if _, err := h.handleRequest(context.Background(), "quicbaas.KMS", "GetPublicKey", "bad"); err == nil {
		t.Fatal("expected error for invalid request type")
	}
}

// =============================================================================
// RevokeKey
// =============================================================================

func TestKMSHandler_RevokeKey_MapsReason(t *testing.T) {
	h, fake := newKMSAdminHandlerWithServer(t, false)

	resp, err := h.handleRequest(context.Background(), "quicbaas.KMS", "RevokeKey",
		&struct{ KeyID, Reason string }{KeyID: "k1", Reason: "compromised"})
	if err != nil {
		t.Fatalf("RevokeKey: %v", err)
	}
	if fake.lastRevokeKeyID != "k1" || fake.lastRevokeReason != "compromised" {
		t.Errorf("server saw (%q, %q), want (k1, compromised)", fake.lastRevokeKeyID, fake.lastRevokeReason)
	}
	if _, ok := resp.(*service.RevokeKeyResponse); !ok {
		t.Fatalf("unexpected response type %T", resp)
	}
}

func TestKMSHandler_RevokeKey_InvalidRequestType(t *testing.T) {
	h, _ := newKMSAdminHandlerWithServer(t, false)
	if _, err := h.handleRequest(context.Background(), "quicbaas.KMS", "RevokeKey", 42); err == nil {
		t.Fatal("expected error for invalid request type")
	}
}

// =============================================================================
// GetKeyMetadata / UpdateKeyMetadata
// =============================================================================

func TestKMSHandler_GetKeyMetadata_Success(t *testing.T) {
	h, _ := newKMSAdminHandlerWithServer(t, false)

	resp, err := h.handleRequest(context.Background(), "quicbaas.KMS", "GetKeyMetadata",
		&struct{ KeyID string }{KeyID: "k1"})
	if err != nil {
		t.Fatalf("GetKeyMetadata: %v", err)
	}
	out, ok := resp.(*service.GetKeyMetadataResponse)
	if !ok {
		t.Fatalf("unexpected response type %T", resp)
	}
	if out.Metadata == nil || out.Metadata.KeyID != "k1" {
		t.Errorf("unexpected metadata %+v", out.Metadata)
	}
}

func TestKMSHandler_GetKeyMetadata_InvalidRequestType(t *testing.T) {
	h, _ := newKMSAdminHandlerWithServer(t, false)
	if _, err := h.handleRequest(context.Background(), "quicbaas.KMS", "GetKeyMetadata", "bad"); err == nil {
		t.Fatal("expected error for invalid request type")
	}
}

func TestKMSHandler_UpdateKeyMetadata_Success(t *testing.T) {
	h, fake := newKMSAdminHandlerWithServer(t, false)

	resp, err := h.handleRequest(context.Background(), "quicbaas.KMS", "UpdateKeyMetadata",
		&kmsclient.UpdateKeyMetadataRequest{KeyID: "k1", Name: "renamed"})
	if err != nil {
		t.Fatalf("UpdateKeyMetadata: %v", err)
	}
	if fake.lastMetadataName != "renamed" {
		t.Errorf("server saw name %q, want renamed", fake.lastMetadataName)
	}
	if _, ok := resp.(*service.UpdateKeyMetadataResponse); !ok {
		t.Fatalf("unexpected response type %T", resp)
	}
}

func TestKMSHandler_UpdateKeyMetadata_InvalidRequestType(t *testing.T) {
	h, _ := newKMSAdminHandlerWithServer(t, false)
	if _, err := h.handleRequest(context.Background(), "quicbaas.KMS", "UpdateKeyMetadata", "bad"); err == nil {
		t.Fatal("expected error for invalid request type")
	}
}

// =============================================================================
// SetKeyPolicy / GetKeyPolicy
// =============================================================================

func TestKMSHandler_SetKeyPolicy_Success(t *testing.T) {
	h, fake := newKMSAdminHandlerWithServer(t, false)

	resp, err := h.handleRequest(context.Background(), "quicbaas.KMS", "SetKeyPolicy",
		&kmsclient.SetKeyPolicyRequest{
			KeyID: "k1",
			Policy: &kmsclient.KeyPolicy{
				KeyID:             "k1",
				AllowedOperations: []kmsclient.KeyUsage{kmsclient.KeyUsageEncrypt},
				UpdatedAt:         time.Now(),
			},
		})
	if err != nil {
		t.Fatalf("SetKeyPolicy: %v", err)
	}
	if fake.lastPolicyKeyID != "k1" {
		t.Errorf("server saw key %q, want k1", fake.lastPolicyKeyID)
	}
	if _, ok := resp.(*service.SetKeyPolicyResponse); !ok {
		t.Fatalf("unexpected response type %T", resp)
	}
}

func TestKMSHandler_SetKeyPolicy_InvalidRequestType(t *testing.T) {
	h, _ := newKMSAdminHandlerWithServer(t, false)
	if _, err := h.handleRequest(context.Background(), "quicbaas.KMS", "SetKeyPolicy", "bad"); err == nil {
		t.Fatal("expected error for invalid request type")
	}
}

// A negative policy version is refused before the call: converted to the
// service's uint64 it would wrap to a huge version.
func TestKMSHandler_SetKeyPolicy_RefusesNegativeVersion(t *testing.T) {
	h, _ := newKMSAdminHandlerWithServer(t, false)
	_, err := h.handleRequest(context.Background(), "quicbaas.KMS", "SetKeyPolicy",
		&kmsclient.SetKeyPolicyRequest{KeyID: "k1", Policy: &kmsclient.KeyPolicy{Version: -1}})
	if !errors.IsInvalidInput(err) || !strings.Contains(err.Error(), "policy version") {
		t.Fatalf("err = %v, want an invalid-input refusal naming the policy version", err)
	}
}

func TestKMSHandler_GetKeyPolicy_Success(t *testing.T) {
	h, _ := newKMSAdminHandlerWithServer(t, false)

	resp, err := h.handleRequest(context.Background(), "quicbaas.KMS", "GetKeyPolicy",
		&struct{ KeyID string }{KeyID: "k1"})
	if err != nil {
		t.Fatalf("GetKeyPolicy: %v", err)
	}
	out, ok := resp.(*service.GetKeyPolicyResponse)
	if !ok {
		t.Fatalf("unexpected response type %T", resp)
	}
	if out.Policy == nil || out.Policy.KeyID != "k1" {
		t.Errorf("unexpected policy %+v", out.Policy)
	}
}

func TestKMSHandler_GetKeyPolicy_InvalidRequestType(t *testing.T) {
	h, _ := newKMSAdminHandlerWithServer(t, false)
	if _, err := h.handleRequest(context.Background(), "quicbaas.KMS", "GetKeyPolicy", 1); err == nil {
		t.Fatal("expected error for invalid request type")
	}
}

// =============================================================================
// Rotation policy
// =============================================================================

func TestKMSHandler_SetKeyRotationPolicy_ConvertsFlatRequest(t *testing.T) {
	h, fake := newKMSAdminHandlerWithServer(t, false)

	// Mirror the SDK client's unexported flat setKeyRotationPolicyRequest.
	resp, err := h.handleRequest(context.Background(), "quicbaas.KMS", "SetKeyRotationPolicy",
		&struct {
			KeyID           string
			AutoRotate      bool
			IntervalSeconds int64
			RetainVersions  int
			Enabled         bool
		}{KeyID: "k1", AutoRotate: true, IntervalSeconds: 86400, RetainVersions: 3, Enabled: true})
	if err != nil {
		t.Fatalf("SetKeyRotationPolicy: %v", err)
	}
	if fake.lastRotationReq == nil || fake.lastRotationReq.KeyId != "k1" {
		t.Fatalf("server saw request %+v, want key k1", fake.lastRotationReq)
	}
	if fake.lastRotationReq.Policy == nil || fake.lastRotationReq.Policy.IntervalSeconds != 86400 ||
		!fake.lastRotationReq.Policy.AutoRotate {
		t.Errorf("unexpected policy %+v", fake.lastRotationReq.Policy)
	}
	out, ok := resp.(*service.SetKeyRotationPolicyResponse)
	if !ok {
		t.Fatalf("unexpected response type %T", resp)
	}
	if out.Policy == nil || out.Policy.Interval != 24*time.Hour {
		t.Errorf("unexpected response policy %+v", out.Policy)
	}
}

func TestKMSHandler_SetKeyRotationPolicy_InvalidRequestType(t *testing.T) {
	h, _ := newKMSAdminHandlerWithServer(t, false)
	if _, err := h.handleRequest(context.Background(), "quicbaas.KMS", "SetKeyRotationPolicy", "bad"); err == nil {
		t.Fatal("expected error for invalid request type")
	}
}

func TestKMSHandler_GetKeyRotationPolicy_Success(t *testing.T) {
	h, _ := newKMSAdminHandlerWithServer(t, false)

	resp, err := h.handleRequest(context.Background(), "quicbaas.KMS", "GetKeyRotationPolicy",
		&struct{ KeyID string }{KeyID: "k1"})
	if err != nil {
		t.Fatalf("GetKeyRotationPolicy: %v", err)
	}
	out, ok := resp.(*service.GetKeyRotationPolicyResponse)
	if !ok {
		t.Fatalf("unexpected response type %T", resp)
	}
	if out.Policy == nil || out.Policy.Interval != time.Hour {
		t.Errorf("unexpected policy %+v", out.Policy)
	}
}

func TestKMSHandler_GetKeyRotationPolicy_InvalidRequestType(t *testing.T) {
	h, _ := newKMSAdminHandlerWithServer(t, false)
	if _, err := h.handleRequest(context.Background(), "quicbaas.KMS", "GetKeyRotationPolicy", 1); err == nil {
		t.Fatal("expected error for invalid request type")
	}
}

func TestKMSHandler_DeleteKeyRotationPolicy_Success(t *testing.T) {
	h, _ := newKMSAdminHandlerWithServer(t, false)

	resp, err := h.handleRequest(context.Background(), "quicbaas.KMS", "DeleteKeyRotationPolicy",
		&struct{ KeyID string }{KeyID: "k1"})
	if err != nil {
		t.Fatalf("DeleteKeyRotationPolicy: %v", err)
	}
	out, ok := resp.(*service.DeleteKeyRotationPolicyResponse)
	if !ok {
		t.Fatalf("unexpected response type %T", resp)
	}
	if out.KeyID != "k1" {
		t.Errorf("unexpected response %+v", out)
	}
}

func TestKMSHandler_DeleteKeyRotationPolicy_InvalidRequestType(t *testing.T) {
	h, _ := newKMSAdminHandlerWithServer(t, false)
	if _, err := h.handleRequest(context.Background(), "quicbaas.KMS", "DeleteKeyRotationPolicy", 1); err == nil {
		t.Fatal("expected error for invalid request type")
	}
}

// TestKMSHandler_ServerError_KeyAdminVerbs asserts server-side failures
// propagate as errors for every new verb.
func TestKMSHandler_ServerError_KeyAdminVerbs(t *testing.T) {
	h, _ := newKMSAdminHandlerWithServer(t, true)
	cases := []struct {
		method string
		req    interface{}
	}{
		{"GetPublicKey", &struct{ KeyID string }{KeyID: "k1"}},
		{"RevokeKey", &struct{ KeyID, Reason string }{KeyID: "k1"}},
		{"GetKeyMetadata", &struct{ KeyID string }{KeyID: "k1"}},
		{"UpdateKeyMetadata", &kmsclient.UpdateKeyMetadataRequest{KeyID: "k1"}},
		{"SetKeyPolicy", &kmsclient.SetKeyPolicyRequest{KeyID: "k1", Policy: &kmsclient.KeyPolicy{}}},
		{"GetKeyPolicy", &struct{ KeyID string }{KeyID: "k1"}},
		{"GetKeyRotationPolicy", &struct{ KeyID string }{KeyID: "k1"}},
		{"DeleteKeyRotationPolicy", &struct{ KeyID string }{KeyID: "k1"}},
	}
	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			if _, err := h.handleRequest(context.Background(), "quicbaas.KMS", tc.method, tc.req); err == nil {
				t.Fatalf("expected server error for %s", tc.method)
			}
		})
	}
}
