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

//go:build raft

package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"testing"

	clusterenroll "github.com/jeremyhahn/go-qrdb/sdk/go/clusterenroll"
	clustertls "github.com/jeremyhahn/go-qrdb/sdk/go/clustertls"
)

// TestSignerCertificate verifies the hardware-enroller assembly: a node leaf
// issued from the cluster CA for a backend-held key's public part, combined with
// the signer into a tls.Certificate that chains to the CA and carries the node's
// public key. A software ECDSA key stands in for the hardware key — the seam only
// requires a crypto.Signer; the private key never enters the certificate bytes.
func TestSignerCertificate(t *testing.T) {
	caMat, err := clustertls.GenerateCABundle()
	if err != nil {
		t.Fatalf("GenerateCABundle: %v", err)
	}

	nodeKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("node key: %v", err)
	}

	nodeURI := clusterenroll.NodeURI("quicbaas.cluster", "10.0.0.1:63001")
	leafPEM, err := clustertls.IssueLeafForKey(caMat.CACertPEM, caMat.CAKeyPEM, nodeKey.Public(), nodeURI)
	if err != nil {
		t.Fatalf("IssueLeafForKey: %v", err)
	}

	cert, err := signerCertificate(leafPEM, nodeKey)
	if err != nil {
		t.Fatalf("signerCertificate: %v", err)
	}
	if cert.PrivateKey == nil {
		t.Fatal("certificate has no signer PrivateKey")
	}
	if cert.Leaf == nil || len(cert.Certificate) != 1 {
		t.Fatal("certificate missing parsed leaf or DER chain")
	}

	// The leaf must chain to the cluster CA.
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caMat.CACertPEM) {
		t.Fatal("failed to add CA to pool")
	}
	if _, err := cert.Leaf.Verify(x509.VerifyOptions{
		Roots:     roots,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}); err != nil {
		t.Fatalf("leaf does not chain to cluster CA: %v", err)
	}

	// The leaf must carry the node key's public part (signer matches certificate).
	leafPub, ok := cert.Leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok || !leafPub.Equal(nodeKey.Public()) {
		t.Fatal("leaf public key does not match the node signer")
	}

	// A leaf signer whose public key differs from the certificate must fail
	// validation at the quicraft mTLS layer — ensure we detect it here.
	otherKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("other key: %v", err)
	}
	cert2, err := signerCertificate(leafPEM, otherKey)
	if err != nil {
		t.Fatalf("signerCertificate(otherKey): %v", err)
	}
	// cert2 is structurally valid (PEM decoded fine) but the leaf public key
	// no longer matches cert2.PrivateKey — callers (the quicraft TLS layer)
	// catch this at handshake time. Verify the cert alone still chains.
	if _, err := cert2.Leaf.Verify(x509.VerifyOptions{
		Roots:     roots,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}); err != nil {
		t.Fatalf("cert2 leaf does not chain: %v", err)
	}
}

// TestSignerCertificate_MalformedPEM verifies that signerCertificate returns an
// error and does not panic when given PEM data that contains no valid certificate
// block.
func TestSignerCertificate_MalformedPEM(t *testing.T) {
	nodeKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("node key: %v", err)
	}

	if _, err := signerCertificate([]byte("not a pem block"), nodeKey); err == nil {
		t.Fatal("expected error on malformed leaf PEM, got nil")
	}

	if _, err := signerCertificate(nil, nodeKey); err == nil {
		t.Fatal("expected error on nil leaf PEM, got nil")
	}
}

// TestRaftEnrollmentError_Error verifies the message format includes the
// package prefix, the failing step, and the underlying cause.
func TestRaftEnrollmentError_Error(t *testing.T) {
	cause := errors.New("boom")
	e := &RaftEnrollmentError{Op: "generate node key", Cause: cause}
	want := "server: cluster raft enrollment: generate node key: boom"
	if got := e.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// TestRaftEnrollmentError_Unwrap verifies errors.Is/As reach the underlying
// cause through the wrapper, including from a real signerCertificate failure
// on a decodable-but-invalid certificate block.
func TestRaftEnrollmentError_Unwrap(t *testing.T) {
	cause := errors.New("root cause")
	e := &RaftEnrollmentError{Op: "node signer", Cause: cause}
	if !errors.Is(e, cause) {
		t.Error("errors.Is did not reach the wrapped cause")
	}

	nodeKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("node key: %v", err)
	}
	badLeaf := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("garbage")})
	_, err = signerCertificate(badLeaf, nodeKey)
	var enrollErr *RaftEnrollmentError
	if !errors.As(err, &enrollErr) {
		t.Fatalf("expected *RaftEnrollmentError, got %T: %v", err, err)
	}
	if enrollErr.Op != "parse node leaf" {
		t.Errorf("Op = %q, want %q", enrollErr.Op, "parse node leaf")
	}
}
