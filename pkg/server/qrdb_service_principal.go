package server

import (
	"context"
	"crypto/x509"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/qrdbadapter"
	"github.com/jeremyhahn/go-quicbaas/pkg/qrdbservice"
)

// serviceKV binds kv to go-quicbaas's service principal (pkg/qrdbservice)
// when the embedded qrdb node serves it. With no embedded node the client is
// remote and carries the server's own mTLS identity, so kv is returned as is.
func (s *Server) serviceKV(kv transport.KVClient) transport.KVClient {
	node := s.getQRDBNode()
	if node == nil {
		return kv
	}
	return qrdbservice.KV(node, kv)
}

// serviceVector wraps vec so a request's authenticated caller reaches qrdb
// as itself, in its tenant, and work with no caller is bound to the node's
// service principal (qrdbservice.Vector).
func (s *Server) serviceVector(vec transport.VectorClient) transport.VectorClient {
	node := s.getQRDBNode()
	if node == nil {
		return vec
	}
	return qrdbservice.Vector(node, vec)
}

// serviceVectorProvider hands the vector service each shard's vector client
// bound to the node's service principal, so its writes reach qrdb the way
// nodeVectorStorage's reads do.
type serviceVectorProvider struct {
	server *Server
}

// VectorForShard implements service.VectorClientProvider.
func (p serviceVectorProvider) VectorForShard(shardID uint64) (transport.VectorClient, error) {
	client, err := p.server.qrdbClient.ForShard(shardID)
	if err != nil {
		return nil, err
	}
	return p.server.serviceVector(client.Vector()), nil
}

// serviceTransaction is serviceKV for the transaction plane.
func (s *Server) serviceTransaction(txn transport.TransactionClient) transport.TransactionClient {
	node := s.getQRDBNode()
	if node == nil {
		return txn
	}
	return qrdbservice.Transaction(node, txn)
}

// serviceCluster binds the scheduler's membership reads to the service
// principal, the way serviceKV and serviceTransaction bind its marks and
// claims.
func (s *Server) serviceCluster(cluster qrdbservice.Membership) qrdbservice.Membership {
	node := s.getQRDBNode()
	if node == nil {
		return cluster
	}
	return qrdbservice.Cluster(node, cluster)
}

// ServiceContext binds ctx to the service principal for calls that reach the
// embedded node through clients serviceKV does not wrap, and for an embedded
// SDK client that acts as the system (quicbaas.ServicePrincipalIdentity). With
// no embedded node ctx is returned as is, for the same reason serviceKV returns
// kv as is.
func (s *Server) ServiceContext(ctx context.Context) (context.Context, error) {
	node := s.getQRDBNode()
	if node == nil {
		return ctx, nil
	}
	return node.ServiceContext(ctx)
}

// issuanceReadContext binds the role and permission reads token issuance
// makes for a user of tenantID (iam.OIDCDependencies.IssuanceReadBinder).
// Issuance has no caller yet, so a system-tenant user's reads run as the
// node's service principal (roles:read, permissions:resolve). go-qrdb lets
// that principal act in its own tenant only; another tenant is reached
// through a delegation that tenant issues, so ctx is returned as is and the
// node refuses the read rather than answer it from the system tenant.
func (s *Server) issuanceReadContext(ctx context.Context, tenantID string) (context.Context, error) {
	if tenantID != qrdbsdk.SystemTenantID {
		return ctx, nil
	}
	return s.ServiceContext(ctx)
}

// webAuthnAdminResolver answers the WebAuthn attestation gate's "is this an
// administrator" question over users and identity's roles. The question is
// asked during registration, before the registrant has a session, so its role
// reads are bound the way issuance binds its reads (issuanceReadContext): a
// system-tenant account's roles are read as the node's service principal.
func (s *Server) webAuthnAdminResolver(users iam.WebAuthnUserResolver, identity *iam.IdentityStore) iam.WebAuthnAdminResolver {
	return iam.NewWebAuthnAdminResolver(users, qrdbadapter.WrapRole(identity), s.issuanceReadContext)
}

// DelegatedContext binds ctx to the identity a tenant-issued delegation
// certificate (chain, leaf first) grants the service principal inside the
// delegating tenant, for an embedded SDK client that acts inside that tenant
// (quicbaas.DelegatedIdentity). The node verifies the delegation on every
// call. A delegation needs the node to verify it, so with no embedded node it
// returns ErrNoEmbeddedNode.
func (s *Server) DelegatedContext(ctx context.Context, chain []*x509.Certificate) (context.Context, error) {
	node := s.getQRDBNode()
	if node == nil {
		return nil, ErrNoEmbeddedNode
	}
	return node.DelegatedContext(ctx, chain)
}

// AuthMiddleware returns the embedded node's authorization middleware, the
// one its network transports enforce with, so an embedded client running as
// the service principal or a delegation is bounded by that identity's grants
// (quicbaas.EmbeddedAuthorizer). It is nil when no node runs or the node
// has authorization disabled.
func (s *Server) AuthMiddleware() *qrdbsdk.AuthMiddleware {
	node := s.getQRDBNode()
	if node == nil {
		return nil
	}
	return node.AuthMiddleware()
}
