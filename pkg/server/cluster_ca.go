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

package server

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"log/slog"
	"os"
	"path/filepath"

	clusterenroll "github.com/jeremyhahn/go-qrdb/sdk/go/clusterenroll"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	xkmsacme "github.com/jeremyhahn/go-xkms/sdk/go/acme"
	clustercasdk "github.com/jeremyhahn/go-xkms/sdk/go/clusterca"
	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// clusterCAShardID is the qrdb shard whose replicated KV store holds the cluster
// CA key blobs under the barrier strategy. It shares the default control-plane
// shard, like ACME state.
const clusterCAShardID uint64 = 0

// defaultRaftTrustDomain is the SPIFFE trust domain stamped into auto-provisioned
// Raft node certificates when RaftEnrollment.TrustDomain is unset. Defined here
// (untagged) so both the raft cluster wiring and the untagged CA/ACME helpers
// can reference it.
const defaultRaftTrustDomain = "quicbaas.cluster"

// clusterCACacheSubdir is the data-dir subdirectory the cluster-enrollment
// provisioner (go-xkms clusterca) writes its first-node CA cache and node-local
// keys into. It mirrors the xkms layout so the two products behave identically.
const clusterCACacheSubdir = "cluster-ca"

// defaultClusterCACNSuffix is appended to the CA root common name to derive the
// dedicated cluster intermediate CN when the operator leaves ClusterCACN unset.
const defaultClusterCACNSuffix = " Cluster CA"

// clusterACMEEnabled reports whether TPM-attested ACME cluster enrollment is
// configured. It gates every cluster-CA behavior so non-cluster and non-ACME
// deployments keep the existing single-root CA path byte-for-byte.
func (s *Server) clusterACMEEnabled() bool {
	return s.getEffectiveMode() == config.ServerModeCluster &&
		s.config.Raft.RaftEnrollment.ACME.Enabled
}

// clusterCACommonName resolves the dedicated cluster intermediate CN: the
// operator-configured ClusterCACN, or a default derived from the CA root name.
func (s *Server) clusterCACommonName() string {
	if cn := s.config.Raft.RaftEnrollment.ClusterCACN; cn != "" {
		return cn
	}
	root := s.config.CA.RootCommonName
	if root == "" {
		root = "QuicBaaS Root CA"
	}
	return root + defaultClusterCACNSuffix
}

// clusterCACacheDir is the node-local directory the cluster provisioner uses for
// the CA cache, node-local keys, and staged certificate set.
func (s *Server) clusterCACacheDir() string {
	return filepath.Join(standaloneQRDBDataDir(s.config), clusterCACacheSubdir)
}

// clusterCAKeyProtection builds the KeyProtection for the cluster CA keystore
// from the cluster enrollment configuration. The operator passphrase is moved
// into a sealed enclave here, at the composition root, because go-xkms holds
// it only as a *secure.Secret so the plaintext is never a long-lived Go string
// on its side. An empty passphrase leaves passphrase mode disabled.
func (s *Server) clusterCAKeyProtection() (clustercasdk.KeyProtection, error) {
	enr := s.config.Raft.RaftEnrollment
	protection := clustercasdk.KeyProtection{
		Mode:          clustercasdk.KeyProtectionMode(enr.CAKeyProtection),
		AllowInsecure: clustercasdk.KeyProtectionMode(enr.CAKeyProtection) == clustercasdk.KeyProtectNone,
	}
	if enr.CAKeyPassphrase != "" {
		passphrase, err := newClusterCAPassphrase([]byte(enr.CAKeyPassphrase))
		if err != nil {
			return clustercasdk.KeyProtection{}, &ClusterCAPassphraseError{Cause: err}
		}
		protection.Passphrase = passphrase
	}
	return protection, nil
}

// newClusterCAPassphrase seals the cluster CA passphrase into protected
// memory. It is a package variable so the failure path can be exercised: the
// enclave needs locked pages the kernel may refuse, and that must fail the
// cluster CA rather than run it with no passphrase.
var newClusterCAPassphrase = secure.NewSecret

// clusterStrategyDeps assembles the dependencies a cluster CA key strategy
// needs. Pre-node calls pass nil replicated storage (bootstrap); post-node calls
// (initializeCA) pass the qrdb-routed replicated stores.
func (s *Server) clusterStrategyDeps(replicatedKey, replicatedCert xkmssdk.StorageBackend) (*clustercasdk.StrategyDeps, error) {
	protection, err := s.clusterCAKeyProtection()
	if err != nil {
		return nil, err
	}
	deps := &clustercasdk.StrategyDeps{
		NodeLocalDataDir:      s.clusterCACacheDir(),
		CAConfig:              s.clusterCAConfig,
		Protection:            protection,
		ReplicatedKeyStorage:  replicatedKey,
		ReplicatedCertStorage: replicatedCert,
		// Bootstrapping is true on the node that pre-stages the CA (no joiner
		// directory URL) — the tpm-kek strategy generates the cluster seed only
		// there; joiners fetch it after enrollment.
		Bootstrapping: s.config.Raft.RaftEnrollment.ACME.DirectoryURL == "",
		Logger:        slog.Default(),
	}
	// The TPM sealer + credential activator (tpm-kek strategy) is the single
	// shared TPM opened by the seal strategy, when TPM-backed. Absent → tpm-kek
	// fails closed (it requires a TPM).
	if s.sealTPM2Backend != nil {
		deps.TPMSealer = s.sealTPM2Backend
		deps.Credentials = s.sealTPM2Backend.TPM()
	}
	return deps, nil
}

// resolveClusterCARuntimeBackend resolves this node's runtime cluster CA
// keystore via the configured strategy, over the replicated (qrdb-routed,
// barrier-encrypted) datastore. For the barrier strategy the first node's
// bootstrap key is promoted into the replicated store and every node loads it,
// so any node can issue.
func (s *Server) resolveClusterCARuntimeBackend() (xkmssdk.ServiceBackend, error) {
	shardClient, err := s.qrdbClient.ForShard(clusterCAShardID)
	if err != nil {
		return nil, err
	}
	kvBackend := xkmssdk.NewQRDBStorageBackend(shardClient.KV())
	replicatedKey, err := xkmssdk.NewStoragePrefixBackend(kvBackend, "cluster-ca-keys/")
	if err != nil {
		return nil, err
	}
	replicatedCert, err := xkmssdk.NewStoragePrefixBackend(kvBackend, "cluster-ca-certs/")
	if err != nil {
		return nil, err
	}
	deps, err := s.clusterStrategyDeps(replicatedKey, replicatedCert)
	if err != nil {
		return nil, err
	}
	return s.clusterCAKeyStrategy.RuntimeBackend(context.Background(), deps)
}

// clusterCARevocationStore returns the cluster CA's revocation store over the
// same replicated (qrdb-routed, barrier-encrypted) shard as its keys and
// certificates. Any node can issue, so any node can revoke, and every node's
// CRL must reflect every revocation — which a node-local store cannot give.
func (s *Server) clusterCARevocationStore() (xkmssdk.StorageBackend, error) {
	shardClient, err := s.qrdbClient.ForShard(clusterCAShardID)
	if err != nil {
		return nil, err
	}
	return xkmssdk.NewStoragePrefixBackend(xkmssdk.NewQRDBStorageBackend(shardClient.KV()), "cluster-ca-revocations/")
}

// ClusterSecretDeliveryService builds the post-enrollment secret-delivery
// service for the active cluster CA key strategy, or nil when the strategy
// delivers no enrollment material (barrier/hsm) or no cluster strategy is
// active. Joining nodes fetch their wrapped provisioning material (the tpm-kek
// seed) from its Handler after enrollment; the requester is authorized by its
// cluster-CA-signed certificate.
func (s *Server) ClusterSecretDeliveryService() *clustercasdk.SecretDeliveryService {
	if s.clusterCAKeyStrategy == nil {
		return nil
	}
	es, ok := clustercasdk.EnrollmentSecretStrategyOf(s.clusterCAKeyStrategy)
	if !ok {
		return nil
	}
	var pool *x509.CertPool
	if s.xkmsCA != nil {
		if p, err := s.xkmsCA.CABundleCertPool(); err == nil {
			pool = p
		}
	}
	svc := clustercasdk.NewSecretDeliveryService(slog.Default(), pool, es.SecretProvider())
	if !svc.HasProviders() {
		return nil
	}
	return svc
}

// clusterACMEIssuerCN returns the dedicated cluster intermediate CN that
// enroll-01 (TCG-CSR-IDEVID) issuance should chain to, or "" to keep default
// issuance. It is active only when cluster ACME enrollment is enabled.
func (s *Server) clusterACMEIssuerCN() string {
	if !s.clusterACMEEnabled() {
		return ""
	}
	return s.clusterCACommonName()
}

// buildClusterPreSignHooks returns the pre-sign hook registry for the ACME
// server. When cluster ACME enrollment is enabled it registers the SPIFFE
// node-identity URI-SAN hook so enrolling nodes' transport certificates carry
// spiffe://<trust-domain>/node/<raft-address>, derived from the TCG permanent
// identifier the joiner presents (clusterenroll.NodeURI is the single source of
// truth for the format). Returns nil when cluster ACME enrollment is off,
// leaving ordinary ACME issuance untouched.
func (s *Server) buildClusterPreSignHooks() *xkmsacme.ACMEOrderPreSignHookRegistry {
	if !s.clusterACMEEnabled() {
		return nil
	}
	trustDomain := s.config.Raft.RaftEnrollment.TrustDomain
	if trustDomain == "" {
		trustDomain = defaultRaftTrustDomain
	}
	registry := xkmsacme.NewACMEOrderPreSignHookRegistry()
	registry.Register(xkmsacme.NewACMEPermanentIdentifierURISANHook(func(permanentID string) string {
		return clusterenroll.NodeURI(trustDomain, permanentID)
	}))
	return registry
}

// buildClusterCAMultiIdentityConfig builds the CA hierarchy the cluster ACME
// first node pre-stages and the runtime CA loads: the existing root plus a
// dedicated cluster intermediate (root signs it) that issues Raft transport
// certificates. It deliberately does NOT set a signing-key password here — in
// cluster-ACME mode the node-local CA keystore is protected by KeyProtection
// (clustercasdk.NewNodeLocalBackends injects the resolved keystore password into
// these identities), so the two paths agree on the key encryption.
//
// The keystore type comes from caStoreType so an operator directing the CA
// signing key at a hardware backend is honored for both identities.
func (s *Server) buildClusterCAMultiIdentityConfig(caStoreType xkmssdk.StoreType) *xkmssdk.TrustedCAMultiIdentityConfig {
	keyAlgo := xkmssdk.AlgorithmECDSA
	if s.config.CA.RootKeyAlgorithm == "RSA" {
		keyAlgo = xkmssdk.AlgorithmRSA
	}
	newKeyConfig := func() *xkmssdk.KeyConfig {
		kc := &xkmssdk.KeyConfig{
			KeyAlgorithm:       keyAlgo,
			KeyType:            xkmssdk.KeyTypeStringCA,
			Hash:               xkmssdk.HashSHA256,
			SignatureAlgorithm: xkmssdk.SigECDSAWithSHA256,
			StoreType:          caStoreType,
			ECCConfig:          &xkmssdk.KeyECCConfig{Curve: xkmssdk.CurveP256},
		}
		if keyAlgo == xkmssdk.AlgorithmRSA {
			kc.RSAConfig = &xkmssdk.KeyRSAConfig{KeySize: 2048}
			kc.SignatureAlgorithm = xkmssdk.SigSHA256WithRSA
			kc.ECCConfig = nil
			return kc
		}
		switch s.config.CA.RootKeyCurve {
		case "P-384":
			kc.ECCConfig = &xkmssdk.KeyECCConfig{Curve: xkmssdk.CurveP384}
			kc.SignatureAlgorithm = xkmssdk.SigECDSAWithSHA384
		case "P-521":
			kc.ECCConfig = &xkmssdk.KeyECCConfig{Curve: xkmssdk.CurveP521}
			kc.SignatureAlgorithm = xkmssdk.SigECDSAWithSHA512
		}
		return kc
	}

	rootCN := s.config.CA.RootCommonName
	if rootCN == "" {
		rootCN = "QuicBaaS Root CA"
	}
	rootValid := s.config.CA.RootValidYears
	if rootValid == 0 {
		rootValid = 10
	}
	intermediateValid := s.config.CA.IntermediateValidYears
	if intermediateValid == 0 {
		intermediateValid = 5
	}

	return &xkmssdk.TrustedCAMultiIdentityConfig{
		Identity: []xkmssdk.TrustedCAIdentity{
			{
				Subject: xkmssdk.TrustedCASubject{
					CommonName:   rootCN,
					Organization: s.config.CA.RootOrganization,
				},
				Valid:  rootValid,
				Keys:   []*xkmssdk.KeyConfig{newKeyConfig()},
				IsRoot: true,
			},
			{
				// Dedicated cluster intermediate — root signs it (ParentCA), and
				// it issues Raft node transport certificates. Root signs
				// intermediates only, keeping the FedRAMP/PCI posture.
				Subject: xkmssdk.TrustedCASubject{
					CommonName:   s.clusterCACommonName(),
					Organization: s.config.CA.RootOrganization,
				},
				Valid:    intermediateValid,
				Keys:     []*xkmssdk.KeyConfig{newKeyConfig()},
				ParentCA: rootCN,
				IsRoot:   false,
			},
		},
		// The root remains the issuing identity for ordinary (TLS server) certs;
		// only cluster node certs route through the intermediate via ClusterCACN.
		SelectedCA:          0,
		DefaultValidityDays: s.config.CA.DefaultValidityDays,
		HomeDir:             standaloneQRDBDataDir(s.config),
	}
}

// adoptStagedClusterCACerts imports the full CA certificate set the cluster ACME
// first-node pre-stage persisted (clustercasdk.StagedCACertsPath) into the
// runtime CA's certificate store, so the CA's Load — which verifies every
// configured intermediate — succeeds without re-running Init (which would fork a
// second root). It is a no-op when no staged material is present (non-cluster
// deployments, joining nodes, or a subsequent boot after adoption), so it is
// safe to call unconditionally before Load.
func (s *Server) adoptStagedClusterCACerts(cs xkmssdk.CertStore) error {
	path := clustercasdk.StagedCACertsPath(s.clusterCACacheDir())
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	rest := data
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, parseErr := x509.ParseCertificate(block.Bytes)
		if parseErr != nil {
			return parseErr
		}
		// Idempotent: a certificate already present from a prior boot's adoption
		// is harmless. Log-and-continue so a duplicate never fails startup.
		if storeErr := cs.StoreCertificate(cert); storeErr != nil {
			s.logger.Debug("staged cluster CA certificate already present or unstorable",
				"cn", cert.Subject.CommonName, "error", storeErr.Error())
			continue
		}
	}
	return nil
}
