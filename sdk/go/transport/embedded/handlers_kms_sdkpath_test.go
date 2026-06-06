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

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	dekclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/dek"
	kmsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
)

func newKMSTransport(t *testing.T) *Transport {
	t.Helper()
	tr, err := New(&mockQuicBaaS{kmsService: &mockKMSService{}})
	require.NoError(t, err)
	return tr
}

// TestHandleKMSGenerateKey_SDKClientType drives the GenerateKey handler with
// the SDK client request type, exercising sdkAlgToKeyAttributes and
// convertSDKUsagesToService across every algorithm and usage value.
func TestHandleKMSGenerateKey_SDKClientType(t *testing.T) {
	tr := newKMSTransport(t)
	ctx := context.Background()

	algorithms := []kmsclient.KeyAlgorithm{
		kmsclient.AlgorithmAES256GCM,
		kmsclient.AlgorithmChaCha20Poly1305,
		kmsclient.AlgorithmRSA2048,
		kmsclient.AlgorithmRSA4096,
		kmsclient.AlgorithmECDSAP256,
		kmsclient.AlgorithmECDSAP384,
		kmsclient.AlgorithmED25519,
		kmsclient.KeyAlgorithm(9999), // default branch
	}
	usages := []kmsclient.KeyUsage{
		kmsclient.KeyUsageEncrypt,
		kmsclient.KeyUsageDecrypt,
		kmsclient.KeyUsageSign,
		kmsclient.KeyUsageVerify,
		kmsclient.KeyUsageWrap,
		kmsclient.KeyUsageUnwrap,
		kmsclient.KeyUsage(9999), // default branch
	}

	for _, alg := range algorithms {
		req := &kmsclient.GenerateKeyRequest{
			Name:      "k",
			Algorithm: alg,
			Usage:     usages,
		}
		resp, err := tr.Request(ctx, "quicbaas.KMS", "GenerateKey", req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	}
}

func TestHandleKMSGenerateKey_WrongType(t *testing.T) {
	tr := newKMSTransport(t)
	resp, err := tr.Request(context.Background(), "quicbaas.KMS", "GenerateKey", &service.PutRequest{})
	require.Error(t, err)
	assert.Nil(t, resp)
}

// sdkKeyIDReq mimics the unexported SDK request structs that carry only a
// KeyID, exercising the reflection-based fallback in GetKey/GetPublicKey.
type sdkKeyIDReq struct{ KeyID string }

func TestHandleKMSGetPublicKey_AllPaths(t *testing.T) {
	tr := newKMSTransport(t)
	ctx := context.Background()

	t.Run("service type", func(t *testing.T) {
		resp, err := tr.Request(ctx, "quicbaas.KMS", "GetPublicKey", &service.GetPublicKeyRequest{KeyID: "k1"})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("sdk struct via reflection", func(t *testing.T) {
		resp, err := tr.Request(ctx, "quicbaas.KMS", "GetPublicKey", &sdkKeyIDReq{KeyID: "k1"})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("non-struct request", func(t *testing.T) {
		bad := "not-a-struct"
		resp, err := tr.Request(ctx, "quicbaas.KMS", "GetPublicKey", &bad)
		require.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("struct missing KeyID", func(t *testing.T) {
		resp, err := tr.Request(ctx, "quicbaas.KMS", "GetPublicKey", &struct{ Other string }{})
		require.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("not configured", func(t *testing.T) {
		nt, err := New(&mockQuicBaaS{})
		require.NoError(t, err)
		resp, err := nt.Request(ctx, "quicbaas.KMS", "GetPublicKey", &service.GetPublicKeyRequest{})
		require.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestHandleKMSGetKey_ReflectionPaths(t *testing.T) {
	tr := newKMSTransport(t)
	ctx := context.Background()

	t.Run("sdk struct via reflection", func(t *testing.T) {
		resp, err := tr.Request(ctx, "quicbaas.KMS", "GetKey", &sdkKeyIDReq{KeyID: "k1"})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("non-struct request", func(t *testing.T) {
		bad := 42
		resp, err := tr.Request(ctx, "quicbaas.KMS", "GetKey", &bad)
		require.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("struct missing KeyID", func(t *testing.T) {
		resp, err := tr.Request(ctx, "quicbaas.KMS", "GetKey", &struct{ Other string }{})
		require.Error(t, err)
		assert.Nil(t, resp)
	})
}

// dekPathReq mimics the dek SDK's unexported getDEKRequest{Path} so the
// dekRequestPath reflection fallback is exercised.
type dekPathReq struct{ Path string }

func TestHandleKMSDEK_SDKClientTypes(t *testing.T) {
	tr := newKMSTransport(t)
	ctx := context.Background()

	t.Run("CreateDEK sdk type all algorithms", func(t *testing.T) {
		algs := []string{
			"AES-256-GCM", "AES256GCM",
			"CHACHA20-POLY1305", "CHACHA20POLY1305",
			"RSA-2048", "RSA2048", "RSA-4096", "RSA4096",
			"ECDSA-P256", "ECDSAP256", "ECDSA-P384", "ECDSAP384",
			"ED25519", "", "unknown-alg",
		}
		for _, a := range algs {
			resp, err := tr.Request(ctx, "quicbaas.DEK", "CreateDEK", &dekclient.CreateDEKRequest{Path: "p", Algorithm: a})
			require.NoError(t, err)
			assert.NotNil(t, resp)
		}
	})

	t.Run("GetDEK via reflection path", func(t *testing.T) {
		resp, err := tr.Request(ctx, "quicbaas.DEK", "GetDEK", &dekPathReq{Path: "p"})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("GetDEKStatus via reflection path", func(t *testing.T) {
		resp, err := tr.Request(ctx, "quicbaas.DEK", "GetDEKStatus", &dekPathReq{Path: "p"})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("ListDEKs sdk type", func(t *testing.T) {
		resp, err := tr.Request(ctx, "quicbaas.DEK", "ListDEKs", &dekclient.ListDEKsRequest{Prefix: "p"})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("RotateDEK sdk type", func(t *testing.T) {
		resp, err := tr.Request(ctx, "quicbaas.DEK", "RotateDEK", &dekclient.RotateDEKRequest{Path: "p"})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("DeleteDEK sdk type", func(t *testing.T) {
		resp, err := tr.Request(ctx, "quicbaas.DEK", "DeleteDEK", &dekclient.DeleteDEKRequest{Path: "p"})
		require.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("GetDEK wrong type", func(t *testing.T) {
		resp, err := tr.Request(ctx, "quicbaas.DEK", "GetDEK", &struct{ NoPath int }{})
		require.Error(t, err)
		assert.Nil(t, resp)
	})

	t.Run("CreateDEK wrong type", func(t *testing.T) {
		resp, err := tr.Request(ctx, "quicbaas.DEK", "CreateDEK", &service.PutRequest{})
		require.Error(t, err)
		assert.Nil(t, resp)
	})
}

// TestDEKRequestPath_EdgeCases drives the structural extraction helper.
func TestDEKRequestPath_EdgeCases(t *testing.T) {
	t.Run("valid pointer struct", func(t *testing.T) {
		path, ok := dekRequestPath(&dekPathReq{Path: "abc"})
		require.True(t, ok)
		assert.Equal(t, "abc", path)
	})

	t.Run("non-struct", func(t *testing.T) {
		s := "hello"
		_, ok := dekRequestPath(&s)
		assert.False(t, ok)
	})

	t.Run("struct without Path", func(t *testing.T) {
		_, ok := dekRequestPath(struct{ Other int }{})
		assert.False(t, ok)
	})

	t.Run("Path field wrong kind", func(t *testing.T) {
		_, ok := dekRequestPath(struct{ Path int }{Path: 1})
		assert.False(t, ok)
	})
}

// TestStringToKeyAlgorithm covers every branch of the string->enum mapping.
func TestStringToKeyAlgorithm(t *testing.T) {
	cases := map[string]service.KeyAlgorithm{
		"AES-256-GCM":       service.KeyAlgAES256GCM,
		"AES256GCM":         service.KeyAlgAES256GCM,
		"CHACHA20-POLY1305": service.KeyAlgChaCha20Poly1305,
		"CHACHA20POLY1305":  service.KeyAlgChaCha20Poly1305,
		"RSA-2048":          service.KeyAlgRSA2048,
		"RSA2048":           service.KeyAlgRSA2048,
		"RSA-4096":          service.KeyAlgRSA4096,
		"RSA4096":           service.KeyAlgRSA4096,
		"ECDSA-P256":        service.KeyAlgECDSAP256,
		"ECDSAP256":         service.KeyAlgECDSAP256,
		"ECDSA-P384":        service.KeyAlgECDSAP384,
		"ECDSAP384":         service.KeyAlgECDSAP384,
		"ED25519":           service.KeyAlgEd25519,
		"":                  service.KeyAlgAES256GCM,
		"garbage":           service.KeyAlgAES256GCM,
	}
	for in, want := range cases {
		assert.Equal(t, want, stringToKeyAlgorithm(in), "input=%q", in)
	}
}

// TestSDKAlgToKeyAttributes verifies the algorithm-to-attributes mapping.
func TestSDKAlgToKeyAttributes(t *testing.T) {
	assert.Equal(t, xkmssdk.SymmetricAES256GCM, sdkAlgToKeyAttributes(kmsclient.AlgorithmAES256GCM).SymmetricAlgorithm)
	assert.Equal(t, xkmssdk.SymmetricChaCha20Poly1305, sdkAlgToKeyAttributes(kmsclient.AlgorithmChaCha20Poly1305).SymmetricAlgorithm)

	rsa := sdkAlgToKeyAttributes(kmsclient.AlgorithmRSA2048)
	assert.Equal(t, x509.RSA, rsa.KeyAlgorithm)
	require.NotNil(t, rsa.RSAAttributes)
	assert.Equal(t, 2048, rsa.RSAAttributes.KeySize)

	ecdsa := sdkAlgToKeyAttributes(kmsclient.AlgorithmECDSAP384)
	assert.Equal(t, x509.ECDSA, ecdsa.KeyAlgorithm)
	require.NotNil(t, ecdsa.ECCAttributes)

	ed := sdkAlgToKeyAttributes(kmsclient.AlgorithmED25519)
	assert.Equal(t, x509.Ed25519, ed.KeyAlgorithm)

	// Default branch falls back to AES-256-GCM.
	assert.Equal(t, xkmssdk.SymmetricAES256GCM, sdkAlgToKeyAttributes(kmsclient.KeyAlgorithm(9999)).SymmetricAlgorithm)
}

// TestConvertSDKUsageToService covers the usage enum mapping including the
// default branch.
func TestConvertSDKUsageToService(t *testing.T) {
	assert.Equal(t, service.KeyUsageEncrypt, convertSDKUsageToService(kmsclient.KeyUsageEncrypt))
	assert.Equal(t, service.KeyUsageDecrypt, convertSDKUsageToService(kmsclient.KeyUsageDecrypt))
	assert.Equal(t, service.KeyUsageSign, convertSDKUsageToService(kmsclient.KeyUsageSign))
	assert.Equal(t, service.KeyUsageVerify, convertSDKUsageToService(kmsclient.KeyUsageVerify))
	assert.Equal(t, service.KeyUsageWrapKey, convertSDKUsageToService(kmsclient.KeyUsageWrap))
	assert.Equal(t, service.KeyUsageWrapKey, convertSDKUsageToService(kmsclient.KeyUsageUnwrap))
	assert.Equal(t, service.KeyUsageEncrypt, convertSDKUsageToService(kmsclient.KeyUsage(9999)))

	// Empty slice short-circuits to nil.
	assert.Nil(t, convertSDKUsagesToService(nil))
}
