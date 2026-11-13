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

package xkms

import (
	xkmssignstore "github.com/jeremyhahn/go-xkms/pkg/signing/store"
)

// SignerStorer persists crypto.Signers and per-blob signatures keyed by
// KeyAttributes. A go-xkms Backend's SignerStore method returns one.
//
// SignerOpts is not aliased here: xkms.go already exports that name as
// xkmssdk.SignerOpts.
type SignerStorer = xkmssignstore.SignerStorer

// NewSignerStore persists crypto.Signers and per-blob signatures on a
// StorageBackend; a Backend's SignerStore method returns one.
var NewSignerStore = xkmssignstore.NewSignerStore
