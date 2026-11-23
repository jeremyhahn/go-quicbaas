package grpc

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/sshfake"
)

func TestSSHServer_SignDigestCarriesAlgorithm(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	key, err := gossh.NewSignerFromKey(priv)
	require.NoError(t, err)
	fake := sshfake.NewClient()
	fake.KeySigner = key
	srv := newSSHTestServer(fake)

	digest := []byte("session-data")
	resp, err := srv.SignSSHDigest(context.Background(), &pb.SSHSignDigestRequest{
		Scope: "user", Owner: "alice", Name: "rsa", Digest: digest, Algorithm: gossh.KeyAlgoRSASHA256,
	})
	require.NoError(t, err)
	assert.Equal(t, gossh.KeyAlgoRSASHA256, resp.GetFormat())
	require.NoError(t, key.PublicKey().Verify(digest, &gossh.Signature{
		Format: resp.GetFormat(), Blob: resp.GetBlob(), Rest: resp.GetRest(),
	}))
}

func TestSSHServer_SignDigestAlgorithmForOtherKeyTypeIsInvalidArgument(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	key, err := gossh.NewSignerFromKey(priv)
	require.NoError(t, err)
	fake := sshfake.NewClient()
	fake.KeySigner = key
	srv := newSSHTestServer(fake)

	_, err = srv.SignSSHDigest(context.Background(), &pb.SSHSignDigestRequest{
		Name: "laptop", Digest: []byte("d"), Algorithm: gossh.KeyAlgoRSASHA512,
	})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}
