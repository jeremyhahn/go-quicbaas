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

package quicbaas

import (
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/server"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Server Type Aliases
// =============================================================================

// Server is the QuicBaaS server that orchestrates all components including
// storage, Raft consensus, API servers, KMS, secrets, and more. Use NewServer
// to create an instance and call Start to begin serving.
type Server = server.Server

// QuicBaaSService is the unified service facade providing all database operations.
// Returned by Server.Service() for direct in-process access to the database.
type QuicBaaSService = service.QuicBaaS

// ServerOption configures optional behavior on the Server during construction.
// Use WithCA and WithTLSCertificate to inject external components.
type ServerOption = server.ServerOption

// =============================================================================
// Configuration Type Aliases
// =============================================================================

// ServerFullConfig is the complete QuicBaaS configuration. It is named
// ServerFullConfig to avoid conflict with the numerous request/response
// Config types already exported in this package.
type ServerFullConfig = config.Config

// ServerNodeConfig contains HTTP/gRPC/QUIC/MCP server listener configuration.
type ServerNodeConfig = config.ServerConfig

// ServerGRPCConfig contains gRPC server configuration.
type ServerGRPCConfig = config.ServerGRPCConfig

// ServerRESTConfig contains REST/HTTP server configuration.
type ServerRESTConfig = config.ServerRESTConfig

// ServerQUICConfig contains QUIC server configuration.
type ServerQUICConfig = config.ServerQUICConfig

// ServerMCPConfig contains MCP (Model Context Protocol) server configuration.
type ServerMCPConfig = config.ServerMCPConfig

// ServerUnixConfig contains Unix domain socket server configuration.
type ServerUnixConfig = config.ServerUnixConfig

// DevClusterConfig configures local multi-node development clusters.
type DevClusterConfig = config.DevClusterConfig

// DevClusterPorts defines base ports for DevCluster auto-increment.
type DevClusterPorts = config.DevClusterPorts

// RaftConfig contains QuicRaft cluster configuration.
type RaftConfig = config.RaftConfig

// StorageConfig contains storage engine configuration.
type StorageConfig = config.StorageConfig

// SecurityConfig contains security and encryption settings.
type SecurityConfig = config.SecurityConfig

// AuthConfig contains authentication configuration.
type AuthConfig = config.AuthConfig

// AuthorizationConfig contains authorization provider configuration.
type AuthorizationConfig = config.AuthorizationConfig

// RBACAuthzConfig contains RBAC-specific authorization settings.
type RBACAuthzConfig = config.RBACAuthzConfig

// ABACAuthzConfig contains ABAC-specific authorization settings.
type ABACAuthzConfig = config.ABACAuthzConfig

// AuthzAuditConfig contains authorization audit logging settings.
type AuthzAuditConfig = config.AuthzAuditConfig

// LoggingConfig contains logging configuration.
type LoggingConfig = config.LoggingConfig

// DiscoveryConfig contains service discovery configuration.
type DiscoveryConfig = config.DiscoveryConfig

// MulticastDiscoveryConfig configures UDP multicast peer discovery.
type MulticastDiscoveryConfig = config.MulticastDiscoveryConfig

// MetricsConfig contains metrics and observability configuration.
type MetricsConfig = config.MetricsConfig

// CodecConfig contains codec-related configuration.
type CodecConfig = config.CodecConfig

// BackupConfig contains backup and restore configuration.
type BackupConfig = config.BackupConfig

// ObjectStorageConfig defines named storage backends referenced by components.
type ObjectStorageConfig = config.ObjectStorageConfig

// StorageBackendConfig defines configuration for a single storage backend.
type StorageBackendConfig = config.StorageBackendConfig

// EnhancedCacheConfig provides entity-level cache configuration support.
type EnhancedCacheConfig = config.EnhancedCacheConfig

// EntityCacheConfig defines cache configuration for a specific entity type.
type EntityCacheConfig = config.EntityCacheConfig

// SchedulerConfig contains job scheduler configuration.
type SchedulerConfig = config.SchedulerConfig

// MessagingConfig contains messaging and watch bridge configuration.
type MessagingConfig = config.MessagingConfig

// WatchBridgeConfig contains watch bridge specific configuration.
type WatchBridgeConfig = config.WatchBridgeConfig

// WatchBridgeMappingConfig defines a single key prefix to channel mapping.
type WatchBridgeMappingConfig = config.WatchBridgeMappingConfig

// KMSConfig contains all KMS-related configuration.
type KMSConfig = config.KMSConfig

// KMSBackendType is an alias for go-xkms's BackendType used in KMS configuration.
type KMSBackendType = config.BackendType

// SecretsConfig contains configuration for the secrets engine.
type SecretsConfig = config.SecretsConfig

// VectorConfig contains complete Vector Database configuration.
type VectorConfig = config.VectorConfig

// HNSWConfig configures the HNSW index parameters.
type HNSWConfig = config.HNSWConfig

// OllamaConfig holds Ollama integration configuration.
type OllamaConfig = config.OllamaConfig

// RAGConfig holds RAG system configuration.
type RAGConfig = config.RAGConfig

// PartitionsConfig maps partition names to their configurations.
type PartitionsConfig = config.PartitionsConfig

// PartitionConfig defines a storage partition with its own data directory.
type PartitionConfig = config.PartitionConfig

// MCPAuthorizationConfig contains MCP authorization settings.
type MCPAuthorizationConfig = config.MCPAuthorizationConfig

// MCPDynamicRegistrationConfig contains DCR settings.
type MCPDynamicRegistrationConfig = config.MCPDynamicRegistrationConfig

// ConfigLoader handles configuration loading from multiple sources.
type ConfigLoader = config.Loader

// =============================================================================
// Server Constructor
// =============================================================================

// NewServer creates a new QuicBaaS server instance from the provided configuration.
// After creation, call Start() to initialize and run all server components.
//
// Example (embedded mode):
//
//	cfg := quicbaas.EmbeddedServerConfig()
//	cfg.Storage.Backend = "memory"
//	cfg.Server.Mode = "standalone"
//
//	srv, err := quicbaas.NewServer(cfg)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	if err := srv.Start(); err != nil {
//	    log.Fatal(err)
//	}
//	defer srv.Shutdown(context.Background())
//
//	svc := srv.Service() // Direct in-process database access
var NewServer = server.NewServer

// WithCA returns a ServerOption that injects an external Certificate Authority.
// When provided, the server skips its internal CA bootstrap and uses the given
// CA for TLS certificate operations.
var WithCA = server.WithCA

// WithTLSCertificate returns a ServerOption that provides a pre-issued TLS
// certificate, skipping certificate issuance from the CA.
var WithTLSCertificate = server.WithTLSCertificate

// =============================================================================
// Configuration Factory Functions
// =============================================================================

// EmbeddedServerConfig returns a configuration optimized for embedded mode usage.
// All network listeners are disabled; access the database directly via Server.Service().
var EmbeddedServerConfig = config.EmbeddedConfig

// DefaultServerConfig returns the default configuration with all features enabled.
var DefaultServerConfig = config.DefaultConfig

// DevelopmentServerConfig returns a development-friendly configuration with
// relaxed security settings and debug logging.
var DevelopmentServerConfig = config.DevelopmentConfig

// ProductionServerConfig returns a production-ready configuration using cluster
// mode for distributed Raft consensus with PebbleDB storage.
var ProductionServerConfig = config.ProductionConfig

// TestServerConfig returns a configuration suitable for testing with in-memory
// storage, random ports, and minimal resource usage.
var TestServerConfig = config.TestConfig

// NewConfigLoader creates a new configuration loader that supports loading
// from files, environment variables, and CLI flags.
var NewConfigLoader = config.NewLoader

// LoadConfigFromFile is a convenience function to load configuration from a specific file.
var LoadConfigFromFile = config.LoadFromFile

// LoadConfigFromEnv loads configuration from environment variables only,
// using defaults as a base.
var LoadConfigFromEnv = config.LoadFromEnv

// DefaultKMSConfig returns the default KMS configuration with all sub-configs
// (Cache, Safety, Defaults, Raft, ClusterDEK, Software, TPM2, PKCS11, etc.)
// properly initialized.
var DefaultKMSConfig = config.DefaultKMSConfig

// =============================================================================
// KMS Backend Type Variables
// =============================================================================
//
// Note: Server mode constants (ServerModeStandalone, ServerModeCluster,
// ServerModeDevCluster) and storage backend constants (StorageBackendMemory,
// StorageBackendFile, StorageBackendPebble) are declared in constants.go.

var (
	// KMSBackendSoftware uses go-xkms's software backend with KVStore persistence.
	KMSBackendSoftware = config.BackendTypeSoftware

	// KMSBackendPKCS11 uses PKCS#11 (HSM) for key management.
	KMSBackendPKCS11 = config.BackendTypePKCS11

	// KMSBackendTPM2 uses TPM 2.0 hardware for key protection.
	KMSBackendTPM2 = config.BackendTypeTPM2

	// KMSBackendAWSKMS uses AWS Key Management Service.
	KMSBackendAWSKMS = config.BackendTypeAWSKMS

	// KMSBackendGCPKMS uses Google Cloud KMS.
	KMSBackendGCPKMS = config.BackendTypeGCPKMS

	// KMSBackendAzureKV uses Azure Key Vault.
	KMSBackendAzureKV = config.BackendTypeAzureKV

	// KMSBackendVault uses HashiCorp Vault.
	KMSBackendVault = config.BackendTypeVault
)
