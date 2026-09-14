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

//go:build !unix
// +build !unix

package main

import (
	"errors"
	"time"
)

// pollSupported reports whether waitReadable can bound a read on this
// platform. It cannot here, so boundedReader falls back to the reader's own
// deadline support and, failing that, to an unbounded read.
const pollSupported = false

// errPollUnsupported is returned by waitReadable on platforms without a
// poll(2) equivalent wired up.
var errPollUnsupported = errors.New("passphrase: descriptor polling is not supported on this platform")

// waitReadable is not implemented here. Windows console and pipe handles
// need a different mechanism (overlapped I/O or WaitForSingleObject), which
// no caller needs yet — the CLI's --passphrase-stdin path is exercised on
// unix. Callers must check pollSupported before relying on this.
func waitReadable(_ uintptr, _ time.Time) error {
	return errPollUnsupported
}
