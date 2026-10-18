// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package embedded

import (
	"context"
	"crypto/x509"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	kmsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// keyMaterialKMS is a KMS double that records the import, export and unwrap
// requests that reach it and answers each with a populated response.
type keyMaterialKMS struct {
	mockKMSService
	imports      []*service.ImportKeyRequest
	exports      []*service.ExportKeyRequest
	unwraps      []*service.UnwrapKeyRequest
	importResult *service.ImportKeyResponse
}

func (k *keyMaterialKMS) ImportKey(_ context.Context, req *service.ImportKeyRequest) (*service.ImportKeyResponse, error) {
	k.imports = append(k.imports, req)
	if k.importResult != nil {
		return k.importResult, nil
	}
	return &service.ImportKeyResponse{
		Key:      &service.KeyHandle{KeyID: "imported-1", Version: 1, Algorithm: service.KeyAlgECDSAP256},
		Metadata: &service.KeyMetadata{KeyID: "imported-1"},
	}, nil
}

func (k *keyMaterialKMS) ExportKey(_ context.Context, req *service.ExportKeyRequest) (*service.ExportKeyResponse, error) {
	k.exports = append(k.exports, req)
	return &service.ExportKeyResponse{
		KeyID:         req.KeyID,
		Version:       3,
		WrappedKey:    []byte("wrapped-bytes"),
		WrappingKeyID: req.WrappingKeyID,
	}, nil
}

func (k *keyMaterialKMS) UnwrapKey(_ context.Context, req *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error) {
	k.unwraps = append(k.unwraps, req)
	return &service.UnwrapKeyResponse{
		KeyID:     req.KeyID,
		Version:   1,
		Algorithm: service.KeyAlgorithm(req.Algorithm),
	}, nil
}

func newKeyMaterialClient(t *testing.T) (kmsclient.Client, *keyMaterialKMS, *Transport) {
	t.Helper()
	kms := &keyMaterialKMS{}
	tr, err := New(&mockQuicBaaS{kmsService: kms})
	require.NoError(t, err)
	return kmsclient.NewClient(tr), kms, tr
}

// TestEmbeddedKMS_ImportKey_ClientRequestReachesHandler drives the real SDK
// KMS client through the embedded transport and checks every field of the
// client request reaches the service with its meaning intact.
func TestEmbeddedKMS_ImportKey_ClientRequestReachesHandler(t *testing.T) {
	client, kms, _ := newKeyMaterialClient(t)
	material := []byte("key-material")

	resp, err := client.ImportKey(context.Background(), &kmsclient.ImportKeyRequest{
		KeyMaterial:   material,
		WrappingKeyID: "wrap-1",
		Name:          "imported",
		Description:   "an imported key",
		Algorithm:     kmsclient.AlgorithmECDSAP256,
		Usage:         []kmsclient.KeyUsage{kmsclient.KeyUsageSign, kmsclient.KeyUsageVerify},
		Tags:          map[string]string{"team": "platform"},
	})
	require.NoError(t, err)
	assert.Equal(t, &kmsclient.ImportKeyResponse{KeyID: "imported-1", Version: 1, Algorithm: kmsclient.AlgorithmECDSAP256}, resp)

	require.Len(t, kms.imports, 1)
	got := kms.imports[0]
	assert.Equal(t, material, got.KeyMaterial)
	assert.Equal(t, "wrap-1", got.WrappingKeyID)
	assert.Equal(t, "imported", got.Name)
	assert.Equal(t, "an imported key", got.Description)
	assert.Equal(t, []service.KeyUsage{service.KeyUsageSign, service.KeyUsageVerify}, got.Usage)
	assert.Equal(t, map[string]string{"team": "platform"}, got.Tags)
	require.NotNil(t, got.KeyAttributes)
	assert.Equal(t, x509.ECDSA, got.KeyAttributes.KeyAlgorithm)
}

// TestEmbeddedKMS_ImportKey_NoMetadataIsTyped checks a service answer without
// metadata comes back as a typed error, not a nil dereference.
func TestEmbeddedKMS_ImportKey_NoMetadataIsTyped(t *testing.T) {
	client, kms, _ := newKeyMaterialClient(t)
	kms.importResult = &service.ImportKeyResponse{}

	_, err := client.ImportKey(context.Background(), &kmsclient.ImportKeyRequest{KeyMaterial: []byte("m"), Name: "k"})
	assert.Equal(t, errors.CodeInternal, errors.Code(err), "err = %v, want Internal", err)
}

func TestEmbeddedKMS_ExportKey_ClientRequestReachesHandler(t *testing.T) {
	client, kms, _ := newKeyMaterialClient(t)

	resp, err := client.ExportKey(context.Background(), &kmsclient.ExportKeyRequest{
		KeyID:             "key-1",
		WrappingKeyID:     "wrap-1",
		WrappingAlgorithm: kmsclient.WrapAlgRSAOAEPSHA256,
		Format:            kmsclient.ExportFormatJWK,
		Version:           3,
	})
	require.NoError(t, err)
	assert.Equal(t, &kmsclient.ExportKeyResponse{
		KeyID:           "key-1",
		WrappingKeyID:   "wrap-1",
		WrappedMaterial: []byte("wrapped-bytes"),
		Version:         3,
	}, resp)

	require.Len(t, kms.exports, 1)
	assert.Equal(t, &service.ExportKeyRequest{
		KeyID:             "key-1",
		Version:           3,
		WrappingKeyID:     "wrap-1",
		WrappingAlgorithm: "RSA-OAEP-SHA256",
		Format:            "jwk",
	}, kms.exports[0])
}

// TestEmbeddedKMS_ExportKey_EveryWrappingAlgorithmAndFormat checks each
// wrapping algorithm and format the SDK client can carry reaches the service
// as the name the service validates.
func TestEmbeddedKMS_ExportKey_EveryWrappingAlgorithmAndFormat(t *testing.T) {
	algorithms := map[kmsclient.WrappingAlgorithm]string{
		kmsclient.WrapAlgAESKW:         "AES-KW",
		kmsclient.WrapAlgAESKWP:        "AES-KWP",
		kmsclient.WrapAlgAESGCM:        "AES-GCM",
		kmsclient.WrapAlgRSAOAEPSHA256: "RSA-OAEP-SHA256",
		kmsclient.WrapAlgRSAOAEPSHA384: "RSA-OAEP-SHA384",
		kmsclient.WrapAlgRSAOAEPSHA512: "RSA-OAEP-SHA512",
		kmsclient.WrapAlgECDHESA256KW:  "ECDH-ES+A256KW",
	}
	formats := map[kmsclient.ExportFormat]string{
		kmsclient.ExportFormatRaw: "raw",
		kmsclient.ExportFormatJWK: "jwk",
		kmsclient.ExportFormatPEM: "pem",
	}
	client, kms, _ := newKeyMaterialClient(t)
	ctx := context.Background()
	for alg, want := range algorithms {
		t.Run(want, func(t *testing.T) {
			_, err := client.ExportKey(ctx, &kmsclient.ExportKeyRequest{KeyID: "key-1", WrappingKeyID: "wrap-1", WrappingAlgorithm: alg})
			require.NoError(t, err)
			assert.Equal(t, want, kms.exports[len(kms.exports)-1].WrappingAlgorithm)
		})
	}
	for format, want := range formats {
		t.Run(want, func(t *testing.T) {
			_, err := client.ExportKey(ctx, &kmsclient.ExportKeyRequest{KeyID: "key-1", WrappingKeyID: "wrap-1", Format: format})
			require.NoError(t, err)
			assert.Equal(t, want, kms.exports[len(kms.exports)-1].Format)
		})
	}
}

func TestEmbeddedKMS_UnwrapKey_ClientRequestReachesHandler(t *testing.T) {
	client, kms, _ := newKeyMaterialClient(t)
	wrapped := []byte("wrapped-material")

	resp, err := client.UnwrapKey(context.Background(), &kmsclient.UnwrapKeyRequest{
		KeyID:             "key-2",
		WrappingKeyID:     "wrap-1",
		WrappingAlgorithm: kmsclient.WrapAlgAESKWP,
		WrappedMaterial:   wrapped,
		Algorithm:         kmsclient.AlgorithmECDSAP384,
		Name:              "unwrapped",
		SourceKeyID:       "key-1",
		SourceVersion:     4,
	})
	require.NoError(t, err)
	assert.Equal(t, &kmsclient.UnwrapKeyResponse{KeyID: "key-2", Version: 1, Algorithm: kmsclient.AlgorithmECDSAP384}, resp)

	require.Len(t, kms.unwraps, 1)
	assert.Equal(t, &service.UnwrapKeyRequest{
		KeyID:             "key-2",
		WrappedKey:        wrapped,
		WrappingKeyID:     "wrap-1",
		WrappingAlgorithm: "AES-KWP",
		Algorithm:         string(service.KeyAlgECDSAP384),
		SourceKeyID:       "key-1",
		SourceVersion:     4,
		Name:              "unwrapped",
	}, kms.unwraps[0])
}

// TestEmbeddedKMS_KeyMaterial_RefusesBadValues checks values the service
// cannot honour are refused before the service, and that no key bytes are
// copied into the error.
func TestEmbeddedKMS_KeyMaterial_RefusesBadValues(t *testing.T) {
	secret := []byte("do-not-leak-this-key")
	cases := []struct {
		name string
		call func(kmsclient.Client) error
	}{
		{"ExportUnknownWrappingAlgorithm", func(c kmsclient.Client) error {
			_, err := c.ExportKey(context.Background(), &kmsclient.ExportKeyRequest{KeyID: "k", WrappingKeyID: "w", WrappingAlgorithm: kmsclient.WrappingAlgorithm(99)})
			return err
		}},
		{"ExportUnknownFormat", func(c kmsclient.Client) error {
			_, err := c.ExportKey(context.Background(), &kmsclient.ExportKeyRequest{KeyID: "k", WrappingKeyID: "w", Format: kmsclient.ExportFormat(99)})
			return err
		}},
		{"ExportNegativeVersion", func(c kmsclient.Client) error {
			_, err := c.ExportKey(context.Background(), &kmsclient.ExportKeyRequest{KeyID: "k", WrappingKeyID: "w", Version: -1})
			return err
		}},
		{"UnwrapUnknownWrappingAlgorithm", func(c kmsclient.Client) error {
			_, err := c.UnwrapKey(context.Background(), &kmsclient.UnwrapKeyRequest{WrappingKeyID: "w", WrappedMaterial: secret, WrappingAlgorithm: kmsclient.WrappingAlgorithm(99)})
			return err
		}},
		{"UnwrapNegativeSourceVersion", func(c kmsclient.Client) error {
			_, err := c.UnwrapKey(context.Background(), &kmsclient.UnwrapKeyRequest{WrappingKeyID: "w", WrappedMaterial: secret, SourceVersion: -1})
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, kms, _ := newKeyMaterialClient(t)
			err := tc.call(client)
			require.Error(t, err)
			assert.True(t, errors.IsInvalidInput(err), "err = %v, want InvalidInput", err)
			assert.NotContains(t, err.Error(), string(secret))
			assert.Empty(t, kms.exports, "a refused request must not reach the service")
			assert.Empty(t, kms.unwraps, "a refused request must not reach the service")
		})
	}
}

// TestEmbeddedKMS_KeyMaterial_WrongRequestType checks each handler still
// refuses a request of the wrong type with the typed error.
func TestEmbeddedKMS_KeyMaterial_WrongRequestType(t *testing.T) {
	for _, method := range []string{"ImportKey", "ExportKey", "UnwrapKey"} {
		t.Run(method, func(t *testing.T) {
			_, kms, tr := newKeyMaterialClient(t)
			_, err := tr.Request(context.Background(), "quicbaas.KMS", method, &kmsclient.UpdateKeyStatusRequest{KeyID: "k"})
			var invalid *InvalidRequestTypeError
			assert.ErrorAs(t, err, &invalid)
			assert.Empty(t, kms.imports)
			assert.Empty(t, kms.exports)
			assert.Empty(t, kms.unwraps)
		})
	}
}

// TestEmbeddedKMS_KeyMaterial_ServiceTypeStillAccepted checks direct service
// callers keep working alongside the SDK client type.
func TestEmbeddedKMS_KeyMaterial_ServiceTypeStillAccepted(t *testing.T) {
	_, kms, tr := newKeyMaterialClient(t)
	ctx := context.Background()

	_, err := tr.Request(ctx, "quicbaas.KMS", "ImportKey", &service.ImportKeyRequest{Name: "k"})
	require.NoError(t, err)
	_, err = tr.Request(ctx, "quicbaas.KMS", "ExportKey", &service.ExportKeyRequest{KeyID: "k"})
	require.NoError(t, err)
	_, err = tr.Request(ctx, "quicbaas.KMS", "UnwrapKey", &service.UnwrapKeyRequest{KeyID: "k"})
	require.NoError(t, err)
	assert.Len(t, kms.imports, 1)
	assert.Len(t, kms.exports, 1)
	assert.Len(t, kms.unwraps, 1)
}
