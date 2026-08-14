// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package quicbaas

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	stderrors "errors"
	"math/big"
	"testing"
	"time"

	attestationclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/attestation"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// recordingAttestationTransport records the last routed request and replays a
// fixed response, so the SDK surface can be exercised with no server.
type recordingAttestationTransport struct {
	transport.Transport

	service string
	method  string
	request interface{}

	response interface{}
	err      error
}

func (t *recordingAttestationTransport) Request(_ context.Context, service, method string, req interface{}) (interface{}, error) {
	t.service = service
	t.method = method
	t.request = req
	if t.err != nil {
		return nil, t.err
	}
	return t.response, nil
}

// newSignerCertDER mints a self-signed ECDSA certificate and returns its DER.
func newSignerCertDER(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "image-signer"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	return der
}

func TestAttestationService_Verify_Routing(t *testing.T) {
	tr := &recordingAttestationTransport{response: &AttestationVerifyResponse{Valid: true}}
	svc := newAttestationService(tr)

	resp, err := svc.Verify(context.Background(), &AttestationVerifyRequest{
		Format:    "tpm2",
		ChainPEM:  [][]byte{[]byte("cert")},
		Signature: []byte("sig"),
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !resp.Valid {
		t.Fatal("response not returned")
	}
	if tr.service != "quicbaas.Attestation" {
		t.Fatalf("service = %q, want quicbaas.Attestation", tr.service)
	}
	if tr.method != "Verify" {
		t.Fatalf("method = %q", tr.method)
	}
}

func TestAttestationService_Verify_Validation(t *testing.T) {
	tr := &recordingAttestationTransport{response: &AttestationVerifyResponse{}}
	svc := newAttestationService(tr)

	tests := []struct {
		name string
		ctx  context.Context
		req  *AttestationVerifyRequest
	}{
		{name: "NilContext", ctx: nil, req: &AttestationVerifyRequest{Format: "tpm2", ChainPEM: [][]byte{{1}}, Signature: []byte{1}}},
		{name: "NilRequest", ctx: context.Background()},
		{name: "NoFormat", ctx: context.Background(), req: &AttestationVerifyRequest{ChainPEM: [][]byte{{1}}, Signature: []byte{1}}},
		{name: "NoChain", ctx: context.Background(), req: &AttestationVerifyRequest{Format: "tpm2", Signature: []byte{1}}},
		{name: "NoSignature", ctx: context.Background(), req: &AttestationVerifyRequest{Format: "tpm2", ChainPEM: [][]byte{{1}}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := svc.Verify(tt.ctx, tt.req); err == nil {
				t.Fatal("expected a validation error")
			}
		})
	}

	t.Run("TransportError", func(t *testing.T) {
		sentinel := stderrors.New("transport down")
		failing := newAttestationService(&recordingAttestationTransport{err: sentinel})
		_, err := failing.Verify(context.Background(), &AttestationVerifyRequest{
			Format: "tpm2", ChainPEM: [][]byte{{1}}, Signature: []byte{1},
		})
		if !stderrors.Is(err, sentinel) {
			t.Fatalf("error %v does not wrap the transport error", err)
		}
	})

	t.Run("WrongResponseType", func(t *testing.T) {
		mismatched := newAttestationService(&recordingAttestationTransport{
			response: &AttestationVerifyChainResponse{},
		})
		_, err := mismatched.Verify(context.Background(), &AttestationVerifyRequest{
			Format: "tpm2", ChainPEM: [][]byte{{1}}, Signature: []byte{1},
		})
		if err == nil {
			t.Fatal("a mismatched response type must be rejected")
		}
	})
}

func TestAttestationService_VerifyChain_Routing(t *testing.T) {
	tr := &recordingAttestationTransport{response: &AttestationVerifyChainResponse{Valid: true}}
	svc := newAttestationService(tr)

	resp, err := svc.VerifyChain(context.Background(), &AttestationVerifyChainRequest{
		Format:   "tpm2",
		ChainPEM: [][]byte{[]byte("cert")},
	})
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if !resp.Valid {
		t.Fatal("response not returned")
	}
	if tr.method != "VerifyChain" {
		t.Fatalf("method = %q", tr.method)
	}
}

func TestAttestationService_VerifyChain_Validation(t *testing.T) {
	svc := newAttestationService(&recordingAttestationTransport{
		response: &AttestationVerifyChainResponse{},
	})

	if _, err := svc.VerifyChain(nil, &AttestationVerifyChainRequest{ChainPEM: [][]byte{{1}}}); err == nil {
		t.Error("expected an error for a nil context")
	}
	if _, err := svc.VerifyChain(context.Background(), nil); err == nil {
		t.Error("expected an error for a nil request")
	}
	if _, err := svc.VerifyChain(context.Background(), &AttestationVerifyChainRequest{}); err == nil {
		t.Error("expected an error for an empty chain")
	}

	mismatched := newAttestationService(&recordingAttestationTransport{
		response: &AttestationVerifyResponse{},
	})
	if _, err := mismatched.VerifyChain(context.Background(), &AttestationVerifyChainRequest{
		ChainPEM: [][]byte{{1}},
	}); err == nil {
		t.Error("a mismatched response type must be rejected")
	}
}

func TestAttestationService_AttestContainer_Routing(t *testing.T) {
	tr := &recordingAttestationTransport{response: &AttestContainerResponse{Attested: true}}
	svc := newAttestationService(tr)

	resp, err := svc.AttestContainer(context.Background(), &AttestContainerRequest{
		Image: "alpine@sha256:abc",
	})
	if err != nil {
		t.Fatalf("AttestContainer: %v", err)
	}
	if !resp.Attested {
		t.Fatal("response not returned")
	}
	if tr.method != "AttestContainer" {
		t.Fatalf("method = %q", tr.method)
	}
}

func TestAttestationService_AttestContainer_Validation(t *testing.T) {
	svc := newAttestationService(&recordingAttestationTransport{
		response: &AttestContainerResponse{},
	})

	if _, err := svc.AttestContainer(nil, &AttestContainerRequest{Image: "alpine"}); err == nil {
		t.Error("expected an error for a nil context")
	}
	if _, err := svc.AttestContainer(context.Background(), nil); err == nil {
		t.Error("expected an error for a nil request")
	}
	if _, err := svc.AttestContainer(context.Background(), &AttestContainerRequest{}); err == nil {
		t.Error("expected an error for a missing image")
	}

	mismatched := newAttestationService(&recordingAttestationTransport{
		response: &AttestationVerifyResponse{},
	})
	if _, err := mismatched.AttestContainer(context.Background(), &AttestContainerRequest{
		Image: "alpine",
	}); err == nil {
		t.Error("a mismatched response type must be rejected")
	}
}

// TestVerifyImageSignature_ComposesVerify proves the Tier 0 endorsement check
// is expressed as an attestation statement rather than a fourth transport
// verb: the digest string becomes the attestation data, and the signer's own
// key is the attested key.
func TestVerifyImageSignature_ComposesVerify(t *testing.T) {
	certDER := newSignerCertDER(t)
	const digest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	tr := &recordingAttestationTransport{response: &AttestationVerifyResponse{Valid: true}}
	svc := newAttestationService(tr)

	resp, err := svc.VerifyImageSignature(context.Background(), &ImageEndorsement{
		Digest:             digest,
		Signature:          []byte("sig"),
		SignerChainPEM:     [][]byte{certDER},
		SignatureAlgorithm: "ECDSA-SHA256",
	}, &AttestationVerifyOptions{AllowSelfSigned: true})
	if err != nil {
		t.Fatalf("VerifyImageSignature: %v", err)
	}
	if !resp.Valid {
		t.Fatal("endorsement should verify")
	}

	if tr.method != "Verify" {
		t.Fatalf("method = %q, want the shared Verify verb", tr.method)
	}
	routed, ok := tr.request.(*attestationclient.VerifyRequest)
	if !ok {
		t.Fatalf("expected *attestation.VerifyRequest, got %T", tr.request)
	}
	if routed.Format != ImageEndorsementFormat {
		t.Errorf("format = %q, want %q", routed.Format, ImageEndorsementFormat)
	}
	if string(routed.AttestationData) != digest {
		t.Errorf("attestation data = %q, want the canonical digest string", routed.AttestationData)
	}
	if len(routed.AttestedKeyDER) == 0 {
		t.Error("the signer's public key must be carried as the attested key")
	}
	if routed.Options == nil || !routed.Options.AllowSelfSigned {
		t.Error("options must be forwarded")
	}
}

func TestVerifyImageSignature_Validation(t *testing.T) {
	certDER := newSignerCertDER(t)
	const digest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	svc := newAttestationService(&recordingAttestationTransport{
		response: &AttestationVerifyResponse{},
	})

	tests := []struct {
		name        string
		ctx         context.Context
		endorsement *ImageEndorsement
	}{
		{
			name: "NilContext",
			ctx:  nil,
			endorsement: &ImageEndorsement{
				Digest: digest, Signature: []byte("s"), SignerChainPEM: [][]byte{certDER},
			},
		},
		{name: "NilEndorsement", ctx: context.Background()},
		{
			name:        "NoDigest",
			ctx:         context.Background(),
			endorsement: &ImageEndorsement{Signature: []byte("s"), SignerChainPEM: [][]byte{certDER}},
		},
		{
			name:        "NoSignature",
			ctx:         context.Background(),
			endorsement: &ImageEndorsement{Digest: digest, SignerChainPEM: [][]byte{certDER}},
		},
		{
			name:        "NoSignerChain",
			ctx:         context.Background(),
			endorsement: &ImageEndorsement{Digest: digest, Signature: []byte("s")},
		},
		{
			name: "MalformedSignerCertificate",
			ctx:  context.Background(),
			endorsement: &ImageEndorsement{
				Digest: digest, Signature: []byte("s"), SignerChainPEM: [][]byte{[]byte("not a cert")},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := svc.VerifyImageSignature(tt.ctx, tt.endorsement, nil); err == nil {
				t.Fatal("expected a validation error")
			}
		})
	}
}

func TestLeafPublicKeyDER(t *testing.T) {
	certDER := newSignerCertDER(t)

	t.Run("AcceptsDER", func(t *testing.T) {
		der, err := leafPublicKeyDER(certDER)
		if err != nil {
			t.Fatalf("leafPublicKeyDER: %v", err)
		}
		if _, err := x509.ParsePKIXPublicKey(der); err != nil {
			t.Fatalf("result is not a PKIX public key: %v", err)
		}
	})

	t.Run("RejectsGarbage", func(t *testing.T) {
		if _, err := leafPublicKeyDER([]byte("nope")); err == nil {
			t.Fatal("expected an error for a malformed certificate")
		}
	})
}

// TestAttestationServiceNameIsShared pins the single routing identifier every
// transport keys on.
func TestAttestationServiceNameIsShared(t *testing.T) {
	if attestationServiceName != "quicbaas.Attestation" {
		t.Fatalf("attestationServiceName = %q", attestationServiceName)
	}
	if attestationclient.ServiceName != attestationServiceName {
		t.Fatal("the SDK and the leaf client package must agree on the service name")
	}
}
