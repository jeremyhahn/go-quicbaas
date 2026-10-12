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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// inviteIAMServer answers CreateUser the way the server does for a pending
// account, remembering the request it was sent.
type inviteIAMServer struct {
	iampb.UnimplementedIAMServiceServer
	got *iampb.CreateUserRequest
}

var inviteExpiry = time.Unix(1700086400, 0).UTC()

func (s *inviteIAMServer) CreateUser(_ context.Context, req *iampb.CreateUserRequest) (*iampb.User, error) {
	s.got = req
	return &iampb.User{
		Id: "u1", TenantId: req.TenantId, Email: req.Email, Lifecycle: "pending",
		InviteToken:     "tok",
		InviteUrl:       "https://id.acme.test/ui/#/invite/acme/tok",
		InviteExpiresAt: timestamppb.New(inviteExpiry),
	}, nil
}

// TestHandleCreateUser_CarriesTheInvite proves the requested expiry reaches
// the server and the invite it issues reaches the SDK caller.
func TestHandleCreateUser_CarriesTheInvite(t *testing.T) {
	backend := &inviteIAMServer{}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		iampb.RegisterIAMServiceServer(s, backend)
	})
	h := newIAMHandler(srv.dial(t))

	out, err := h.handleRequest(context.Background(), "quicbaas.IAM.User", "Create", map[string]interface{}{
		"tenant_id": "acme",
		"request":   map[string]interface{}{"email": "pat@acme.test", "invite_expiry_hours": 48},
	})
	require.NoError(t, err)

	require.NotNil(t, backend.got)
	assert.Equal(t, int32(48), backend.got.InviteExpiryHours)

	user, ok := out.(*iam.User)
	require.True(t, ok, "unexpected result type %T", out)
	assert.Equal(t, "tok", user.InviteToken)
	assert.Equal(t, "https://id.acme.test/ui/#/invite/acme/tok", user.InviteURL)
	require.NotNil(t, user.InviteExpiresAt)
	assert.True(t, inviteExpiry.Equal(*user.InviteExpiresAt))
}

// TestConvertProtoUserToSDK_NoInvite proves a user with no invite carries
// none, rather than a zero expiry that reads as already expired.
func TestConvertProtoUserToSDK_NoInvite(t *testing.T) {
	user := convertProtoUserToSDK(&iampb.User{Id: "u1", Lifecycle: "active"})
	assert.Empty(t, user.InviteToken)
	assert.Empty(t, user.InviteURL)
	assert.Nil(t, user.InviteExpiresAt)
}
