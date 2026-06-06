// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

// Package bootstrap re-exports go-xkms's canonical trust-bootstrap
// dispatch so downstream callers (the QuicBaaS SDK Trust accessor,
// go-trusted-platform's bootstrap client, the xkmsctl CLI when
// reached through the SDK) talk to trust bootstrap through the SDK
// path rather than reaching into go-xkms directly.
//
// All types are pure aliases of `xkms/pkg/trust/bootstrap`. There is
// no transport, no client, no shimming — the package exists solely so
// the platform repo can write
//
//	import bootstrap "github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/trust/bootstrap"
//
// and stay on the SDK-only contract documented in
// platform/CLAUDE.md §"SDK Consumption Invariant".
package bootstrap

import xkmsbootstrap "github.com/jeremyhahn/go-xkms/pkg/trust/bootstrap"

// =============================================================================
// Mode + constants
// =============================================================================

// Mode is re-exported from xkms.
type Mode = xkmsbootstrap.Mode

const (
	ModeAuto   = xkmsbootstrap.ModeAuto
	ModeDANE   = xkmsbootstrap.ModeDANE
	ModeNoise  = xkmsbootstrap.ModeNoise
	ModeSPKI   = xkmsbootstrap.ModeSPKI
	ModeDirect = xkmsbootstrap.ModeDirect
)

// DefaultNoisePort is the canonical Noise bootstrap port.
const DefaultNoisePort = xkmsbootstrap.DefaultNoisePort

// =============================================================================
// Config / sub-configs / result
// =============================================================================

type (
	Config       = xkmsbootstrap.Config
	DANEConfig   = xkmsbootstrap.DANEConfig
	NoiseConfig  = xkmsbootstrap.NoiseConfig
	SPKIConfig   = xkmsbootstrap.SPKIConfig
	DirectConfig = xkmsbootstrap.DirectConfig
	CABundle     = xkmsbootstrap.CABundle
)

// =============================================================================
// Errors
// =============================================================================

var (
	ErrInvalidMode       = xkmsbootstrap.ErrInvalidMode
	ErrModeNotConfigured = xkmsbootstrap.ErrModeNotConfigured
	ErrServerURLRequired = xkmsbootstrap.ErrServerURLRequired
	ErrFetch             = xkmsbootstrap.ErrFetch
)

// FetchError is re-exported from xkms.
type FetchError = xkmsbootstrap.FetchError

// =============================================================================
// Functions
// =============================================================================

var (
	Fetch           = xkmsbootstrap.Fetch
	DeriveNoiseAddr = xkmsbootstrap.DeriveNoiseAddr
)
