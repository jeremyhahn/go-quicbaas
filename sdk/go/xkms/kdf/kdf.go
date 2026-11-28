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

// Package kdf re-exports go-xkms's password hasher (the FIPS-aware
// password-based key derivation function) and its sentinel errors. Platform
// code that derives keys from passwords imports this package to stay on the
// SDK-only contract documented in platform/CLAUDE.md §"SDK Consumption
// Invariant".
//
// All exported names are pure type aliases, variable aliases or one-line
// forwarders of the canonical go-xkms definitions, taken through the go-xkms
// SDK frontage (never the internal pkg/crypto/kdf path). There is no
// transport, no runtime state, and no shimming.
package kdf

import xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

// PasswordHasher derives keys from passwords with the KDF the build's FIPS
// mode selects. Aliased through the go-xkms SDK frontage
// (xkmssdk.PasswordHasher), so the underlying type is identical.
type PasswordHasher = xkmssdk.PasswordHasher

// HashVersion identifies the KDF algorithm and parameters a key was derived
// with; store it beside the salt so DeriveKey can reproduce the key.
type HashVersion = xkmssdk.PasswordHashVersion

// PasswordKeyParams carries the parameters of a password-derived key.
type PasswordKeyParams = xkmssdk.PasswordKeyParams

// NewFIPSAwareHasher returns a PasswordHasher that uses the FIPS-approved
// KDF when the build runs in FIPS mode, and the default KDF otherwise.
func NewFIPSAwareHasher() *PasswordHasher { return xkmssdk.NewFIPSAwareHasher() }

// Sentinel errors returned by PasswordHasher. These are the same underlying
// values as the SDK's ErrPasswordHash* names (identity-preserving aliases),
// so errors.Is comparisons across the boundary still hold.
var (
	// ErrInvalidVersion is returned when the hash version is unknown.
	ErrInvalidVersion = xkmssdk.ErrPasswordHashInvalidVersion

	// ErrInvalidPassword is returned when the password is empty.
	ErrInvalidPassword = xkmssdk.ErrPasswordHashInvalidPassword

	// ErrInvalidSalt is returned when the salt is too short.
	ErrInvalidSalt = xkmssdk.ErrPasswordHashInvalidSalt

	// ErrParamsMismatch is returned when stored key parameters do not match.
	ErrParamsMismatch = xkmssdk.ErrPasswordHashParamsMismatch
)
