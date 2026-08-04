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

package secrets

import qrdbsecret "github.com/jeremyhahn/go-qrdb/sdk/go/secret"

// ---------------------------------------------------------------------------
// Canonical type aliases — single source of truth in go-qrdb/pkg/secret
// ---------------------------------------------------------------------------

// ConfigProcessor walks configuration structs via reflection and resolves
// exported string fields that contain known-scheme secret URIs in place.
// The canonical implementation lives in go-qrdb/pkg/secret; this is a
// re-export for quicbaas consumers.
type ConfigProcessor = qrdbsecret.ConfigProcessor

// ProcessorOption is a functional option for ConfigProcessor.
type ProcessorOption = qrdbsecret.ProcessorOption

// ProcessResult summarises the outcome of a ConfigProcessor.Process walk.
// Resolved is the count of string fields successfully resolved and written
// back in place (not a slice of per-field records).
type ProcessResult = qrdbsecret.ProcessResult

// NewConfigProcessor creates a ConfigProcessor backed by reg.
// Options are applied in order. Pass WithFailClosed to treat any unresolved
// reference as a hard error.
var NewConfigProcessor = qrdbsecret.NewConfigProcessor

// WithFailClosed returns a ProcessorOption that makes Process return
// ErrUnresolvedSecretURI alongside the ProcessResult when any secret
// reference could not be resolved.
var WithFailClosed = qrdbsecret.WithFailClosed

// ---------------------------------------------------------------------------
// Error types (re-exported from go-qrdb/pkg/secret)
// ---------------------------------------------------------------------------

// ProcessorInputError is returned by ConfigProcessor.Process when the
// cfgPtr argument is nil, not a pointer, or points to a non-struct value.
type ProcessorInputError = qrdbsecret.ProcessorInputError

// ErrUnresolvedSecretURI is returned by ConfigProcessor.Process under
// WithFailClosed when any secret reference could not be resolved.
var ErrUnresolvedSecretURI = qrdbsecret.ErrUnresolvedSecretURI
