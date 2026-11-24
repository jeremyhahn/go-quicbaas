package quicbaas

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"errors"
	"testing"

	sdkcrypto "github.com/jeremyhahn/go-quicbaas/sdk/go/client/crypto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

func TestSignRequestForOpts(t *testing.T) {
	digest := make([]byte, 32)
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	pub := &priv.PublicKey

	tests := []struct {
		name     string
		opts     crypto.SignerOpts
		wantPad  service.SignaturePadding
		wantSalt int
	}{
		{"plain hash is PKCS1v15", crypto.SHA256, "", 0},
		{"PSS salt equals hash", &rsa.PSSOptions{Hash: crypto.SHA256, SaltLength: rsa.PSSSaltLengthEqualsHash}, service.SignaturePaddingPSS, 0},
		{"PSS salt auto", &rsa.PSSOptions{Hash: crypto.SHA256, SaltLength: rsa.PSSSaltLengthAuto}, service.SignaturePaddingPSS, 222},
		{"PSS explicit salt kept", &rsa.PSSOptions{Hash: crypto.SHA256, SaltLength: 20}, service.SignaturePaddingPSS, 20},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, err := signRequestForOpts("key-1", pub, digest, tc.opts)
			if err != nil {
				t.Fatalf("signRequestForOpts: %v", err)
			}
			if req.KeyID != "key-1" || !req.PreHashed || req.HashAlg != sdkcrypto.HashSHA256 {
				t.Errorf("unexpected base request: %+v", req)
			}
			if req.Padding != tc.wantPad {
				t.Errorf("Padding = %q, want %q", req.Padding, tc.wantPad)
			}
			if req.PSSSaltLength != tc.wantSalt {
				t.Errorf("PSSSaltLength = %d, want %d", req.PSSSaltLength, tc.wantSalt)
			}
			if req.MGF1Hash != nil {
				t.Errorf("MGF1Hash = %v, want nil", *req.MGF1Hash)
			}
		})
	}

	if _, err := signRequestForOpts("key-1", pub, digest, crypto.MD5); err == nil {
		t.Error("expected an error for an unsupported hash")
	}
}

func TestSignRequestForOpts_PSSAutoSalt(t *testing.T) {
	rsa2048, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey 2048: %v", err)
	}
	rsa1024, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("GenerateKey 1024: %v", err)
	}

	auto := []struct {
		name     string
		pub      *rsa.PublicKey
		hash     crypto.Hash
		wantSalt int
	}{
		{"RSA-2048 SHA-256", &rsa2048.PublicKey, crypto.SHA256, 222},
		{"RSA-2048 SHA-512", &rsa2048.PublicKey, crypto.SHA512, 190},
		{"RSA-1024 SHA-512", &rsa1024.PublicKey, crypto.SHA512, 62},
	}
	for _, tc := range auto {
		t.Run("auto "+tc.name, func(t *testing.T) {
			digest := make([]byte, tc.hash.Size())
			req, err := signRequestForOpts("key-1", tc.pub, digest,
				&rsa.PSSOptions{Hash: tc.hash, SaltLength: rsa.PSSSaltLengthAuto})
			if err != nil {
				t.Fatalf("signRequestForOpts: %v", err)
			}
			if req.Padding != service.SignaturePaddingPSS {
				t.Errorf("Padding = %q, want PSS", req.Padding)
			}
			if req.PSSSaltLength != tc.wantSalt {
				t.Errorf("PSSSaltLength = %d, want %d", req.PSSSaltLength, tc.wantSalt)
			}
		})
	}

	t.Run("auto on a non-RSA key is refused", func(t *testing.T) {
		ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatalf("ecdsa.GenerateKey: %v", err)
		}
		_, err = signRequestForOpts("key-1", &ec.PublicKey, make([]byte, 32),
			&rsa.PSSOptions{Hash: crypto.SHA256, SaltLength: rsa.PSSSaltLengthAuto})
		var target *ErrUnsupportedKeyType
		if !errors.As(err, &target) {
			t.Fatalf("err = %v, want *ErrUnsupportedKeyType", err)
		}
	})

	t.Run("other negative salt is refused", func(t *testing.T) {
		_, err := signRequestForOpts("key-1", &rsa2048.PublicKey, make([]byte, 32),
			&rsa.PSSOptions{Hash: crypto.SHA256, SaltLength: -2})
		var target *ErrSignerPSSSaltLength
		if !errors.As(err, &target) {
			t.Fatalf("err = %v, want *ErrSignerPSSSaltLength", err)
		}
		if target.SaltLength != -2 {
			t.Errorf("SaltLength = %d, want -2", target.SaltLength)
		}
	})

	t.Run("equals hash and explicit salt unchanged", func(t *testing.T) {
		for _, c := range []struct{ in, want int }{{rsa.PSSSaltLengthEqualsHash, 0}, {20, 20}} {
			req, err := signRequestForOpts("key-1", &rsa2048.PublicKey, make([]byte, 32),
				&rsa.PSSOptions{Hash: crypto.SHA256, SaltLength: c.in})
			if err != nil {
				t.Fatalf("signRequestForOpts(%d): %v", c.in, err)
			}
			if req.PSSSaltLength != c.want {
				t.Errorf("salt %d: PSSSaltLength = %d, want %d", c.in, req.PSSSaltLength, c.want)
			}
		}
	})

	t.Run("auto salt agrees with crypto/rsa", func(t *testing.T) {
		sum := sha256.Sum256([]byte("pss auto salt"))
		digest := sum[:]
		req, err := signRequestForOpts("key-1", &rsa2048.PublicKey, digest,
			&rsa.PSSOptions{Hash: crypto.SHA256, SaltLength: rsa.PSSSaltLengthAuto})
		if err != nil {
			t.Fatalf("signRequestForOpts: %v", err)
		}

		sig, err := rsa.SignPSS(rand.Reader, rsa2048, crypto.SHA256, digest,
			&rsa.PSSOptions{SaltLength: req.PSSSaltLength, Hash: crypto.SHA256})
		if err != nil {
			t.Fatalf("SignPSS with sent salt: %v", err)
		}
		if err := rsa.VerifyPSS(&rsa2048.PublicKey, crypto.SHA256, digest, sig,
			&rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthAuto, Hash: crypto.SHA256}); err != nil {
			t.Errorf("VerifyPSS auto of sent-salt signature: %v", err)
		}

		autoSig, err := rsa.SignPSS(rand.Reader, rsa2048, crypto.SHA256, digest,
			&rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthAuto, Hash: crypto.SHA256})
		if err != nil {
			t.Fatalf("SignPSS auto: %v", err)
		}
		if err := rsa.VerifyPSS(&rsa2048.PublicKey, crypto.SHA256, digest, autoSig,
			&rsa.PSSOptions{SaltLength: req.PSSSaltLength, Hash: crypto.SHA256}); err != nil {
			t.Errorf("VerifyPSS with sent salt of auto signature: %v", err)
		}
	})
}
