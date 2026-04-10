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

package service

import (
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// Compile-time check that the test mock satisfies the TimeSeriesStorage interface.
// The concrete implementation (pkg/storage/timeseries stub) uses qrdb type aliases
// and is verified implicitly through its method signatures.
var _ interfaces.TimeSeriesStorage = (*testMockTimeSeriesStorage)(nil)
