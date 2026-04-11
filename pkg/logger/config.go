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

package logger

import (
	sdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// LogConfig contains configuration for the logger.
// It re-exports the canonical type from go-qrdb.
type LogConfig = sdk.LogConfig

// Default returns a default LogConfig with sensible values.
var Default = sdk.DefaultLogConfig
