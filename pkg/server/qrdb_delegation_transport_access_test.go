//go:build !integration

package server_test

import (
	"crypto/x509"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/server"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	quicbaassdk "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/embedded"
)

// TestDelegatedClient_TransportWithholdsTheService proves a delegated
// client's public transport accessors never hand out the unbound service: a
// consumer holding only the delegated client could otherwise reach
// SecretRegistry() and resolve credentials with no identity at all.
func TestDelegatedClient_TransportWithholdsTheService(t *testing.T) {
	s := server.NewDelegatingServer(t)
	leaf := server.IssueTestDelegation(t, s, "kv:get")
	c, err := quicbaassdk.NewClient("embedded://local",
		quicbaassdk.WithEmbedded(&delegationFacade{secrets: &delegationSecrets{}}),
		quicbaassdk.WithEmbeddedIdentity(quicbaassdk.DelegatedIdentity(s, []*x509.Certificate{leaf})))
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	_, isService := c.Transport().Conn().(service.QuicBaaS)
	require.False(t, isService, "a delegated client's Transport().Conn() exposed the raw service")

	et, ok := c.Transport().(*embedded.Transport)
	require.True(t, ok, "embedded client transport is %T", c.Transport())
	require.Nil(t, et.Service(), "a delegated client's embedded transport exposed the raw service")
}

// TestServicePrincipalClient_TransportKeepsTheService pins the service
// principal's access: it runs as the service, so its transport still serves
// the in-process service.
func TestServicePrincipalClient_TransportKeepsTheService(t *testing.T) {
	s := server.NewDelegatingServer(t)
	c, err := quicbaassdk.NewClient("embedded://local",
		quicbaassdk.WithEmbedded(&delegationFacade{secrets: &delegationSecrets{}}),
		quicbaassdk.WithEmbeddedIdentity(quicbaassdk.ServicePrincipalIdentity(s)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	require.NotNil(t, c.Transport().(*embedded.Transport).Service())
}
