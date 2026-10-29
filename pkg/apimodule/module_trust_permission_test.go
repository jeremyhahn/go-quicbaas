package apimodule

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
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

	for _, method := range []string{"AutoImportAIA", "PublishDANE"} {
		perm := trustMethodPermission(t, method)
		assert.Equal(t, "trust:write", perm, "%s permission", method)
		assert.False(t, issuer[perm], "%s must refuse a certs:generate-only caller", method)
		assert.True(t, trustWriter[perm], "%s must admit a trust:write caller", method)
	}
}

// TestTrustMethodPermission_UnknownMethodHasNoDescriptor covers the error
// path: a method the service does not declare does not resolve.
func TestTrustMethodPermission_UnknownMethodHasNoDescriptor(t *testing.T) {
	name := protoreflect.FullName("quicbaas.certificate.v1.TrustService.NoSuchMethod")
	_, err := protoregistry.GlobalFiles.FindDescriptorByName(name)
	assert.ErrorIs(t, err, protoregistry.NotFound)
}
