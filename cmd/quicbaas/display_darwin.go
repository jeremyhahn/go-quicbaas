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

//go:build darwin

package main

import "os"

// hasDisplay returns true if a graphical display is available on macOS.
// On macOS, a display is always available unless connected via SSH.
func hasDisplay() bool {
	return os.Getenv("SSH_TTY") == ""
}
