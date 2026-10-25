//go:build !integration

package server

import (
	"context"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/qrdbadapter"
)

// serviceContext is a context bound to the node's service principal, the
// authenticated caller that creates the fixture's accounts and roles.
func (f *passkeyFixture) serviceContext(t *testing.T) context.Context {
	t.Helper()
	ctx, err := f.srv.ServiceContext(context.Background())
	require.NoError(t, err)
	return ctx
}

// systemRoleID registers the system tenant's built-in role named name through
// the node's own role service, as first boot does, and returns its ID resolved
// by name through the node's service principal, the way the administrator
// check reads it.
func (f *passkeyFixture) systemRoleID(ctx context.Context, t *testing.T, name string) string {
	t.Helper()
	svcCtx := f.serviceContext(t)
	var err error
	for _, r := range qrdbsdk.BuiltInRoles(qrdbsdk.SystemTenantID) {
		if r.Name != name {
			continue
		}
		_, err = f.node.Services().Role.CreateRole(svcCtx, &qrdbsdk.CreateRoleRequest{
			ID:          r.ID,
			Name:        r.Name,
			TenantID:    r.TenantID,
			Permissions: r.Permissions,
		})
		require.NoError(t, err)
	}
	role, err := qrdbadapter.WrapRole(f.c.identityStore).GetRoleByName(svcCtx, qrdbsdk.SystemTenantID, name)
	require.NoError(t, err)
	return role.ID
}

// TestWebAuthnAdminResolver_OrdinaryAccountEnrollsSoftwarePasskey runs an
// anonymous invite activation for a system-tenant user who holds the User
// role, on a node that authorizes every read. Under assurance=any that user
// is not an administrator, so a passkey no trusted root vouches for enrolls.
// The administrator check reads the account's roles with no caller, so it
// must run as the node's service principal; read as the anonymous request it
// is refused, and a refusal is not evidence the account is an administrator.
func TestWebAuthnAdminResolver_OrdinaryAccountEnrollsSoftwarePasskey(t *testing.T) {
	f := newPasskeyFixtureAssured(t, newPrincipalNode, iam.WebAuthnAssuranceAny)
	ctx := context.Background()
	invite := f.inviteUser(f.serviceContext(t), t, qrdbsdk.SystemTenantID, f.systemRoleID(ctx, t, qrdbsdk.RoleNameUser))

	software, _ := newPasskeyAuthenticator(t)
	activation := f.activation()
	options, err := activation.ActivateFIDO2Begin(ctx, qrdbsdk.SystemTenantID, invite.token)
	require.NoError(t, err)
	activated, err := activation.ActivateFIDO2Finish(ctx, qrdbsdk.SystemTenantID, invite.token, software.register(options))
	require.NoError(t, err, "an ordinary account enrolls a software passkey under assurance=any")
	assert.Equal(t, invite.userID, activated.UserID)
}

// TestWebAuthnAdminResolver_AdministratorStillNeedsHardware is the same
// activation for a system-tenant Admin: assurance=any does not lower the
// administrator bar, so the software passkey is refused and the hardware one
// enrolls.
func TestWebAuthnAdminResolver_AdministratorStillNeedsHardware(t *testing.T) {
	f := newPasskeyFixtureAssured(t, newPrincipalNode, iam.WebAuthnAssuranceAny)
	ctx := context.Background()
	invite := f.inviteUser(f.serviceContext(t), t, qrdbsdk.SystemTenantID, f.systemRoleID(ctx, t, qrdbsdk.RoleNameAdmin))

	software, _ := newPasskeyAuthenticator(t)
	activation := f.activation()
	options, err := activation.ActivateFIDO2Begin(ctx, qrdbsdk.SystemTenantID, invite.token)
	require.NoError(t, err)
	_, err = activation.ActivateFIDO2Finish(ctx, qrdbsdk.SystemTenantID, invite.token, software.register(options))
	require.Error(t, err, "an administrator's software passkey is refused")
	f.requirePending(ctx, t, qrdbsdk.SystemTenantID, invite.userID, invite.token)

	options, err = activation.ActivateFIDO2Begin(ctx, qrdbsdk.SystemTenantID, invite.token)
	require.NoError(t, err)
	_, err = activation.ActivateFIDO2Finish(ctx, qrdbsdk.SystemTenantID, invite.token, f.key.register(options))
	require.NoError(t, err, "an administrator's hardware passkey enrolls")
}

// TestWebAuthnAdminResolver_RefusedRoleReadIsAnError reads the roles of an
// ordinary account as the anonymous request, without the service principal.
// The node refuses the read, and the resolver reports that as a typed error
// rather than answering "administrative".
func TestWebAuthnAdminResolver_RefusedRoleReadIsAnError(t *testing.T) {
	f := newPasskeyFixtureAssured(t, newPrincipalNode, iam.WebAuthnAssuranceAny)
	ctx := context.Background()
	f.inviteUser(f.serviceContext(t), t, qrdbsdk.SystemTenantID, f.systemRoleID(ctx, t, qrdbsdk.RoleNameUser))

	unbound := iam.NewWebAuthnAdminResolver(f.users, qrdbadapter.WrapRole(f.c.identityStore), nil)
	isAdmin, err := unbound.IsAdministrativeAccount(ctx, qrdbsdk.SystemTenantID, "invitee@example.com")
	var roleErr *iam.WebAuthnRoleResolverError
	require.ErrorAs(t, err, &roleErr, "a refused role read is a typed error")
	assert.False(t, isAdmin, "a refused role read is not an answer of administrative")

	bound := f.srv.webAuthnAdminResolver(f.users, f.c.identityStore)
	isAdmin, err = bound.IsAdministrativeAccount(ctx, qrdbsdk.SystemTenantID, "invitee@example.com")
	require.NoError(t, err, "the service principal may read the account's roles")
	assert.False(t, isAdmin)
}
