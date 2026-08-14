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

// Package secrets provides secret resolution capabilities for configuration files.
// It supports multiple resolution strategies including literal values, local/remote KMS,
// and encrypted values.
//
// The core SecretResolver, SecretValue, ResolverRegistry, and ResolvedStrategy types
// are re-exported from the canonical go-qrdb secret package. Only quicbaas-specific
// concrete resolvers (LocalKMSResolver, RemoteKMSResolver) are implemented here.
package secrets

import qrdbsecret "github.com/jeremyhahn/go-qrdb/sdk/go/secret"

// ---------------------------------------------------------------------------
// Canonical type aliases — single source of truth in go-qrdb/pkg/secret
// ---------------------------------------------------------------------------

// ResolvedStrategy identifies how a SecretValue was produced during resolution.
type ResolvedStrategy = qrdbsecret.ResolvedStrategy

// Strategy constants shared with go-qrdb.
const (
	// StrategyLiteral indicates the value was used as-is without transformation.
	StrategyLiteral = qrdbsecret.StrategyLiteral

	// StrategyLocalKMS indicates the value was resolved using local KMS decryption.
	StrategyLocalKMS = qrdbsecret.StrategyLocalKMS

	// StrategyRemoteKMS indicates the value was resolved using remote KMS decryption.
	StrategyRemoteKMS = qrdbsecret.StrategyRemoteKMS

	// StrategyAutoGen indicates the value was generated rather than read from
	// an existing entry. The constant is owned by go-qrdb and re-exported here
	// for resolvers that mint values; quicbaas's own generate-on-first-use path
	// (kms://<path>?ensure=true) reports StrategyLocalKMS because the value is
	// served from the KMS on every call after the first.
	StrategyAutoGen = qrdbsecret.StrategyAutoGen

	// StrategyEncrypted indicates the value was resolved from an encrypted source.
	// This is a quicbaas-specific extension not present in go-qrdb.
	StrategyEncrypted ResolvedStrategy = "encrypted"
)

// SecretValue holds a resolved secret with its backing plaintext bytes and
// resolution metadata. Call Clear when done to reduce heap plaintext exposure.
// Plaintext is carried as bytes, not a retained string, to support zeroization.
type SecretValue = qrdbsecret.SecretValue

// SecretResolver resolves raw secret reference URIs to their plaintext values.
// Implementations handle a specific URI scheme (e.g. "kms://", "remote-kms://").
type SecretResolver = qrdbsecret.SecretResolver

// ResolverRegistry dispatches secret resolution to the first registered
// SecretResolver whose CanResolve returns true for the raw value.
// It is safe for concurrent use.
type ResolverRegistry = qrdbsecret.ResolverRegistry

// NewResolverRegistry creates an empty ResolverRegistry.
var NewResolverRegistry = qrdbsecret.NewResolverRegistry

// NewSecretValue constructs a SecretValue from a raw reference, byte slice, and
// strategy. The bytes are copied into a private backing slice so callers can
// safely reuse or zero their buffer independently.
var NewSecretValue = qrdbsecret.NewSecretValue
