//go:build !integration

package server

import (
	"context"
	"slices"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/qrdbservice"
)

// cliSecurityPermissions are the permissions the pinned go-qrdb declares on
// the RPCs the CLI's embedded client calls through Security().Barrier(),
// Ceremony(), Credentials() and Auth().Factors(), and through
// TenantMgmtService() for a tenant barrier (cmd/quicbaas).
var cliSecurityPermissions = []qrdbsdk.Permission{
	"seal:status",            // barrier status
	"seal:bootstrap",         // barrier init
	"seal:unseal",            // barrier unseal
	"seal:seal",              // barrier seal
	"seal:generate_recovery", // barrier recovery-keys
	"seal:rekey",             // barrier rekey
	"seal:officer_revoke",    // barrier officer revoke
	"seal:officer_register",  // init officer register
	"seal:officer_list",      // init officer list
	"ceremony:status",
	"ceremony:initialize",
	"ceremony:sign_csr",
	"credentials:submit",
	"credentials:list",
	"credentials:rotate",
	"credentials:retrieve_version",
	"credentials:list_versions",
	"credentials:delete",
	"credentials:gc_expired",
	"user_authfactors:add",
	"user_authfactors:list",
	"user_authfactors:remove",
	"user_authfactors:rotate",
	"user_authfactors:relabel",
}

// withoutCLISecurityGrants is a service principal holding the production
// grants minus every CLI security permission, so a test can see the node
// refuse them.
const withoutCLISecurityGrants = "quicbaas-test-no-cli-security"

func newPrincipalNodeWithoutCLISecurity(t *testing.T) *qrdbnode.Node {
	t.Helper()
	perms, err := qrdbservice.Grants()
	require.NoError(t, err)
	perms = slices.DeleteFunc(perms, func(p qrdbsdk.Permission) bool {
		return slices.Contains(cliSecurityPermissions, p)
	})
	qrdbsdk.RegisterPermissionRoleGrant(withoutCLISecurityGrants, perms...)

	cfg := &qrdbsdk.DaemonConfig{}
	cfg.Raft.DataDir = t.TempDir()
	cfg.Databases = []qrdbsdk.DaemonDatabaseConfig{{Name: "default", Backend: "memory"}}
	node, err := qrdbnode.NewStandaloneNode(context.Background(), cfg, nil, qrdbnode.WithServicePrincipal(withoutCLISecurityGrants))
	require.NoError(t, err)
	t.Cleanup(func() { _ = node.Close() })
	return node
}

// authorizeCLISecurity runs each CLI security permission through the node's
// own authorization middleware as the node's service principal.
func authorizeCLISecurity(t *testing.T, node *qrdbnode.Node) map[qrdbsdk.Permission]error {
	t.Helper()
	mw := node.AuthMiddleware()
	require.NotNil(t, mw, "the node has authorization disabled")
	ctx, err := node.ServiceContext(context.Background())
	require.NoError(t, err)
	results := make(map[qrdbsdk.Permission]error, len(cliSecurityPermissions))
	for _, perm := range cliSecurityPermissions {
		results[perm] = mw.AuthorizeTenant(ctx, qrdbsdk.SystemTenantID, perm)
	}
	return results
}

func TestServicePrincipal_HoldsTheCLISecurityGrants(t *testing.T) {
	for perm, err := range authorizeCLISecurity(t, newPrincipalNode(t)) {
		require.NoError(t, err, "the node refused the service principal %s", perm)
	}
}

func TestServicePrincipal_WithoutTheCLISecurityGrantsIsRefused(t *testing.T) {
	for perm, err := range authorizeCLISecurity(t, newPrincipalNodeWithoutCLISecurity(t)) {
		require.Error(t, err, "the node granted %s to a principal without it", perm)
	}
}

// The CLI's read verbs, run through the node's client as its service
// principal: allowed with the grants, refused without them.
func cliSecurityReads(t *testing.T, node *qrdbnode.Node) []error {
	t.Helper()
	ctx, err := node.ServiceContext(context.Background())
	require.NoError(t, err)
	c := node.Client()
	_, statusErr := c.Barrier().BarrierStatus(ctx)
	_, ceremonyErr := c.Ceremony().CeremonyStatus(ctx)
	_, listErr := c.Credentials().CredentialList(ctx, &qrdbtransport.CredentialListRequest{})
	return []error{statusErr, ceremonyErr, listErr}
}

func TestServicePrincipal_CLISecurityReadsWithoutGrantsAreRefused(t *testing.T) {
	for i, err := range cliSecurityReads(t, newPrincipalNodeWithoutCLISecurity(t)) {
		require.Error(t, err, "read %d", i)
		require.NotErrorIs(t, err, qrdbsdk.ErrUnauthenticated, "read %d: the principal was not bound", i)
	}
}
