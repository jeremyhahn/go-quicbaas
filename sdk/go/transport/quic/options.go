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

package quic

import (
	"crypto/tls"
	"time"
)

// Options holds QUIC-specific transport options.
type Options struct {
	// TLSConfig is the TLS configuration (required for QUIC).
	// QUIC mandates TLS 1.3, so MinVersion will be set automatically.
	TLSConfig *tls.Config

	// HandshakeTimeout is the timeout for the QUIC handshake.
	HandshakeTimeout time.Duration

	// MaxIdleTimeout is the maximum idle time before connection closure.
	MaxIdleTimeout time.Duration

	// KeepAlivePeriod is the keep-alive period for the connection.
	KeepAlivePeriod time.Duration

	// MaxIncomingStreams is the maximum number of concurrent incoming streams.
	MaxIncomingStreams int64

	// MaxIncomingUniStreams is the maximum number of incoming unidirectional streams.
	MaxIncomingUniStreams int64

	// InitialStreamReceiveWindow is the initial receive window for streams.
	InitialStreamReceiveWindow uint64

	// InitialConnectionReceiveWindow is the initial receive window for the connection.
	InitialConnectionReceiveWindow uint64

	// Enable0RTT enables 0-RTT connection resumption (requires session cache).
	Enable0RTT bool

	// AuthToken is the authentication token to include with requests.
	AuthToken string
}

// Option is a functional option for configuring QUIC transport.
type Option func(*Options)

// DefaultOptions returns the default QUIC transport options.
func DefaultOptions() *Options {
	return &Options{
		HandshakeTimeout:               10 * time.Second,
		MaxIdleTimeout:                 30 * time.Second,
		KeepAlivePeriod:                15 * time.Second,
		MaxIncomingStreams:             100,
		MaxIncomingUniStreams:          100,
		InitialStreamReceiveWindow:     512 * 1024,       // 512KB
		InitialConnectionReceiveWindow: 1024 * 1024 * 15, // 15MB
		Enable0RTT:                     false,
	}
}

// WithTLSConfig sets the TLS configuration.
func WithTLSConfig(cfg *tls.Config) Option {
	return func(o *Options) {
		o.TLSConfig = cfg
	}
}

// WithHandshakeTimeout sets the handshake timeout.
func WithHandshakeTimeout(d time.Duration) Option {
	return func(o *Options) {
		o.HandshakeTimeout = d
	}
}

// WithMaxIdleTimeout sets the maximum idle timeout.
func WithMaxIdleTimeout(d time.Duration) Option {
	return func(o *Options) {
		o.MaxIdleTimeout = d
	}
}

// WithKeepAlivePeriod sets the keep-alive period.
func WithKeepAlivePeriod(d time.Duration) Option {
	return func(o *Options) {
		o.KeepAlivePeriod = d
	}
}

// WithMaxIncomingStreams sets the maximum number of incoming streams.
func WithMaxIncomingStreams(n int64) Option {
	return func(o *Options) {
		o.MaxIncomingStreams = n
	}
}

// WithMaxIncomingUniStreams sets the maximum number of incoming unidirectional streams.
func WithMaxIncomingUniStreams(n int64) Option {
	return func(o *Options) {
		o.MaxIncomingUniStreams = n
	}
}

// WithInitialStreamReceiveWindow sets the initial stream receive window.
func WithInitialStreamReceiveWindow(n uint64) Option {
	return func(o *Options) {
		o.InitialStreamReceiveWindow = n
	}
}

// WithInitialConnectionReceiveWindow sets the initial connection receive window.
func WithInitialConnectionReceiveWindow(n uint64) Option {
	return func(o *Options) {
		o.InitialConnectionReceiveWindow = n
	}
}

// WithEnable0RTT enables 0-RTT connection resumption.
func WithEnable0RTT(enable bool) Option {
	return func(o *Options) {
		o.Enable0RTT = enable
	}
}

// WithAuthToken sets the authentication token.
func WithAuthToken(token string) Option {
	return func(o *Options) {
		o.AuthToken = token
	}
}

// Apply applies all options to the Options struct.
func (o *Options) Apply(opts ...Option) {
	for _, opt := range opts {
		opt(o)
	}
}

// Validate validates the options.
func (o *Options) Validate() error {
	// TLS is required for QUIC - but can be nil for testing with insecure mode
	// Validation will happen at transport creation time

	if o.HandshakeTimeout <= 0 {
		o.HandshakeTimeout = 10 * time.Second
	}

	if o.MaxIdleTimeout <= 0 {
		o.MaxIdleTimeout = 30 * time.Second
	}

	if o.MaxIncomingStreams <= 0 {
		o.MaxIncomingStreams = 100
	}

	if o.MaxIncomingUniStreams <= 0 {
		o.MaxIncomingUniStreams = 100
	}

	return nil
}

// Clone creates a deep copy of the options.
func (o *Options) Clone() *Options {
	if o == nil {
		return nil
	}

	clone := &Options{
		HandshakeTimeout:               o.HandshakeTimeout,
		MaxIdleTimeout:                 o.MaxIdleTimeout,
		KeepAlivePeriod:                o.KeepAlivePeriod,
		MaxIncomingStreams:             o.MaxIncomingStreams,
		MaxIncomingUniStreams:          o.MaxIncomingUniStreams,
		InitialStreamReceiveWindow:     o.InitialStreamReceiveWindow,
		InitialConnectionReceiveWindow: o.InitialConnectionReceiveWindow,
		Enable0RTT:                     o.Enable0RTT,
		AuthToken:                      o.AuthToken,
	}

	if o.TLSConfig != nil {
		clone.TLSConfig = o.TLSConfig.Clone()
	}

	return clone
}
