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
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
)

type inviteActivationIAMServer struct {
	iampb.UnimplementedIAMServiceServer

	gotGet    *iampb.GetInviteRequest
	gotBegin  *iampb.ActivateInviteRequest
	gotFinish *iampb.ActivateFIDO2FinishRequest
	gotPIV    *iampb.ActivatePIVRequest
	expires   time.Time
}

func (s *inviteActivationIAMServer) GetInvite(_ context.Context, req *iampb.GetInviteRequest) (*iampb.GetInviteResponse, error) {
	s.gotGet = req
	if req.Token == "spent" {
		return nil, status.Error(codes.NotFound, "invite link is not valid")
	}
	return &iampb.GetInviteResponse{
		UserId: "u1", Email: "a@example.com", AuthMethods: []string{"fido2"},
		ExpiresAt: timestamppb.New(s.expires),
	}, nil
}

func (s *inviteActivationIAMServer) ActivateFIDO2Begin(_ context.Context, req *iampb.ActivateInviteRequest) (*iampb.ActivateFIDO2BeginResponse, error) {
	s.gotBegin = req
	return &iampb.ActivateFIDO2BeginResponse{OptionsJson: `{"publicKey":{"challenge":"c"}}`}, nil
}

func (s *inviteActivationIAMServer) ActivateFIDO2Finish(_ context.Context, req *iampb.ActivateFIDO2FinishRequest) (*iampb.ActivateInviteResponse, error) {
	s.gotFinish = req
	return &iampb.ActivateInviteResponse{UserId: "u1", Message: "activation successful"}, nil
}

func (s *inviteActivationIAMServer) ActivatePIV(_ context.Context, req *iampb.ActivatePIVRequest) (*iampb.ActivateInviteResponse, error) {
	s.gotPIV = req
	return &iampb.ActivateInviteResponse{UserId: "u1", Message: "PIV activation successful"}, nil
}

func newInviteHandler(t *testing.T) (*iamHandler, *inviteActivationIAMServer) {
	t.Helper()
	backend := &inviteActivationIAMServer{expires: time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		iampb.RegisterIAMServiceServer(s, backend)
	})
	return newIAMHandler(srv.dial(t)), backend
}

// TestInviteHandlerOverGRPC drives every invite verb through the IAMService
// RPCs and asserts the response carries the JSON names the SDK invite client
// decodes, the same contract REST and the embedded transport produce.
func TestInviteHandlerOverGRPC(t *testing.T) {
	h, backend := newInviteHandler(t)
	ctx := context.Background()
	scope := func(extra map[string]interface{}) map[string]interface{} {
		req := map[string]interface{}{"tenant_id": "t1", "token": "tok"}
		for k, v := range extra {
			req[k] = v
		}
		return req
	}

	resp, err := h.handleRequest(ctx, iamInviteServicePrefix, "GetInvite", scope(nil))
	if err != nil {
		t.Fatalf("GetInvite: %v", err)
	}
	if backend.gotGet.TenantId != "t1" || backend.gotGet.Token != "tok" {
		t.Errorf("server received %+v, want t1/tok", backend.gotGet)
	}
	meta := resp.(map[string]interface{})
	if meta["user_id"] != "u1" || meta["expires_at"] != backend.expires {
		t.Errorf("GetInvite = %#v", meta)
	}

	resp, err = h.handleRequest(ctx, iamInviteServicePrefix, "ActivateFIDO2Begin", scope(nil))
	if err != nil {
		t.Fatalf("ActivateFIDO2Begin: %v", err)
	}
	if _, ok := resp.(map[string]interface{})["publicKey"]; !ok {
		t.Errorf("begin = %#v, want the decoded creation options", resp)
	}

	if _, err := h.handleRequest(ctx, iamInviteServicePrefix, "ActivateFIDO2Finish",
		scope(map[string]interface{}{"credential": map[string]interface{}{"id": "c"}})); err != nil {
		t.Fatalf("ActivateFIDO2Finish: %v", err)
	}
	if backend.gotFinish.CredentialJson != `{"id":"c"}` {
		t.Errorf("credential_json = %q", backend.gotFinish.CredentialJson)
	}

	resp, err = h.handleRequest(ctx, iamInviteServicePrefix, "ActivatePIV",
		scope(map[string]interface{}{"cert_pem": "PEM"}))
	if err != nil {
		t.Fatalf("ActivatePIV: %v", err)
	}
	if backend.gotPIV.CertPem != "PEM" || resp.(map[string]interface{})["message"] != "PIV activation successful" {
		t.Errorf("PIV: server got %+v, response %#v", backend.gotPIV, resp)
	}
}

// TestInviteHandlerErrorsOverGRPC fails closed on a missing scope, an unknown
// verb, and a server-side rejection.
func TestInviteHandlerErrorsOverGRPC(t *testing.T) {
	h, backend := newInviteHandler(t)
	ctx := context.Background()

	cases := map[string]struct {
		method string
		req    map[string]interface{}
	}{
		"MissingTenant": {"GetInvite", map[string]interface{}{"token": "tok"}},
		"MissingToken":  {"GetInvite", map[string]interface{}{"tenant_id": "t1"}},
		"UnknownVerb":   {"Frobnicate", map[string]interface{}{"tenant_id": "t1", "token": "tok"}},
		"SpentInvite":   {"GetInvite", map[string]interface{}{"tenant_id": "t1", "token": "spent"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := h.handleRequest(ctx, iamInviteServicePrefix, tc.method, tc.req); err == nil {
				t.Error("expected an error")
			}
		})
	}
	if backend.gotGet != nil && backend.gotGet.Token != "spent" {
		t.Errorf("an incomplete request reached the server: %+v", backend.gotGet)
	}
}
