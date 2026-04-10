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

//go:build !desktop

package main

import "github.com/jeremyhahn/go-quicbaas/pkg/logger"

// launchGUI returns a GUIUnavailableError when the binary is built without
// the desktop build tag. This allows the main function to detect the stub
// and fall through to CLI mode.
func launchGUI(_ *logger.Logger, _ string) error {
	return &GUIUnavailableError{}
}
