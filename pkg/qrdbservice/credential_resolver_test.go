package qrdbservice_test

import (
	"context"
	"errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"
	qrdbsecure "github.com/jeremyhahn/go-qrdb/sdk/go/secure"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/qrdbservice"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// credentialMissingError is the typed error currentCredentials returns for
// a credential it does not hold, or a version other than the current one.
type credentialMissingError struct {
	name string
}

func (e *credentialMissingError) Error() string {
	return "credential " + e.name + " is not stored"
}

// currentCredentials stands in for qrdb's credential servicer, which sits
// behind the node's authorization: a call reaches it only when the node's
// middleware has granted the verb. Version 0 is the current version, as in
// the pinned go-qrdb.
type currentCredentials struct {
	quicbaas.QRDBCredentialServicer
	values map[string]string
}

func (c *currentCredentials) lookup(name string) (*qrdbsecure.Buffer, error) {
	value, ok := c.values[name]
	if !ok {
		return nil, &credentialMissingError{name: name}
	}
	return qrdbsecure.NewBufferFromBytes([]byte(value))
}

func (c *currentCredentials) CredentialRetrieveVersionBuffer(_ context.Context, name string, version uint64) (*qrdbsecure.Buffer, error) {
	if version != 0 {
		return nil, &credentialMissingError{name: name}
	}
	return c.lookup(name)
}

func (c *currentCredentials) CredentialRetrieveBuffer(_ context.Context, name string) (*qrdbsecure.Buffer, error) {
	return c.lookup(name)
}

// newPrincipalClient builds an embedded client acting as the service
// principal of a node that carries this package's production grants.
func newPrincipalClient(t *testing.T, creds *currentCredentials) quicbaas.Client {
	t.Helper()
	cfg := &qrdbsdk.DaemonConfig{}
	cfg.Raft.DataDir = t.TempDir()
	cfg.Databases = []qrdbsdk.DaemonDatabaseConfig{{Name: "default", Backend: "memory"}}
	principal, err := qrdbservice.NodeOption()
	require.NoError(t, err)
	node, err := qrdbnode.NewStandaloneNode(context.Background(), cfg, nil, principal)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, node.Close()) })

	svcs := node.Services()
	require.NotNil(t, svcs)
	c, err := quicbaas.NewClient("embedded://local",
		quicbaas.WithEmbedded(struct{ service.QuicBaaS }{}),
		quicbaas.WithEmbeddedQRDB(quicbaas.QRDBEmbeddedServicers{
			KV:         svcs.KV,
			Admin:      svcs.Admin,
			Credential: creds,
		}),
		quicbaas.WithEmbeddedIdentity(quicbaas.ServicePrincipalIdentity(node)))
	require.NoError(t, err)
	return c
}

// TestServicePrincipal_ResolvesCredentialURIs resolves credential:// URIs as
// the service principal under the production grants, which hold
// credentials:retrieve_version and not credentials:retrieve.
func TestServicePrincipal_ResolvesCredentialURIs(t *testing.T) {
	c := newPrincipalClient(t, &currentCredentials{values: map[string]string{"db-password": "s3cret"}})
	resolver := c.Security().SecretResolver()
	require.NotNil(t, resolver)
	ctx := context.Background()

	t.Run("stored credential resolves", func(t *testing.T) {
		value, err := resolver.Resolve(ctx, "credential://db-password")
		require.NoError(t, err)
		defer value.Clear()
		var got string
		require.NoError(t, value.WithBytes(ctx, func(b []byte) error {
			got = string(b)
			return nil
		}))
		require.Equal(t, "s3cret", got)
	})

	t.Run("missing credential keeps its typed error", func(t *testing.T) {
		_, err := resolver.Resolve(ctx, "credential://absent")
		var missing *credentialMissingError
		require.True(t, errors.As(err, &missing), "Resolve(credential://absent) = %v, want *credentialMissingError", err)
		require.Equal(t, "absent", missing.name)
	})
}
