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

//go:build integration

package helpers

import (
	"fmt"
	"os"
	"testing"
)

// ErrEmbeddedServiceNotAvailable is returned when the embedded service is not available
// for integration testing. This typically occurs when running integration tests against
// Docker containers instead of an in-process embedded service.
type ErrEmbeddedServiceNotAvailable struct{}

func (e ErrEmbeddedServiceNotAvailable) Error() string {
	return "embedded service not available for integration tests (tests run against Docker containers)"
}

// EmbeddedServer is a placeholder type for the embedded server.
// In a real implementation, this would hold the actual server instance.
type EmbeddedServer struct{}

// embeddedServiceRegistry holds a registered embedded service instance.
// Test packages that initialize an embedded server can register it here
// for use by cross-protocol tests that call GetEmbeddedService.
var embeddedServiceRegistry struct {
	svc interface{}
	set bool
}

// RegisterEmbeddedService registers an embedded QuicBaaS service for use by
// integration tests. This is called by test packages that initialize an
// embedded server (e.g., test/integration/embedded) to make the service
// available to cross-protocol tests via GetEmbeddedService.
func RegisterEmbeddedService(svc interface{}) {
	embeddedServiceRegistry.svc = svc
	embeddedServiceRegistry.set = true
}

// IsEmbeddedAvailable returns true if the embedded service has been
// successfully registered and is available for multi-protocol testing.
// This is used by multi-protocol test runners to conditionally include
// the Embedded protocol in their test table. This is NOT a skip guard -
// it determines which protocols are testable in the current environment.
func IsEmbeddedAvailable() bool {
	return os.Getenv("QUICBAAS_EMBEDDED_ENABLED") == "true" && embeddedServiceRegistry.set
}

// GetEmbeddedService returns an embedded QuicBaaS service for testing.
// Returns an error if the embedded service is not available. The service
// becomes available when:
//  1. QUICBAAS_EMBEDDED_ENABLED is set to "true", AND
//  2. A test package has registered the service via RegisterEmbeddedService.
//
// For standalone embedded tests, use the test/integration/embedded package
// which starts its own in-process server via TestMain.
func GetEmbeddedService(t *testing.T) (interface{}, error) {
	t.Helper()

	// Check if embedded service is explicitly enabled via environment variable
	if os.Getenv("QUICBAAS_EMBEDDED_ENABLED") != "true" {
		return nil, ErrEmbeddedServiceNotAvailable{}
	}

	// Check if a service has been registered
	if embeddedServiceRegistry.set {
		return embeddedServiceRegistry.svc, nil
	}

	return nil, fmt.Errorf("embedded service enabled but not registered; use test/integration/embedded package for embedded tests")
}

// IsTLSAvailable returns true if TLS certificates are configured for
// integration testing. TLS requires QUICBAAS_TLS_CERT, QUICBAAS_TLS_KEY,
// and QUICBAAS_TLS_CA environment variables to be set.
// This is used by tests that require TLS to conditionally include
// TLS-dependent test cases. This is NOT a skip guard - it determines
// which test configurations are valid in the current environment.
func IsTLSAvailable() bool {
	return os.Getenv("QUICBAAS_TLS_CERT") != "" && os.Getenv("QUICBAAS_TLS_KEY") != ""
}

// IsBackupAvailable returns true if the backup service is expected to be
// available in the integration test environment. Backup requires server-side
// backup manager initialization and storage configuration.
func IsBackupAvailable() bool {
	return os.Getenv("QUICBAAS_BACKUP_ENABLED") == "true"
}

// IsMessagingAvailable returns true if the messaging service is expected to be
// available in the integration test environment. Messaging requires server-side
// service initialization with broker configuration.
func IsMessagingAvailable() bool {
	return os.Getenv("QUICBAAS_MESSAGING_ENABLED") == "true"
}

// GetTestEndpoint returns the primary test endpoint
func GetTestEndpoint() string {
	endpoint := "localhost:50051"
	// Override with environment variable if set
	if ep := GetClusterEndpoints(); len(ep) > 0 {
		endpoint = ep[0]
	}
	return endpoint
}

// GetClusterEndpoints returns cluster endpoints from environment or defaults
func GetClusterEndpoints() []string {
	// Try gRPC-specific environment variables first (used in docker-compose.integration.yml)
	node1 := os.Getenv("QUICBAAS_NODE1_GRPC")
	if node1 == "" {
		// Fall back to non-gRPC specific variable
		node1 = os.Getenv("QUICBAAS_NODE1")
	}
	if node1 == "" {
		node1 = "localhost:50051"
	}

	node2 := os.Getenv("QUICBAAS_NODE2_GRPC")
	if node2 == "" {
		node2 = os.Getenv("QUICBAAS_NODE2")
	}
	if node2 == "" {
		node2 = "localhost:50052"
	}

	node3 := os.Getenv("QUICBAAS_NODE3_GRPC")
	if node3 == "" {
		node3 = os.Getenv("QUICBAAS_NODE3")
	}
	if node3 == "" {
		node3 = "localhost:50053"
	}

	return []string{node1, node2, node3}
}

// GetRESTEndpoints returns all REST API endpoints for testing
func GetRESTEndpoints() []string {
	node1 := os.Getenv("QUICBAAS_NODE1_URL")
	if node1 == "" {
		node1 = "https://test-node1:8080"
	}

	node2 := os.Getenv("QUICBAAS_NODE2_URL")
	if node2 == "" {
		node2 = "https://test-node2:8080"
	}

	node3 := os.Getenv("QUICBAAS_NODE3_URL")
	if node3 == "" {
		node3 = "https://test-node3:8080"
	}

	return []string{node1, node2, node3}
}

// GetQUICEndpoints returns all QUIC endpoints for testing
func GetQUICEndpoints() []string {
	node1 := os.Getenv("QUICBAAS_NODE1_QUIC")
	if node1 == "" {
		node1 = "test-node1:4433"
	}

	node2 := os.Getenv("QUICBAAS_NODE2_QUIC")
	if node2 == "" {
		node2 = "test-node2:4433"
	}

	node3 := os.Getenv("QUICBAAS_NODE3_QUIC")
	if node3 == "" {
		node3 = "test-node3:4433"
	}

	return []string{node1, node2, node3}
}

// GetMCPEndpoints returns all MCP endpoints for testing. The cluster's MCP
// listener terminates TLS using the same chain as the REST/gRPC/QUIC listeners
// (see pkg/api/mcp/server.go Config.TLSConfig), so endpoints are https://
// against the self-signed integration CA. Test clients must skip cert
// verification (TLSClientConfig{InsecureSkipVerify: true}) when dialing.
func GetMCPEndpoints() []string {
	node1 := os.Getenv("QUICBAAS_NODE1_MCP")
	if node1 == "" {
		node1 = "https://test-node1:8888/mcp"
	}

	node2 := os.Getenv("QUICBAAS_NODE2_MCP")
	if node2 == "" {
		node2 = "https://test-node2:8888/mcp"
	}

	node3 := os.Getenv("QUICBAAS_NODE3_MCP")
	if node3 == "" {
		node3 = "https://test-node3:8888/mcp"
	}

	return []string{node1, node2, node3}
}

// GetUnixSocketEndpoints returns all Unix socket paths for testing
func GetUnixSocketEndpoints() []string {
	node1 := os.Getenv("QUICBAAS_NODE1_UNIX")
	if node1 == "" {
		node1 = "/var/run/quicbaas/quicbaas.sock"
	}

	node2 := os.Getenv("QUICBAAS_NODE2_UNIX")
	if node2 == "" {
		node2 = "/var/run/quicbaas/quicbaas.sock"
	}

	node3 := os.Getenv("QUICBAAS_NODE3_UNIX")
	if node3 == "" {
		node3 = "/var/run/quicbaas/quicbaas.sock"
	}

	return []string{node1, node2, node3}
}

// GetUnixSocketPath returns the primary Unix socket path for testing.
// This is a convenience function that returns the first Unix socket endpoint.
func GetUnixSocketPath() string {
	endpoints := GetUnixSocketEndpoints()
	if len(endpoints) > 0 {
		return endpoints[0]
	}
	return "/var/run/quicbaas/quicbaas.sock"
}
