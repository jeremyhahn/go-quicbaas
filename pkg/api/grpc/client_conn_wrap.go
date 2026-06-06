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

package grpc

import (
	"google.golang.org/grpc"

	"github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// NewClientWithConn wraps an already-established *grpc.ClientConn in a Client
// without dialing a new connection.
//
// It exists so callers that already hold a live connection — most notably the
// SDK gRPC/Unix transports, which dial once and multiplex every service over
// the single conn — can reuse this package's fully-tested struct<->protobuf
// conversion logic (KMS, Crypto, SecretsManagement, DEK) instead of forking it.
//
// The returned Client is marked connected so the conversion methods skip their
// lazy-dial path. The caller retains ownership of the connection's lifecycle;
// the returned Client is intended to be used ephemerally per request and its
// Close must not be called, since that would tear down the borrowed
// connection. opts and logger are intentionally nil because the conversion
// methods this constructor enables (KMS/Crypto/Secrets/DEK) never touch them.
func NewClientWithConn(conn *grpc.ClientConn) (*Client, error) {
	if conn == nil {
		return nil, errors.Newf(
			errors.ErrInvalidInput,
			"grpc.NewClientWithConn",
			"connection is required",
		)
	}

	return &Client{
		conn:      conn,
		client:    proto.NewQuicBaaSClient(conn),
		connected: true,
	}, nil
}
