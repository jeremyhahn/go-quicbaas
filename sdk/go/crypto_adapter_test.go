package quicbaas

import (
	"crypto"
	"crypto/rsa"
	"testing"

	sdkcrypto "github.com/jeremyhahn/go-quicbaas/sdk/go/client/crypto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

func TestSignRequestForOpts(t *testing.T) {
	digest := make([]byte, 32)

	tests := []struct {
		name     string
		opts     crypto.SignerOpts
		wantPad  service.SignaturePadding
		wantSalt int
	}{
		{"plain hash is PKCS1v15", crypto.SHA256, "", 0},
		{"PSS salt equals hash", &rsa.PSSOptions{Hash: crypto.SHA256, SaltLength: rsa.PSSSaltLengthEqualsHash}, service.SignaturePaddingPSS, 0},
		{"PSS salt auto", &rsa.PSSOptions{Hash: crypto.SHA256, SaltLength: rsa.PSSSaltLengthAuto}, service.SignaturePaddingPSS, 0},
		{"PSS explicit salt kept", &rsa.PSSOptions{Hash: crypto.SHA256, SaltLength: 20}, service.SignaturePaddingPSS, 20},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, err := signRequestForOpts("key-1", digest, tc.opts)
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

	if _, err := signRequestForOpts("key-1", digest, crypto.MD5); err == nil {
		t.Error("expected an error for an unsupported hash")
	}
}
