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

// BootstrapServicer is the trust-bootstrap facade the REST handlers
// consult for /api/v1/bootstrap endpoints. The Server stores a value
// of this type via SetBootstrapService (called by the lifecycle when
// the truststrap subsystem is online); handlers report 503 Service
// Unavailable when the value is nil.
//
// The interface is empty today because the handler implementations
// currently emit 503 unconditionally — they don't yet call into a
// real method surface. Methods will land here as the
// truststrap-subsystem REST surface is fleshed out. Keeping the
// interface empty rather than removing it preserves the
// SetBootstrapService wiring that lifecycle.go already calls.
type BootstrapServicer interface{}
