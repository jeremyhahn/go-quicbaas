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

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// refusingLoginClient is a LoginClient whose FinishLogin fails the way qrdb
// does when the session issuer refuses a non-system tenant login.
type refusingLoginClient struct {
	fakeLoginClient
}

func (r *refusingLoginClient) FinishLogin(_ context.Context, _ qrdbtransport.FinishLoginRequest) (*qrdbtransport.FinishLoginResponse, error) {
	return nil, service.RefuseTenantLogin("acme")
}

// TestLoginServer_FinishLogin_TenantRefusedIsFailedPrecondition checks that a
// non-system tenant login refused at token issuance answers FailedPrecondition
// with the refusal's guidance, not Internal. go-quicbaas #1816 removes the
// refusal.
func TestLoginServer_FinishLogin_TenantRefusedIsFailedPrecondition(t *testing.T) {
	srv := NewLoginServer(service.NewLoginService(&refusingLoginClient{}), nil)

	_, err := srv.FinishLogin(context.Background(), &pb.FinishLoginRequest{
		Type:      string(qrdbtransport.FactorTypePIV),
		LookupKey: []byte("piv-slot-hash"),
		Signature: []byte("sig"),
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("FinishLogin code = %s (%v), want FailedPrecondition", status.Code(err), err)
	}
	if got, want := status.Convert(err).Message(), (&sdkerrors.TenantLoginRefusedError{}).Error(); got != want {
		t.Errorf("FinishLogin message = %q, want %q", got, want)
	}
}
