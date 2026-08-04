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
	"context"
	"crypto"
	"crypto/elliptic"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"
	"strconv"

	clusterenroll "github.com/jeremyhahn/go-qrdb/sdk/go/clusterenroll"
	clustertls "github.com/jeremyhahn/go-qrdb/sdk/go/clustertls"
	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// RaftEnrollmentError wraps a failure in a cluster Raft mTLS enrollment step
// (backend resolution, node-key generation, signer construction, or leaf
// parsing) with the underlying cause.
type RaftEnrollmentError struct {
	// Op describes the enrollment step that failed.
	Op string
	// Cause is the underlying error.
	Cause error
}

// Error implements the error interface.
func (e *RaftEnrollmentError) Error() string {
	return "server: cluster raft enrollment: " + e.Op + ": " + e.Cause.Error()
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *RaftEnrollmentError) Unwrap() error {
	return e.Cause
}

// ErrRaftLeafDecode is returned when the issued Raft node leaf PEM cannot be
// decoded — an internal invariant violation that must fail node startup rather
// than allow the Raft transport to start with a malformed certificate.
type ErrRaftLeafDecode struct{}

// Error returns a descriptive message.
func (e *ErrRaftLeafDecode) Error() string {
	return "server: cluster raft enrollment: issued node leaf PEM did not decode"
}

// provisionRaftClusterTLSHardware provisions the Raft node mTLS material with a
// hardware-resident node key. It resolves the named backend via BackendResolver,
// generates (or, across restarts, loads) an ECDSA P-256 signing key on that
// backend (the private key never leaves the device), resolves the cluster CA via
// clusterenroll.ResolveCA, issues a node leaf for the hardware key's public part,
// and returns an MTLSConfig that carries the signer-backed certificate through
// the quicraft Certificate seam. The returned Advertiser is non-nil only on the
// first (bootstrapping) node.
func (s *Server) provisionRaftClusterTLSHardware(members map[uint64]string) (*qrdbnode.RaftMTLSConfig, *clusterenroll.Advertiser, error) {
	enr := s.config.Raft.RaftEnrollment

	// Resolve the named backend to a ServiceBackend, then extract its
	// KeyProvider. BackendResolver.ServiceBackend constructs a fresh in-memory
	// backend instance — adequate here because the key lives in hardware and
	// the provider is only used to drive the signer, not to persist key bytes.
	resolver := NewBackendResolver(s.config)
	sb, err := resolver.ServiceBackend(BackendSpec{Backend: xkmssdk.BackendType(enr.KeyBackend)})
	if err != nil {
		return nil, nil, &RaftEnrollmentError{Op: "key backend " + strconv.Quote(enr.KeyBackend), Cause: err}
	}
	kp := sb.KeyProvider()

	ctx := context.Background()
	signer, err := s.raftNodeSigner(ctx, kp)
	if err != nil {
		return nil, nil, err
	}

	// Resolve ONLY the cluster CA — the node leaf is issued below for the
	// hardware key's public part via clustertls.IssueLeafForKey.
	caCertPEM, caKeyPEM, adv, err := clusterenroll.ResolveCA(ctx, s.buildClusterEnrollOptions(members), slog.Default())
	if err != nil {
		return nil, nil, err
	}

	trustDomain := enr.TrustDomain
	if trustDomain == "" {
		trustDomain = defaultRaftTrustDomain
	}
	nodeURI := clusterenroll.NodeURI(trustDomain, s.config.Raft.RaftAddress)

	leafPEM, err := clustertls.IssueLeafForKey(caCertPEM, caKeyPEM, signer.Public(), nodeURI)
	if err != nil {
		return nil, nil, err
	}

	cert, err := signerCertificate(leafPEM, signer)
	if err != nil {
		return nil, nil, err
	}

	s.logger.Info("Raft inter-node mTLS auto-provisioned with hardware-resident key",
		"key_backend", enr.KeyBackend,
		"advertising", adv != nil)

	return &qrdbnode.RaftMTLSConfig{
		CACert:      caCertPEM,
		Certificate: cert,
	}, adv, nil
}

// raftNodeSigner returns the crypto.Signer for this node's Raft TLS key on kp,
// reusing an existing key across restarts and generating an ECDSA P-256 key on
// first boot. For hardware backends the private key never leaves the device; the
// returned signer drives all TLS handshake signatures in-device.
func (s *Server) raftNodeSigner(ctx context.Context, kp xkmssdk.KeyProvider) (crypto.Signer, error) {
	attrs := &xkmssdk.KeyAttributes{
		CN:            fmt.Sprintf("raft-node-%d", s.config.Raft.NodeID),
		StoreType:     xkmssdk.ParseStoreType(string(kp.Type())),
		KeyType:       xkmssdk.KeyTypeSigning,
		KeyAlgorithm:  x509.ECDSA,
		ECCAttributes: &xkmssdk.ECCAttributes{Curve: elliptic.P256()},
	}
	// Reuse the key if it already exists (restart path); otherwise generate.
	if signer, err := kp.Signer(ctx, attrs); err == nil {
		return signer, nil
	}
	if _, err := kp.GenerateKey(ctx, attrs); err != nil {
		return nil, &RaftEnrollmentError{Op: "generate node key", Cause: err}
	}
	signer, err := kp.Signer(ctx, attrs)
	if err != nil {
		return nil, &RaftEnrollmentError{Op: "node signer", Cause: err}
	}
	return signer, nil
}

// signerCertificate assembles a tls.Certificate from the issued leaf PEM and the
// backend-held signer. The signer (not exportable key bytes) drives all TLS
// handshake signatures so a hardware-resident private key never leaves the device.
func signerCertificate(leafPEM []byte, signer crypto.Signer) (*tls.Certificate, error) {
	block, _ := pem.Decode(leafPEM)
	if block == nil {
		return nil, &ErrRaftLeafDecode{}
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, &RaftEnrollmentError{Op: "parse node leaf", Cause: err}
	}
	return &tls.Certificate{
		Certificate: [][]byte{block.Bytes},
		PrivateKey:  signer,
		Leaf:        leaf,
	}, nil
}
