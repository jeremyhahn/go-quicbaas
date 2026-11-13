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

package config

// DefaultRESTPort is the defaults-layer value of server.rest.port. Code that
// needs the REST port when none is configured reads this constant, never a
// second literal that can drift from it.
const DefaultRESTPort = 8443

// DefaultQRDBQUICPort is the defaults-layer value of server.qrdb_quic.port:
// the UDP port of go-qrdb's native QUIC listener. It is 0, kernel-assigned,
// so the default can never collide with REST's HTTP/3 on UDP DefaultRESTPort
// or with another node on the host. Operators who publish the listener pin
// a port.
const DefaultQRDBQUICPort = 0
