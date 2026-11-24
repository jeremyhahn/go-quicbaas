package service

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"testing"

	"github.com/stretchr/testify/require"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	qbcrypto "github.com/jeremyhahn/go-quicbaas/pkg/crypto"
)

func TestValidateSignRequest_Padding(t *testing.T) {
	svc := &cryptoService{}
	base := func(mod func(r *SignRequest)) *SignRequest {
		r := &SignRequest{KeyID: "k", Message: []byte("m")}
		mod(r)
		return r
	}

	tests := []struct {
		name    string
		req     *SignRequest
		wantErr bool
	}{
		{"empty padding", base(func(r *SignRequest) {}), false},
		{"explicit PKCS1V15", base(func(r *SignRequest) { r.Padding = SignaturePaddingPKCS1v15 }), false},
		{"PSS SHA256 salt 0", base(func(r *SignRequest) {
			r.Padding = SignaturePaddingPSS
			r.HashAlgorithm = HashAlgSHA256
		}), false},
		{"PSS default hash", base(func(r *SignRequest) { r.Padding = SignaturePaddingPSS }), false},
		{"PSS SHA512 salt 64 MGF1 SHA512", base(func(r *SignRequest) {
			r.Padding = SignaturePaddingPSS
			r.HashAlgorithm = HashAlgSHA512
			r.PSSSaltLength = 64
			r.MGF1Hash = HashAlgSHA512
		}), false},
		{"bad padding name", base(func(r *SignRequest) { r.Padding = "OAEP" }), true},
		{"PSS with SHA1", base(func(r *SignRequest) {
			r.Padding = SignaturePaddingPSS
			r.HashAlgorithm = "SHA1"
		}), true},
		{"PSS with SHA3-256", base(func(r *SignRequest) {
			r.Padding = SignaturePaddingPSS
			r.HashAlgorithm = HashAlgSHA3_256
		}), true},
		{"PSS negative salt", base(func(r *SignRequest) {
			r.Padding = SignaturePaddingPSS
			r.PSSSaltLength = -1
		}), true},
		{"PSS MGF1 differs from hash", base(func(r *SignRequest) {
			r.Padding = SignaturePaddingPSS
			r.HashAlgorithm = HashAlgSHA256
			r.MGF1Hash = HashAlgSHA384
		}), true},
		{"salt without PSS", base(func(r *SignRequest) { r.PSSSaltLength = 32 }), true},
		{"MGF1 without PSS", base(func(r *SignRequest) { r.MGF1Hash = HashAlgSHA256 }), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := svc.validateSignRequest(tc.req)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCryptoSign_PSS_RealSoftwareBackend(t *testing.T) {
	manager, cleanup := realKMSManagerForTest(t)
	defer cleanup()

	ctx := systemTenantCtx()
	kmsSvc := newKMSService(manager, newMockLogger())

	const keyName = "pss-signing-key"
	_, err := kmsSvc.GenerateKey(ctx, &GenerateKeyRequest{
		Name: keyName,
		KeyAttributes: &xkmssdk.KeyAttributes{
			CN:            keyName,
			KeyType:       xkmssdk.KeyTypeSigning,
			KeyAlgorithm:  x509.RSA,
			RSAAttributes: &xkmssdk.RSAAttributes{KeySize: 2048},
		},
		Usage: []KeyUsage{KeyUsageSign},
	})
	require.NoError(t, err)

	pubResp, err := kmsSvc.GetPublicKey(ctx, &GetPublicKeyRequest{KeyID: keyName})
	require.NoError(t, err)
	parsed, err := x509.ParsePKIXPublicKey(pubResp.PublicKey)
	require.NoError(t, err)
	pub, ok := parsed.(*rsa.PublicKey)
	require.True(t, ok, "expected an RSA public key, got %T", parsed)

	cryptoSvc := newCryptoService(qbcrypto.NewMemoryCryptoManager(manager), manager, newMockLogger(), nil)

	message := []byte("sign me with RSA-PSS")
	sum := sha256.Sum256(message)
	opts := &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash, Hash: crypto.SHA256}

	t.Run("message hashed by the service", func(t *testing.T) {
		resp, err := cryptoSvc.Sign(ctx, &SignRequest{
			KeyID:         keyName,
			Message:       message,
			HashAlgorithm: HashAlgSHA256,
			Padding:       SignaturePaddingPSS,
		})
		require.NoError(t, err)
		require.NoError(t, rsa.VerifyPSS(pub, crypto.SHA256, sum[:], resp.Signature, opts))
	})

	t.Run("pre-hashed digest", func(t *testing.T) {
		resp, err := cryptoSvc.Sign(ctx, &SignRequest{
			KeyID:         keyName,
			Message:       sum[:],
			HashAlgorithm: HashAlgSHA256,
			PreHashed:     true,
			Padding:       SignaturePaddingPSS,
		})
		require.NoError(t, err)
		require.NoError(t, rsa.VerifyPSS(pub, crypto.SHA256, sum[:], resp.Signature, opts))
	})
}

// TestCryptoSign_PSS_SHA384_RealSoftwareBackend signs with RSA-PSS and
// SHA-384, a raw message (hashed by the service) and a pre-hashed digest.
func TestCryptoSign_PSS_SHA384_RealSoftwareBackend(t *testing.T) {
	manager, cleanup := realKMSManagerForTest(t)
	defer cleanup()

	ctx := systemTenantCtx()
	kmsSvc := newKMSService(manager, newMockLogger())

	const keyName = "pss-sha384-signing-key"
	_, err := kmsSvc.GenerateKey(ctx, &GenerateKeyRequest{
		Name: keyName,
		KeyAttributes: &xkmssdk.KeyAttributes{
			CN:            keyName,
			KeyType:       xkmssdk.KeyTypeSigning,
			KeyAlgorithm:  x509.RSA,
			RSAAttributes: &xkmssdk.RSAAttributes{KeySize: 2048},
		},
		Usage: []KeyUsage{KeyUsageSign},
	})
	require.NoError(t, err)

	pubResp, err := kmsSvc.GetPublicKey(ctx, &GetPublicKeyRequest{KeyID: keyName})
	require.NoError(t, err)
	parsed, err := x509.ParsePKIXPublicKey(pubResp.PublicKey)
	require.NoError(t, err)
	pub, ok := parsed.(*rsa.PublicKey)
	require.True(t, ok, "expected an RSA public key, got %T", parsed)

	cryptoSvc := newCryptoService(qbcrypto.NewMemoryCryptoManager(manager), manager, newMockLogger(), nil)

	message := []byte("sign me with RSA-PSS and SHA-384")
	sum := sha512.Sum384(message)
	opts := &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash, Hash: crypto.SHA384}

	t.Run("message hashed by the service", func(t *testing.T) {
		resp, err := cryptoSvc.Sign(ctx, &SignRequest{
			KeyID:         keyName,
			Message:       message,
			HashAlgorithm: HashAlgSHA384,
			Padding:       SignaturePaddingPSS,
		})
		require.NoError(t, err)
		require.NoError(t, rsa.VerifyPSS(pub, crypto.SHA384, sum[:], resp.Signature, opts))
	})

	t.Run("pre-hashed digest", func(t *testing.T) {
		resp, err := cryptoSvc.Sign(ctx, &SignRequest{
			KeyID:         keyName,
			Message:       sum[:],
			HashAlgorithm: HashAlgSHA384,
			PreHashed:     true,
			Padding:       SignaturePaddingPSS,
		})
		require.NoError(t, err)
		require.NoError(t, rsa.VerifyPSS(pub, crypto.SHA384, sum[:], resp.Signature, opts))
	})
}
