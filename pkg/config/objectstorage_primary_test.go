package config

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func primaryTestBackends(names ...string) map[string]*StorageBackendConfig {
	backends := make(map[string]*StorageBackendConfig, len(names))
	for _, name := range names {
		backends[name] = &StorageBackendConfig{Type: "memory"}
	}
	return backends
}

func TestResolvePrimaryBackend_Resolves(t *testing.T) {
	tests := []struct {
		name string
		cfg  ObjectStorageConfig
		want string
	}{
		{
			name: "explicit primary among several backends",
			cfg:  ObjectStorageConfig{Backends: primaryTestBackends("hot", "cold"), PrimaryBackend: "cold"},
			want: "cold",
		},
		{
			name: "explicit primary with one backend",
			cfg:  ObjectStorageConfig{Backends: primaryTestBackends("hot"), PrimaryBackend: "hot"},
			want: "hot",
		},
		{
			name: "unset with the single automatic default backend",
			cfg:  ObjectStorageConfig{Enabled: true, Backends: primaryTestBackends("default")},
			want: "default",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.cfg.ResolvePrimaryBackend()
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolvePrimaryBackend_UnknownPrimaryRefused(t *testing.T) {
	cfg := ObjectStorageConfig{Backends: primaryTestBackends("hot", "cold"), PrimaryBackend: "missing"}

	got, err := cfg.ResolvePrimaryBackend()
	require.Error(t, err)
	assert.Empty(t, got)

	var unknownErr *UnknownPrimaryBackendError
	require.True(t, errors.As(err, &unknownErr))
	assert.Equal(t, "missing", unknownErr.Name)
	assert.Equal(t, []string{"cold", "hot"}, unknownErr.Backends)
	assert.Contains(t, err.Error(), `"missing"`)
	assert.Contains(t, err.Error(), "cold, hot")
}

func TestResolvePrimaryBackend_UnresolvedRefused(t *testing.T) {
	tests := []struct {
		name     string
		cfg      ObjectStorageConfig
		backends []string
		message  string
	}{
		{
			name:     "several backends without a primary",
			cfg:      ObjectStorageConfig{Backends: primaryTestBackends("hot", "cold", "archive")},
			backends: []string{"archive", "cold", "hot"},
			message:  "archive, cold, hot",
		},
		{
			name:     "no backends",
			cfg:      ObjectStorageConfig{},
			backends: []string{},
			message:  "no backends are configured",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.cfg.ResolvePrimaryBackend()
			require.Error(t, err)
			assert.Empty(t, got)

			var unresolvedErr *PrimaryBackendUnresolvedError
			require.True(t, errors.As(err, &unresolvedErr))
			assert.Equal(t, tt.backends, unresolvedErr.Backends)
			assert.Contains(t, err.Error(), tt.message)
		})
	}
}
