//go:build raft

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// ---------------------------------------------------------------------------
// transportFactory.isEmbedded edge cases
// ---------------------------------------------------------------------------

func TestTransportFactory_IsEmbedded_NilConfig(t *testing.T) {
	t.Run("nil config returns true", func(t *testing.T) {
		factory := newTransportFactory(nil, nil, nil)
		assert.True(t, factory.isEmbedded())
	})
}

// ---------------------------------------------------------------------------
// transportFactory.transportType edge cases
// ---------------------------------------------------------------------------

func TestTransportFactory_TransportType_EdgeCases(t *testing.T) {
	t.Run("nil config returns embedded", func(t *testing.T) {
		factory := newTransportFactory(nil, nil, nil)
		assert.Equal(t, TransportEmbedded, factory.transportType())
	})

	t.Run("empty transport returns embedded", func(t *testing.T) {
		cfg := &config.QRDBConfig{Transport: ""}
		factory := newTransportFactory(cfg, nil, nil)
		assert.Equal(t, TransportEmbedded, factory.transportType())
	})

	t.Run("explicit transport type is preserved", func(t *testing.T) {
		cfg := &config.QRDBConfig{Transport: config.QRDBTransportGRPC}
		factory := newTransportFactory(cfg, nil, nil)
		assert.Equal(t, TransportGRPC, factory.transportType())
	})
}

// ---------------------------------------------------------------------------
// buildTransportOptions - nil and Unix transport paths
// ---------------------------------------------------------------------------

func TestBuildTransportOptions_NilConfig(t *testing.T) {
	t.Run("nil config returns nil options", func(t *testing.T) {
		opts := buildTransportOptions(nil)
		assert.Nil(t, opts)
	})
}

func TestBuildTransportOptions_UnixTransport(t *testing.T) {
	t.Run("unix transport uses socket path as address", func(t *testing.T) {
		cfg := &config.QRDBConfig{
			Transport:      config.QRDBTransportUnix,
			UnixSocketPath: "/var/run/qrdb.sock",
		}
		opts := buildTransportOptions(cfg)
		assert.Len(t, opts, 1)
	})

	t.Run("unix transport with empty socket returns no opts", func(t *testing.T) {
		cfg := &config.QRDBConfig{
			Transport:      config.QRDBTransportUnix,
			UnixSocketPath: "",
		}
		opts := buildTransportOptions(cfg)
		assert.Empty(t, opts)
	})

	t.Run("unix transport with TLS adds two options", func(t *testing.T) {
		cfg := &config.QRDBConfig{
			Transport:      config.QRDBTransportUnix,
			UnixSocketPath: "/var/run/qrdb.sock",
			TLSEnabled:     true,
			TLSCertFile:    "/cert.pem",
			TLSKeyFile:     "/key.pem",
			TLSCAFile:      "/ca.pem",
		}
		opts := buildTransportOptions(cfg)
		assert.Len(t, opts, 2)
	})
}

// ---------------------------------------------------------------------------
// validateNetworkConfig - nil config edge cases
// ---------------------------------------------------------------------------

func TestTransportFactory_ValidateNetworkConfig_NilConfig(t *testing.T) {
	t.Run("nil config with unix transport returns socket error", func(t *testing.T) {
		factory := newTransportFactory(nil, nil, nil)
		err := factory.validateNetworkConfig(TransportUnix)
		assert.Error(t, err)
	})

	t.Run("nil config with network transport returns address error", func(t *testing.T) {
		factory := newTransportFactory(nil, nil, nil)
		err := factory.validateNetworkConfig(TransportGRPC)
		assert.Error(t, err)
	})
}
