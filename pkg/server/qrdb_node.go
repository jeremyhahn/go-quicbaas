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
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	qrdbcluster "github.com/jeremyhahn/go-qrdb/pkg/cluster"
	qrdbconfig "github.com/jeremyhahn/go-qrdb/pkg/config"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"
	quicraftconfig "github.com/jeremyhahn/go-quicraft/pkg/config"
	xkmsfido2 "github.com/jeremyhahn/go-xkms/pkg/factor/fido2"
	xkmspiv "github.com/jeremyhahn/go-xkms/pkg/factor/piv"
	xkmspkcs11 "github.com/jeremyhahn/go-xkms/pkg/factor/pkcs11"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// QRDBNodeError describes a failure in the standalone qrdb node lifecycle
// (construction, shutdown). Stage names the step that failed; Cause is the
// underlying error.
type QRDBNodeError struct {
	Stage string
	Cause error
}

// Error implements the error interface.
func (e *QRDBNodeError) Error() string {
	if e.Cause == nil {
		return "qrdb node: " + e.Stage + " failed"
	}
	return "qrdb node: " + e.Stage + " failed: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is/As traversal.
func (e *QRDBNodeError) Unwrap() error { return e.Cause }

// buildQRDBConfig maps a quicbaas *config.Config to a *qrdbconfig.Config.
// When clusterMode is false (standalone), Raft and Discovery fields are left
// at zero values — the standalone node builder uses a LocalBackend with no
// quicraft host. When clusterMode is true, Raft address, shard/replica IDs,
// deployment ID, static peer list, and the discovery method are populated
// from the quicbaas Raft and Discovery config sections.
func buildQRDBConfig(cfg *config.Config, clusterMode bool) *qrdbconfig.Config {
	qcfg := &qrdbconfig.Config{}

	// ------------------------------------------------------------------ //
	// Data directory: quicbaas uses Storage.DataDir; qrdb uses Raft.DataDir
	// (the Node builder reads DataDir from cfg.Raft.DataDir for the pebble
	// root and seal sub-store).
	// ------------------------------------------------------------------ //
	dataDir := cfg.Storage.DataDir
	if dataDir == "" {
		dataDir = cfg.Seal.BarrierDir // fall back to the barrier dir as the root
	}
	qcfg.Raft.DataDir = dataDir

	// ------------------------------------------------------------------ //
	// Storage backend: translate quicbaas StorageBackend* constants to the
	// qrdb DatabaseConfig Backend field. The Node builder reads
	// cfg.Databases[0].Backend to select the engine.
	// ------------------------------------------------------------------ //
	backend := cfg.Storage.Backend
	if backend == "" {
		backend = config.StorageBackendMemory
	}
	// Map quicbaas names → qrdb names (they are identical, so this is a
	// direct copy; the map makes the translation explicit and future-proof).
	backendMap := map[string]string{
		config.StorageBackendMemory: "memory",
		config.StorageBackendFile:   "file",
		config.StorageBackendPebble: "pebble",
	}
	qrdbBackend, ok := backendMap[backend]
	if !ok {
		qrdbBackend = "memory"
	}
	qcfg.Databases = []qrdbconfig.DatabaseConfig{
		{Name: "default", Engine: "mvcc", Backend: qrdbBackend},
	}

	// ------------------------------------------------------------------ //
	// Seal barrier: map quicbaas SealConfig → qrdb SealConfig.
	// ------------------------------------------------------------------ //
	qcfg.Seal.Enabled = cfg.Seal.Enabled
	qcfg.Seal.Strategy = cfg.Seal.Strategy
	if qcfg.Seal.Strategy == "" {
		qcfg.Seal.Strategy = "passphrase"
	}
	qcfg.Seal.ShamirThreshold = cfg.Seal.Threshold
	qcfg.Seal.ShamirTotal = cfg.Seal.TotalShares

	if !clusterMode {
		// Standalone: no Raft host, no discovery. The node builder applies
		// safe defaults when the ports are zero.
		return qcfg
	}

	// ------------------------------------------------------------------ //
	// Cluster mode: populate Raft address, shard/replica IDs, deployment
	// ID, and peer list from the quicbaas Raft config.
	// ------------------------------------------------------------------ //
	qcfg.Raft.RaftAddress = cfg.Raft.RaftAddress
	// Derive the listen address for multi-node cluster mode. The quicbaas
	// default for ListenAddress is "127.0.0.1:63000" which is only valid for
	// single-node localhost clusters. For a real multi-node cluster the Raft
	// QUIC transport must listen on 0.0.0.0:<raft-port> so that other nodes on
	// the Docker network can reach it. Resolution order:
	//   1. cfg.Raft.ListenAddress if explicitly set to something other than the
	//      quicbaas single-node default ("127.0.0.1:63000").
	//   2. Derive "0.0.0.0:<port>" from cfg.Raft.RaftAddress's port component
	//      when the raft address has a named host (indicating a real multi-node
	//      cluster where the node must accept connections on all interfaces).
	//   3. Fall back to cfg.Raft.ListenAddress (the quicbaas default).
	if listenAddr := cfg.Raft.ListenAddress; listenAddr != "" && listenAddr != "127.0.0.1:63000" {
		qcfg.Raft.ListenAddress = listenAddr
	} else if raftAddr := cfg.Raft.RaftAddress; raftAddr != "" {
		if _, raftPort, found := strings.Cut(raftAddr, ":"); found && raftPort != "" {
			qcfg.Raft.ListenAddress = "0.0.0.0:" + raftPort
		} else {
			qcfg.Raft.ListenAddress = cfg.Raft.ListenAddress
		}
	} else {
		qcfg.Raft.ListenAddress = cfg.Raft.ListenAddress
	}
	qcfg.Raft.ReplicaID = cfg.Raft.NodeID
	// Use shard 1 (the KV shard) for the primary cluster group.
	qcfg.Raft.ShardID = 1
	// DeploymentID isolates this cluster from other quicraft clusters on the
	// same network. All nodes in the cluster MUST share the same value.
	// Resolution order:
	//   1. cfg.Raft.DeploymentID if non-zero (operator-set; preferred for
	//      production — ensures all nodes agree regardless of their NodeID).
	//   2. cfg.Raft.NodeID when DeploymentID is unset (backward-compatible
	//      single-node dev/test default, NOT suitable for multi-node clusters
	//      because each node would derive a different value).
	//   3. Literal 1 as the last-resort non-zero fallback.
	switch {
	case cfg.Raft.DeploymentID != 0:
		qcfg.Raft.DeploymentID = cfg.Raft.DeploymentID
	case cfg.Raft.NodeID != 0:
		qcfg.Raft.DeploymentID = cfg.Raft.NodeID
	default:
		qcfg.Raft.DeploymentID = 1
	}

	// Join mode: when cfg.Raft.Join is true this node is joining an existing
	// cluster rather than bootstrapping a new one. Passed directly to
	// host.StartShard so quicraft marks the peer as a joiner and bypasses the
	// "self must be present in members" validation.
	qcfg.Raft.Join = cfg.Raft.Join

	// Map the quicbaas static peer list (JoinAddresses) into the qrdb
	// "replicaID=address" Peers format. quicbaas JoinAddresses carry raw
	// host:port strings with no replicaID prefix; we cannot know the
	// remote IDs here, so we build a comma-separated list that
	// qrdb.cluster.ParsePeers will accept (the caller must supply peers
	// in "id=addr" format via cfg.Raft.JoinAddresses if they contain
	// the ID prefix). If no prefix is found we treat the list as seed
	// addresses only and skip the Peers field so qrdb's discovery picks
	// them up via the static method.
	var peerEntries []string
	for _, addr := range cfg.Raft.JoinAddresses {
		if strings.Contains(addr, "=") {
			peerEntries = append(peerEntries, addr)
		}
	}
	if len(peerEntries) > 0 {
		qcfg.Raft.Peers = strings.Join(peerEntries, ",")
	}

	// ------------------------------------------------------------------ //
	// Discovery: map quicbaas DiscoveryConfig → qrdb DiscoveryConfig.
	// The qrdb Discovery.Method controls how the node finds its peers;
	// quicbaas's Discovery.Type uses similar names.
	// ------------------------------------------------------------------ //
	discoveryMethodMap := map[string]string{
		"static":     "static",
		"multicast":  "multicast",
		"dns":        "dns",
		"token":      "token",
		"kubernetes": "static", // kubernetes uses static initial peer list
		"consul":     "static", // consul uses static initial peer list
	}
	method, mok := discoveryMethodMap[cfg.Discovery.Type]
	if !mok {
		method = "static"
	}
	qcfg.Discovery.Method = method

	// Multicast discovery parameters.
	if method == "multicast" {
		qcfg.Discovery.MulticastSecret = cfg.Discovery.Multicast.ClusterSecret
		if cfg.Discovery.Multicast.Group != "" {
			qcfg.Discovery.MulticastGroup = cfg.Discovery.Multicast.Group
		}
	}

	// DNS discovery parameters.
	if method == "dns" {
		qcfg.Discovery.DNSDomain = cfg.Discovery.DNSName
	}

	// Token discovery parameters.
	if method == "token" {
		qcfg.Discovery.TokenEndpoint = cfg.Discovery.ConsulAddress
	}

	return qcfg
}

// buildXKMSCryptoOptions collects hardware strategy factories registered via
// the quicbaas seal_factory_*.go files and constructs the canonical
// []qrdbsdk.ServerOption via XKMSCryptoOptions. When no hardware strategy is
// selected (software/none/shamir), the returned slice still carries the
// default StrategyRegistry + PINBackend + FactorRegistry so the qrdb server
// always starts with a complete composition.
func buildXKMSCryptoOptions(cfg *config.Config, logger *slog.Logger) ([]qrdbsdk.ServerOption, error) {
	if logger == nil {
		logger = slog.Default()
	}

	// Populate StrategyFactories from the hardware registry for the
	// configured strategy. Only the active strategy's factory is needed;
	// the registry also covers future multi-strategy configs.
	var strategyFactories map[qrdbsdk.StrategyID]qrdbsdk.StrategyFactory
	strategyID := qrdbsdk.StrategyID(cfg.Seal.Strategy)
	if isHardwareStrategy(strategyID) {
		factory, ok := hardwareStrategyFactories[strategyID]
		if ok {
			// Wrap the quicbaas hardwareStrategyFactory (which takes
			// *config.Config) as a qrdbsdk.StrategyFactory (which takes
			// no args and returns SealingStrategy). The config is captured
			// in the closure.
			capturedCfg := cfg
			capturedFactory := factory
			strategyFactories = map[qrdbsdk.StrategyID]qrdbsdk.StrategyFactory{
				strategyID: func() (qrdbsdk.SealingStrategy, error) {
					return capturedFactory(capturedCfg)
				},
			}
		}
	}

	// Select the PIN backend via the shared go-xkms pinselect decision point.
	// Today this yields a PERSISTENT software backend (previously the composer
	// left PINBackend nil, which produced an in-memory-only backend whose SO/
	// User PINs did not survive a daemon restart).
	pinBackend, err := buildPINBackend(cfg, logger)
	if err != nil {
		return nil, err
	}

	cryptoCfg := &XKMSCryptoConfig{
		Logger:            logger,
		StrategyFactories: strategyFactories,
		Factors:           buildXKMSFactors(cfg),
		PINBackend:        pinBackend,
	}

	return XKMSCryptoOptions(cryptoCfg)
}

// buildXKMSFactors constructs the go-xkms auth-factor plugins registered into
// qrdb's FactorRegistry via WithFactorRegistry. They give the qrdb-backed
// factor-login path (Security().Auth().Factors().Login) the ability to verify
// WebAuthn/FIDO2, PIV, and PKCS#11/HSM credentials in addition to qrdb-native
// mTLS — the composition seam that lets WebAuthn/PIV/HSM login work end-to-end
// through qrdb.
//
// All three plugins are self-contained, lazy, and HSM-free at construction:
// the PIV plugin falls back to a software PIV key generator, the FIDO2 plugin
// defaults its relying-party ID to the configured IAM WebAuthn RPID (or
// "localhost" when unset), and the PKCS#11 plugin only verifies signatures
// against a DER public key captured at enrollment — it never opens a PKCS#11
// module or touches a slot/token. Consequently the PKCS#11 factor is safe to
// register unconditionally (mirroring FIDO2/PIV): a node with no HSM simply
// never enrolls a PKCS#11 credential, leaving the plugin inert. This differs
// from the PKCS#11 SEAL strategy (seal_factory_pkcs11.go), which is build-tag
// gated precisely because its backend opens the live module; the factor has no
// such dependency, so no build tag or config gate applies here. Verification
// crypto lives in go-xkms; qrdb only stores the opaque enrollment and routes
// (type, lookupKey) → plugin.Verify.
func buildXKMSFactors(cfg *config.Config) []qrdbsdk.AuthFactorPlugin {
	var rpID string
	if cfg != nil {
		rpID = cfg.IAM.WebAuthn.RPID
	}
	return []qrdbsdk.AuthFactorPlugin{
		xkmsfido2.New(xkmsfido2.Config{RPID: rpID}),
		xkmspiv.New(xkmspiv.Config{}),
		xkmspkcs11.New(xkmspkcs11.Config{}),
	}
}

// initStandaloneQRDBNode constructs the real qrdb standalone node for
// quicbaas standalone mode. It:
//
//  1. Builds a *qrdbconfig.Config from the quicbaas config.
//  2. Calls XKMSCryptoOptions to inject hardware crypto strategies
//     from the seal_factory_*.go registry into qrdb's server.New.
//  3. Calls qrdbnode.NewStandaloneNode with the composed server options so
//     hardware crypto, at-rest encryption, tenant/barrier isolation, and all
//     qrdb KV/MVCC/TS/Vector services are live inside the node.
//  4. Stores the node on s.qrdbNode so lifecycle.go can wire its SDK client
//     into the QuicBaaS service builder.
//
// On success the server's data-plane encryptor, data-plane barrier, and
// dataPlaneBarrierCloser are all owned by the qrdb node — quicbaas MUST NOT
// construct a duplicate barrier for the standalone path.
//
// A background context is used because this is called from the synchronous
// server initialization path, which has no cancellable context.
func (s *Server) initStandaloneQRDBNode() error {
	ctx := context.Background()
	s.logger.Info("Standalone mode: constructing real qrdb engine via NewStandaloneNode")

	qcfg := buildQRDBConfig(s.config, false)

	serverOpts, err := buildXKMSCryptoOptions(s.config, slog.Default())
	if err != nil {
		return &QRDBNodeError{Stage: "build xkms crypto options", Cause: err}
	}

	// Node options: we pass slog.Default() as the logger; in a well-configured
	// quicbaasd process the default slog handler is set to the same output as
	// the quicbaas logger so log lines route correctly.
	nodeOpts := []qrdbnode.NodeOption{
		qrdbnode.WithLogger(slog.Default()),
	}

	// Inject the late-bound OIDC session issuer so a successful unified login
	// through the node's in-process ceremony mints an OIDC bearer. The issuer
	// is created here (provider unbound) and has its provider attached after
	// IAM init via bindLoginCeremonyProvider. No-op (nil) when IAM is absent.
	s.buildLoginSessionIssuer()
	if s.loginSessionIssuer != nil {
		nodeOpts = append(nodeOpts, qrdbnode.WithSessionIssuer(s.loginSessionIssuer))
	}

	// Inject the late-bound hardware FactorVerifiers (PIV + tenant-aware
	// WebAuthn) so the node's login ceremony can verify PIV and WebAuthn
	// proofs. The verifier shells are created here (dependencies unbound) and
	// have their dependencies attached after IAM init via
	// bindLoginCeremonyProvider. None (empty) when IAM is absent.
	s.buildLoginFactorVerifiers()
	for _, v := range s.loginFactorVerifiers {
		if v != nil {
			nodeOpts = append(nodeOpts, qrdbnode.WithLoginFactorVerifier(v))
		}
	}

	// When the operator supplied a passphrase (via server.WithSealPassphrase),
	// inject it into the node builder so the barrier is auto-unsealed on
	// startup. Without this, a passphrase-strategy node boots sealed and
	// node.Client() returns nil, which causes GetKVStore to panic.
	if len(s.opts.sealPassphrase) > 0 && qcfg.Seal.Enabled {
		nodeOpts = append(nodeOpts, qrdbnode.WithSealPassphrase(s.opts.sealPassphrase))
	}

	// When a hardware/cloud seal strategy is configured (awskms, vault, gcpkms,
	// azurekv, tpm2, pkcs11), build it and inject it into the node builder via
	// WithSealStrategy so the barrier is sealed with the cloud strategy and can
	// auto-unseal on startup. Without this the node falls back to the deferred-
	// software (passphrase) strategy and auto-unseal fails with empty credentials.
	if qcfg.Seal.Enabled {
		strategies, stratErr := buildHardwareSealStrategies(s.config)
		if stratErr != nil {
			return &QRDBNodeError{Stage: "build hardware seal strategy", Cause: stratErr}
		}
		if len(strategies) > 0 {
			nodeOpts = append(nodeOpts, qrdbnode.WithSealStrategy(strategies[0]))
			s.logger.Info("Standalone node: injecting hardware/cloud seal strategy",
				"strategy", strategies[0].ID())
		}
	}

	node, err := qrdbnode.NewStandaloneNode(ctx, qcfg, serverOpts, nodeOpts...)
	// Zero the seal passphrase now that the node builder has consumed it.
	for i := range s.opts.sealPassphrase {
		s.opts.sealPassphrase[i] = 0
	}
	s.opts.sealPassphrase = nil
	if err != nil {
		return &QRDBNodeError{Stage: "new standalone node", Cause: err}
	}

	s.qrdbNode = node

	s.logger.Info("Standalone qrdb node started; at-rest encryption and hardware crypto active")
	return nil
}

// stopQRDBNode closes the qrdb node (standalone or clustered) and clears
// the reference. It is idempotent; a nil node is silently skipped.
func (s *Server) stopQRDBNode() error {
	node := s.qrdbNode
	if node == nil {
		return nil
	}
	s.qrdbNode = nil
	if err := node.Close(); err != nil {
		return &QRDBNodeError{Stage: "close", Cause: err}
	}
	return nil
}

// initClusteredQRDBNode constructs the real qrdb clustered node for
// quicbaas cluster mode. It:
//
//  1. Builds a *qrdbconfig.Config from the quicbaas config with Raft and
//     Discovery fields populated for multi-node operation.
//  2. Parses the members map (replicaID → raftAddress) from the static
//     peer list in cfg.Raft.JoinAddresses (format: "id=host:port").
//     A single-member map falls back to single-node mode (WithoutTransport).
//  3. Loads mTLS certificate material from cfg.Raft.TLSCertFile /
//     TLSKeyFile / TLSCAFile and injects it via node.WithRaftMTLS so all
//     inter-node QUIC connections are mutually authenticated. mTLS is
//     skipped for single-node clusters.
//  4. Calls XKMSCryptoOptions to inject hardware crypto strategies
//     from the seal_factory_*.go registry into qrdb's server.New.
//  5. Calls qrdbnode.NewClusteredNode with the composed options so the
//     full Raft consensus, at-rest encryption, and qrdb services are live.
//  6. Stores the node on s.qrdbNode so lifecycle.go can wire its SDK
//     client into the QuicBaaS service builder.
//
// On success the server's data-plane encryptor, data-plane barrier, and
// dataPlaneBarrierCloser are all owned by the qrdb node — quicbaas MUST
// NOT construct duplicate barrier infrastructure for the cluster path.
func (s *Server) initClusteredQRDBNode() error {
	ctx := context.Background()
	s.logger.Info("Cluster mode: constructing real qrdb clustered node via NewClusteredNode")

	qcfg := buildQRDBConfig(s.config, true)

	members, err := buildClusterMembers(s.config)
	if err != nil {
		return &QRDBNodeError{Stage: "build cluster members", Cause: err}
	}

	serverOpts, err := buildXKMSCryptoOptions(s.config, slog.Default())
	if err != nil {
		return &QRDBNodeError{Stage: "build xkms crypto options", Cause: err}
	}

	nodeOpts := []qrdbnode.NodeOption{
		qrdbnode.WithLogger(slog.Default()),
	}

	// Inject the late-bound OIDC session issuer (see initStandaloneQRDBNode).
	s.buildLoginSessionIssuer()
	if s.loginSessionIssuer != nil {
		nodeOpts = append(nodeOpts, qrdbnode.WithSessionIssuer(s.loginSessionIssuer))
	}

	// Inject the late-bound hardware FactorVerifiers (see initStandaloneQRDBNode).
	s.buildLoginFactorVerifiers()
	for _, v := range s.loginFactorVerifiers {
		if v != nil {
			nodeOpts = append(nodeOpts, qrdbnode.WithLoginFactorVerifier(v))
		}
	}

	// When QRDB_NOSYNC_WRITES=1 is set, disable PebbleDB fsync to avoid
	// proposal timeouts in Docker/overlay2 testing environments where fsync
	// latency can exceed the REST request timeout. Never enable in production.
	if os.Getenv("QRDB_NOSYNC_WRITES") == "1" {
		nodeOpts = append(nodeOpts, qrdbnode.WithNoSyncWrites())
	}

	// When the operator supplied a passphrase (via server.WithSealPassphrase),
	// inject it into the node builder so the barrier is auto-unsealed on
	// startup. Without this, a passphrase-strategy node boots sealed and
	// node.Client() returns nil, which causes GetKVStore to panic.
	if len(s.opts.sealPassphrase) > 0 && qcfg.Seal.Enabled {
		nodeOpts = append(nodeOpts, qrdbnode.WithSealPassphrase(s.opts.sealPassphrase))
	}

	// When a hardware/cloud seal strategy is configured, build it and inject
	// it into the node builder so the barrier is sealed with the cloud strategy
	// and auto-unseals on cluster boot.
	if qcfg.Seal.Enabled {
		strategies, stratErr := buildHardwareSealStrategies(s.config)
		if stratErr != nil {
			return &QRDBNodeError{Stage: "build hardware seal strategy", Cause: stratErr}
		}
		if len(strategies) > 0 {
			nodeOpts = append(nodeOpts, qrdbnode.WithSealStrategy(strategies[0]))
			s.logger.Info("Clustered node: injecting hardware/cloud seal strategy",
				"strategy", strategies[0].ID())
		}
	}

	// Inject mTLS only for multi-node clusters. Single-node uses
	// WithoutTransport (no QUIC transport, no cert material needed).
	if len(members) > 1 {
		mtls, mtlsErr := buildRaftMTLSConfig(s.config)
		if mtlsErr != nil {
			return &QRDBNodeError{Stage: "build raft mtls", Cause: mtlsErr}
		}
		if mtls != nil {
			nodeOpts = append(nodeOpts, qrdbnode.WithRaftMTLS(mtls))
		}
	}

	node, err := qrdbnode.NewClusteredNode(ctx, qcfg, members, serverOpts, nodeOpts...)
	// Zero the seal passphrase now that the node builder has consumed it.
	// NewClusteredNode copies the passphrase into a string before returning;
	// the original byte slice is no longer needed and should not linger.
	for i := range s.opts.sealPassphrase {
		s.opts.sealPassphrase[i] = 0
	}
	s.opts.sealPassphrase = nil
	if err != nil {
		return &QRDBNodeError{Stage: "new clustered node", Cause: err}
	}

	s.qrdbNode = node

	s.logger.Infof("Clustered qrdb node started; members: %d, at-rest encryption and hardware crypto active",
		len(members))
	return nil
}

// buildClusterMembers parses the quicbaas cluster peer list into the
// members map expected by qrdbnode.NewClusteredNode. Peers must be
// expressed in "id=host:port" format inside cfg.Raft.JoinAddresses.
//
// When JoinAddresses is empty or none of the entries carry an id= prefix,
// a single-node members map {1: cfg.Raft.RaftAddress} is returned so the
// node can start in single-node-clustered mode (useful for dev/test).
func buildClusterMembers(cfg *config.Config) (map[uint64]string, error) {
	var peerStrings []string
	for _, addr := range cfg.Raft.JoinAddresses {
		if strings.Contains(addr, "=") {
			peerStrings = append(peerStrings, addr)
		}
	}

	if len(peerStrings) > 0 {
		return qrdbcluster.ParsePeers(peerStrings)
	}

	// No id-prefixed peers: single-node clustered mode.
	raftAddr := cfg.Raft.RaftAddress
	if raftAddr == "" {
		raftAddr = "127.0.0.1:63101"
	}
	replicaID := cfg.Raft.NodeID
	if replicaID == 0 {
		replicaID = 1
	}
	return map[uint64]string{replicaID: raftAddr}, nil
}

// buildRaftMTLSConfig loads PEM-encoded certificate material from the
// quicbaas Raft TLS file paths and returns a quicraft MTLSConfig. Returns
// nil without error when none of the Raft TLS paths are configured (the
// caller treats nil as "no mTLS" and falls back to plaintext, which is
// only acceptable for single-node development clusters).
func buildRaftMTLSConfig(cfg *config.Config) (*quicraftconfig.MTLSConfig, error) {
	certFile := cfg.Raft.TLSCertFile
	keyFile := cfg.Raft.TLSKeyFile
	caFile := cfg.Raft.TLSCAFile

	if certFile == "" && keyFile == "" && caFile == "" {
		return nil, nil
	}

	mtls := &quicraftconfig.MTLSConfig{}

	if caFile != "" {
		caPEM, err := os.ReadFile(caFile)
		if err != nil {
			return nil, &RaftMTLSLoadError{File: caFile, Cause: err}
		}
		mtls.CACert = caPEM
	}
	if certFile != "" {
		certPEM, err := os.ReadFile(certFile)
		if err != nil {
			return nil, &RaftMTLSLoadError{File: certFile, Cause: err}
		}
		mtls.Cert = certPEM
	}
	if keyFile != "" {
		keyPEM, err := os.ReadFile(keyFile)
		if err != nil {
			return nil, &RaftMTLSLoadError{File: keyFile, Cause: err}
		}
		mtls.Key = keyPEM
	}

	return mtls, nil
}

// newNodeBackedShardedClient returns a *ShardedClient backed by the qrdb
// node's SDK client. All shard-ID lookups return the same client via the
// nodeClient short-circuit in ShardedClient.ForShard.
//
// The factory is wired with the embedded transport config so IsEmbedded()
// returns true — callers that check the transport type still see the correct
// embedded indicator. The qrdb node owns all per-shard storage; quicbaas
// does not manage separate per-shard stores.
//
// The node client's lifetime is owned by the qrdb node, not the ShardedClient.
// When nodeOwned is true, Close() is a no-op on the ShardedClient side;
// the node closes the client during node.Close().
func newNodeBackedShardedClient(node *qrdbnode.Node) *ShardedClient {
	embeddedCfg := &config.QRDBConfig{Transport: config.QRDBTransportEmbedded}
	return &ShardedClient{
		factory:    newTransportFactory(embeddedCfg, nil, nil),
		nodeClient: node.Client(),
		nodeOwned:  true,
	}
}

// standaloneQRDBDataDir returns the effective data directory that the
// standalone qrdb node will use. Mirrors buildQRDBConfig's DataDir logic
// so callers that need the path before constructing the node can resolve it.
func standaloneQRDBDataDir(cfg *config.Config) string {
	if cfg.Storage.DataDir != "" {
		return cfg.Storage.DataDir
	}
	if cfg.Seal.BarrierDir != "" {
		return filepath.Dir(cfg.Seal.BarrierDir)
	}
	return ""
}
