package quicbaas_test

import (
	"testing"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestEmbeddedSecretRegistryAutoWire starts an embedded QuicBaaS server the way
// platformd does and asserts the SDK client auto-adopts the secret registry.
func TestEmbeddedSecretRegistryAutoWire(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig()

	cfg.Storage.Backend = config.StorageBackendFile
	cfg.Storage.DataDir = dir
	// Disable network listeners — we only exercise the embedded wiring.
	// Embedded mode: in-process, no network listeners — exactly how platformd runs.
	cfg.Server.Embedded = true

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

	c, err := quicbaas.NewClient("embedded", quicbaas.WithEmbedded(svc))
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
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Storage.Backend = config.StorageBackendFile
	cfg.Storage.DataDir = dir
	cfg.Server.Embedded = true
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
	c, err := quicbaas.NewClient("embedded", quicbaas.WithEmbedded(svc))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if c.Security().SecretResolver() == nil {
		t.Errorf("client Security().SecretResolver() is nil with TLS disabled — kms:// would fail closed")
	}
}
