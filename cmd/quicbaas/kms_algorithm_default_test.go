package main

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// runKMSKeyCommand builds the `kms key` tree exactly as the CLI does — every
// subcommand registered, flag defaults applied in registration order — and
// executes args against it with a client that records the generate request.
func runKMSKeyCommand(t *testing.T, args ...string) (*service.GenerateKeyRequest, error) {
	t.Helper()
	origFactory := kmsClientFactory
	t.Cleanup(func() {
		kmsClientFactory = origFactory
		kmsAlgorithm = ""
		kmsDescription = ""
		kmsExportable = false
		kmsOutputFmt = ""
	})

	var sent *service.GenerateKeyRequest
	kmsClientFactory = func() (KMSClient, error) {
		return &MockKMSClient{
			KMSGenerateKeyFunc: func(_ context.Context, req *service.GenerateKeyRequest) (*service.GenerateKeyResponse, error) {
				sent = req
				return &service.GenerateKeyResponse{
					Key: &service.KeyHandle{KeyID: req.Name, Version: 1},
				}, nil
			},
		}, nil
	}

	cmd := newKMSKeyCommand()
	cmd.SetArgs(args)
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	return sent, cmd.Execute()
}

// `kms key generate` with no --algorithm generates the documented default,
// AES-256-GCM. The unwrap subcommand, registered after generate, declares its
// own --algorithm with an empty default; sharing one variable let that later
// registration blank generate's default ("unsupported algorithm: ").
func TestKMSKeyGenerate_NoAlgorithmFlagUsesDefault(t *testing.T) {
	sent, err := runKMSKeyCommand(t, "generate", "cli-flag-default")
	require.NoError(t, err)
	require.NotNil(t, sent, "generate must reach the KMS client")
	require.NotNil(t, sent.KeyAttributes)
	require.NotEmpty(t, sent.KeyAttributes.SymmetricAlgorithm,
		"the default algorithm must be the symmetric AES-256-GCM")
}

// An unknown --algorithm is still refused before any request is sent.
func TestKMSKeyGenerate_UnknownAlgorithmRefused(t *testing.T) {
	sent, err := runKMSKeyCommand(t, "generate", "bad-alg", "--algorithm", "NOT-AN-ALG")
	require.Error(t, err)
	require.Nil(t, sent, "an unsupported algorithm must not reach the KMS client")
}
