package apimodule

import (
	"context"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// argumentIAM is a grpcapi.IAMService whose UpdateUser records that it ran;
// every other method is the nil embedded interface.
type argumentIAM struct {
	grpcapi.IAMService
	updated bool
}

func (s *argumentIAM) UpdateUser(_ context.Context, user *iam.User) (*iam.User, error) {
	s.updated = true
	return &iam.User{ID: user.ID, TenantID: user.TenantID}, nil
}

func (s *argumentIAM) CreateUser(context.Context, *iam.User, time.Duration) (*iam.User, string, *iam.InviteRecord, error) {
	panic("not used")
}

// grantAuthorizer is a go-qrdb authorizer that grants the permissions in its set.
type grantAuthorizer map[string]bool

func (g grantAuthorizer) Authorize(_ context.Context, _ *qrdbsdk.Identity, resource, action string) error {
	if g[resource+":"+action] {
		return nil
	}
	return &qrdbsdk.PermissionDeniedError{}
}

// TestModule_IAMUpdateUserWithRolesNeedsRolesAssign drives the unified
// listener, which enforces only UpdateUser's descriptor permission: a role
// write must still be refused without roles:assign, by the shared handler.
func TestModule_IAMUpdateUserWithRolesNeedsRolesAssign(t *testing.T) {
	fake := &argumentIAM{}
	srv := grpcapi.NewIAMServer(fake, testLogger(t))
	grants := grantAuthorizer{"users:update": true}
	srv.SetArgumentAuthorizer(func() qrdbsdk.Authorizer { return grants })

	m, err := NewModule(func() service.QuicBaaS { return &mocks.MockQuicBaaS{} }, testLogger(t),
		WithHostGRPCServers(func() (HostGRPCServers, error) {
			return HostGRPCServers{IAM: srv}, nil
		}))
	require.NoError(t, err)
	client := iampb.NewIAMServiceClient(startAuthenticatedModule(t, m, "tenant-a"))
	req := &iampb.UpdateUserRequest{TenantId: "tenant-a", UserId: "7", Roles: []string{"admin"}}

	_, err = client.UpdateUser(context.Background(), req)
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err), "got %v", err)
	assert.False(t, fake.updated, "the service must not run without roles:assign")

	grants["roles:assign"] = true
	_, err = client.UpdateUser(context.Background(), req)
	require.NoError(t, err, "with roles:assign the same call passes the tenant gate")
	assert.True(t, fake.updated)
}
