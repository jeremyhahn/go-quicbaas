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
	"log/slog"
	"path/filepath"
	"strings"

	clusterenroll "github.com/jeremyhahn/go-qrdb/sdk/go/clusterenroll"
	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// raftTLSCacheSubdir is the subdirectory of the effective data dir where
// auto-provisioned cluster TLS material (CA + node cert/key + Noise key) is
// cached so restarts reuse it.
const raftTLSCacheSubdir = "raft-tls"

// init overrides provisionClusterMTLS with the full raft-build precedence switch.
// The non-raft default (explicit PEM files only) is replaced here so that the
// software and hardware enrollment paths are active when building with -tags raft.
func init() {
	provisionClusterMTLS = raftClusterMTLS
}

// raftClusterMTLS implements the three-way mTLS precedence for a multi-node
// Raft cluster:
//
//  1. Operator-supplied explicit PEM files (TLSCertFile/TLSKeyFile/TLSCAFile).
//  2. Hardware-resident node key (RaftEnrollment.KeyBackend set: tpm2/pkcs11).
//  3. Software node key auto-provisioned via the shared cluster-TLS enrollment
//     toolkit (clusterenroll.Resolve).
//
// It is registered as provisionClusterMTLS in init() so the untagged
// initClusteredQRDBNode picks it up via the function variable.
func raftClusterMTLS(s *Server, members map[uint64]string) (*qrdbnode.RaftMTLSConfig, interface{ Stop() }, error) {
	enr := s.config.Raft.RaftEnrollment
	switch {
	case raftTLSFilesConfigured(s.config):
		// Precedence 1: operator-supplied explicit PEM files.
		mtls, err := buildRaftMTLSConfig(s.config)
		if err != nil {
			return nil, nil, err
		}
		s.logger.Info("Raft inter-node mTLS from operator-supplied files",
			"ca_file", s.config.Raft.TLSCAFile,
			"cert_file", s.config.Raft.TLSCertFile)
		return mtls, nil, nil
	case enr.ACME.Enabled:
		// Precedence 2: TPM-attested ACME enroll-01 with the runtime TrustedCA
		// as the single cluster trust anchor. The first node pre-stages the CA
		// (root + dedicated cluster intermediate) and issues its own leaf;
		// joiners enroll their attested node key through an existing node's ACME
		// directory. Uses the shared go-xkms clustercasdk toolkit.
		ctx := context.Background()
		mtls, adv, err := s.provisionRaftClusterACME(ctx, members)
		if err != nil {
			return nil, nil, err
		}
		s.logger.Info("Raft inter-node mTLS via TPM-attested ACME cluster enrollment",
			"data_dir", s.clusterCACacheDir(),
			"cluster_ca_cn", s.clusterCACommonName(),
			"advertising", adv != nil)
		return mtls, adv, nil
	case enr.KeyBackend != "":
		// Precedence 2: hardware-resident node key (tpm2/pkcs11). The private
		// key is generated inside and never leaves the device; the Raft
		// transport uses a signer-backed certificate through the quicraft
		// Certificate seam.
		return s.provisionRaftClusterTLSHardware(members)
	default:
		// Precedence 3: software node key auto-provisioned via cluster
		// enrollment. The first node generates the cluster CA and starts a
		// Noise NK bootstrap server; joining nodes fetch the CA.
		ctx := context.Background()
		mtls, adv, err := s.provisionRaftClusterTLS(ctx, members)
		if err != nil {
			return nil, nil, err
		}
		dataDir := filepath.Join(standaloneQRDBDataDir(s.config), raftTLSCacheSubdir)
		s.logger.Info("Raft inter-node mTLS auto-provisioned via cluster enrollment",
			"data_dir", dataDir,
			"advertising", adv != nil)
		return mtls, adv, nil
	}
}

// raftTLSFilesConfigured reports whether all three Raft TLS file paths are set.
// A partial set (1 or 2 of 3) is treated as unconfigured to avoid ambiguous state.
func raftTLSFilesConfigured(cfg *config.Config) bool {
	return cfg.Raft.TLSCAFile != "" &&
		cfg.Raft.TLSCertFile != "" &&
		cfg.Raft.TLSKeyFile != ""
}

// provisionRaftClusterTLS auto-provisions the inter-node Raft mTLS material by
// delegating to go-qrdb's shared cluster-TLS enrollment (clusterenroll.Resolve).
// The first node generates a dedicated self-signed cluster CA and returns a
// running Noise advertiser for peer enrollment; joining nodes fetch the CA and
// issue their own leaf. Generated material is cached under the raft-tls subdir
// of the effective data directory so restarts reuse it without re-enrolling.
// The returned Advertiser is nil except on the bootstrapping node.
func (s *Server) provisionRaftClusterTLS(ctx context.Context, members map[uint64]string) (*qrdbnode.RaftMTLSConfig, *clusterenroll.Advertiser, error) {
	mat, adv, err := clusterenroll.Resolve(ctx, s.buildClusterEnrollOptions(members), slog.Default())
	if err != nil {
		return nil, nil, err
	}
	return &qrdbnode.RaftMTLSConfig{
		CACert: mat.CACertPEM,
		Cert:   mat.CertPEM,
		Key:    mat.KeyPEM,
	}, adv, nil
}

// buildClusterEnrollOptions maps the quicbaas Raft and enrollment configuration
// onto the qrdb clusterenroll Options, applying defaults (trust domain, advertise
// port) and computing SingleNode from the membership map. It is pure (no side
// effects) so the mapping can be unit-tested without starting the Noise advertiser.
func (s *Server) buildClusterEnrollOptions(members map[uint64]string) *clusterenroll.Options {
	enr := s.config.Raft.RaftEnrollment

	trustDomain := enr.TrustDomain
	if trustDomain == "" {
		trustDomain = defaultRaftTrustDomain
	}
	advPort := enr.AdvertiseNoisePort
	if advPort == 0 {
		advPort = clusterenroll.DefaultNoisePort
	}

	return &clusterenroll.Options{
		RaftAddress: s.config.Raft.RaftAddress,
		TrustDomain: trustDomain,
		DataDir:     filepath.Join(standaloneQRDBDataDir(s.config), raftTLSCacheSubdir),
		SingleNode:  len(members) <= 1,
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
	}
}

// raftJoinPeers filters JoinAddresses to only the "id=addr" entries that
// clusterenroll accepts for static peer discovery. Raw "host:port" entries
// without an id prefix are skipped (they cannot be mapped to a replica ID).
func raftJoinPeers(addrs []string) []string {
	if len(addrs) == 0 {
		return nil
	}
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if strings.Contains(a, "=") {
			out = append(out, a)
		}
	}
	return out
}
