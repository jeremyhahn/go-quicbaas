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
	"os"
	"path/filepath"
	"time"
)

// DefaultUnixSocket is the default Unix domain socket path.
// Uses the user's home directory so both server and client find
// the same socket regardless of working directory.
var DefaultUnixSocket = homeUnixSocket()

func homeUnixSocket() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, "quicbaas.sock")
	}
	return "./quicbaas.sock"
}

// DefaultConfig returns the default configuration with all features enabled.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host:              "127.0.0.1",
			SealedBindHost:    DefaultSealedBindHost,
			ExposeShardHeader: true,
			Embedded:          false,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      30 * time.Second,
			IdleTimeout:       60 * time.Second,
			ShutdownTimeout:   30 * time.Second,
			MaxConnections:    1000,
			GRPC: ServerGRPCConfig{
				Enabled: true,
				Port:    9443,
			},
			REST: ServerRESTConfig{
				Enabled: true,
				Port:    8443,
			},
			MCP: ServerMCPConfig{
				Enabled: true,
				Port:    11443,
				Authorization: MCPAuthorizationConfig{
					Enabled:               true,
					RequireHTTPSRedirects: true,
				},
				DynamicRegistration: MCPDynamicRegistrationConfig{
					Enabled: true,
				},
			},
			Unix: ServerUnixConfig{
				Enabled: true,
				Socket:  DefaultUnixSocket,
			},
		},
		Storage: StorageConfig{
			Backend:               StorageBackendPebble,
			DataDir:               "/var/lib/quicbaas",
			WALDir:                "/var/lib/quicbaas/wal",
			CacheSize:             256 * 1024 * 1024, // 256MB
			MaxOpenFiles:          1000,
			MaxConcurrentReads:    256,
			MaxConcurrentWrites:   256,
			BytesPerSync:          1 * 1024 * 1024,  // 1MB
			MemTableSize:          64 * 1024 * 1024, // 64MB
			MemTableStopWrites:    2,
			L0CompactionThreshold: 4,
			L0StopWritesThreshold: 12,
			BlockSize:             4096,             // 4KB
			TargetFileSize:        64 * 1024 * 1024, // 64MB
			CompressionType:       "snappy",
		},
		Security: SecurityConfig{
			TLSEnabled:  true,
			SealEnabled: false,
			// Secure default: the REST surface enforces the default-deny scope ->
			// RBAC gate. Non-public, non-cluster-mTLS requests must present a valid
			// platform token carrying the required "<svc>.<verb>" scope; token-less
			// requests are denied. Operators needing the legacy anonymous
			// data-plane posture set require_authz=false explicitly.
			RequireAuthz:     true,
			AuthzMode:        DefaultAuthzMode,
			LivenessCacheTTL: DefaultLivenessCacheTTL,
			Authorization: AuthorizationConfig{
				Provider: "rbac",
				RBAC:     RBACAuthzConfig{Enabled: true, AllowAnonymous: false},
				ABAC:     ABACAuthzConfig{Enabled: true},
				Audit:    AuthzAuditConfig{Enabled: true, LogDenied: true, LogAllowed: true},
			},
		},
		KMS: KMSConfig{
			Enabled: true,
			Backend: BackendTypeSoftware,
			Cache:   KMSCacheConfig{Enabled: true},
		},
		Secrets: SecretsConfig{
			Enabled:    true,
			Versioning: SecretsVersioningConfig{Enabled: true},
			SoftDelete: SecretsSoftDeleteConfig{Enabled: true},
		},
		TimeSeries: DefaultTimeSeriesConfig(),
		Vector: VectorConfig{
			Enabled:      true,
			CacheEnabled: true,
		},
		Ollama: OllamaConfig{
			Enabled: true,
			BaseURL: "http://localhost:11434",
		},
		Backup: BackupConfig{
			Enabled:            true,
			DefaultCompression: "zstd",
			DefaultEncryption:  "aes-256-gcm",
		},
		Discovery: DiscoveryConfig{
			Enabled: true,
			Type:    "multicast",
		},
		Raft: RaftConfig{
			NodeID:              1,
			RaftAddress:         "127.0.0.1:63000",
			ListenAddress:       "127.0.0.1:63000",
			MutualTLS:           true,
			EnableMetrics:       true,
			NotifyCommit:        true,
			EnableBootstrap:     true,
			ElectionRTT:         10,
			HeartbeatRTT:        1,
			SnapshotEntries:     10000,
			CompactionOverhead:  5000,
			RequestTimeout:      5 * time.Second,
			RTTMillisecond:      200,
			MaxInMemLogSize:     64 * 1024 * 1024, // 64MB
			MaxSendQueueSize:    1024,
			MaxReceiveQueueSize: 1024,
		},
		Cache: DefaultEnhancedCacheConfig(),
		Metrics: MetricsConfig{
			Enabled:         true,
			AdvancedEnabled: false,
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
			Output: "stdout",
		},
		Scheduler: DefaultSchedulerConfig(),
		Messaging: DefaultMessagingConfig(),
		Orchestration: OrchestrationConfig{
			Enabled:          true,
			HeartbeatTimeout: 90 * time.Second,
		},
		Seal:      DefaultSealConfig(),
		CA:        DefaultCAConfig(),
		QRDB:      DefaultQRDBConfig(),
		Databases: DefaultDatabasesConfig(),
	}
}

// DefaultSchedulerConfig returns the default scheduler configuration.
// Scheduler is enabled by default: it uses only in-memory state backed by the
// existing KVStore and carries no external dependencies.
func DefaultSchedulerConfig() SchedulerConfig {
	return SchedulerConfig{
		Enabled:           true,
		TickInterval:      1 * time.Second,
		MaxConcurrentJobs: 10,
	}
}

// DefaultMessagingConfig returns the default messaging configuration.
// Messaging is enabled by default: the hub is purely in-memory and requires no
// external broker or configuration.
func DefaultMessagingConfig() MessagingConfig {
	return MessagingConfig{
		Enabled:      true,
		BufferSize:   4096,
		DedupEnabled: true,
		DedupWindow:  1 * time.Minute,
	}
}

// DefaultSealConfig returns the default seal/unseal configuration.
func DefaultSealConfig() SealConfig {
	return SealConfig{
		Enabled:           false,
		Strategy:          "passphrase",
		Mode:              "manual",
		Threshold:         2,
		TotalShares:       3,
		PreferenceOrder:   []string{"passphrase"},
		RecoveryEnabled:   true,
		RecoveryThreshold: 3,
		RecoveryTotal:     5,
		TPM2: TPM2SealConfig{
			Device:  "/dev/tpmrm0",
			PCRBank: "SHA256",
		},
		Vault: VaultSealConfig{
			TransitPath: "transit",
		},
	}
}

// DefaultCAConfig returns the default Certificate Authority configuration.
func DefaultCAConfig() CAConfig {
	return CAConfig{
		RootCommonName:           "QuicBaaS Root CA",
		RootOrganization:         "QuicBaaS",
		RootValidYears:           10,
		RootKeyAlgorithm:         "ECDSA",
		RootKeyCurve:             "P-256",
		IntermediateEnabled:      true,
		IntermediateCommonName:   "QuicBaaS Intermediate CA",
		IntermediateValidYears:   5,
		IntermediateKeyAlgorithm: "ECDSA",
		IntermediateKeyCurve:     "P-256",
		DefaultValidityDays:      365,
		IncludeLocalhostSANS:     true,
	}
}

// DefaultQRDBConfig returns the default qrdb SDK client transport configuration.
// The default uses embedded transport for zero-overhead in-process communication.
func DefaultQRDBConfig() QRDBConfig {
	return QRDBConfig{
		Transport: QRDBTransportEmbedded,
	}
}

// DefaultServerConfig returns the default server configuration.
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		Host:              "127.0.0.1",
		SealedBindHost:    DefaultSealedBindHost,
		ExposeShardHeader: true,
		Embedded:          false,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		ShutdownTimeout:   30 * time.Second,
		MaxConnections:    1000,
		GRPC: ServerGRPCConfig{
			Enabled: true,
			Port:    9443,
		},
		REST: ServerRESTConfig{
			Enabled: true,
			Port:    8443,
		},
		MCP: ServerMCPConfig{
			Enabled: true,
			Port:    11443,
			Authorization: MCPAuthorizationConfig{
				Enabled:               true,
				RequireHTTPSRedirects: true,
			},
			DynamicRegistration: MCPDynamicRegistrationConfig{
				Enabled: true,
			},
		},
		Unix: ServerUnixConfig{
			Enabled: true,
			Socket:  DefaultUnixSocket,
		},
	}
}

// DefaultRaftConfig returns the default Raft configuration.
func DefaultRaftConfig() RaftConfig {
	return RaftConfig{
		NodeID:              1,
		ElectionRTT:         10,
		HeartbeatRTT:        1,
		SnapshotEntries:     10000,
		CompactionOverhead:  5000,
		RequestTimeout:      5 * time.Second,
		RTTMillisecond:      200,
		RaftAddress:         "127.0.0.1:63000",
		ListenAddress:       "127.0.0.1:63000",
		MutualTLS:           true,
		MaxInMemLogSize:     64 * 1024 * 1024, // 64MB
		MaxSendQueueSize:    1024,
		MaxReceiveQueueSize: 1024,
		MaxSnapshotSendBPS:  0, // unlimited
		MaxSnapshotRecvBPS:  0, // unlimited
		NotifyCommit:        true,
		EnableMetrics:       true,
		EnableBootstrap:     true,
	}
}

// DefaultStorageConfig returns the default storage configuration.
func DefaultStorageConfig() StorageConfig {
	return StorageConfig{
		Backend:               StorageBackendPebble,
		DataDir:               "/var/lib/quicbaas",
		WALDir:                "/var/lib/quicbaas/wal",
		CacheSize:             256 * 1024 * 1024, // 256MB
		MaxOpenFiles:          1000,
		MaxConcurrentReads:    100,
		MaxConcurrentWrites:   100,
		DisableWAL:            false,
		SyncWrites:            true,
		BytesPerSync:          1024 * 1024,      // 1MB
		MemTableSize:          64 * 1024 * 1024, // 64MB
		MemTableStopWrites:    2,
		L0CompactionThreshold: 4,
		L0StopWritesThreshold: 12,
		BlockSize:             4096,
		TargetFileSize:        64 * 1024 * 1024, // 64MB
		CompressionType:       "snappy",
	}
}

// DefaultSecurityConfig returns the default security configuration.
func DefaultSecurityConfig() SecurityConfig {
	return SecurityConfig{
		TLSEnabled:          true,
		TLSCertFile:         "",
		TLSKeyFile:          "",
		TLSCAFile:           "",
		TLSClientAuth:       false,
		FIPSMode:            false,
		EncryptionAlgorithm: "aes-256-gcm",
		SealEnabled:         false,
		// Secure default: enforce the default-deny scope -> RBAC gate. See the
		// RequireAuthz field doc in config.go.
		RequireAuthz:            true,
		EmbeddedSystemPrincipal: EmbeddedSystemPrincipalDefault,
		AuthzMode:               DefaultAuthzMode,
		LivenessCacheTTL:        DefaultLivenessCacheTTL,
		Authorization:           DefaultAuthorizationConfig(),
		TenantIsolation: TenantIsolationConfig{
			Mode: TenantIsolationKeyspace,
		},
	}
}

// DefaultAuthorizationConfig returns the default authorization configuration.
func DefaultAuthorizationConfig() AuthorizationConfig {
	return AuthorizationConfig{
		Provider: "rbac",
		RBAC: RBACAuthzConfig{
			Enabled:        true,
			DefaultRole:    "",
			AllowAnonymous: false,
		},
		ABAC: ABACAuthzConfig{
			Enabled:   true,
			CacheTTL:  5 * time.Minute,
			PolicyDir: "",
		},
		Audit: AuthzAuditConfig{
			Enabled:    true,
			LogDenied:  true,
			LogAllowed: true,
		},
	}
}

// DefaultLoggingConfig returns the default logging configuration.
func DefaultLoggingConfig() LoggingConfig {
	return LoggingConfig{
		Level:     "info",
		Format:    "json",
		Output:    "stdout",
		File:      "./quicbaas.log",
		Component: "",
	}
}

// DefaultBackupConfig returns the default backup configuration.
func DefaultBackupConfig() BackupConfig {
	return BackupConfig{
		Enabled:            true,
		DefaultCompression: "zstd",
		DefaultEncryption:  "aes-256-gcm",
	}
}

// DefaultDiscoveryConfig returns the default discovery configuration.
func DefaultDiscoveryConfig() DiscoveryConfig {
	return DiscoveryConfig{
		Enabled: true,
		Type:    "multicast",
	}
}

// DefaultMetricsConfig returns the default metrics configuration.
func DefaultMetricsConfig() MetricsConfig {
	return MetricsConfig{
		Enabled:         true,
		AdvancedEnabled: false,
	}
}

// Default returns a default configuration using all default sub-configs.
func Default() *Config {
	return DefaultConfig()
}

// ProductionConfig returns a production-ready configuration.
// Uses cluster mode for distributed Raft consensus with PebbleDB storage.
func ProductionConfig() *Config {
	cfg := DefaultConfig()

	// Production-specific overrides
	cfg.Server.Mode = ServerModeCluster // Explicitly set cluster mode for Raft
	cfg.Server.Host = "0.0.0.0"
	cfg.Server.MaxConnections = 10000
	cfg.Server.Unix.Socket = DefaultUnixSocket

	cfg.Raft.RaftAddress = "0.0.0.0:63000"
	cfg.Raft.ListenAddress = "0.0.0.0:63000"
	cfg.Raft.SnapshotEntries = 50000
	cfg.Raft.CompactionOverhead = 10000
	cfg.Raft.RequestTimeout = 10 * time.Second
	cfg.Raft.MaxInMemLogSize = 128 * 1024 * 1024 // 128MB
	cfg.Raft.MaxSendQueueSize = 2048
	cfg.Raft.MaxReceiveQueueSize = 2048

	// Use PebbleDB with cluster mode for distributed storage
	cfg.Storage.Backend = StorageBackendPebble
	cfg.Storage.DataDir = "/var/lib/quicbaas/data"
	cfg.Storage.WALDir = "/var/lib/quicbaas/wal"
	cfg.Storage.CacheSize = 512 * 1024 * 1024 // 512MB
	cfg.Storage.MaxOpenFiles = 5000
	cfg.Storage.MaxConcurrentReads = 500
	cfg.Storage.MaxConcurrentWrites = 500
	cfg.Storage.BytesPerSync = 4 * 1024 * 1024   // 4MB
	cfg.Storage.MemTableSize = 128 * 1024 * 1024 // 128MB
	cfg.Storage.MemTableStopWrites = 3
	cfg.Storage.L0CompactionThreshold = 8
	cfg.Storage.L0StopWritesThreshold = 20
	cfg.Storage.BlockSize = 8192
	cfg.Storage.TargetFileSize = 128 * 1024 * 1024 // 128MB
	cfg.Storage.CompressionType = "zstd"

	cfg.Security.TLSCertFile = "/etc/quicbaas/tls/server.crt"
	cfg.Security.TLSKeyFile = "/etc/quicbaas/tls/server.key"
	cfg.Security.TLSCAFile = "/etc/quicbaas/tls/ca.crt"
	cfg.Security.TLSClientAuth = true
	cfg.Security.Authorization.ABAC.PolicyDir = "/etc/quicbaas/policies"

	cfg.Logging.Output = "file"
	cfg.Logging.File = "/var/log/quicbaas/quicbaas.log"

	return cfg
}

// DevelopmentConfig returns a development-friendly configuration.
func DevelopmentConfig() *Config {
	cfg := DefaultConfig()

	// Development-specific overrides
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.MaxConnections = 100
	cfg.Server.ShutdownTimeout = 10 * time.Second
	cfg.Server.Unix.Socket = "/tmp/quicbaas.sock"

	// Disable MCP in development by default
	cfg.Server.MCP.Enabled = false

	cfg.Raft.ElectionRTT = 5 // Faster for development
	cfg.Raft.SnapshotEntries = 1000
	cfg.Raft.CompactionOverhead = 500
	cfg.Raft.RequestTimeout = 3 * time.Second
	cfg.Raft.RTTMillisecond = 100
	cfg.Raft.MutualTLS = false
	cfg.Raft.MaxInMemLogSize = 32 * 1024 * 1024 // 32MB
	cfg.Raft.MaxSendQueueSize = 512
	cfg.Raft.MaxReceiveQueueSize = 512

	cfg.Storage.DataDir = "./dev-data"
	cfg.Storage.WALDir = "./dev-data/wal"
	cfg.Storage.CacheSize = 64 * 1024 * 1024 // 64MB
	cfg.Storage.MaxOpenFiles = 500
	cfg.Storage.MaxConcurrentReads = 50
	cfg.Storage.MaxConcurrentWrites = 50
	cfg.Storage.SyncWrites = false // Faster for development
	cfg.Storage.MemTableSize = 32 * 1024 * 1024
	cfg.Storage.L0StopWritesThreshold = 8
	cfg.Storage.TargetFileSize = 32 * 1024 * 1024

	// TLS stays ON even for local development: disabling it produces a
	// certless bootstrap (no CA -> no admin mTLS credential) and plaintext
	// listeners, both forbidden.
	cfg.Security.TLSEnabled = true
	cfg.Security.Authorization.RBAC.AllowAnonymous = true
	cfg.Security.Authorization.ABAC.Enabled = false
	// Development runs anonymous (AllowAnonymous=true); the default-deny gate is
	// off so local data-plane calls do not require a platform token.
	cfg.Security.RequireAuthz = false
	// Development enables network transports with authorization disabled, so it
	// must explicitly acknowledge the insecure fail-open posture; otherwise the
	// authz-posture validation rule would refuse to start. This is a local-dev
	// profile only and must NEVER be used in production.
	cfg.Security.InsecureDevMode = true

	cfg.Logging.Level = "debug"
	cfg.Logging.Format = "console"
	cfg.Logging.File = "./dev-quicbaas.log"

	return cfg
}

// TestConfig returns a configuration suitable for testing. Network serving
// (REST/gRPC/MCP) is disabled and TLS is off: this profile targets fast,
// non-network unit tests of unrelated config sections (storage, security,
// raft, logging). Since network serving requires TLS
// (validateNetworkServingTLS — qrdb's unified server transports are
// TLS-only), a test that specifically exercises network-serving posture must
// opt in to both together rather than relying on this baseline.
func TestConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host:              "127.0.0.1",
			SealedBindHost:    DefaultSealedBindHost,
			ExposeShardHeader: true,
			ReadTimeout:       5 * time.Second,
			WriteTimeout:      5 * time.Second,
			IdleTimeout:       10 * time.Second,
			ShutdownTimeout:   5 * time.Second,
			MaxConnections:    10,
			GRPC: ServerGRPCConfig{
				Enabled: false,
				Port:    0, // Random port
			},
			REST: ServerRESTConfig{
				Enabled: false,
				Port:    0, // Random port
			},
			MCP: ServerMCPConfig{
				Enabled: false,
				Port:    0,
			},
			Unix: ServerUnixConfig{
				Enabled: false,
				Socket:  "",
			},
		},
		Raft: RaftConfig{
			NodeID:              1,
			ElectionRTT:         3, // Fast for tests
			HeartbeatRTT:        1,
			SnapshotEntries:     100,
			CompactionOverhead:  50,
			RequestTimeout:      2 * time.Second,
			RTTMillisecond:      50,
			RaftAddress:         "127.0.0.1:0",
			ListenAddress:       "127.0.0.1:0",
			MutualTLS:           false,
			MaxInMemLogSize:     8 * 1024 * 1024,
			MaxSendQueueSize:    128,
			MaxReceiveQueueSize: 128,
			MaxSnapshotSendBPS:  0,
			MaxSnapshotRecvBPS:  0,
			NotifyCommit:        false,
			EnableMetrics:       false,
			EnableBootstrap:     false,
		},
		Storage: StorageConfig{
			Backend:               StorageBackendMemory,
			DataDir:               "",
			WALDir:                "",
			CacheSize:             8 * 1024 * 1024,
			MaxOpenFiles:          100,
			MaxConcurrentReads:    10,
			MaxConcurrentWrites:   10,
			DisableWAL:            false,
			SyncWrites:            false,
			BytesPerSync:          0,
			MemTableSize:          4 * 1024 * 1024,
			MemTableStopWrites:    2,
			L0CompactionThreshold: 2,
			L0StopWritesThreshold: 4,
			BlockSize:             4096,
			TargetFileSize:        4 * 1024 * 1024,
			CompressionType:       "none",
		},
		Security: SecurityConfig{
			TLSEnabled:    false,
			TLSCertFile:   "",
			TLSKeyFile:    "",
			TLSCAFile:     "",
			TLSClientAuth: false,
			// TestConfig leaves RequireAuthz at its zero value (false) while
			// enabling the gRPC and REST transports above. Acknowledge the
			// insecure posture so the authz-posture validation rule does not
			// refuse configs built from this test profile.
			InsecureDevMode:     true,
			FIPSMode:            false,
			EncryptionAlgorithm: "aes-256-gcm",
			SealEnabled:         false,
			Authorization: AuthorizationConfig{
				Provider: "rbac",
				RBAC: RBACAuthzConfig{
					Enabled:        true,
					DefaultRole:    "",
					AllowAnonymous: true,
				},
				ABAC: ABACAuthzConfig{
					Enabled:   false,
					CacheTTL:  1 * time.Minute,
					PolicyDir: "",
				},
				Audit: AuthzAuditConfig{
					Enabled:    false,
					LogDenied:  false,
					LogAllowed: false,
				},
			},
		},
		KMS: KMSConfig{
			Enabled: false,
		},
		Secrets: SecretsConfig{
			Enabled: false,
		},
		TimeSeries: TimeSeriesConfig{
			Enabled: false,
		},
		Vector: VectorConfig{
			Enabled: false,
		},
		Ollama: OllamaConfig{
			Enabled: false,
		},
		Backup: BackupConfig{
			Enabled: false,
		},
		Discovery: DiscoveryConfig{
			Enabled: false,
		},
		Cache: EnhancedCacheConfig{
			Enabled:         true,
			DefaultTTL:      5 * time.Minute,
			MaxSize:         1000,
			CleanupInterval: 1 * time.Minute,
			Entities:        make(map[string]EntityCacheConfig),
		},
		Metrics: MetricsConfig{
			Enabled:         false,
			AdvancedEnabled: false,
		},
		Logging: LoggingConfig{
			Level:     "debug",
			Format:    "json",
			Output:    "stdout",
			File:      "",
			Component: "",
		},
		Scheduler: SchedulerConfig{
			Enabled:           false,
			TickInterval:      1 * time.Second,
			MaxConcurrentJobs: 10,
		},
		Seal: SealConfig{
			Enabled: false,
		},
		CA:   CAConfig{},
		QRDB: DefaultQRDBConfig(),
		Databases: DatabasesConfig{
			DataDir:                     "data",
			MaxPerTenant:                10,
			DefaultBackend:              "memory",
			DefaultEngine:               "kv",
			AllowedEngines:              []string{"kv"},
			AllowedBackends:             []string{"memory"},
			EnablePerDatabaseEncryption: false,
		},
	}
}

// EmbeddedSystemPrincipalDefault is the system identity used for in-process
// calls when the server runs in embedded mode. It matches the reserved
// system-tenant convention and is used to label the composition context so
// daemon-internal operations carry an explicit identity.
const EmbeddedSystemPrincipalDefault = "system"

// EmbeddedConfig returns a configuration optimized for embedded mode usage.
//
// Security posture: all network transports are disabled, so RequireAuthz
// governs no network-facing gate. Embedded callers are trusted in-process
// code; anonymous access is NOT permitted. Callers that need to bypass
// service-layer per-key authorization must use auth.NewSystemContext on the
// request context. InsecureDevMode and explicit AllowAnonymous overrides are
// reserved for development-only builds; production configs must never set
// them.
func EmbeddedConfig() *Config {
	cfg := DefaultConfig()

	// Embedded mode - no network listeners
	cfg.Server.Embedded = true
	cfg.Server.MaxConnections = 0
	cfg.Server.GRPC.Enabled = false
	cfg.Server.GRPC.Port = 0
	cfg.Server.REST.Enabled = false
	cfg.Server.REST.Port = 0
	cfg.Server.MCP.Enabled = false
	cfg.Server.MCP.Port = 0
	cfg.Server.Unix.Enabled = false
	cfg.Server.Unix.Socket = ""

	// Use local storage
	cfg.Storage.DataDir = "./data"
	cfg.Storage.WALDir = "./data/wal"
	cfg.Storage.CacheSize = 128 * 1024 * 1024

	// Secure embedded defaults: all network transports are disabled so
	// RequireAuthz has no effect on the network surface, but we keep it true
	// to document the intent and prevent accidental re-enabling of a transport
	// with the wrong posture.  Anonymous access is explicitly prohibited;
	// embedded callers must carry auth.NewSystemContext or an injected Subject.
	// TLS stays ON even though embedded servers bind no network listener:
	// the CA bootstrap it gates is what issues the platform admin's mTLS
	// credential during first-boot, and a certless admin is forbidden.
	cfg.Security.TLSEnabled = true
	cfg.Security.RequireAuthz = true
	cfg.Security.Authorization.RBAC.AllowAnonymous = false
	// Bind the in-process system identity so service-layer guards that inspect
	// auth.IsSystemContext() can distinguish daemon-internal calls from
	// unauthenticated ones.
	cfg.Security.EmbeddedSystemPrincipal = EmbeddedSystemPrincipalDefault

	// Disable most services
	cfg.KMS.Enabled = false
	cfg.Secrets.Enabled = false
	cfg.Vector.Enabled = false
	cfg.TimeSeries.Enabled = false
	cfg.Ollama.Enabled = false
	cfg.Backup.Enabled = false
	cfg.Discovery.Enabled = false

	return cfg
}
