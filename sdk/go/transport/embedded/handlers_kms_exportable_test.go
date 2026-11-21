// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package embedded

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	kmsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// exportableKMS is a KMS double that records the import and unwrap requests
// it receives.
type exportableKMS struct {
	mockKMSService
	imports []*service.ImportKeyRequest
	unwraps []*service.UnwrapKeyRequest
}

func (k *exportableKMS) ImportKey(_ context.Context, req *service.ImportKeyRequest) (*service.ImportKeyResponse, error) {
	k.imports = append(k.imports, req)
	return &service.ImportKeyResponse{Metadata: &service.KeyMetadata{KeyID: "imported"}}, nil
}

func (k *exportableKMS) UnwrapKey(_ context.Context, req *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error) {
	k.unwraps = append(k.unwraps, req)
	return &service.UnwrapKeyResponse{KeyID: req.KeyID}, nil
}

func newExportableClient(t *testing.T) (kmsclient.Client, *exportableKMS) {
	t.Helper()
	kms := &exportableKMS{}
	tr, err := New(&mockQuicBaaS{kmsService: kms})
	require.NoError(t, err)
	return kmsclient.NewClient(tr), kms
}

// TestEmbeddedKMS_ImportKey_CarriesExportable drives the SDK KMS client
// through the embedded transport and checks the service sees Exportable.
func TestEmbeddedKMS_ImportKey_CarriesExportable(t *testing.T) {
	client, kms := newExportableClient(t)
	for _, exportable := range []bool{true, false} {
		resp, err := client.ImportKey(context.Background(), &kmsclient.ImportKeyRequest{
			KeyMaterial:   []byte("material"),
			WrappingKeyID: "kek",
			Name:          "imported",
			Algorithm:     kmsclient.AlgorithmAES256GCM,
			Usage:         []kmsclient.KeyUsage{kmsclient.KeyUsageEncrypt},
			Exportable:    exportable,
		})
		require.NoError(t, err)
		assert.Equal(t, "imported", resp.KeyID)
		got := kms.imports[len(kms.imports)-1]
		require.NotNil(t, got.KeyAttributes)
		assert.Equal(t, exportable, got.KeyAttributes.Exportable)
		assert.Equal(t, "kek", got.WrappingKeyID)
		assert.Equal(t, []service.KeyUsage{service.KeyUsageEncrypt}, got.Usage)
	}
}

// TestEmbeddedKMS_UnwrapKey_CarriesExportable checks the unwrap request
// reaches the service with Exportable and the service's algorithm names.
func TestEmbeddedKMS_UnwrapKey_CarriesExportable(t *testing.T) {
	client, kms := newExportableClient(t)
	for _, exportable := range []bool{true, false} {
		_, err := client.UnwrapKey(context.Background(), &kmsclient.UnwrapKeyRequest{
			KeyID:             "unwrapped",
			WrappingKeyID:     "kek",
			WrappingAlgorithm: kmsclient.WrapAlgRSAOAEPSHA256,
			WrappedMaterial:   []byte("wrapped"),
			Algorithm:         kmsclient.AlgorithmAES256GCM,
			SourceKeyID:       "source",
			SourceVersion:     3,
			Exportable:        exportable,
		})
		require.NoError(t, err)
		got := kms.unwraps[len(kms.unwraps)-1]
		require.NotNil(t, got.KeyAttributes)
		assert.Equal(t, exportable, got.KeyAttributes.Exportable)
		assert.Equal(t, "RSA-OAEP-SHA256", got.WrappingAlgorithm)
		assert.Equal(t, kmsclient.AlgorithmAES256GCM.String(), got.Algorithm)
		assert.Equal(t, []byte("wrapped"), got.WrappedKey)
		assert.Equal(t, uint64(3), got.SourceVersion)
	}
}

// TestEmbeddedKMS_UnwrapKey_RefusesBadRequests checks an unknown wrapping
// algorithm, a negative source version and a wrong request type are refused
// before the service is called.
func TestEmbeddedKMS_UnwrapKey_RefusesBadRequests(t *testing.T) {
	client, kms := newExportableClient(t)
	ctx := context.Background()

	_, err := client.UnwrapKey(ctx, &kmsclient.UnwrapKeyRequest{
		WrappingKeyID:     "kek",
		WrappingAlgorithm: kmsclient.WrappingAlgorithm(99),
		WrappedMaterial:   []byte("wrapped"),
	})
	assert.True(t, errors.IsInvalidInput(err), "unknown wrapping algorithm: err = %v, want InvalidInput", err)

	_, err = client.UnwrapKey(ctx, &kmsclient.UnwrapKeyRequest{
		WrappingKeyID:   "kek",
		WrappedMaterial: []byte("wrapped"),
		SourceVersion:   -1,
	})
	assert.True(t, errors.IsInvalidInput(err), "negative source version: err = %v, want InvalidInput", err)

	tr, err := New(&mockQuicBaaS{kmsService: kms})
	require.NoError(t, err)
	var invalid *InvalidRequestTypeError
	_, err = tr.Request(ctx, "quicbaas.KMS", "UnwrapKey", &kmsclient.ImportKeyRequest{})
	assert.ErrorAs(t, err, &invalid)
	_, err = tr.Request(ctx, "quicbaas.KMS", "ImportKey", &kmsclient.UnwrapKeyRequest{})
	assert.ErrorAs(t, err, &invalid)

	assert.Empty(t, kms.unwraps, "a refused unwrap must not reach the service")
	assert.Empty(t, kms.imports, "a refused import must not reach the service")
}
