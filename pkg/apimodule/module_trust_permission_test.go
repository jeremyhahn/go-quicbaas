package apimodule

import (
	"context"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	quicbaaspermission "github.com/jeremyhahn/go-quicbaas/pkg/permission"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/permscope"
)

// trustMethodPermission resolves the qrdb.v1.permission a TrustService RPC
// declares on its descriptor, the permission the node enforces on TCP gRPC.
func trustMethodPermission(t *testing.T, method string) string {
	t.Helper()
	name := protoreflect.FullName("quicbaas.certificate.v1.TrustService." + method)
	desc, err := protoregistry.GlobalFiles.FindDescriptorByName(name)
	require.NoError(t, err, "resolve %s", name)
	md, ok := desc.(protoreflect.MethodDescriptor)
	require.True(t, ok, "%s is not a method", name)
	return methodPermission(md)
}

// TestTrustWriteRPCs_RefuseCertsGenerateHolder guards Codex 8259: the AIA
// import and DANE publish RPCs require trust:write, as their REST routes do,
// so a caller holding only certs:generate is refused.
func TestTrustWriteRPCs_RefuseCertsGenerateHolder(t *testing.T) {
	issuer := map[string]bool{"certs:generate": true}
	trustWriter := map[string]bool{"trust:write": true}

	perm := trustMethodPermission(t, "PublishDANE")
	assert.Equal(t, "trust:write", perm, "PublishDANE permission")
	assert.False(t, issuer[perm], "PublishDANE must refuse a certs:generate-only caller")
	assert.True(t, trustWriter[perm], "PublishDANE must admit a trust:write caller")
}

// TestAIAImportRPCs_AreSystemScoped pins the fix for Codex 8322: the AIA
// import writes the server CA's stores, which every tenant shares, so a
// tenant admin's tenant-scoped trust:write or certs:generate must not reach
// it. Both RPCs that run the importer declare aia:import, and qrdb
// resolves that permission cross-tenant, which its authorizer grants only to
// a system-scope caller.
func TestAIAImportRPCs_AreSystemScoped(t *testing.T) {
	// The composition root registers the broker's permissions before it
	// builds the module (pkg/server/server.go), so this test does too.
	quicbaaspermission.Register()
	_, err := NewModule(func() service.QuicBaaS { return nil }, testLogger(t))
	require.NoError(t, err)
	resolver, err := permscope.New()
	require.NoError(t, err)

	methods := map[protoreflect.FullName]bool{
		"quicbaas.certificate.v1.TrustService.AutoImportAIA": true,
		"quicbaas.certificate.v1.CertificateService.Chain":   true,
	}
	for name := range methods {
		desc, err := protoregistry.GlobalFiles.FindDescriptorByName(name)
		require.NoError(t, err, "resolve %s", name)
		md, ok := desc.(protoreflect.MethodDescriptor)
		require.True(t, ok, "%s is not a method", name)
		perm := methodPermission(md)
		assert.Equal(t, "aia:import", perm, "%s permission", name)

		scope, err := resolver.Scope(context.Background(), qrdbsdk.Permission(perm))
		require.NoError(t, err, "%s has no declared scope", perm)
		assert.Equal(t, qrdbsdk.PermissionScopeCross, scope,
			"%s must be cross-tenant so a tenant-scoped caller is denied", name)
	}
}

// TestTrustMethodPermission_UnknownMethodHasNoDescriptor covers the error
// path: a method the service does not declare does not resolve.
func TestTrustMethodPermission_UnknownMethodHasNoDescriptor(t *testing.T) {
	name := protoreflect.FullName("quicbaas.certificate.v1.TrustService.NoSuchMethod")
	_, err := protoregistry.GlobalFiles.FindDescriptorByName(name)
	assert.ErrorIs(t, err, protoregistry.NotFound)
}
