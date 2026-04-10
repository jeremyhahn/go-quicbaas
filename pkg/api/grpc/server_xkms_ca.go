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
	"context"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CA bundle operations are not available via the QuicBaaS xkms bridge.
// QuicBaaS manages its own certificate authority through its KMS service.
// This method satisfies the KeystoreServiceServer interface and returns
// codes.Unimplemented.

// GetCABundle retrieves the CA certificate bundle.
func (s *XKMSServer) GetCABundle(ctx context.Context, req *xkmssdk.ProtoGetCABundleRequest) (*xkmssdk.ProtoGetCABundleResponse, error) {
	return nil, status.Error(codes.Unimplemented, "CA bundle operations not available via QuicBaaS xkms bridge")
}
