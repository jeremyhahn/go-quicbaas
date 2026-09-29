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

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// daemonCtx is a context carrying an enrolled daemon of tenantID.
func daemonCtx(tenantID string) context.Context {
	return auth.SubjectIntoContext(context.Background(), interfaces.Subject{
		ID: "agent-" + tenantID, TenantID: tenantID, Authenticated: true,
	})
}

func recordingReservationService(reserved **service.ReserveAccountSessionRequest, released **service.ReleaseAccountSessionRequest) *mocks.MockAccountService {
	return &mocks.MockAccountService{
		ReserveAccountSessionFunc: func(_ context.Context, req *service.ReserveAccountSessionRequest) (*service.ReserveAccountSessionResponse, error) {
			*reserved = req
			return &service.ReserveAccountSessionResponse{ReservationID: "r1", Timestamp: time.Now().UTC()}, nil
		},
		ReleaseAccountSessionFunc: func(_ context.Context, req *service.ReleaseAccountSessionRequest) (*service.ReleaseAccountSessionResponse, error) {
			*released = req
			return &service.ReleaseAccountSessionResponse{ReservationID: req.ReservationID, Timestamp: time.Now().UTC()}, nil
		},
	}
}

func TestAccountServer_ReserveAndReleaseSession(t *testing.T) {
	var reserved *service.ReserveAccountSessionRequest
	var released *service.ReleaseAccountSessionRequest
	server := newAccountServer(recordingReservationService(&reserved, &released))
	ctx := daemonCtx("tenant-a")

	two := int32(2)
	resp, err := server.ReserveAccountSession(ctx, &pb.ReserveAccountSessionRequest{AccountId: "a", ExpectedLive: &two})
	if err != nil || resp.GetReservationId() != "r1" {
		t.Fatalf("ReserveAccountSession = (%v, %v)", resp, err)
	}
	if reserved.TenantID != "tenant-a" || reserved.ExpectedLive == nil || *reserved.ExpectedLive != 2 {
		t.Errorf("service asked %+v, want the principal's tenant and the expected count", reserved)
	}
	rel, err := server.ReleaseAccountSession(ctx, &pb.ReleaseAccountSessionRequest{AccountId: "a", ReservationId: "r1"})
	if err != nil || rel.GetReservationId() != "r1" || released.TenantID != "tenant-a" {
		t.Fatalf("ReleaseAccountSession = (%v, %v), asked %+v", rel, err, released)
	}
}

// TestAccountServer_ReservationsAreBoundToTheCallersTenant is the round-3 P1:
// a principal of tenant A cannot reserve or release tenant B's accounts, and an
// unauthenticated caller cannot use the verbs at all.
func TestAccountServer_ReservationsAreBoundToTheCallersTenant(t *testing.T) {
	var reserved *service.ReserveAccountSessionRequest
	var released *service.ReleaseAccountSessionRequest
	server := newAccountServer(recordingReservationService(&reserved, &released))

	if _, err := server.ReserveAccountSession(daemonCtx("tenant-a"), &pb.ReserveAccountSessionRequest{TenantId: "tenant-b", AccountId: "b-acct"}); status.Code(err) != codes.PermissionDenied {
		t.Errorf("cross-tenant reserve: code = %v, want PermissionDenied", status.Code(err))
	}
	if _, err := server.ReleaseAccountSession(daemonCtx("tenant-a"), &pb.ReleaseAccountSessionRequest{TenantId: "tenant-b", AccountId: "b-acct", ReservationId: "r"}); status.Code(err) != codes.PermissionDenied {
		t.Errorf("cross-tenant release: code = %v, want PermissionDenied", status.Code(err))
	}
	if reserved != nil || released != nil {
		t.Fatalf("a refused cross-tenant call reached the service: %+v %+v", reserved, released)
	}
	if _, err := server.ReserveAccountSession(context.Background(), &pb.ReserveAccountSessionRequest{AccountId: "a"}); status.Code(err) != codes.Unauthenticated {
		t.Errorf("unauthenticated reserve: code = %v, want Unauthenticated", status.Code(err))
	}
	if _, err := server.ReleaseAccountSession(context.Background(), &pb.ReleaseAccountSessionRequest{AccountId: "a", ReservationId: "r"}); status.Code(err) != codes.Unauthenticated {
		t.Errorf("unauthenticated release: code = %v, want Unauthenticated", status.Code(err))
	}
	unbound := auth.SubjectIntoContext(context.Background(), interfaces.Subject{ID: "x", Authenticated: true})
	if _, err := server.ReserveAccountSession(unbound, &pb.ReserveAccountSessionRequest{AccountId: "a"}); status.Code(err) != codes.InvalidArgument {
		t.Errorf("tenantless reserve: code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestAccountServer_ReservationRefusalsMapToTheirCodes(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code codes.Code
	}{
		{&orchestration.AccountsSaturatedError{TenantID: "t", Candidates: 1}, codes.ResourceExhausted},
		{&orchestration.AccountSessionCountChangedError{AccountID: "a", Expected: 0, Actual: 1}, codes.Aborted},
	} {
		refusal := tc.err
		server := newAccountServer(&mocks.MockAccountService{
			ReserveAccountSessionFunc: func(context.Context, *service.ReserveAccountSessionRequest) (*service.ReserveAccountSessionResponse, error) {
				return nil, refusal
			},
		})
		_, err := server.ReserveAccountSession(daemonCtx("t"), &pb.ReserveAccountSessionRequest{AccountId: "a"})
		if status.Code(err) != tc.code {
			t.Errorf("%T: code = %v, want %v", refusal, status.Code(err), tc.code)
		}
	}
}

func TestAccountServer_ReservationRejectsBadInput(t *testing.T) {
	server := newAccountServer(&mocks.MockAccountService{})
	ctx := daemonCtx("t")
	for name, call := range map[string]func() error{
		"reserve nil": func() error { _, err := server.ReserveAccountSession(ctx, nil); return err },
		"reserve no account": func() error {
			_, err := server.ReserveAccountSession(ctx, &pb.ReserveAccountSessionRequest{})
			return err
		},
		"release nil": func() error { _, err := server.ReleaseAccountSession(ctx, nil); return err },
		"release no reservation": func() error {
			_, err := server.ReleaseAccountSession(ctx, &pb.ReleaseAccountSessionRequest{AccountId: "a"})
			return err
		},
	} {
		if status.Code(call()) != codes.InvalidArgument {
			t.Errorf("%s: want InvalidArgument", name)
		}
	}
}
