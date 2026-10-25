package qrdbservice

import (
	"context"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// userRecorder is a user client that records whether each call arrived bound
// to the principal. Calls it does not declare panic on the nil embedded client.
type userRecorder struct {
	transport.UserClient
	recorder
}

func (u *userRecorder) GetUser(ctx context.Context, id uint64) (*transport.User, error) {
	u.see(ctx)
	return &transport.User{ID: id}, nil
}

func (u *userRecorder) GetUserByUsername(ctx context.Context, tenantID, username string) (*transport.User, error) {
	u.see(ctx)
	return &transport.User{TenantID: tenantID, Username: username}, nil
}

func (u *userRecorder) GetUserByEmail(ctx context.Context, tenantID, email string) (*transport.User, error) {
	u.see(ctx)
	return &transport.User{TenantID: tenantID, Email: email}, nil
}

func (u *userRecorder) UpdateUser(ctx context.Context, req *transport.UpdateUserRequest) (*transport.User, error) {
	u.see(ctx)
	return &transport.User{ID: req.ID, Name: req.Name}, nil
}

func userReadCalls() map[string]func(transport.UserClient) error {
	ctx := context.Background()
	return map[string]func(transport.UserClient) error{
		"GetUser": func(c transport.UserClient) error {
			_, err := c.GetUser(ctx, 7)
			return err
		},
		"GetUserByUsername": func(c transport.UserClient) error {
			_, err := c.GetUserByUsername(ctx, qrdbsdk.SystemTenantID, "alice")
			return err
		},
		"GetUserByEmail": func(c transport.UserClient) error {
			_, err := c.GetUserByEmail(ctx, qrdbsdk.SystemTenantID, "alice@example.com")
			return err
		},
	}
}

func TestUserReads_BindsEveryAccountRead(t *testing.T) {
	for name, call := range userReadCalls() {
		t.Run(name, func(t *testing.T) {
			rec := &userRecorder{}
			require.NoError(t, call(UserReads(fakeBinder{}, rec)))
			assert.Equal(t, 1, rec.bound)
		})
	}
}

func TestUserReads_UnboundContextNeverReachesTheNode(t *testing.T) {
	for name, call := range userReadCalls() {
		t.Run(name, func(t *testing.T) {
			rec := &userRecorder{}
			assert.ErrorIs(t, call(UserReads(fakeBinder{err: errBind}, rec)), errBind)
			assert.Zero(t, rec.calls)
		})
	}
}

// TestUserReads_LeavesWritesToTheCaller pins that only reads are lent the
// principal: a write runs as whoever made it.
func TestUserReads_LeavesWritesToTheCaller(t *testing.T) {
	rec := &userRecorder{}
	_, err := UserReads(fakeBinder{}, rec).UpdateUser(context.Background(), &transport.UpdateUserRequest{ID: 7, Name: "Alice"})
	require.NoError(t, err)
	assert.Equal(t, 1, rec.calls)
	assert.Zero(t, rec.bound)
}

// TestGrants_IncludeTheUsersRead pins the grant an unauthenticated WebAuthn
// ceremony's account lookup needs: without users:read the bound read is
// still refused.
func TestGrants_IncludeTheUsersRead(t *testing.T) {
	perms, err := Grants()
	require.NoError(t, err)
	assert.Contains(t, perms, qrdbsdk.PermissionUsersRead)
	assert.NotContains(t, perms, qrdbsdk.PermissionUsersUpdate)
}
