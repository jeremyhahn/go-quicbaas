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
	stderrors "errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/iamtest"
)

const testInviteOrigin = "https://id.acme.test"

// newInviteGRPCStack is newRealIAMGRPCStack with invites wired in, as the
// server wires them, and activation links built under testInviteOrigin.
func newInviteGRPCStack(t *testing.T, invites iam.InviteStore) (*IAMServer, *iam.MemoryStorage) {
	t.Helper()
	srv, mem := newRealIAMGRPCStack(t)
	srv.iamService.(*IAMServiceAdapter).SetInviteStore(invites)
	srv.SetInviteOrigin(testInviteOrigin)
	return srv, mem
}

// failingInviteStore refuses to issue invites.
type failingInviteStore struct {
	iam.InviteStore
	err error
}

func (f *failingInviteStore) Create(context.Context, string, uint64, time.Duration) (string, *iam.InviteRecord, error) {
	return "", nil, f.err
}

// TestGRPC_CreateUserReturnsARedeemableInvite proves gRPC hands back the
// activation invite REST does: a pending account is useless without it, and
// the token is returned only on this call.
func TestGRPC_CreateUserReturnsARedeemableInvite(t *testing.T) {
	invites := iamtest.NewMemoryInviteStore()
	srv, _ := newInviteGRPCStack(t, invites)
	ctx := callerContext("acme")

	created, err := srv.CreateUser(ctx, &iampb.CreateUserRequest{
		TenantId:          "acme",
		Email:             "pat@acme.test",
		Username:          "pat",
		InviteExpiryHours: 48,
	})
	require.NoError(t, err)

	assert.Equal(t, string(iam.LifecyclePending), created.Lifecycle)
	require.NotEmpty(t, created.InviteToken, "a pending user came back without its invite")
	assert.Equal(t, iam.InviteURL(testInviteOrigin, "acme", created.InviteToken), created.InviteUrl)
	require.NotNil(t, created.InviteExpiresAt)
	assert.WithinDuration(t, time.Now().Add(48*time.Hour), created.InviteExpiresAt.AsTime(), time.Minute)

	record, err := invites.Redeem(ctx, "acme", created.InviteToken)
	require.NoError(t, err, "the returned token does not redeem")
	assert.Equal(t, created.Id, strconv.FormatUint(record.UserID, 10))
}

// TestGRPC_CreateActiveUserCarriesNoInvite proves an account with nothing to
// activate gets no token.
func TestGRPC_CreateActiveUserCarriesNoInvite(t *testing.T) {
	srv, _ := newInviteGRPCStack(t, iamtest.NewMemoryInviteStore())

	created, err := srv.CreateUser(callerContext("acme"), &iampb.CreateUserRequest{
		TenantId:  "acme",
		Email:     "quinn@acme.test",
		Lifecycle: string(iam.LifecycleActive),
	})
	require.NoError(t, err)
	assert.Empty(t, created.InviteToken)
	assert.Empty(t, created.InviteUrl)
	assert.Nil(t, created.InviteExpiresAt)
}

// TestGRPC_CreateUserInviteFailureLeavesNoUser proves a create whose invite
// cannot be issued fails and leaves no unactivatable pending user behind.
func TestGRPC_CreateUserInviteFailureLeavesNoUser(t *testing.T) {
	srv, mem := newInviteGRPCStack(t, &failingInviteStore{err: stderrors.New("invite store unreachable")})
	ctx := callerContext("acme")

	_, err := srv.CreateUser(ctx, &iampb.CreateUserRequest{TenantId: "acme", Email: "rae@acme.test"})
	require.Error(t, err)

	_, getErr := mem.GetUserByEmail(ctx, "acme", "rae@acme.test")
	assert.Error(t, getErr, "the user survived a create whose invite could not be issued")
}

// TestGRPC_CreateUserWithoutInviteStoreIsUnavailable proves a node with no
// invite store refuses a pending create as Unavailable, before writing.
func TestGRPC_CreateUserWithoutInviteStoreIsUnavailable(t *testing.T) {
	srv, mem := newRealIAMGRPCStack(t)
	ctx := callerContext("acme")

	_, err := srv.CreateUser(ctx, &iampb.CreateUserRequest{TenantId: "acme", Email: "sam@acme.test"})
	assert.Equal(t, codes.Unavailable, status.Code(err))

	_, getErr := mem.GetUserByEmail(ctx, "acme", "sam@acme.test")
	assert.Error(t, getErr, "a pending user was written with no way to activate it")
}
