package grpc

import (
	"context"
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

// countingInviteStore records every invite Create reaches, so a refused
// request can be proven to have issued none.
type countingInviteStore struct {
	iam.InviteStore
	creates int
}

func (s *countingInviteStore) Create(ctx context.Context, tenantID string, userID uint64, ttl time.Duration) (string, *iam.InviteRecord, error) {
	s.creates++
	return s.InviteStore.Create(ctx, tenantID, userID, ttl)
}

// An invitation is created in the authenticated principal's tenant: a body
// that names another tenant is refused before any user or invite is written
// (#1832, #1836, owner ruling 133).
func TestGRPC_CreateInviteTenantComesFromPrincipalNotBody(t *testing.T) {
	invites := &countingInviteStore{InviteStore: iamtest.NewMemoryInviteStore()}
	srv, mem := newInviteGRPCStack(t, invites)
	ctx := callerContext("acme")
	// globex exists, so a refusal is the principal check and not a missing tenant.
	_, err := iam.NewDefaultTenantManager(mem).CreateTenant(callerContext("globex"),
		iam.CreateTenantOptions{ID: "globex", Name: "Globex"})
	require.NoError(t, err)

	_, err = srv.CreateUser(ctx, &iampb.CreateUserRequest{
		TenantId: "globex",
		Email:    "eve@globex.test",
		Username: "eve",
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err), "a body tenant other than the caller's was not refused: %v", err)
	assert.Zero(t, invites.creates, "a refused create issued an invite")

	for _, tenant := range []string{"globex", "acme"} {
		_, getErr := mem.GetUserByEmail(context.Background(), tenant, "eve@globex.test")
		assert.Error(t, getErr, "a refused create wrote a user in tenant %s", tenant)
	}

	// The principal's own tenant, named or omitted, is the tenant the invite
	// and its user are created in.
	created, err := srv.CreateUser(ctx, &iampb.CreateUserRequest{TenantId: "acme", Email: "pat@acme.test", Username: "pat"})
	require.NoError(t, err)
	require.NotEmpty(t, created.InviteToken)
	assert.Equal(t, 1, invites.creates)
	record, err := invites.Lookup(ctx, "acme", created.InviteToken)
	require.NoError(t, err)
	assert.Equal(t, "acme", record.TenantID)
	_, err = invites.Lookup(ctx, "globex", created.InviteToken)
	assert.Error(t, err, "acme's invite resolved under another tenant")
}
