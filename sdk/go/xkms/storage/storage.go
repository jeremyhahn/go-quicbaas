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

// Package storage re-exports go-xkms's storage.Backend interface and its
// canonical sentinel errors. Platform code that must implement or reference
// the Backend interface (e.g. the TOFU pin-store adapter in
// pkg/storage/quicbaas) imports this package to stay on the SDK-only
// contract documented in platform/CLAUDE.md §"SDK Consumption Invariant".
//
// All exported names are pure type aliases or variable aliases of the
// canonical go-xkms definitions. There is no transport, no runtime state,
// and no shimming.
package storage

import xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

// Backend is the canonical key-value storage backend interface.
// Implement it to provide a custom storage layer for go-xkms primitives
// such as StoragePinStore. Aliased through the go-xkms SDK frontage
// (xkmssdk.StorageBackend) — never the internal pkg/storage path — so this
// package stays on the SDK-only contract. The local name is kept (callers
// reference storage.Backend) while the underlying type is identical.
type Backend = xkmssdk.StorageBackend

// Sentinel errors returned by Backend implementations. These are the same
// underlying values as the SDK's ErrStorage* names (identity-preserving
// aliases), so errors.Is comparisons across the boundary still hold.
var (
	// ErrNotFound is returned by Get and Delete when the key does not exist.
	ErrNotFound = xkmssdk.ErrStorageNotFound

	// ErrAlreadyExists is returned when an insert would overwrite an existing key
	// and the caller requested fail-on-conflict semantics.
	ErrAlreadyExists = xkmssdk.ErrStorageAlreadyExists

	// ErrClosed is returned when operating on a closed backend.
	ErrClosed = xkmssdk.ErrStorageClosed
)
