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

// Package proto provides cluster management client extensions.
// This file provides a compatibility wrapper for tests that use QuicBaaSClientExt.
// The main QuicBaaSClient interface now includes all cluster management methods.
package proto

import (
	"google.golang.org/grpc"
)

// QuicBaaSClientExt is now just an alias for QuicBaaSClient since all methods
// are included in the generated protobuf code.
type QuicBaaSClientExt interface {
	QuicBaaSClient
}

// NewQuicBaaSClientExt creates a QuicBaaS client with cluster management support.
// This is now just a wrapper around NewQuicBaaSClient for backward compatibility.
func NewQuicBaaSClientExt(cc grpc.ClientConnInterface) QuicBaaSClientExt {
	return NewQuicBaaSClient(cc)
}
