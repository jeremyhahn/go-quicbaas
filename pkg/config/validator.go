// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	xkmsresolver "github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/trust/resolver"
)

// validStorageBackends contains the set of valid storage backend types.
// NOTE: "raft" is NOT a valid backend. Use server.mode=cluster for Raft consensus.
var validStorageBackends = map[string]bool{
	StorageBackendMemory: true,
	StorageBackendFile:   true,
	StorageBackendPebble: true,
}

// validServerModes contains the set of valid server mode types.
var validServerModes = map[string]bool{
	ServerModeStandalone: true,
	ServerModeCluster:    true,
	ServerModeDevCluster: true,
	"":                   true, // Empty defaults to standalone
}

// ValidationError represents a configuration validation error.
type ValidationError struct {
	Field   string
	Message string

	// Cause is the typed error this validation failure came from, when there
	// is one. It lets a caller tell failures apart with errors.As instead of
	// matching on Message.
	Cause error
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error for field '%s': %s", e.Field, e.Message)
}

// Unwrap exposes Cause to errors.Is and errors.As.
func (e *ValidationError) Unwrap() error {
	return e.Cause
}

// ValidationErrors represents multiple validation errors.
type ValidationErrors []ValidationError

// Error implements the error interface.
func (e ValidationErrors) Error() string {
	if len(e) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("configuration validation failed with %d error(s):\n", len(e)))
	for i, err := range e {
		sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, err.Error()))
	}
	return sb.String()
}

// Unwrap exposes each aggregated failure to errors.Is and errors.As, so a
// typed cause recorded by a section validator (for example
// *fips.AEADNotApprovedError) stays reachable from the top-level Validate
// result instead of surviving only as message text.
func (e ValidationErrors) Unwrap() []error {
	if len(e) == 0 {
		return nil
	}
	errs := make([]error, len(e))
	for i := range e {
		errs[i] = &e[i]
	}
	return errs
}

// Validator validates configuration.
type Validator struct {
	errors ValidationErrors
}

// NewValidator creates a new configuration validator.
func NewValidator() *Validator {
	return &Validator{
		errors: make(ValidationErrors, 0),
	}
}

// Errors returns the accumulated validation errors, or nil when none were
// recorded. It lets callers run an individual section validator (e.g.
// validateAuth) and inspect the result without driving the full Validate pass.
func (v *Validator) Errors() error {
	if len(v.errors) > 0 {
		return v.errors
	}
	return nil
}

// Validate validates the entire configuration.
func (v *Validator) Validate(cfg *Config) error {
	if cfg == nil {
		return api.NewNilConfigError("config.Validator.Validate")
	}

	// Validate each section
	v.validateServer(&cfg.Server)
	v.validateStorage(&cfg.Storage)
	v.validateRaft(&cfg.Raft, &cfg.Server)
	v.validateSecurity(&cfg.Security)
	v.validateAuthzPosture(&cfg.Security, &cfg.Server)
	v.validateNetworkServingTLS(&cfg.Server, &cfg.Security)
	v.validateAuth(&cfg.Auth)
	v.validateLogging(&cfg.Logging)
	v.validateKMS(&cfg.KMS)
	v.validateCrypto(&cfg.Crypto)
	v.validateOrchestration(&cfg.Orchestration)
	v.validateSDLC(&cfg.SDLC)
	v.validateTrust(cfg.Trust)
	v.validateIAM(&cfg.IAM)

	// Return errors if any
	if len(v.errors) > 0 {
		return v.errors
	}

	return nil
}

// validateServer validates server configuration.
func (v *Validator) validateServer(cfg *ServerConfig) {
	// Validate mode
	if !validServerModes[cfg.Mode] {
		v.addError("server.mode", fmt.Sprintf("invalid server mode: %s (valid: standalone, cluster, devcluster)", cfg.Mode))
	}

	if cfg.Host == "" {
		v.addError("server.host", "host cannot be empty")
	}

	// Validate REST configuration
	if cfg.REST.Enabled {
		if cfg.REST.Port < 1 || cfg.REST.Port > 65535 {
			v.addError("server.rest.port", fmt.Sprintf("REST port must be between 1 and 65535, got %d", cfg.REST.Port))
		}
	}

	// Validate gRPC configuration
	if cfg.GRPC.Enabled {
		if cfg.GRPC.Port < 1 || cfg.GRPC.Port > 65535 {
			v.addError("server.grpc.port", fmt.Sprintf("gRPC port must be between 1 and 65535, got %d", cfg.GRPC.Port))
		}
	}

	// Validate MCP configuration
	if cfg.MCP.Enabled {
		if cfg.MCP.Port < 1 || cfg.MCP.Port > 65535 {
			v.addError("server.mcp.port", fmt.Sprintf("MCP port must be between 1 and 65535, got %d", cfg.MCP.Port))
		}
	}

	// Validate port conflicts when both REST and gRPC are enabled
	if cfg.REST.Enabled && cfg.GRPC.Enabled && cfg.REST.Port == cfg.GRPC.Port {
		v.addError("server.grpc.port", "gRPC port must be different from REST port")
	}

	if cfg.ReadTimeout <= 0 {
		v.addError("server.read_timeout", "read timeout must be positive")
	}

	if cfg.WriteTimeout <= 0 {
		v.addError("server.write_timeout", "write timeout must be positive")
	}

	if cfg.IdleTimeout <= 0 {
		v.addError("server.idle_timeout", "idle timeout must be positive")
	}

	if cfg.ShutdownTimeout <= 0 {
		v.addError("server.shutdown_timeout", "shutdown timeout must be positive")
	}

	if cfg.MaxConnections < 1 {
		v.addError("server.max_connections", "max connections must be at least 1")
	}
}

// validateAuth validates authentication configuration. OIDC token TTLs are
// additive and backward-compatible: zero means "use the default" and is not an
// error. When an explicit access-token TTL is set it must stay within the
// short-lived [DefaultOIDCAccessTokenTTLMin, DefaultOIDCAccessTokenTTLMax]
// replay window. Refresh/ID/auth-code TTLs, when set, must be positive.
func (v *Validator) validateAuth(cfg *AuthConfig) {
	if cfg.AccessTokenTTL != 0 {
		if cfg.AccessTokenTTL < DefaultOIDCAccessTokenTTLMin || cfg.AccessTokenTTL > DefaultOIDCAccessTokenTTLMax {
			v.addError("auth.access_token_ttl", fmt.Sprintf(
				"access token TTL must be between %s and %s, got %s",
				DefaultOIDCAccessTokenTTLMin, DefaultOIDCAccessTokenTTLMax, cfg.AccessTokenTTL))
		}
	}
	if cfg.RefreshTokenTTL < 0 {
		v.addError("auth.refresh_token_ttl", "refresh token TTL must not be negative")
	}
	if cfg.IDTokenTTL < 0 {
		v.addError("auth.id_token_ttl", "ID token TTL must not be negative")
	}
	if cfg.AuthCodeTTL < 0 {
		v.addError("auth.auth_code_ttl", "auth code TTL must not be negative")
	}
}

// validateRaft validates Raft configuration.
// It takes server config to conditionally validate based on server mode.
func (v *Validator) validateRaft(cfg *RaftConfig, serverCfg *ServerConfig) {
	mode := serverCfg.Mode
	if mode == "" {
		mode = ServerModeStandalone
	}

	// When server mode is cluster or devcluster, Raft configuration is required
	if IsClusterLikeMode(mode) {
		if cfg.NodeID == 0 {
			v.addError("raft.node_id", "node ID must be greater than 0 when server.mode is 'cluster' or 'devcluster'")
		}
		if cfg.RaftAddress == "" {
			v.addError("raft.raft_address", "Raft address cannot be empty when server.mode is 'cluster' or 'devcluster'")
		}
	}

	// Skip remaining validation if standalone mode and no Raft values are set
	if mode == ServerModeStandalone && cfg.NodeID == 0 {
		return
	}

	// Full raft validation when raft is being used
	if cfg.NodeID == 0 {
		v.addError("raft.node_id", "node ID must be greater than 0")
	}

	if cfg.ElectionRTT < 1 {
		v.addError("raft.election_rtt", "election RTT must be at least 1")
	}

	if cfg.HeartbeatRTT < 1 {
		v.addError("raft.heartbeat_rtt", "heartbeat RTT must be at least 1")
	}

	if cfg.ElectionRTT <= cfg.HeartbeatRTT {
		v.addError("raft.election_rtt", "election RTT must be greater than heartbeat RTT")
	}

	if cfg.SnapshotEntries < 1 {
		v.addError("raft.snapshot_entries", "snapshot entries must be at least 1")
	}

	if cfg.CompactionOverhead < 1 {
		v.addError("raft.compaction_overhead", "compaction overhead must be at least 1")
	}

	if cfg.RequestTimeout <= 0 {
		v.addError("raft.request_timeout", "request timeout must be positive")
	}

	if cfg.RTTMillisecond < 1 {
		v.addError("raft.rtt_millisecond", "RTT millisecond must be at least 1")
	}

	if cfg.RaftAddress == "" {
		v.addError("raft.raft_address", "Raft address cannot be empty")
	}

	if cfg.ListenAddress == "" {
		v.addError("raft.listen_address", "listen address cannot be empty")
	}

	if cfg.MaxInMemLogSize < 1024*1024 { // At least 1MB
		v.addError("raft.max_in_mem_log_size", "max in-memory log size must be at least 1MB")
	}

	if cfg.MaxSendQueueSize < 1 {
		v.addError("raft.max_send_queue_size", "max send queue size must be at least 1")
	}

	if cfg.MaxReceiveQueueSize < 1 {
		v.addError("raft.max_receive_queue_size", "max receive queue size must be at least 1")
	}
}

// validateStorage validates storage configuration.
func (v *Validator) validateStorage(cfg *StorageConfig) {
	// Validate storage backend
	switch {
	case cfg.Backend == "":
		v.addError("storage.backend", "storage backend cannot be empty")
	case cfg.Backend == "raft":
		// Special error message for legacy "raft" backend usage
		v.addError("storage.backend", "storage backend 'raft' is no longer valid; use server.mode='cluster' or 'devcluster' for Raft consensus with any storage backend (memory, file, pebble)")
	case !validStorageBackends[cfg.Backend]:
		v.addError("storage.backend", fmt.Sprintf("invalid storage backend: %s (valid: memory, file, pebble)", cfg.Backend))
	}

	// Skip directory validation for memory backend
	if cfg.Backend == StorageBackendMemory {
		return
	}

	if cfg.DataDir == "" {
		v.addError("storage.data_dir", "data directory cannot be empty")
	} else {
		// Check if data directory is writable (create if doesn't exist)
		if err := v.ensureDirectoryWritable(cfg.DataDir); err != nil {
			v.addError("storage.data_dir", fmt.Sprintf("data directory error: %v", err))
		}
	}

	if cfg.WALDir == "" {
		v.addError("storage.wal_dir", "WAL directory cannot be empty")
	} else {
		// Check if WAL directory is writable (create if doesn't exist)
		if err := v.ensureDirectoryWritable(cfg.WALDir); err != nil {
			v.addError("storage.wal_dir", fmt.Sprintf("WAL directory error: %v", err))
		}
	}

	if cfg.CacheSize < 1024*1024 { // At least 1MB
		v.addError("storage.cache_size", "cache size must be at least 1MB")
	}

	if cfg.MaxOpenFiles < 10 {
		v.addError("storage.max_open_files", "max open files must be at least 10")
	}

	if cfg.MaxConcurrentReads < 1 {
		v.addError("storage.max_concurrent_reads", "max concurrent reads must be at least 1")
	}

	if cfg.MaxConcurrentWrites < 1 {
		v.addError("storage.max_concurrent_writes", "max concurrent writes must be at least 1")
	}

	if cfg.BytesPerSync < 0 {
		v.addError("storage.bytes_per_sync", "bytes per sync cannot be negative")
	}

	if cfg.MemTableSize < 1024*1024 { // At least 1MB
		v.addError("storage.mem_table_size", "MemTable size must be at least 1MB")
	}

	if cfg.MemTableStopWrites < 1 {
		v.addError("storage.mem_table_stop_writes", "MemTable stop writes must be at least 1")
	}

	if cfg.L0CompactionThreshold < 1 {
		v.addError("storage.l0_compaction_threshold", "L0 compaction threshold must be at least 1")
	}

	if cfg.L0StopWritesThreshold < 1 {
		v.addError("storage.l0_stop_writes_threshold", "L0 stop writes threshold must be at least 1")
	}

	if cfg.L0StopWritesThreshold <= cfg.L0CompactionThreshold {
		v.addError("storage.l0_stop_writes_threshold", "L0 stop writes threshold must be greater than L0 compaction threshold")
	}

	if cfg.BlockSize < 1024 { // At least 1KB
		v.addError("storage.block_size", "block size must be at least 1KB")
	}

	if cfg.TargetFileSize < 1024*1024 { // At least 1MB
		v.addError("storage.target_file_size", "target file size must be at least 1MB")
	}

	validCompressionTypes := map[string]bool{
		"none":   true,
		"snappy": true,
		"zstd":   true,
	}
	if !validCompressionTypes[cfg.CompressionType] {
		v.addError("storage.compression_type", fmt.Sprintf("invalid compression type: %s (valid: none, snappy, zstd)", cfg.CompressionType))
	}
}

// validateSecurity validates security configuration.
func (v *Validator) validateSecurity(cfg *SecurityConfig) {
	// TLS validation. Empty cert/key paths are valid: the server lifecycle
	// auto-provisions a per-node xkms CA and issues the server certificate at
	// boot when no operator-provided material is configured (see
	// pkg/server/lifecycle.go). Operator-provided paths must exist and must be
	// supplied as a complete cert+key pair.
	if cfg.TLSEnabled {
		if (cfg.TLSCertFile == "") != (cfg.TLSKeyFile == "") {
			v.addError("security.tls_cert_file", "TLS cert file and key file must be configured together (leave both empty to auto-provision via the built-in CA)")
		}

		if cfg.TLSCertFile != "" && !v.fileExists(cfg.TLSCertFile) {
			v.addError("security.tls_cert_file", fmt.Sprintf("TLS cert file does not exist: %s", cfg.TLSCertFile))
		}

		if cfg.TLSKeyFile != "" && !v.fileExists(cfg.TLSKeyFile) {
			v.addError("security.tls_key_file", fmt.Sprintf("TLS key file does not exist: %s", cfg.TLSKeyFile))
		}

		if cfg.TLSClientAuth && cfg.TLSCAFile == "" {
			v.addError("security.tls_ca_file", "TLS CA file is required when client auth is enabled")
		}

		if cfg.TLSCAFile != "" && !v.fileExists(cfg.TLSCAFile) {
			v.addError("security.tls_ca_file", fmt.Sprintf("TLS CA file does not exist: %s", cfg.TLSCAFile))
		}
	}

	// FIPS mode validation
	if cfg.FIPSMode {
		// Check if FIPS mode is available (this is a runtime check)
		if !v.isFIPSAvailable() {
			v.addError("security.fips_mode", "FIPS mode is not available on this system")
		}
	}
}

// validateAuthzPosture enforces a fail-closed authorization posture for
// compliance regimes (FedRAMP/HIPAA/PCI/IL4-5): a server must never silently
// reach a fail-open state on a network-reachable surface. When the default-deny
// authorization gate is disabled (RequireAuthz == false) AND at least one
// network transport (REST, gRPC, MCP) is enabled, startup is REFUSED
// unless the operator has explicitly acknowledged insecure development mode via
// Security.InsecureDevMode. The Unix domain socket is an in-host transport and
// is intentionally excluded — it does not expose a network-reachable surface.
func (v *Validator) validateAuthzPosture(sec *SecurityConfig, server *ServerConfig) {
	if sec.RequireAuthz || sec.InsecureDevMode {
		return
	}

	enabled := make([]string, 0, 4)
	if server.REST.Enabled {
		enabled = append(enabled, "server.rest")
	}
	if server.GRPC.Enabled {
		enabled = append(enabled, "server.grpc")
	}
	if server.MCP.Enabled {
		enabled = append(enabled, "server.mcp")
	}

	if len(enabled) == 0 {
		return
	}

	v.addError("security.require_authz", fmt.Sprintf(
		"authorization is disabled (require_authz=false) while network transport(s) [%s] are enabled, "+
			"which would expose a fail-open surface on a network-reachable listener; "+
			"set security.require_authz=true for production, "+
			"or set security.insecure_dev_mode=true to explicitly acknowledge running without authorization for local development only",
		strings.Join(enabled, ", ")))
}

// validateNetworkServingTLS enforces that network serving requires security
// TLS. Network serving runs exclusively on the embedded qrdb node's unified
// multi-protocol server (see (*Config).NetworkServing): whenever
// server.rest.enabled, server.grpc.enabled, or server.mcp.enabled is true,
// the node's own listener for that protocol comes up on the configured
// port — and qrdb's transports are TLS-only, so enabling any of them without
// security TLS would compose a listener that can never serve a handshake.
// Per-protocol port range validation is already performed by validateServer;
// this check only adds the TLS requirement.
func (v *Validator) validateNetworkServingTLS(server *ServerConfig, sec *SecurityConfig) {
	if sec.TLSEnabled {
		return
	}

	enabled := make([]string, 0, 3)
	if server.REST.Enabled {
		enabled = append(enabled, "server.rest.enabled")
	}
	if server.GRPC.Enabled {
		enabled = append(enabled, "server.grpc.enabled")
	}
	if server.MCP.Enabled {
		enabled = append(enabled, "server.mcp.enabled")
	}
	if len(enabled) == 0 {
		return
	}

	v.addError("security.tls_enabled", fmt.Sprintf(
		"network serving requires TLS (security.tls_enabled=false) while [%s] are enabled; "+
			"the embedded qrdb node's unified server transports are TLS-only",
		strings.Join(enabled, ", ")))
}

// validateLogging validates logging configuration.
func (v *Validator) validateLogging(cfg *LoggingConfig) {
	validLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}
	if !validLevels[cfg.Level] {
		v.addError("logging.level", fmt.Sprintf("invalid log level: %s (valid: debug, info, warn, error)", cfg.Level))
	}

	validFormats := map[string]bool{
		"json":    true,
		"text":    true,
		"console": true,
	}
	if !validFormats[cfg.Format] {
		v.addError("logging.format", fmt.Sprintf("invalid log format: %s (valid: json, text, console)", cfg.Format))
	}

	validOutputs := map[string]bool{
		"stdout": true,
		"file":   true,
	}
	if !validOutputs[cfg.Output] {
		v.addError("logging.output", fmt.Sprintf("invalid log output: %s (valid: stdout, file)", cfg.Output))
	}

	if cfg.Output == "file" && cfg.File == "" {
		v.addError("logging.file", "log file is required when output is set to 'file'")
	}
}

// validateKMS validates KMS configuration.
func (v *Validator) validateKMS(cfg *KMSConfig) {
	// Skip validation if KMS is disabled
	if !cfg.Enabled {
		return
	}

	// Use the KMSConfig's own Validate method which validates
	// backend type and backend-specific configuration
	if err := cfg.Validate(); err != nil {
		var validationErr *ValidationError
		if errors.As(err, &validationErr) {
			// Keep the whole error, Cause included, so the typed failure it
			// carries reaches the caller of Validate.
			v.errors = append(v.errors, *validationErr)
		} else {
			v.addError("kms", err.Error())
		}
	}
}

// validateTrust validates the optional trust resolver configuration. The section
// is not required; when nil the outbound connections use the OS trust store.
// The parameter is the concrete pointer (not an interface) so a nil *Config is
// caught by the guard instead of being boxed into a non-nil interface value.
func (v *Validator) validateTrust(cfg *xkmsresolver.Config) {
	if cfg == nil {
		return
	}
	if err := cfg.Validate(); err != nil {
		v.addError("trust", err.Error())
	}
}

// validateIAM validates the IAM section. It is always run: the sub-sections it
// covers have no "enabled" flag to gate on, because the IAM surface is mounted
// whenever IAM is compiled in.
//
// The WebAuthn block is validated for shape only — an empty relying-party
// identity is the supported derive-from-issuer deployment, not an error. See
// IAMWebAuthnConfig.Validate for the required-versus-derived contract, and
// IAMWebAuthnConfig.ValidateResolved for the check applied to the value the
// relying party is actually built from.
func (v *Validator) validateIAM(cfg *IAMConfig) {
	if cfg == nil {
		return
	}
	if err := cfg.WebAuthn.Validate(); err != nil {
		var cve *ConfigValidationError
		if errors.As(err, &cve) {
			v.addError(cve.Field, cve.Message)
		} else {
			v.addError("iam.webauthn", err.Error())
		}
	}
}

// addError adds a validation error.
// validateCrypto validates the process-wide crypto backend policy. It is
// always run (the zero value is valid = software everywhere); an unknown
// backend type in any slot yields a typed validation error.
func (v *Validator) validateCrypto(cfg *CryptoConfig) {
	if err := cfg.Validate(); err != nil {
		var validationErr *ValidationError
		if errors.As(err, &validationErr) {
			v.addError(validationErr.Field, validationErr.Message)
		} else {
			v.addError("crypto", err.Error())
		}
	}
}

func (v *Validator) addError(field, message string) {
	v.errors = append(v.errors, ValidationError{
		Field:   field,
		Message: message,
	})
}

// fileExists checks if a file exists.
func (v *Validator) fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// ensureDirectoryWritable checks if a directory exists and is writable
// If it doesn't exist, it attempts to create it.
func (v *Validator) ensureDirectoryWritable(path string) error {
	// Get absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return api.NewPathOperationError("config.Validator.ensureDirectoryWritable", "get absolute path", err)
	}

	// Check if directory exists
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Try to create directory with stricter permissions (0750)
			if err := os.MkdirAll(absPath, 0o750); err != nil {
				return api.NewPathOperationError("config.Validator.ensureDirectoryWritable", "create directory", err)
			}
			return nil
		}
		return api.NewPathOperationError("config.Validator.ensureDirectoryWritable", "stat directory", err)
	}

	// Check if it's actually a directory
	if !info.IsDir() {
		return api.NewNotADirectoryError("config.Validator.ensureDirectoryWritable")
	}

	// Check if writable by trying to create a temp file
	testFile := filepath.Join(absPath, ".write_test")
	f, err := os.Create(testFile)
	if err != nil {
		return api.NewDirectoryNotWritableError("config.Validator.ensureDirectoryWritable", err)
	}
	_ = f.Close()           //nolint:gosec // G104: intentionally ignoring close error for test file
	_ = os.Remove(testFile) //nolint:gosec // G104: intentionally ignoring remove error for test file

	return nil
}

// validateOrchestration validates orchestration configuration. The only
// configurable nested policy at present is host_affinity.mode which must be
// either "soft" (warn-and-fallback) or "hard" (typed-error). Empty maps to
// "soft" for backward compatibility with existing deployments.
func (v *Validator) validateOrchestration(cfg *OrchestrationConfig) {
	if cfg == nil {
		return
	}
	switch cfg.HostAffinity.Mode {
	case "", HostAffinityModeSoft, HostAffinityModeHard:
		// valid
	default:
		v.addError("orchestration.host_affinity.mode",
			fmt.Sprintf("invalid mode: %q (valid: %q, %q)",
				cfg.HostAffinity.Mode, HostAffinityModeSoft, HostAffinityModeHard))
	}

	// The managed-workspace root and every extra-mount allowlist entry are
	// compared against container mount sources by absolute-path prefix, so a
	// relative entry could never match and would silently disable the policy
	// it was meant to configure. Reject them up front.
	if cfg.ProjectsRoot != "" && !filepath.IsAbs(cfg.ProjectsRoot) {
		v.addError("orchestration.projects_root",
			fmt.Sprintf("must be an absolute path: %q", cfg.ProjectsRoot))
	}
	// The sockets root is the parent of every per-session agent socket
	// directory and is compared against mount sources the same way, so a
	// relative entry could never match either.
	if cfg.SocketsRoot != "" && !filepath.IsAbs(cfg.SocketsRoot) {
		v.addError("orchestration.sockets_root",
			fmt.Sprintf("must be an absolute path: %q", cfg.SocketsRoot))
	}
	for i, root := range cfg.Sandbox.AllowedMountRoots {
		if root == "" || !filepath.IsAbs(root) {
			v.addError(fmt.Sprintf("orchestration.sandbox.allowed_mount_roots[%d]", i),
				fmt.Sprintf("must be an absolute path: %q", root))
		}
	}

	// An unrecognized trust policy must fail the config rather than fall back
	// to the zero value: the fallback is the WEAKEST policy, so a typo in
	// "attested" would silently disable the integrity floor the operator
	// believed they had configured. Empty is the documented default.
	if cfg.TrustPolicy != "" && !slices.Contains(OrchestrationTrustPolicies, cfg.TrustPolicy) {
		v.addError("orchestration.trust_policy",
			fmt.Sprintf("invalid policy: %q (valid: %s)",
				cfg.TrustPolicy, strings.Join(OrchestrationTrustPolicies, ", ")))
	}

	// A negative lease TTL would make every holder stale on the read after
	// it took the lease; zero is the documented "take the default".
	if cfg.ReapLeaseTTL < 0 {
		v.addError("orchestration.reap_lease_ttl",
			fmt.Sprintf("must not be negative: %s", cfg.ReapLeaseTTL))
	}

	// A threshold on a state the detector does not watch would leave the
	// operator believing a state is bounded that is not. Negative values are
	// the documented off switch and are accepted.
	for state := range cfg.IdleThresholds {
		if !slices.Contains(OrchestrationWatchedJobStates, state) {
			v.addError("orchestration.idle_thresholds."+state,
				fmt.Sprintf("no job state is named %q (valid: %s)",
					state, strings.Join(OrchestrationWatchedJobStates, ", ")))
		}
	}

	v.validateAccountProviders(cfg.AccountProviders)

	// The task-source poller's cadence. Zero means unset and takes the
	// default (EffectiveSweepInterval); a negative interval is a value
	// somebody wrote on purpose and got wrong, and is refused.
	if cfg.SweepInterval < 0 {
		v.addError("orchestration.sweep_interval", "must not be negative; zero takes the default")
	}

	// The deployment's worker-slot bound. Zero leaves the fleet axis
	// undeclared, which is the default; a negative count is a number somebody
	// wrote on purpose and got wrong, and a bound below zero cannot be
	// distinguished downstream from the undeclared case.
	if cfg.Fleet.MaxConcurrentJobs < 0 {
		v.addError("orchestration.fleet.max_concurrent_jobs",
			fmt.Sprintf("must not be negative: %d; zero leaves the fleet capacity undeclared",
				cfg.Fleet.MaxConcurrentJobs))
	}

	// The command allowlist is matched exactly, so an empty entry would
	// allow the empty command and hide a typo; the guard refuses it and so
	// does the config.
	for i, c := range cfg.VerificationCommands {
		if strings.TrimSpace(c) == "" {
			v.addError(fmt.Sprintf("orchestration.verification_commands[%d]", i),
				"must not be empty; an empty allowlist entry would allow the empty command")
		}
	}

	// The agent configuration directory is put on a session's environment
	// verbatim, so a relative path would resolve against whatever the
	// session's working directory is — a different directory per session.
	if cfg.AgentConfigDir != "" && !filepath.IsAbs(cfg.AgentConfigDir) {
		v.addError("orchestration.agent_config_dir",
			fmt.Sprintf("must be an absolute path: %q", cfg.AgentConfigDir))
	}

	v.validateOrchestrationStageTimeouts(cfg.StageTimeouts)
}

// validateOrchestrationStageTimeouts refuses a per-moment bound a job cannot
// be held to.
//
// NEGATIVE IS REFUSED, ZERO IS NOT. Zero means the operator did not set this
// moment and takes the engine's defaults layer; a negative duration is a value
// somebody wrote on purpose and got wrong, and it would expire the moment
// before it began — every job failing at its first hook, reporting a timeout
// nobody configured.
func (v *Validator) validateOrchestrationStageTimeouts(cfg OrchestrationStageTimeouts) {
	bounds := map[string]time.Duration{
		"prepare":    cfg.Prepare,
		"reviewable": cfg.Reviewable,
		"complete":   cfg.Complete,
		"cleanup":    cfg.Cleanup,
		"blocked":    cfg.Blocked,
	}
	stages := make([]string, 0, len(bounds))
	for stage := range bounds {
		stages = append(stages, stage)
	}
	// Sorted so an operator with two bad bounds is told about them in the same
	// order every time; the validator accumulates rather than failing fast.
	sort.Strings(stages)
	for _, stage := range stages {
		if bounds[stage] < 0 {
			v.addError("orchestration.stage_timeouts."+stage,
				"must not be negative; a negative bound expires before the moment begins, and zero takes the default")
		}
	}
}

// isFIPSAvailable checks if FIPS mode is available on the host system.
// On Linux, it reads /proc/sys/crypto/fips_enabled to determine FIPS status.
// On non-Linux systems, FIPS is reported as unavailable.
func (v *Validator) isFIPSAvailable() bool {
	// Check for FIPS mode via procfs (Linux)
	if _, err := os.Stat("/proc/sys/crypto/fips_enabled"); err == nil {
		data, err := os.ReadFile("/proc/sys/crypto/fips_enabled")
		if err == nil && len(data) > 0 && data[0] == '1' {
			return true
		}
	}

	// Non-Linux systems do not expose FIPS status through procfs
	return false
}

// Validate is a convenience function to validate a configuration.
func Validate(cfg *Config) error {
	validator := NewValidator()
	return validator.Validate(cfg)
}

// validateSDLCPublicURL refuses a node that could onboard a project into the
// void.
//
// Onboarding provisions a forge webhook pointing at this node's public
// origin. An absent or malformed one is refused AT LOAD rather than at the
// first onboarding, because the alternative is twenty-eight repositories
// configured to deliver somewhere that does not answer, each reported
// provisioned.
func (v *Validator) validateSDLCPublicURL(public string) {
	if err := ValidateSDLCWebhookPublicURL(public); err != nil {
		v.addError("sdlc.webhook.public_url", err.Error())
	}
}

// ValidateSDLCWebhookPublicURL reports whether a value is usable as this
// node's public webhook origin. It is PURE — it reads no filesystem and no
// environment — so the rule can be exercised on its own, which is what lets a
// drift test pin it against the module's own refusal
// (sdlc.NormalizeWebhookPublicURL). pkg/config cannot import pkg/sdlc, so the
// rule is stated twice and the drift test is what keeps the two the same.
func ValidateSDLCWebhookPublicURL(public string) error {
	trimmed := strings.TrimSpace(public)
	if trimmed == "" {
		return &SDLCWebhookPublicURLError{
			Reason: "required when the module is enabled; onboarding provisions forge webhooks pointing at this node, and it cannot guess the node's public origin",
		}
	}
	parsed, err := url.Parse(trimmed)
	switch {
	case err != nil:
		return &SDLCWebhookPublicURLError{Reason: "must be a URL: " + err.Error()}
	case parsed.Scheme != "http" && parsed.Scheme != "https":
		return &SDLCWebhookPublicURLError{Reason: "must be an absolute http or https URL; a forge delivers over neither scheme otherwise"}
	case parsed.Host == "":
		return &SDLCWebhookPublicURLError{Reason: "must name a host a forge can reach"}

	// AN ORIGIN, NOT MERELY A URL. The module appends the webhook route to
	// this value textually, so "https://factory.example?x=1" — which has a
	// scheme and a host and passes every check above — yields a delivery URL
	// whose path is "/" and whose query holds the route. A fragment swallows
	// it the same way. Neither fails loudly: the delivery is provisioned,
	// read-back verification compares the same malformed string and agrees,
	// and conformance calls the repository healthy while nothing arrives. A
	// PATH is allowed, for a node mounted behind a reverse proxy.
	case parsed.RawQuery != "" || parsed.ForceQuery:
		return &SDLCWebhookPublicURLError{Reason: "must carry no query string; the webhook route is appended to it, and a query swallows the route"}
	case parsed.Fragment != "":
		return &SDLCWebhookPublicURLError{Reason: "must carry no fragment; the webhook route is appended to it, and a fragment swallows the route"}
	case parsed.User != nil:
		return &SDLCWebhookPublicURLError{Reason: "must carry no userinfo; the forge stores and displays the delivery URL, so a credential in it is a credential published"}
	}
	return nil
}

// SDLCWebhookPublicURLError is a public webhook origin the node cannot
// append its webhook route to.
type SDLCWebhookPublicURLError struct {
	// Reason says which property of an origin the value does not have.
	Reason string
}

func (e *SDLCWebhookPublicURLError) Error() string { return e.Reason }

// validateSDLC validates the SDLC module's settings when the module is
// enabled. Every zero that would mean "never" or "unbounded" is refused.
func (v *Validator) validateSDLC(cfg *SDLCConfig) {
	if cfg == nil || !cfg.Enabled {
		return
	}
	if cfg.IssuePageLimit <= 0 {
		v.addError("sdlc.issue_page_limit", "must be positive; a sweep that lists no issues adopts nothing")
	}
	if cfg.MergeAttempts <= 0 {
		v.addError("sdlc.merge_attempts", "must be positive; a merge must be attempted at least once")
	}
	if cfg.Webhook.MaxBody <= 0 {
		v.addError("sdlc.webhook.max_body", "must be positive; verification buffers the whole body, so an unbounded one is an unauthenticated memory sink")
	}
	v.validateSDLCPublicURL(cfg.Webhook.PublicURL)
	for i, command := range cfg.VerificationCommands {
		if strings.TrimSpace(command) == "" {
			v.addError("sdlc.verification_commands["+strconv.Itoa(i)+"]", "must not be empty; an empty criterion runs the empty command")
		}
	}
	for i, host := range cfg.RepoHosts {
		if strings.TrimSpace(host) == "" {
			v.addError("sdlc.repo_hosts["+strconv.Itoa(i)+"]", "must not be empty; an empty entry matches no host and hides a typo")
		}
	}
	// NEGATIVE IS REFUSED, ZERO IS NOT here too: zero takes the module's
	// default cadence, while a negative interval would make a ticker the
	// runtime panics on.
	if cfg.LabelReconcile.Interval < 0 {
		v.addError("sdlc.label_reconcile.interval",
			"must not be negative; zero takes the default and a negative cadence runs no sweep at all")
	}
	if cfg.LabelReconcile.ForgeInterval < 0 {
		v.addError("sdlc.label_reconcile.forge_interval",
			"must not be negative; zero takes the default and a negative spacing paces nothing")
	}
	// NEGATIVE IS REFUSED, ZERO IS NOT: zero takes the module's defaults
	// layer, while a negative value would abandon a parked task's question
	// before its first post.
	if cfg.Surfacing.RetryAttempts < 0 {
		v.addError("sdlc.surfacing.retry_attempts",
			"must not be negative; zero takes the default and a negative budget abandons every park before its first post")
	}
	if cfg.Surfacing.RetryInterval < 0 {
		v.addError("sdlc.surfacing.retry_interval",
			"must not be negative; zero takes the default and a negative interval paces nothing")
	}
	switch {
	case cfg.Surfacing.PostTimeout < 0:
		v.addError("sdlc.surfacing.post_timeout",
			"must not be negative; zero takes the default and a negative deadline cancels every post before it is made")
	case cfg.Surfacing.PostTimeout > 0 && cfg.Surfacing.PostTimeout < SDLCSurfacingMinPostTimeout:
		v.addError("sdlc.surfacing.post_timeout",
			"a deadline this short cannot carry a forge round trip; zero takes the default")
	case cfg.Surfacing.PostTimeout > SDLCSurfacingMaxPostTimeout:
		v.addError("sdlc.surfacing.post_timeout",
			"a single post allowed more than a day is not a deadline; zero takes the default")
	}
}
