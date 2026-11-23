package service

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/sshfake"
)

func rsaSSHSigner(t *testing.T) gossh.Signer {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	s, err := gossh.NewSignerFromKey(priv)
	require.NoError(t, err)
	return s
}

func TestSSHService_SignDigestWithRSASHA2Algorithm(t *testing.T) {
	key := rsaSSHSigner(t)
	fake := sshfake.NewClient()
	fake.KeySigner = key
	svc := NewSSHService(fake)

	for _, alg := range []string{gossh.KeyAlgoRSASHA256, gossh.KeyAlgoRSASHA512} {
		t.Run(alg, func(t *testing.T) {
			digest := []byte("session-data")
			sig, err := svc.SignDigest(context.Background(), SSHSignRequest{
				Scope: "user", Owner: "alice", Name: "rsa", Digest: digest, Algorithm: alg,
			})
			require.NoError(t, err)
			assert.Equal(t, alg, sig.Format)
			require.NoError(t, key.PublicKey().Verify(digest, &gossh.Signature{Format: sig.Format, Blob: sig.Blob, Rest: sig.Rest}))
		})
	}
}

func TestSSHService_SignDigestRejectsAlgorithmForOtherKeyType(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	key, err := gossh.NewSignerFromKey(priv)
	require.NoError(t, err)
	fake := sshfake.NewClient()
	fake.KeySigner = key
	svc := NewSSHService(fake)

	_, err = svc.SignDigest(context.Background(), SSHSignRequest{
		Name: "laptop", Digest: []byte("d"), Algorithm: gossh.KeyAlgoRSASHA256,
	})
	var algErr *SSHSignAlgorithmError
	require.ErrorAs(t, err, &algErr)
	assert.Equal(t, gossh.KeyAlgoRSASHA256, algErr.Algorithm)
	assert.Equal(t, gossh.KeyAlgoED25519, algErr.KeyType)
}

func TestSSHService_SignDigestKeyTypeAlgorithmWithPlainSigner(t *testing.T) {
	fake := sshfake.NewClient()
	fake.SignerPublicKey = testSSHPublicKey(t)
	svc := NewSSHService(fake)

	sig, err := svc.SignDigest(context.Background(), SSHSignRequest{
		Name: "laptop", Digest: []byte("d"), Algorithm: gossh.KeyAlgoED25519,
	})
	require.NoError(t, err)
	assert.Equal(t, "ssh-ed25519", sig.Format)
}

func TestSSHService_SignDigestSHA2NeedsAlgorithmSigner(t *testing.T) {
	fake := sshfake.NewClient()
	fake.SignerPublicKey = rsaSSHSigner(t).PublicKey()
	svc := NewSSHService(fake)

	_, err := svc.SignDigest(context.Background(), SSHSignRequest{
		Name: "rsa", Digest: []byte("d"), Algorithm: gossh.KeyAlgoRSASHA512,
	})
	var algErr *SSHSignAlgorithmError
	require.ErrorAs(t, err, &algErr)
}
