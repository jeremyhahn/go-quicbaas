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

// Package unix provides a Unix Domain Socket transport for the QuicBaaS SDK.
//
// This transport wraps the gRPC transport and provides lower latency for local
// connections by using Unix sockets instead of TCP. Unix sockets avoid the
// TCP/IP stack overhead, making them ideal for same-host communication.
// Unix sockets use insecure gRPC credentials since TLS is not needed for
// local IPC.
//
// Usage:
//
//	cfg := &transport.Config{
//		Address: "/var/run/quicbaas.sock",
//		Timeout: 5 * time.Second,
//	}
//	t, err := unix.New(cfg)
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer t.Close()
//
// The transport automatically normalizes socket paths for gRPC dialing,
// accepting both raw paths (e.g., "/var/run/quicbaas.sock") and prefixed
// paths (e.g., "unix:///var/run/quicbaas.sock").
package unix
