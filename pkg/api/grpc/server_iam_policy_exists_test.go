package grpc

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
)

// TestIAMServer_CreatePolicy_DuplicateIsAlreadyExists pins that creating a
// policy whose name is taken answers codes.AlreadyExists, not Internal.
func TestIAMServer_CreatePolicy_DuplicateIsAlreadyExists(t *testing.T) {
	srv, _, _ := newNestedConditionIAMServer(t)
	ctx := callerContext("t1")
	req := &iampb.CreatePolicyRequest{TenantId: "t1", Name: "p1", SpecYaml: []byte(flatConditionSpecYAML)}

	_, err := srv.CreatePolicy(ctx, req)
	require.NoError(t, err, "a fresh create must succeed")

	_, err = srv.CreatePolicy(ctx, req)
	require.Error(t, err)
	require.Equal(t, codes.AlreadyExists, status.Code(err), "got %v", err)
}
