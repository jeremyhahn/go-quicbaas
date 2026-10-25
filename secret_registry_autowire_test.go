package quicbaas_test

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// TestEmbeddedSecretRegistryAutoWire starts an embedded QuicBaaS server the way
// platformd does and asserts the SDK client auto-adopts the secret registry.
// testTrustDomain is the SPIFFE trust domain the embedded servers in this file
// issue their first-boot admin certificate under.
const testTrustDomain = "quicbaas.example.test"

// embeddedConfig is an in-process server configuration, exactly how platformd
// runs: embedded mode with every network listener off, so the test binds no
// port and cannot collide with another server on the host.
func embeddedConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Storage.Backend = config.StorageBackendFile
	cfg.Storage.DataDir = t.TempDir()
	cfg.Server.Embedded = true
	cfg.Server.REST.Enabled = false
	cfg.Server.GRPC.Enabled = false
	cfg.Server.MCP.Enabled = false
	if cfg.NetworkServing() {
		t.Fatal("embedded test configuration still serves a network listener")
	}
	// First-boot issues the admin certificate and fails closed without a trust domain.
	cfg.IAM.SPIFFETrustDomain = testTrustDomain
	return cfg
}

func TestEmbeddedSecretRegistryAutoWire(t *testing.T) {
	cfg := embeddedConfig(t)

	srv, err := quicbaas.NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = srv.Shutdown(t.Context()) }()

	if srv.GetSecretRegistry() == nil {
		t.Errorf("server.GetSecretRegistry() is nil")
	}

	svc := srv.Service()
	if svc == nil {
		t.Fatalf("Service() is nil")
	}
	if svc.SecretRegistry() == nil {
		t.Errorf("service.SecretRegistry() is nil")
	}

	c, err := quicbaas.NewClient("embedded", quicbaas.WithEmbedded(svc),
		quicbaas.WithEmbeddedIdentity(quicbaas.ServicePrincipalIdentity(srv)))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if c.Security().SecretResolver() == nil {
		t.Errorf("client Security().SecretResolver() is nil — kms:// would fail closed")
	}
}

// TestEmbeddedSecretRegistryAutoWireTLSDisabled reproduces platformd's exact
// embedded mode (in-process, TLS disabled). initializeCA returns early on that
// path, so the secret registry must be built independently of CA bootstrap —
// otherwise kms:// URIs fail closed and platformd FATALs on startup.
func TestEmbeddedSecretRegistryAutoWireTLSDisabled(t *testing.T) {
	cfg := embeddedConfig(t)
	cfg.Security.TLSEnabled = false

	srv, err := quicbaas.NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if err := srv.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = srv.Shutdown(t.Context()) }()

	if srv.GetSecretRegistry() == nil {
		t.Errorf("server.GetSecretRegistry() is nil with TLS disabled")
	}
	svc := srv.Service()
	if svc == nil {
		t.Fatalf("Service() is nil")
	}
	if svc.SecretRegistry() == nil {
		t.Errorf("service.SecretRegistry() is nil with TLS disabled")
	}
	c, err := quicbaas.NewClient("embedded", quicbaas.WithEmbedded(svc),
		quicbaas.WithEmbeddedIdentity(quicbaas.ServicePrincipalIdentity(srv)))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if c.Security().SecretResolver() == nil {
		t.Errorf("client Security().SecretResolver() is nil with TLS disabled — kms:// would fail closed")
	}
}
