//go:build !quicbaas_signal

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

// Package signal integrates go-signal as a multi-tenant Signal messaging
// service into QuicBaaS.
//
// This file ensures the signal package compiles when the signal build tag
// is not active. All service types and constructors are unavailable without
// the tag.
package signal
