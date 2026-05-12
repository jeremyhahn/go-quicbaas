//go:build rest

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

package rest

import (
	"crypto/rand"
	"io"
)

// sshCryptoRand returns the process-wide crypto/rand.Reader. Kept as
// a small helper so handlers_ssh.go does not import crypto/rand
// directly — this makes swapping in a deterministic reader for tests a
// single-line change.
func sshCryptoRand() io.Reader { return rand.Reader }
