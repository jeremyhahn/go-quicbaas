// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package apimodule

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	grpcapi "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
)

// errNodeHMACUnserved is what the node answers for GenerateHMAC and
// VerifyHMAC.
var errNodeHMACUnserved = status.Error(codes.Unimplemented,
	"HMAC is not served on the node: the crypto backend has no stable MAC key")

// nodeCryptoServer is the quicbaas.kms.v1.Crypto server the node mounts. It
// serves every method of the embedded *grpcapi.CryptoServer except
// GenerateHMAC and VerifyHMAC. The production crypto backend
// (pkg/crypto's MemoryCryptoManager) derives a fresh random key on every
// HMAC call (GenerateDataKey), so VerifyHMAC of a MAC it generated always
// returns false. Until that backend has a stable MAC key, the node answers
// both RPCs Unimplemented instead of newly exposing a MAC that cannot be
// verified.
type nodeCryptoServer struct {
	*grpcapi.CryptoServer
}

var _ pb.CryptoServer = nodeCryptoServer{}

// GenerateHMAC is not served on the node; see nodeCryptoServer.
func (nodeCryptoServer) GenerateHMAC(context.Context, *pb.HMACRequest) (*pb.HMACResponse, error) {
	return nil, errNodeHMACUnserved
}

// VerifyHMAC is not served on the node; see nodeCryptoServer.
func (nodeCryptoServer) VerifyHMAC(context.Context, *pb.VerifyHMACRequest) (*pb.VerifyHMACResponse, error) {
	return nil, errNodeHMACUnserved
}
