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
	"crypto/tls"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbnode "github.com/jeremyhahn/go-qrdb/sdk/go/node"
	xkmslib "github.com/jeremyhahn/go-xkms/pkg/xkms"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	mcpapi "github.com/jeremyhahn/go-quicbaas/pkg/api/mcp"
	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/apimodule"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/routing"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	qbshards "github.com/jeremyhahn/go-quicbaas/pkg/shards"
)

// defaultVolumePoolDirName is the subdirectory under the node data dir that
// holds the single default volume pool. Its Pebble instances are assigned to
// barrier-mode tenants by the barrier registry when RegisterTenant is called
// without an explicit storage backend.
// provisionClusterMTLS provides the mTLS certificate material for a multi-node
// Raft cluster. Without -tags raft, only operator-supplied PEM files are
// supported (buildRaftMTLSConfig). With -tags raft, qrdb_node_cluster.go
// overrides this via init() to the full three-way precedence switch:
//
//  1. Operator-supplied explicit PEM files (TLSCertFile/TLSKeyFile/TLSCAFile)
//  2. Hardware-resident node key (RaftEnrollment.KeyBackend set)
//  3. Software node key auto-provisioned via cluster-TLS enrollment
//
// The returned Advertiser is non-nil only on the first (bootstrapping) node
// in the software or hardware enrollment paths; callers must stop it on shutdown.
var provisionClusterMTLS = defaultRaftMTLSProvision

// defaultRaftMTLSProvision is the non-raft fallback: explicit PEM files only.
// It is replaced by the full precedence switch in qrdb_node_cluster.go when
// building with -tags raft.
func defaultRaftMTLSProvision(s *Server, _ map[uint64]string) (*qrdbnode.RaftMTLSConfig, interface{ Stop() }, error) {
	mtls, err := buildRaftMTLSConfig(s.config)
	return mtls, nil, err
}

const defaultVolumePoolDirName = "tenant-pools/default"

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

// buildQRDBConfig maps a quicbaas *config.Config to a *qrdbsdk.DaemonConfig.
// When clusterMode is false (standalone), Raft and Discovery fields are left
// at zero values — the standalone node builder uses a LocalBackend with no
// quicraft host. When clusterMode is true, Raft address, shard/replica IDs,
// deployment ID, static peer list, and the discovery method are populated
// from the quicbaas Raft and Discovery config sections.
func buildQRDBConfig(cfg *config.Config, clusterMode bool) (*qrdbsdk.DaemonConfig, error) {
	qcfg := &qrdbsdk.DaemonConfig{}

	// Authentication and authorization on the embedded node mirror
	// quicbaas's own enforcement posture (Security.RequireAuthz — the same
	// flag that gates the gRPC interceptors and the module token fallback):
	// the node's middleware guards every qrdb-native plane and provides the
	// upstream identity for module routes. Stated EXPLICITLY even though
	// qrdb's zero value is fail-safe (nil ⇒ enabled), so the posture is
	// legible at the embedding site.
	qcfg.Auth.Enabled = qrdbsdk.BoolPtr(cfg.Security.RequireAuthz)

	// The node's mTLS authenticator pins the same SPIFFE trust domain the
	// first-boot admin certificate is issued under (iam.spiffe_trust_domain),
	// and the node's built-in CA stamps it on the client certificates it
	// mints. Left empty, the node keeps qrdb's default: no SPIFFE pin.
	qcfg.Auth.SPIFFETrustDomain = cfg.IAM.SPIFFETrustDomain

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
	qcfg.Databases = []qrdbsdk.DaemonDatabaseConfig{
		{Name: "default", Engine: "mvcc", Backend: qrdbBackend},
	}

	// ------------------------------------------------------------------ //
	// Seal barrier: map quicbaas SealConfig → qrdb SealConfig, honoring the
	// init-marker precedence (explicit operator override > persisted
	// init-done marker > config default). This is what makes a
	// `quicbaasd init --barrier-strategy shamir --shares N --threshold M`
	// data dir boot the NODE's barrier with the SAME shamir scheme the
	// ceremony initialized, instead of the "passphrase" config default that
	// could never unseal it.
	// ------------------------------------------------------------------ //
	qcfg.Seal.Enabled = cfg.Seal.Enabled
	qcfg.Seal.Strategy = cfg.Seal.Strategy
	if qcfg.Seal.Strategy == "" {
		qcfg.Seal.Strategy = "passphrase"
	}
	qcfg.Seal.ShamirThreshold = cfg.Seal.Threshold
	qcfg.Seal.ShamirTotal = cfg.Seal.TotalShares
	resolved, threshold, total, fromMarker, resolveErr := resolveBarrierStrategy(cfg, cfg.Seal.StrategyExplicit)
	if resolveErr != nil {
		return nil, resolveErr
	}
	if resolved != "" {
		qcfg.Seal.Strategy = string(resolved)
		qcfg.Seal.ShamirThreshold = threshold
		qcfg.Seal.ShamirTotal = total

		// A strategy taken from the init marker also ENABLES the seal layer.
		// Resolving only the strategy is not enough: `quicbaasd init
		// --barrier-strategy shamir` deliberately initializes no barrier (that
		// is the ceremony's job — go-qrdb pkg/bootstrap), so if seal stayed off
		// here the node would boot UNSEALED, skip the sealed window, and run
		// first-boot against an unprotected data plane. The M-of-N barrier
		// would then never be created and the operator's chosen strategy would
		// be silently ignored.
		//
		// Only the marker path does this — hence fromMarker rather than a test
		// on `resolved`. The config default is itself "passphrase", so a
		// non-empty strategy proves nothing about operator intent; enabling on
		// that would seal every node whose config never mentioned a barrier.
		// Under an explicit override the operator's own seal.enabled wins.
		if fromMarker && resolved != qrdbsdk.StrategyNone {
			qcfg.Seal.Enabled = true
		}
	}

	// Provision a default volume pool so the barrier registry can satisfy
	// RegisterTenant calls that do not supply an explicit storage backend.
	// Without this the node returns ErrPoolNotConfigured for every per-tenant
	// barrier registration. The pool is skipped for pure in-memory nodes
	// (dataDir == "") because there is no durable path to anchor it to.
	if dataDir != "" && len(qcfg.Storage.VolumePools) == 0 {
		poolPath := filepath.Join(dataDir, defaultVolumePoolDirName)
		qcfg.Storage.VolumePools = []qrdbsdk.DaemonVolumePoolConfig{
			{Name: "default", Paths: []string{poolPath}, Strategy: "round_robin"},
		}
	}

	// Unified API server: network serving runs exclusively on the embedded
	// node's own multi-protocol server (see (*config.Config).NetworkServing).
	// The node's REST/gRPC/MCP listeners come up on the SAME ports the legacy
	// per-protocol config sections have always carried and serve the quicbaas
	// capability plane through the ServiceModule registered in
	// unifiedModuleServerOptions, gated per-RPC by qrdb's reflection
	// interceptor over option (qrdb.v1.permission). A protocol left disabled
	// (Enabled=false) starts no transport for it. Enabling any protocol here
	// means node construction runs the full qrdb server start pipeline —
	// module surfaces answer Unavailable until quicbaasd finishes composing
	// the QuicBaaS facade.
	if cfg.NetworkServing() {
		qcfg.Server.Host = cfg.Server.Host
		qcfg.Server.AdminPort = cfg.Server.REST.Port
		qcfg.Server.GRPCPort = cfg.Server.GRPC.Port
		qcfg.Protocols.REST = cfg.Server.REST.Enabled
		qcfg.Protocols.GRPC = cfg.Server.GRPC.Enabled
		qcfg.Protocols.MCP = cfg.Server.MCP.Enabled
		if qcfg.Protocols.MCP {
			qcfg.MCP.Port = cfg.Server.MCP.Port
		}
	}

	if !clusterMode {
		// Standalone: no Raft host, no discovery. The node builder applies
		// safe defaults when the ports are zero.
		return qcfg, nil
	}

	// ------------------------------------------------------------------ //
	// Cluster mode: populate Raft address, shard/replica IDs, deployment
	// ID, and peer list from the quicbaas Raft config.
	// ------------------------------------------------------------------ //
	qcfg.Raft.RaftAddress = cfg.Raft.RaftAddress
	// The bind address comes from configuration: raft.listen_address when any
	// layer set it, otherwise raft.listen_host on the advertised port.
	listenAddr, listenErr := cfg.Raft.ListenAddr()
	if listenErr != nil {
		return nil, &QRDBNodeError{Stage: "raft listen address", Cause: listenErr}
	}
	qcfg.Raft.ListenAddress = listenAddr
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

	// go-quicraft admits a member by its identity only; qrdb parses and
	// validates this "replicaID=identity" list, so it is passed through as is.
	qcfg.Raft.MemberIdentities = strings.Join(cfg.Raft.MemberIdentities, ",")

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

	return qcfg, nil
}

// buildXKMSCryptoOptions collects hardware strategy factories registered via
// the quicbaas seal_factory_*.go files and constructs the canonical
// []qrdbsdk.ServerOption via XKMSCryptoOptions. When no hardware strategy is
// selected (software/none/shamir), the returned slice still carries the
// default StrategyRegistry + PINBackend + FactorRegistry so the qrdb server
// always starts with a complete composition.
//
// sharedTPM is the open TPM2 seal backend (from buildSealStrategyOnce) when the
// seal strategy is TPM-backed, or nil otherwise. It is threaded into the PIN
// backend so a tpm2 PIN verifies over the SAME single TPM open as the seal
// strategy — never a second open. Nil means no TPM2 seal is active; a tpm2 PIN
// then fails closed (PINRequiresTPMSealError).
func buildXKMSCryptoOptions(cfg *config.Config, logger *slog.Logger, sharedTPM *xkmssdk.TPM2Backend, ceremonyCA qrdbsdk.CeremonyCA) ([]qrdbsdk.ServerOption, qrdbsdk.PINBackend, error) {
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
	// software/auto yields a PERSISTENT software backend; an explicit tpm2 PIN
	// verifies in-hardware over the seal strategy's shared TPM open (sharedTPM).
	pinBackend, err := buildPINBackend(cfg, logger, newSealSharedTPMPINProvider(sharedTPM, cfg, logger))
	if err != nil {
		return nil, nil, err
	}

	cryptoCfg := &XKMSCryptoConfig{
		Logger:            logger,
		StrategyFactories: strategyFactories,
		Factors:           buildXKMSFactors(cfg),
		PINBackend:        pinBackend,
		CeremonyCA:        ceremonyCA,
	}

	opts, err := XKMSCryptoOptions(cryptoCfg)
	if err != nil {
		return nil, nil, err
	}
	return opts, pinBackend, nil
}

// buildSealStrategyOnce builds the configured hardware/cloud seal strategy a
// single time and caches it (and, when TPM-backed, its open TPM2 backend) on the
// Server. This is the P3/P4 single-TPM-open guarantee: the one open made here is
// reused by BOTH the embedded qrdb node's barrier (injected via WithSealStrategy)
// AND the PIN backend (which verifies over the same TPM session). It MUST run
// before buildXKMSCryptoOptions so the shared backend is available to the PIN
// path.
//
// It is a no-op (leaving s.sealStrategy and s.sealTPM2Backend nil) when seal is
// disabled or the strategy is software/none/shamir — the embedded node then
// builds its own software strategy, exactly as before. A configured-but-not-
// compiled cloud/HSM strategy surfaces as a typed StrategyNotCompiledError.
// Idempotent: a second call with an already-built strategy returns immediately.
func (s *Server) buildSealStrategyOnce(sealEnabled bool) error {
	if !sealEnabled || s.sealStrategy != nil {
		return nil
	}
	strategies, tpmBackend, err := buildHardwareSealStrategiesShared(s.config, slog.Default())
	if err != nil {
		return &QRDBNodeError{Stage: "build hardware seal strategy", Cause: err}
	}
	if len(strategies) > 0 {
		s.sealStrategy = strategies[0]
		s.sealTPM2Backend = tpmBackend
	}
	return nil
}

// buildXKMSFactors returns the go-xkms auth-factor plugins that are registered
// into qrdb's FactorRegistry via WithFactorRegistry. These plugins power the
// CUSTODIAN enrollment/challenge/verify surface (qrdb's unified
// AuthFactorService, used for Shamir unseal ceremonies and custodian factor
// enrollment) reached via server.WithFactorRegistry. They are NOT the factor
// verifiers used for the interactive login path: login verification goes
// through qrdb's embedded FactorVerifier instances wired in
// login_factor_verifiers.go (lateBoundPIVVerifier, tenantWebAuthnVerifier) and
// the auth.LoginResolver.
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
		// When the operator does not configure an explicit RP ID, derive it
		// from the canonical issuer URL so the FIDO2 factor verifier agrees
		// with the WebAuthn relying party, the OIDC issuer, and the activation
		// banner. An IP-literal issuer yields no valid RP ID; in that case the
		// factor falls back to the xkms default ("localhost") and the
		// authoritative fail-closed validation happens during IAM init.
		if rpID == "" {
			if derived, _, err := webauthnDefaultsFromIssuer(issuerURLFromConfig(cfg)); err == nil {
				rpID = derived
			}
		}
	}
	return xkmssdk.DefaultAuthFactors(xkmssdk.FactorConfig{
		FIDO2RPID: rpID,
	})
}

// UnifiedTLSNotReadyError is returned to a unified-server TLS handshake that
// arrives before initializeCA has composed the server TLS material. The
// handshake fails closed; once the material is composed every new handshake
// serves the CA-backed configuration.
type UnifiedTLSNotReadyError struct{}

// Error implements the error interface.
func (e *UnifiedTLSNotReadyError) Error() string {
	return "qrdb node: unified server TLS material is not yet composed (CA still initializing)"
}

// quicBaaSServiceRef returns the composed service.QuicBaaS facade reference
// under the server mutex, or nil while lifecycle.go is still composing it.
// It is the late-binding seam the quicbaas ServiceModule resolves through
// (mirroring xkms's xkmsServiceRef).
//
// The assignment site (startService in pkg/server/lifecycle.go) publishes
// s.quicBaaSService under s.mu.Lock/Unlock, matching this RLock read — the
// wave-1 finding (unsynchronized publish racing the embedded qrdb node's
// unified-server request goroutines, which can begin serving before
// startService finishes composing the facade) is fixed.
func (s *Server) quicBaaSServiceRef() service.QuicBaaS {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.quicBaaSService
}

// serverTLSConfigRef returns the CA-backed *tls.Config composed by
// initializeCA under the server mutex, or nil while it is not yet composed.
func (s *Server) serverTLSConfigRef() *tls.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tlsConfig
}

// unifiedServerTLSConfig builds the TLS configuration the embedded node's
// unified transports serve with. The material is late-bound: initializeCA
// composes s.tlsConfig AFTER node construction, so every handshake defers to
// serverTLSConfigRef via GetConfigForClient — failing closed with the typed
// UnifiedTLSNotReadyError until the CA-backed configuration exists, and
// picking up later certificate rotations without listener restarts.
func (s *Server) unifiedServerTLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) {
			cfg := s.serverTLSConfigRef()
			if cfg == nil {
				return nil, &UnifiedTLSNotReadyError{}
			}
			return cfg, nil
		},
	}
}

// buildModuleShardRouter constructs the identical multi-shard router and
// vector-operations shard ID pkg/server/lifecycle.go's startGRPCServer
// builds for the legacy gRPC transport's facade (same kvShards +
// validateRoutingFn + routing.NewWithShards + EntityShardPins sequence, over
// the same s.config.Server.Shards / s.config.Vector / s.config.KMS.Raft.ShardID
// / s.config.TimeSeries.ShardID configuration), so
// unifiedModuleServerOptions's apimodule.WithShardRouting call threads the
// SAME routing decisions into the unified server's shared facade. Returns a
// *QRDBNodeError if an entity shard pin references a shard outside the
// configured shard list — the same fail-fast contract
// startGRPCServer enforces via validateRoutingFn.
func (s *Server) buildModuleShardRouter() (apimodule.ShardRouter, uint64, error) {
	vectorShardID := s.config.Vector.ShardID
	if vectorShardID == 0 && s.config.Vector.Enabled {
		vectorShardID = qbshards.PlatformShardID
	}
	kmsShardID := s.config.KMS.Raft.ShardID
	if kmsShardID == 0 {
		kmsShardID = qbshards.PlatformShardID
	}
	kvShardList := kvShards(s.config.Server.Shards, vectorShardID, s.config.TimeSeries.ShardID, kmsShardID)
	if err := validateRoutingFn(kvShardList, s.config.Server.EntityShardPins); err != nil {
		return nil, 0, &QRDBNodeError{Stage: "build module shard router", Cause: err}
	}
	router := routing.NewWithShards(kvShardList)
	for et, shard := range s.config.Server.EntityShardPins {
		router.SetEntityShard(et, shard)
	}
	return router, vectorShardID, nil
}

// unifiedModuleServerOptions appends the quicbaas ServiceModule, go-xkms's
// own ServiceModule ("xkms" — Phase 3 of the login-plane plan, so the
// unified server carries the canonical hardware crypto + identity plane
// alongside quicbaas's broker/core plane), and, when server.unified is
// enabled, the unified server TLS material — to the qrdb server options
// both node constructors pass to the node builder. go-qrdb's route-collision
// guard fails construction loudly if the two modules' registrations ever
// overlap.
//
// Phase 6 of the unified API server (plans/unified-api-server-2026-08-14.md):
// the module carries the broker capability planes (Scheduler, Orchestrator,
// Project, Account, SSH, Messaging, Embedding) as lazily-bound gRPC services.
// Registration is ADDITIVE — the legacy transport stacks keep serving as
// today; the module surfaces activate only when the node's server transports
// are enabled (server.unified.enabled, see buildQRDBConfig), gated per-RPC by
// qrdb's reflection interceptor over option (qrdb.v1.permission).
//
// ORDERING: this runs from node construction, long before lifecycle.go
// composes the QuicBaaS facade — and when node transports are enabled, node
// construction invokes the module's RegisterGRPC immediately. The module
// therefore takes a lazy resolver and binds on the first RPC: until
// s.quicBaaSService is set, every module RPC answers Unavailable.
// defaultQRDBAuditRingCapacity is the in-memory audit ring size used ONLY
// when Storage.DataDir is empty (embedded/test contexts with no durable
// storage root at all — the same posture the storage layer itself falls
// back to). Every real deployment has a DataDir, so this path is a
// defensive fallback, never the production default: P5 forbids an
// in-memory sink that silently loses records on restart.
const defaultQRDBAuditRingCapacity = 1024

// qrdbAuditLogSubdir is the subdirectory under Storage.DataDir holding the
// node's HMAC-chained audit log, mirroring the "databases"/"barrier"
// subdirectory convention lifecycle.go/lifecycle_databases.go already use
// and go-xkms's own Auth.Audit.Path pattern (a path under the existing
// storage/data root, no new top-level config section).
const qrdbAuditLogSubdir = "audit"

// qrdbAuditLogFile is the chained audit log's filename inside
// qrdbAuditLogSubdir.
const qrdbAuditLogFile = "node-audit.log"

// newQRDBAuditSink builds the ONE audit plane every module and quicbaas's
// own audit emit sites (pkg/server/node_audit_logger.go's NodeAuditLogger)
// write onto (P5.2 of the login-plane plan, qrdbsdk.WithAuditLogger below):
// a durable, tamper-evident HMAC-chained file log under
// Storage.DataDir/audit/, mirroring go-xkms's own newQRDBAuditSink. An
// in-memory ring buffer is used ONLY as a defensive fallback when
// Storage.DataDir is empty or the file sink fails to open — logged loudly,
// never silent, since P5 explicitly forbids a lossy fallback becoming the
// deployed default. The returned sink is retained on s.qrdbAuditSink and
// closed during stopQRDBNode, after the node itself, so no in-flight emit
// races the close.
func (s *Server) newQRDBAuditSink() qrdbsdk.NodeAuditLogger {
	dataDir := s.config.Storage.DataDir
	if dataDir != "" {
		auditDir := filepath.Join(dataDir, qrdbAuditLogSubdir)
		if err := os.MkdirAll(auditDir, 0o750); err != nil {
			s.logger.Error("qrdb node audit sink: failed to create audit directory, falling back to in-memory ring buffer",
				"dir", auditDir, "error", err)
		} else {
			path := filepath.Join(auditDir, qrdbAuditLogFile)
			sink, err := qrdbsdk.NewAuditFileLogger(path)
			if err != nil {
				s.logger.Error("qrdb node audit sink: failed to open chained file logger, falling back to in-memory ring buffer",
					"path", path, "error", err)
			} else {
				s.logger.Info("qrdb node audit sink: HMAC-chained file logger", "path", path)
				return sink
			}
		}
	}
	s.logger.Warn("qrdb node audit sink: no durable storage root available, using in-memory ring buffer "+
		"(records do NOT survive a restart)",
		"capacity", defaultQRDBAuditRingCapacity)
	return qrdbsdk.NewAuditRingBuffer(defaultQRDBAuditRingCapacity)
}

func (s *Server) unifiedModuleServerOptions(serverOpts []qrdbsdk.ServerOption) ([]qrdbsdk.ServerOption, error) {
	// The ONE audit plane (P5.2): every capability module (quicbaas's own
	// and go-xkms's) and, over the site-by-site migration, quicbaas's own
	// IAM/OIDC/federation/orchestration/PIV-login/introspection auditing
	// emit qrdbsdk.NodeAuditEvent onto this SAME sink via the node's
	// Client().Audit(), instead of a parallel per-tenant KV logger or an
	// in-memory gRPC fallback.
	if s.qrdbAuditSink == nil {
		s.qrdbAuditSink = s.newQRDBAuditSink()
	}
	serverOpts = append(serverOpts, qrdbsdk.WithAuditLogger(s.qrdbAuditSink))

	module, err := s.newQuicBaaSModule()
	if err != nil {
		return nil, err
	}
	serverOpts = append(serverOpts, qrdbsdk.WithModule(module))

	// Compose go-xkms's own ServiceModule ("xkms") alongside quicbaas's on
	// the SAME unified server (Phase 3 of the login-plane plan): the
	// resolver reaches the process-global XKMS service quicbaasd already
	// initializes via pkg/xkms.Initialize (lifecycle.go's initXKMS,
	// dbxkms.Initialize -> pkgxkms.Get's backing store) and
	// pkg/xkms.RegisterBackend (registerXKMSBackendFn) — the SAME service
	// instance quicbaas's own KMS/Crypto/Certificate/Trust/Attestation/PIV
	// facades already delegate to (pkg/service/kms.go, crypto.go, ca.go,
	// certificate_service.go, trust_service.go, attestation_service.go,
	// piv_service.go). A nil/error return during composition answers the
	// module's RPCs Unavailable rather than panicking (xkms's own
	// apimodule.Module.keystore does the same nil-check).
	// The composed go-xkms module's own WebAuthn registration ceremony must
	// enroll the SAME qrdb fido2 login factor quicbaas's own surface enrolls,
	// or a passkey registered through it (what `xkey auth register` drives)
	// can never complete a login — go-quicbaas#484. Built here so a
	// misconfiguration is a startup failure rather than a ceremony that
	// rejects every registrant.
	webauthnCredentialStoreDecorator, err := s.xkmsModuleWebAuthnCredentialStoreDecorator()
	if err != nil {
		return nil, &QRDBNodeError{Stage: "build xkms module webauthn credential store decorator", Cause: err}
	}

	// The self-service flag below opens public WebAuthn registration on the
	// xkms module; say so once, at the point the flag takes effect.
	s.warnSelfServiceRegistrationPosture()

	xkmsModule, err := xkmssdk.NewServiceModule(
		func() *xkmslib.XKMSService {
			svc, getErr := xkmssdk.Get()
			if getErr != nil {
				return nil
			}
			return svc
		},
		// go-xkms v0.7.1-alpha: without these Host self-build options the
		// module registered zero HTTP routes (RegisterHTTP returned
		// immediately for lack of a REST resolver -- there is no SDK
		// re-export for WithServiceModuleRESTServer, and none is needed).
		// The module now self-builds a non-listening REST surface
		// (pkg/api/rest NewHostServer) around the resolved XKMSService when
		// these options are supplied instead.
		xkmssdk.WithServiceModuleQRDBClient(func() *qrdbsdk.Client { return s.GetQRDBNodeClient() }),
		xkmssdk.WithServiceModuleWebAuthn(quicbaasWebAuthnConfigToXKMS(
			s.config.IAM.WebAuthn, s.config.IAM.SelfServiceRegistration.Enabled)),
		xkmssdk.WithServiceModuleWebAuthnCredentialStoreDecorator(webauthnCredentialStoreDecorator),
		xkmssdk.WithServiceModuleSession(&xkmssdk.ServiceModuleSessionOptions{}),
		// The xkms module serves its OWN PIV login at /api/v1/auth/login/piv.
		// Since go-xkms v0.49.0-alpha it checks the slot attestation itself,
		// so it has to be told that the operator accepted unattested login.
		// It stays mounted ONLY under allow_unattested, so a deployment that
		// requires hardware has exactly one PIV path, the quicbaas handler at
		// /api/v1/iam/login/piv. See pivLoginModuleOptions.
		xkmssdk.WithServiceModulePIVLogin(s.pivLoginModuleOptions()),
	)
	if err != nil {
		return nil, &QRDBNodeError{Stage: "new xkms module", Cause: err}
	}

	// Route precedence: the composer decides who owns a path when both
	// modules carry it (see apimodule.XKMSRouteExclusions). QuicBaaS owns
	// GET /api/v1/cluster/status, which go-xkms registers too: the quicbaas
	// handler reports every shard the composed node hosts, in the shape
	// quicbaas's SDK clients and UI decode; and the /api/v1/tenants/ control
	// plane. go-xkms no longer registers /api/v1/barrier/* in a host
	// composition, so quicbaas's barrier surface (go-quicbaas#405) needs no
	// exclusion.
	hostedXKMSModule, err := apimodule.NewRouteExcludingModule(xkmsModule, apimodule.XKMSRouteExclusions)
	if err != nil {
		return nil, &QRDBNodeError{Stage: "compose xkms module route precedence", Cause: err}
	}
	serverOpts = append(serverOpts, qrdbsdk.WithModule(hostedXKMSModule))

	// Host-supplied capability modules (WithModule, module_host.go) mount
	// LAST, on the SAME node, so a downstream host such as go-trusted-platform
	// serves its own plane over the node's transports under the node's single
	// authorizer and audit sink instead of standing up a parallel stack. Their
	// names were validated at NewServer; their REST routes are checked against
	// quicbaas's and xkms's here, where all three module instances first exist
	// together, so a collision fails node construction with a typed error
	// naming both owners rather than panicking inside go-qrdb's route
	// registry during REST server construction. Only conflicts a host module
	// is party to are reported — overlap between quicbaas's own module and
	// xkms's predates this seam and stays go-qrdb's to enforce. The check is
	// skipped entirely when no host module is registered, leaving the default
	// composition byte-for-byte as it was.
	if len(s.opts.hostModules) > 0 {
		mounted := []qrdbsdk.ServiceModule{module, xkmsModule}
		if err := assertNoHostModuleRouteConflicts(mounted, s.opts.hostModules); err != nil {
			return nil, &QRDBNodeError{Stage: "mount host modules", Cause: err}
		}
		for _, hostModule := range s.opts.hostModules {
			s.logger.Info("qrdb node: mounting host capability module", "module", hostModule.Name())
			serverOpts = append(serverOpts, qrdbsdk.WithModule(hostModule))
		}
	}

	// Unified API server TLS: qrdb's transports are TLS-only and take their
	// material from the WithServerTLS option, not from the daemon config.
	if s.config.NetworkServing() {
		serverOpts = append(serverOpts, qrdbsdk.WithServerTLS(s.unifiedServerTLSConfig()))
	}
	return serverOpts, nil
}

// newQuicBaaSModule builds the quicbaas ServiceModule the unified server
// mounts: every quicbaas gRPC service the node's TCP port serves, plus the
// module's HTTP and MCP surfaces.
func (s *Server) newQuicBaaSModule() (*apimodule.Module, error) {
	// Build the same multi-shard router and vector shard ID
	// startGRPCServer's legacy gRPC transport applies to its own facade
	// instance (buildModuleShardRouter, below) so the module's shared
	// *grpcapi.Server facade — backing QuicBaaS, FIDO2Service, and, when
	// enabled, Embedding — routes identically once the unified server's
	// transports are enabled. s.config is already fully loaded at this point
	// (config load precedes qrdb node construction), so unlike the QuicBaaS
	// facade and REST/MCP servers below, the router needs no late-binding
	// resolver.
	shardRouter, vectorShardID, err := s.buildModuleShardRouter()
	if err != nil {
		return nil, err
	}

	// The REST and MCP resolvers late-bind the module's HTTP surface
	// (pkg/apimodule module_http.go, rest builds) and real MCP tools
	// (module_mcp.go, mcp builds) to servers built from the SAME
	// configuration path the legacy listeners use (buildRESTServer /
	// buildMCPServer). Both return nil while the facade composes; module
	// routes answer 503 and module tools a tool error until then. On
	// tag-less builds the accessors and the hooks are matching no-ops.
	module, err := apimodule.NewModule(
		func() service.QuicBaaS { return s.quicBaaSServiceRef() },
		s.logger,
		apimodule.WithNodeInfo(s.config.Raft.NodeID, s.config.Server.Host),
		apimodule.WithRESTServer(func() *restapi.Server { return s.ModuleRESTServer() }),
		apimodule.WithMCPServer(func() *mcpapi.Server { return s.ModuleMCPServer() }),
		apimodule.WithShardRouting(shardRouter, vectorShardID),
		apimodule.WithSPIFFETrustDomain(s.config.IAM.SPIFFETrustDomain),
		apimodule.WithHostGRPCServers(s.hostGRPCServers),
	)
	if err != nil {
		return nil, &QRDBNodeError{Stage: "new quicbaas module", Cause: err}
	}
	return module, nil
}

// quicbaasWebAuthnConfigToXKMS projects quicbaas's iam.webauthn config
// section (config.IAMWebAuthnConfig) onto go-xkms's operator-facing
// WebAuthnConfig shape (xkmssdk.ServiceModuleWebAuthnConfig), so ONE config
// block drives both quicbaas's own RP layer and the composed xkms module's
// self-built WebAuthn surface. Enabled tracks whether the quicbaas section
// is actually configured (RPID set — there is no separate disable flag on
// that type), so an unconfigured host leaves the xkms module's WebAuthn
// routes unmounted exactly as WithServiceModuleWebAuthn's doc comment
// describes. Note this reads the OPERATOR's rp_id, not the resolved one:
// quicbaas's own relying party derives an RP ID from the issuer URL when the
// operator supplies none (see webauthnFactoryConfigFromOperator), but the
// composed xkms module's separate surface is mounted only on an explicit
// opt-in.
func quicbaasWebAuthnConfigToXKMS(c config.IAMWebAuthnConfig, allowPublicRegistration bool) *xkmssdk.ServiceModuleWebAuthnConfig {
	return &xkmssdk.ServiceModuleWebAuthnConfig{
		Enabled: c.RPID != "",
		// ONE decision drives both relying parties. Without this the module's
		// ceremony routes would refuse an unknown email on a node whose
		// operator has switched self-service registration on, while
		// quicbaas's own surface accepted it — the two registration lanes
		// disagreeing again, which is the class of defect this PR closes.
		AllowPublicRegistration: &allowPublicRegistration,
		RPID:                    c.RPID,
		RPOrigins:               c.RPOrigins,
		RPDisplayName:           c.RPDisplayName,
		AttestationPreference:   c.AttestationPreference,
		AuthenticatorAttachment: c.AuthenticatorAttachment,
		ResidentKey:             c.ResidentKeyRequirement,
		UserVerification:        c.UserVerification,
	}
}

// initStandaloneQRDBNode constructs the real qrdb standalone node for
// quicbaas standalone mode. It:
//
//  1. Builds a *qrdbsdk.DaemonConfig from the quicbaas config.
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

	qcfg, qcfgErr := buildQRDBConfig(s.config, false)
	if qcfgErr != nil {
		return &QRDBNodeError{Stage: "build qrdb config", Cause: qcfgErr}
	}

	// Build the hardware seal strategy ONCE up front (single TPM open) so its
	// open TPM2 backend can be shared with both the embedded node's barrier
	// (WithSealStrategy below) and the PIN backend (inside buildXKMSCryptoOptions).
	if err := s.buildSealStrategyOnce(qcfg.Seal.Enabled); err != nil {
		return err
	}

	// First-boot TPM PIN enrollment: when the PIN backend is tpm2 and a TPM2
	// seal is active, bind the operator passphrase as the PlatformSRK auth over
	// the SAME shared TPM open so the TPM PIN path works end-to-end. No-op for
	// software PIN, idempotent across boots, fail-closed on a genuine TPM error.
	if err := enrollTPMPlatformSRKFirstBoot(s.config, s.sealTPM2Backend, s.opts.sealPassphrase, slog.Default()); err != nil {
		return &QRDBNodeError{Stage: "enroll tpm platform SRK", Cause: err}
	}

	serverOpts, pinBackend, err := buildXKMSCryptoOptions(s.config, slog.Default(), s.sealTPM2Backend, s.ceremonyCA)
	if err != nil {
		return &QRDBNodeError{Stage: "build xkms crypto options", Cause: err}
	}
	// Keep the PIN backend reachable so the init ceremony can enroll the
	// operator's SO / user PINs into the SAME backend the node verifies against.
	s.pinBackend = pinBackend

	// Register the quicbaas ServiceModule (and unified TLS when enabled).
	serverOpts, err = s.unifiedModuleServerOptions(serverOpts)
	if err != nil {
		return err
	}

	// Node options: we pass slog.Default() as the logger; in a well-configured
	// quicbaasd process the default slog handler is set to the same output as
	// the quicbaas logger so log lines route correctly.
	nodeOpts := []qrdbnode.NodeOption{
		qrdbnode.WithLogger(slog.Default()),
		// Enable per-tenant volume/raft isolation so the node accepts tenant
		// barriers under dedicated DataRoots. Required for EnableEncryption
		// targeting tenant scopes and for IsolationVolume / IsolationRaft tenants.
		qrdbnode.WithTenantIsolation(),
		// Guarantee the node's RBAC authorizer is always wrapped in a
		// reloadable ABAC decorator (Node.ABACPolicyReloader), even before
		// any policy exists: pkg/iam's policy CRUD (iam.ABACSync, driven by
		// qrdbABACSync) is the downstream policy store that calls Reload
		// after boot, including to add the very first policy. Without this
		// option a node built with zero initial ABAC policies would run RBAC
		// undecorated and ABACPolicyReloader would return nil.
		qrdbnode.WithABACLiveReload(),
		// Record the node's composed authorizer (returned unchanged) for the
		// REST barrier scope authorizer (go-quicbaas#990).
		s.nodeAuthorizerCapture(),
		qrdbnode.WithAuthenticators(newPlatformBearerAuthenticator(s)),
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
	// startup. Without this, a passphrase-strategy node boots SEALED
	// (node.Client() returns nil): a daemon boot then enters the
	// sealed-window phase (initialize() serves only the canonical
	// barrier/ceremony surface until an operator unseals — see
	// sealed_window.go), while an embedded boot is refused with the typed
	// EmbeddedSealedBootError.
	if len(s.opts.sealPassphrase) > 0 && qcfg.Seal.Enabled {
		nodeOpts = append(nodeOpts, qrdbnode.WithSealPassphrase(s.opts.sealPassphrase))
		// Retain a copy for the first-boot bootstrap orchestrator
		// (qrdbsdk.BootstrapSystem): the node builder consumes and zeroizes
		// opts.sealPassphrase below, but first-boot — which runs later in
		// Start — must still be able to initialize a sealed
		// passphrase-strategy system barrier. Zeroized by firstBootBootstrap
		// as soon as first-boot completes.
		s.bootstrapSealPassphrase = append([]byte(nil), s.opts.sealPassphrase...)
	}

	// When a hardware/cloud seal strategy is configured (awskms, vault, gcpkms,
	// azurekv, tpm2, pkcs11), build it and inject it into the node builder via
	// WithSealStrategy so the barrier is sealed with the cloud strategy and can
	// auto-unseal on startup. Without this the node falls back to the deferred-
	// software (passphrase) strategy and auto-unseal fails with empty credentials.
	if qcfg.Seal.Enabled && s.sealStrategy != nil {
		nodeOpts = append(nodeOpts, qrdbnode.WithSealStrategy(s.sealStrategy))
		s.logger.Info("Standalone node: injecting hardware/cloud seal strategy",
			"strategy", s.sealStrategy.ID())
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

	s.setQRDBNode(node)

	s.logger.Info("Standalone qrdb node started; at-rest encryption and hardware crypto active")
	return nil
}

// stopQRDBNode closes the qrdb node (standalone or clustered), clears the
// reference, and releases the node-scoped resources this Server built for
// it: the audit sink and, when no node was ever constructed, the seal
// strategy. It is idempotent, and it does real work even when the node is
// nil -- both of those resources are created BEFORE the node, so a Start
// that failed during node construction can only be cleaned up by a call
// that does not require a node. The audit sink is closed AFTER the node so
// no in-flight emit from a still-running node races the close.
func (s *Server) stopQRDBNode() error {
	var closeErr error

	node := s.getQRDBNode()
	if node != nil {
		s.setQRDBNode(nil)
		s.logger.Info("Shutting down embedded qrdb node...")
		closeErr = node.Close()
	}

	// The audit sink and the seal strategy are built BEFORE the node they
	// belong to -- newQRDBAuditSink from unifiedModuleServerOptions and
	// buildSealStrategyOnce from the node builder -- so a node construction
	// that fails leaves them held by this Server with no node to release
	// them. Releasing them only when a node exists is exactly the shape
	// that leaks an open audit file, and an open TPM session, out of a
	// failed Start: see the Start/Shutdown contract on Server.Start.
	//
	// Closing the sink AFTER the node preserves the original ordering
	// guarantee: no in-flight emit races the close.
	if s.qrdbAuditSink != nil {
		sink := s.qrdbAuditSink
		s.qrdbAuditSink = nil
		if err := sink.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}

	// A built node owns the seal strategy and closes it itself, so this
	// releases the strategy only when no node was ever built. Close is
	// required to be idempotent by the SealingStrategy contract, but
	// staying off the node-owned path leaves a successful boot's teardown
	// byte-for-byte as it was.
	if node == nil && s.sealStrategy != nil {
		strategy := s.sealStrategy
		s.sealStrategy = nil
		// Same instance as inside the strategy -- never closed separately.
		s.sealTPM2Backend = nil
		if err := strategy.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}

	if closeErr != nil {
		return &QRDBNodeError{Stage: "close", Cause: closeErr}
	}
	if node != nil {
		s.logger.Info("Embedded qrdb node shutdown complete")
	}
	return nil
}

// initClusteredQRDBNode constructs the real qrdb clustered node for
// quicbaas cluster mode. It:
//
//  1. Builds a *qrdbsdk.DaemonConfig from the quicbaas config with Raft and
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

	qcfg, qcfgErr := buildQRDBConfig(s.config, true)
	if qcfgErr != nil {
		return &QRDBNodeError{Stage: "build qrdb config", Cause: qcfgErr}
	}

	members, err := buildClusterMembers(s.config)
	if err != nil {
		return &QRDBNodeError{Stage: "build cluster members", Cause: err}
	}

	// Build the hardware seal strategy ONCE up front (single TPM open) so its
	// open TPM2 backend can be shared with both the clustered node's barrier
	// (WithSealStrategy below) and the PIN backend (inside buildXKMSCryptoOptions).
	if err := s.buildSealStrategyOnce(qcfg.Seal.Enabled); err != nil {
		return err
	}

	// First-boot TPM PIN enrollment: when the PIN backend is tpm2 and a TPM2
	// seal is active, bind the operator passphrase as the PlatformSRK auth over
	// the SAME shared TPM open so the TPM PIN path works end-to-end. No-op for
	// software PIN, idempotent across boots, fail-closed on a genuine TPM error.
	if err := enrollTPMPlatformSRKFirstBoot(s.config, s.sealTPM2Backend, s.opts.sealPassphrase, slog.Default()); err != nil {
		return &QRDBNodeError{Stage: "enroll tpm platform SRK", Cause: err}
	}

	serverOpts, pinBackend, err := buildXKMSCryptoOptions(s.config, slog.Default(), s.sealTPM2Backend, s.ceremonyCA)
	if err != nil {
		return &QRDBNodeError{Stage: "build xkms crypto options", Cause: err}
	}
	// Keep the PIN backend reachable so the init ceremony can enroll the
	// operator's SO / user PINs into the SAME backend the node verifies against.
	s.pinBackend = pinBackend

	// Register the quicbaas ServiceModule (and unified TLS when enabled).
	serverOpts, err = s.unifiedModuleServerOptions(serverOpts)
	if err != nil {
		return err
	}

	nodeOpts := []qrdbnode.NodeOption{
		qrdbnode.WithLogger(slog.Default()),
		// Enable per-tenant volume/raft isolation (see initStandaloneQRDBNode
		// for rationale).
		qrdbnode.WithTenantIsolation(),
		// Reloadable ABAC decorator even with zero initial policies (see
		// initStandaloneQRDBNode for rationale).
		qrdbnode.WithABACLiveReload(),
		// Record the node's composed authorizer (returned unchanged) for the
		// REST barrier scope authorizer (go-quicbaas#990).
		s.nodeAuthorizerCapture(),
		qrdbnode.WithAuthenticators(newPlatformBearerAuthenticator(s)),
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
	// startup. Without this, a passphrase-strategy node boots SEALED and the
	// daemon serves the sealed-window phase until an operator unseals (see
	// sealed_window.go); embedded boots are refused with the typed
	// EmbeddedSealedBootError.
	if len(s.opts.sealPassphrase) > 0 && qcfg.Seal.Enabled {
		nodeOpts = append(nodeOpts, qrdbnode.WithSealPassphrase(s.opts.sealPassphrase))
		// Retain a copy for the first-boot bootstrap orchestrator
		// (qrdbsdk.BootstrapSystem): the node builder consumes and zeroizes
		// opts.sealPassphrase below, but first-boot — which runs later in
		// Start — must still be able to initialize a sealed
		// passphrase-strategy system barrier. Zeroized by firstBootBootstrap
		// as soon as first-boot completes.
		s.bootstrapSealPassphrase = append([]byte(nil), s.opts.sealPassphrase...)
	}

	// When a hardware/cloud seal strategy is configured, build it and inject
	// it into the node builder so the barrier is sealed with the cloud strategy
	// and auto-unseals on cluster boot.
	if qcfg.Seal.Enabled && s.sealStrategy != nil {
		nodeOpts = append(nodeOpts, qrdbnode.WithSealStrategy(s.sealStrategy))
		s.logger.Info("Clustered node: injecting hardware/cloud seal strategy",
			"strategy", s.sealStrategy.ID())
	}

	// Inject mTLS only for multi-node clusters. Single-node uses
	// WithoutTransport (no QUIC transport, no cert material needed).
	// provisionClusterMTLS implements a three-way precedence (raft build):
	//   1. Operator-supplied explicit PEM files
	//   2. Hardware-resident node key (RaftEnrollment.KeyBackend set)
	//   3. Software auto-enrollment via cluster-TLS enrollment
	// Without -tags raft, only explicit PEM files are supported.
	if len(members) > 1 {
		mtls, adv, mtlsErr := provisionClusterMTLS(s, members)
		if mtlsErr != nil {
			return &QRDBNodeError{Stage: "provision raft mtls", Cause: mtlsErr}
		}
		if adv != nil {
			s.raftClusterAdvertiser = adv
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

	s.setQRDBNode(node)

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
		return qrdbsdk.ParsePeers(peerStrings)
	}

	// No id-prefixed peers: single-node clustered mode advertises the
	// configured raft.raft_address. The validator refuses an empty one in
	// cluster mode, so reaching here without it is a configuration defect,
	// not a reason to invent an address.
	raftAddr := cfg.Raft.RaftAddress
	if raftAddr == "" {
		return nil, &QRDBNodeError{Stage: "cluster members", Cause: &config.ValidationError{
			Field:   "raft.raft_address",
			Message: "Raft address cannot be empty in cluster mode",
		}}
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
func buildRaftMTLSConfig(cfg *config.Config) (*qrdbnode.RaftMTLSConfig, error) {
	certFile := cfg.Raft.TLSCertFile
	keyFile := cfg.Raft.TLSKeyFile
	caFile := cfg.Raft.TLSCAFile

	if certFile == "" && keyFile == "" && caFile == "" {
		return nil, nil
	}

	mtls := &qrdbnode.RaftMTLSConfig{}

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

// schedulerClustering assembles the clustered scheduler's wiring, or nil on a
// standalone boot.
//
// The topology signal is the EXISTING one — the same getEffectiveMode() ==
// ServerModeCluster that already decides NewClusteredNode vs
// NewStandaloneNode — so there is no second, drifting notion of "are we
// clustered".
//
// Both handles come off the clustered node's SDK client, which is the only
// place they exist: go-qrdb gates the transaction plane (and MVCC, lease and
// watch) behind clustered mode under its ratified run-modes split, and
// leadership is meaningless without a Raft group. That the claim arbiter and
// the leader checker are available exactly when they are needed is a property
// of qrdb's design, not a coincidence this code arranges.
//
// Returning a partially-filled value rather than nil on a clustered boot is
// deliberate: the service builder turns that into a construction error, so a
// clustered node missing part of its wiring refuses to start instead of
// falling back to the single-node scheduler on every node.
func (s *Server) schedulerClustering() *service.SchedulerClustering {
	// One predicate decides what "multi-node" means, shared with config
	// validation, so a mode added later cannot be clustered for one and
	// standalone for the other.
	if !config.IsClusterLikeMode(s.getEffectiveMode()) {
		return nil
	}

	clustering := &service.SchedulerClustering{
		NodeID: strconv.FormatUint(s.config.Raft.NodeID, 10),
	}

	if s.qrdbClient == nil {
		return clustering
	}
	client, err := s.qrdbClient.ForShard(qbshards.PlatformShardID)
	if err != nil || client == nil {
		s.logger.WithError(err).Warn("scheduler: no shard client for clustered scheduler wiring")
		return clustering
	}

	clustering.Leader = service.NewQRDBLeaderChecker(
		client, qbshards.PlatformShardID, s.config.Raft.NodeID)
	clustering.Claims = client.Transaction()
	clustering.Marks = client.KV()
	return clustering
}

// standaloneRegistryNotice returns the one startup line a standalone boot
// logs about the service registry, or "" on a cluster-like boot.
//
// The registry is built on the lease, MVCC and watch planes, which go-qrdb
// gates behind clustered mode. A standalone boot does not refuse to start
// over it, by the same rule schedulerClustering, taskClaimArbiter and
// sdlcTransactor follow: single-node modes run without the raft planes rather
// than fail. The registry stays wired on every transport and refuses each
// call with registry.RegistryRequiresRaftError; this line says so up front.
func (s *Server) standaloneRegistryNotice() string {
	if config.IsClusterLikeMode(s.getEffectiveMode()) {
		return ""
	}
	return "Service registry unavailable on a standalone qrdb node: " +
		"it needs the raft lease and watch planes, so registry calls return RegistryRequiresRaftError"
}

// pivLoginModuleOptions builds the options of the xkms module's own PIV login.
//
// The module checks the slot attestation itself (go-xkms v0.49.0-alpha), so
// AllowUnattested carries the operator's choice through; without it the module
// refuses every software certificate the operator allowed (#1592).
func (s *Server) pivLoginModuleOptions() *xkmssdk.ServiceModulePIVLoginOptions {
	return &xkmssdk.ServiceModulePIVLoginOptions{
		Enabled:         s.pivLoginModuleEnabled(),
		TrustedIssuers:  s.config.IAM.PIVLogin.TrustedIssuers,
		AllowUnattested: s.config.IAM.PIVLogin.AllowUnattested,
	}
}

// pivLoginModuleEnabled reports whether the xkms module's own PIV login surface
// (/api/v1/auth/login/piv) should be mounted.
//
// It requires BOTH that PIV login is enabled and that the operator explicitly
// accepted unattested PIV login. The module checks attestation, but it is
// given no attestation roots, so on a deployment that requires hardware it
// would only be a second door to the same credential — and an attacker
// chooses the door.
//
// A deployment that wants attested PIV login gets exactly one PIV path, the
// hardened handler at /api/v1/iam/login/piv. One that has set
// allow_unattested has said, in as many words, that it does not require the
// proof; mounting the module surface there changes nothing about its posture.
func (s *Server) pivLoginModuleEnabled() bool {
	cfg := s.config.IAM.PIVLogin
	if !cfg.Enabled {
		return false
	}
	if !cfg.AllowUnattested {
		s.logger.Warn("iam.piv_login: the xkms module PIV login surface " +
			"(/api/v1/auth/login/piv) is NOT mounted, so that a deployment requiring " +
			"hardware has one PIV path: the attested handler at " +
			"/api/v1/iam/login/piv. Set iam.piv_login.allow_unattested: true to mount it.")
		return false
	}
	return true
}
