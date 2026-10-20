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
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// lifecycleIAMServer records the UpdateUser request it is sent, or refuses it
// with refuse when that is set.
type lifecycleIAMServer struct {
	iampb.UnimplementedIAMServiceServer
	refuse   error
	updated  *iampb.UpdateUserRequest
	approved *iampb.ApproveUserRequest
}

func (s *lifecycleIAMServer) ApproveUser(_ context.Context, req *iampb.ApproveUserRequest) (*iampb.ApproveUserResponse, error) {
	if s.refuse != nil {
		return nil, s.refuse
	}
	s.approved = req
	return &iampb.ApproveUserResponse{User: &iampb.User{Id: req.GetUserId(), TenantId: req.GetTenantId(), Lifecycle: "active"}}, nil
}

func (s *lifecycleIAMServer) UpdateUser(_ context.Context, req *iampb.UpdateUserRequest) (*iampb.User, error) {
	if s.refuse != nil {
		return nil, s.refuse
	}
	s.updated = req
	return &iampb.User{Id: req.GetUserId(), TenantId: req.GetTenantId(), Lifecycle: req.GetLifecycle()}, nil
}

// iamParityServer is a bufconn server carrying the IAM services. calls counts
// every RPC that reaches it, registered or not, so a test can prove that a
// verb was refused in the SDK rather than sent to the server.
type iamParityServer struct {
	iam   *lifecycleIAMServer
	calls atomic.Int32
}

func newIAMParityServer(t *testing.T) (*iamParityServer, *Transport) {
	t.Helper()
	p := &iamParityServer{iam: &lifecycleIAMServer{}}
	count := func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		p.calls.Add(1)
		return handler(ctx, req)
	}
	unknown := func(any, grpc.ServerStream) error {
		p.calls.Add(1)
		return status.Error(codes.Unimplemented, "unknown method")
	}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		iampb.RegisterIAMServiceServer(s, p.iam)
		iampb.RegisterAuditServiceServer(s, &iampb.UnimplementedAuditServiceServer{})
		iampb.RegisterSessionServiceServer(s, &iampb.UnimplementedSessionServiceServer{})
	}, grpc.UnaryInterceptor(count), grpc.UnknownServiceHandler(unknown))
	return p, &Transport{cfg: &transport.Config{Timeout: 5 * time.Second}, conn: srv.dial(t)}
}

// Approve goes through ApproveUser, which carries users:enable, and never
// through UpdateUser, which no grant authorizes.
func TestIAMUserApprove_CallsApproveUser(t *testing.T) {
	server, tr := newIAMParityServer(t)

	err := iam.NewUserClient(tr).Approve(context.Background(), "acme", "u-1")
	require.NoError(t, err)

	want := &iampb.ApproveUserRequest{TenantId: "acme", UserId: "u-1"}
	assert.True(t, proto.Equal(want, server.iam.approved), "ApproveUser request = %v, want %v", server.iam.approved, want)
	assert.Nil(t, server.iam.updated, "Approve must not call UpdateUser")
}

func TestIAMUserApprove_ServerErrorIsTyped(t *testing.T) {
	server, tr := newIAMParityServer(t)
	server.iam.refuse = status.Error(codes.NotFound, "user not found")

	err := iam.NewUserClient(tr).Approve(context.Background(), "acme", "missing")
	require.Error(t, err)
	assert.True(t, sdkerrors.IsNotFound(err), "want a typed not-found error, got %v", err)
}

// TestIAMUnmappedVerbs_RefusedWithoutAServerCall covers the verbs that have no
// server RPC with the same meaning. Each must fail with the handler's typed
// unimplemented error before anything is sent.
func TestIAMUnmappedVerbs_RefusedWithoutAServerCall(t *testing.T) {
	ctx := context.Background()
	verbs := map[string]func(transport.Transport) error{
		// ListUsers is no lookup: the server drops its email filter.
		"User.GetByEmail": func(tr transport.Transport) error {
			_, err := iam.NewUserClient(tr).GetByEmail(ctx, "acme", "a@example.com")
			return err
		},
		// No RPC selects a user by username.
		"User.FindByUsername": func(tr transport.Transport) error {
			_, err := iam.NewUserClient(tr).FindByUsername(ctx, "acme", "alice")
			return err
		},
		// UpdateUser could disable the account but has no field for the reason.
		"User.Reject": func(tr transport.Transport) error {
			return iam.NewUserClient(tr).Reject(ctx, "acme", "u-1", "duplicate account")
		},
		// AuditService has no delete RPC.
		"Audit.Delete": func(tr transport.Transport) error {
			return iam.NewAuditClient(tr).Delete(ctx, &iam.AuditDeleteRequest{
				TenantID: "acme", Before: time.Now(), Confirm: true,
			})
		},
		// Waiting on the coordinator ruling #1564.
		"Policy.GetEffectivePermissions": func(tr transport.Transport) error {
			_, err := iam.NewPolicyClient(tr).GetEffectivePermissions(ctx, "acme", &iam.EffectivePermissionsRequest{
				Subject: iam.Subject{ID: "u-1"},
			})
			return err
		},
		// Waiting on the coordinator ruling #1564.
		"Session.Touch": func(tr transport.Transport) error {
			return iam.NewSessionClient(tr).Touch(ctx, "s-1")
		},
	}
	for name, call := range verbs {
		t.Run(name, func(t *testing.T) {
			server, tr := newIAMParityServer(t)

			err := call(tr)
			require.Error(t, err)
			assert.True(t, sdkerrors.IsUnimplemented(err), "want a typed unimplemented error, got %v", err)
			assert.Zero(t, server.calls.Load(), "the verb reached the server")
		})
	}
}
