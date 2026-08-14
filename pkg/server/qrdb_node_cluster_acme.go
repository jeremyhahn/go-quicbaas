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
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	clusterenroll "github.com/jeremyhahn/go-qrdb/sdk/go/clusterenroll"
	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	clustercasdk "github.com/jeremyhahn/go-xkms/sdk/go/clusterca"
)

// defaultClusterACMEChallengePort is the local HTTP port a node serves the
// enroll-01 credential-activation exchange on while enrolling, matching the
// clusterca and ACME server defaults.
const defaultClusterACMEChallengePort = "8080"

// provisionRaftClusterACME provisions inter-node Raft mTLS via TPM-attested ACME
// enroll-01 with the runtime TrustedCA as the single cluster trust anchor. The
// first node pre-stages the CA hierarchy (root + dedicated cluster intermediate)
// from clusterCAConfig — the datastore does not exist yet — and issues its own
// transport leaf; joining nodes fetch the trust anchor via truststrap and enroll
// their attested node key through the existing node's ACME directory. It uses
// the shared clustercasdk toolkit so quicbaas provisions cluster mTLS identically
// to xkmsd. Returns the qrdb WithRaftMTLS material and, on the first node, the
// running Noise advertiser to stop at shutdown.
func (s *Server) provisionRaftClusterACME(ctx context.Context, members map[uint64]string) (*qrdbnode.RaftMTLSConfig, *clusterenroll.Advertiser, error) {
	caStoreType, err := s.resolveCAStoreType()
	if err != nil {
		return nil, nil, err
	}

	// Build the CA hierarchy once (root + cluster intermediate) and stash it so
	// the pre-stage and the runtime CA (initializeCA) agree on identities and the
	// injected keystore password. Every node needs it: the barrier strategy has
	// every node build the CA from the replicated key.
	s.clusterCAConfig = s.buildClusterCAMultiIdentityConfig(caStoreType)

	// Build the cluster CA key custody strategy and retain it: it is stateful
	// (the barrier strategy holds its bootstrap key-storage handle for
	// promotion), so initializeCA uses this SAME instance for RuntimeBackend so
	// any node — not just the first — can issue.
	strategy, err := clustercasdk.NewClusterCAKeyStrategy(
		clustercasdk.KeyStrategyName(s.config.Raft.RaftEnrollment.CAKeyStrategy))
	if err != nil {
		return nil, nil, err
	}
	s.clusterCAKeyStrategy = strategy

	// The pre-node Backends seam the provisioner uses to pre-stage the CA and
	// issue this node's transport leaf comes from the strategy's bootstrap
	// backends (node-local for barrier/tpm-kek, the shared HSM for hsm).
	backends, err := strategy.BootstrapBackends(ctx, s.clusterStrategyDeps(nil, nil))
	if err != nil {
		return nil, nil, err
	}

	opts := s.buildClusterACMEOptions(members, s.clusterCAConfig, backends)
	provisioner, err := clustercasdk.New(opts, slog.Default())
	if err != nil {
		return nil, nil, err
	}
	result, err := provisioner.Provision(ctx)
	if err != nil {
		return nil, nil, err
	}

	// Joining node: after enrollment, fetch any wrapped provisioning material the
	// cluster CA key strategy delivers (the tpm-kek cluster seed) and hand it to
	// the strategy so RuntimeBackend can derive the KEK. A no-op for barrier/hsm.
	if es, ok := clustercasdk.EnrollmentSecretStrategyOf(strategy); ok &&
		s.config.Raft.RaftEnrollment.ACME.DirectoryURL != "" {
		if err := s.fetchClusterEnrollmentSecret(ctx, es, result.MTLS); err != nil {
			return nil, nil, err
		}
	}
	return result.MTLS, result.Advertiser, nil
}

// fetchClusterEnrollmentSecret runs on a JOINING node after enrollment: it reads
// this node's EK certificate and attestation-key public area from the TPM, POSTs
// them with its just-issued node certificate to the enrolling node's
// secret-delivery endpoint, and hands the returned (TPM-wrapped) material to the
// strategy's ConsumeEnrollmentSecret.
func (s *Server) fetchClusterEnrollmentSecret(ctx context.Context, es clustercasdk.EnrollmentSecretStrategy, mtls *qrdbnode.RaftMTLSConfig) error {
	if s.sealTPM2Backend == nil {
		return clustercasdk.ErrStrategyMisconfigured
	}
	tpm := s.sealTPM2Backend.TPM()
	ekCert, err := tpm.EKCertificate()
	if err != nil {
		return err
	}
	akProfile, err := tpm.AKProfile()
	if err != nil {
		return err
	}
	ekPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ekCert.Raw})

	baseURL, err := clusterSecretBaseURL(s.config.Raft.RaftEnrollment.ACME.DirectoryURL)
	if err != nil {
		return err
	}
	client := clusterSecretHTTPClient(mtls.CACert)

	secrets, err := clustercasdk.FetchClusterSecrets(ctx, client, baseURL, mtls.Cert, ekPEM, akProfile.AKPub)
	if err != nil {
		return err
	}
	for _, material := range secrets {
		if err := es.ConsumeEnrollmentSecret(ctx, material); err != nil {
			return err
		}
	}
	s.logger.Info("fetched and consumed cluster enrollment secret via post-enrollment delivery")
	return nil
}

// clusterSecretBaseURL derives the scheme://host base of the delivery endpoint
// from the ACME directory URL (the delivery endpoint is co-hosted).
func clusterSecretBaseURL(directoryURL string) (string, error) {
	u, err := url.Parse(directoryURL)
	if err != nil {
		return "", err
	}
	if u.Scheme == "" || u.Host == "" {
		return "", errInvalidACMEDirectoryURL
	}
	return u.Scheme + "://" + u.Host, nil
}

// clusterSecretHTTPClient builds an HTTP client trusting the cluster CA anchor
// for the delivery fetch. Confidentiality of the material is guaranteed by the
// TPM credential-activation wrapping; this protects transport integrity.
func clusterSecretHTTPClient(caPEM []byte) *http.Client {
	pool := x509.NewCertPool()
	if len(caPEM) > 0 {
		pool.AppendCertsFromPEM(caPEM)
	}
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS13},
		},
	}
}

// errInvalidACMEDirectoryURL is returned when the configured ACME directory URL
// cannot be parsed into a delivery base URL.
var errInvalidACMEDirectoryURL = errors.New("server: invalid acme directory_url for cluster secret delivery")

// buildClusterACMEOptions maps the quicbaas cluster configuration onto the
// clustercasdk Options. The first node (no bootstrap directory) pre-stages the
// CA from caConfig; joining nodes (DirectoryURL set) enroll against it.
func (s *Server) buildClusterACMEOptions(
	members map[uint64]string,
	caConfig *xkmssdk.TrustedCAMultiIdentityConfig,
	backends func(string) (xkmssdk.ServiceBackend, error),
) *clustercasdk.Options {
	enr := s.config.Raft.RaftEnrollment

	trustDomain := enr.TrustDomain
	if trustDomain == "" {
		trustDomain = defaultRaftTrustDomain
	}
	advPort := enr.AdvertiseNoisePort
	if advPort == 0 {
		advPort = clusterenroll.DefaultNoisePort
	}
	challengePort := enr.ACME.ChallengePort
	if challengePort == "" {
		challengePort = defaultClusterACMEChallengePort
	}

	return &clustercasdk.Options{
		RaftAddress: s.config.Raft.RaftAddress,
		NodeID:      s.config.Raft.NodeID,
		TrustDomain: trustDomain,
		Members:     members,
		DataDir:     s.clusterCACacheDir(),
		ACME: &clustercasdk.ACMEOptions{
			DirectoryURL:  enr.ACME.DirectoryURL,
			ChallengePort: challengePort,
		},
		KeyBackend: enr.KeyBackend,
		Bootstrap: clusterenroll.BootstrapOptions{
			NoiseKey:        enr.Bootstrap.NoiseKey,
			NoiseAddr:       enr.Bootstrap.NoiseAddr,
			SPKIPin:         enr.Bootstrap.SPKIPin,
			ServerAddr:      enr.Bootstrap.ServerAddr,
			DANEDNS:         enr.Bootstrap.DANEDNS,
			DANEDoT:         enr.Bootstrap.DANEDoT,
			DirectURL:       enr.Bootstrap.DirectURL,
			DiscoveryMethod: "static",
			Peers:           raftJoinPeers(s.config.Raft.JoinAddresses),
		},
		Advertise: clusterenroll.AdvertiseOptions{
			NoisePort: advPort,
		},
		Backends:    backends,
		CAConfig:    caConfig,
		ClusterCACN: s.clusterCACommonName(),
	}
}
