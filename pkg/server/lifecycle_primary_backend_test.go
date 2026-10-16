package server

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

func memoryBackends(names ...string) map[string]*config.StorageBackendConfig {
	backends := make(map[string]*config.StorageBackendConfig, len(names))
	for _, name := range names {
		backends[name] = &config.StorageBackendConfig{Type: "memory", Config: map[string]string{}}
	}
	return backends
}

func newPrimaryBackendTestServer(t *testing.T, primary string, names ...string) *Server {
	t.Helper()
	cfg := createTestConfig(t)
	t.Cleanup(func() { cleanupTestData(t, cfg) })
	cfg.ObjectStorage.Enabled = true
	cfg.ObjectStorage.Backends = memoryBackends(names...)
	cfg.ObjectStorage.PrimaryBackend = primary

	srv, err := NewServer(cfg)
	require.NoError(t, err)
	return srv
}

func stopCatalogComponents(t *testing.T, srv *Server) {
	t.Helper()
	require.NoError(t, srv.stopWorkerManager())
	require.NoError(t, srv.stopBackendManager())
	require.NoError(t, srv.stopCatalogManager())
}

func TestStartCatalogManager_PrimaryBackendResolved(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		primary string
		names   []string
	}{
		{name: "explicit primary among several backends", primary: "cold", names: []string{"hot", "cold"}},
		{name: "automatic default backend", primary: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newPrimaryBackendTestServer(t, tt.primary, tt.names...)

			require.NoError(t, srv.startCatalogManager())
			require.NotNil(t, srv.backendManager)
			stopCatalogComponents(t, srv)
		})
	}
}

func TestStartCatalogManager_SeveralBackendsWithoutPrimaryRefused(t *testing.T) {
	t.Parallel()

	srv := newPrimaryBackendTestServer(t, "", "hot", "cold")

	err := srv.startCatalogManager()
	var unresolvedErr *config.PrimaryBackendUnresolvedError
	require.True(t, errors.As(err, &unresolvedErr), "got %v", err)
	assert.Equal(t, []string{"cold", "hot"}, unresolvedErr.Backends)
	assert.Nil(t, srv.catalogManager)
	assert.Nil(t, srv.backendManager)
}

func TestStartCatalogManager_UnknownPrimaryRefused(t *testing.T) {
	t.Parallel()

	srv := newPrimaryBackendTestServer(t, "missing", "hot", "cold")

	err := srv.startCatalogManager()
	var unknownErr *config.UnknownPrimaryBackendError
	require.True(t, errors.As(err, &unknownErr), "got %v", err)
	assert.Equal(t, "missing", unknownErr.Name)
	assert.Nil(t, srv.catalogManager)
}

func TestNewObjectStoreResolver_CarriesPrimaryBackend(t *testing.T) {
	t.Parallel()

	srv := newPrimaryBackendTestServer(t, "cold", "hot", "cold")

	resolver, err := srv.newObjectStoreResolver()
	require.NoError(t, err)
	backend, err := resolver.ResolveBackend("tenant-a", 0)
	require.NoError(t, err)
	assert.Equal(t, "cold", backend)

	resolved, err := resolver.ResolveConfig("tenant-a", 0)
	require.NoError(t, err)
	assert.Equal(t, []string{"cold", "hot"}, resolved.EnabledBackends)
}

func TestNewObjectStoreResolver_UnresolvedPrimaryRefused(t *testing.T) {
	t.Parallel()

	srv := newPrimaryBackendTestServer(t, "", "hot", "cold")

	resolver, err := srv.newObjectStoreResolver()
	require.Nil(t, resolver)
	var unresolvedErr *config.PrimaryBackendUnresolvedError
	require.True(t, errors.As(err, &unresolvedErr), "got %v", err)
}
