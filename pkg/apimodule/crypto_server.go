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

// errNodeSignUnserved is what the node answers for Sign and Verify.
var errNodeSignUnserved = status.Error(codes.Unimplemented,
	"Sign and Verify are not served on the node: the crypto backend does not resolve the signing key version")

// nodeCryptoServer is the quicbaas.kms.v1.Crypto server the node mounts. It
// serves every method of the embedded *grpcapi.CryptoServer except
// GenerateHMAC and VerifyHMAC. The production crypto backend
// (pkg/crypto's MemoryCryptoManager) derives a fresh random key on every
// HMAC call (GenerateDataKey), so VerifyHMAC of a MAC it generated always
// returns false. Until that backend has a stable MAC key, the node answers
// both RPCs Unimplemented instead of newly exposing a MAC that cannot be
// verified.
//
// It also refuses GenerateDataKey and GenerateWrappedDataKey requests that
// carry a non-empty context (additional authenticated data). The production
// path drops that context: pkg/service's crypto service does not pass it to
// KMSManager.GenerateDataKey, and pkg/kms's xkms backend wraps the data key
// with Encrypt(ctx, keyID, plaintext, nil). A blob returned for such a
// request would not be bound to the context the caller asked for, so until
// the backend binds it the node answers InvalidArgument instead. Requests
// with an empty context are served by the embedded server.
//
// It does not serve Sign or Verify either. The production crypto backend
// signs with the key's original version even after RotateKey (it never
// resolves the key metadata's current Version), and Verify ignores the
// requested KeyVersion, so serving them would newly expose signing on a
// stale key. Until the backend resolves the signing key version, the node
// answers both RPCs Unimplemented.
type nodeCryptoServer struct {
	*grpcapi.CryptoServer
}

// errNodeDataKeyContextUnsupported is what the node answers for
// GenerateDataKey and GenerateWrappedDataKey when the request carries a
// non-empty context.
var errNodeDataKeyContextUnsupported = status.Error(codes.InvalidArgument,
	"context (additional authenticated data) is not supported for data-key generation on the node")

// GenerateDataKey refuses a non-empty context; see nodeCryptoServer.
func (s nodeCryptoServer) GenerateDataKey(ctx context.Context, req *pb.GenerateDataKeyRequest) (*pb.GenerateDataKeyResponse, error) {
	if len(req.GetContext()) > 0 {
		return nil, errNodeDataKeyContextUnsupported
	}
	return s.CryptoServer.GenerateDataKey(ctx, req)
}

// GenerateWrappedDataKey refuses a non-empty context; see nodeCryptoServer.
func (s nodeCryptoServer) GenerateWrappedDataKey(ctx context.Context, req *pb.GenerateWrappedDataKeyRequest) (*pb.GenerateWrappedDataKeyResponse, error) {
	if len(req.GetContext()) > 0 {
		return nil, errNodeDataKeyContextUnsupported
	}
	return s.CryptoServer.GenerateWrappedDataKey(ctx, req)
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

// Sign is not served on the node; see nodeCryptoServer.
func (nodeCryptoServer) Sign(context.Context, *pb.SignRequest) (*pb.SignResponse, error) {
	return nil, errNodeSignUnserved
}

// Verify is not served on the node; see nodeCryptoServer.
func (nodeCryptoServer) Verify(context.Context, *pb.VerifyRequest) (*pb.VerifyResponse, error) {
	return nil, errNodeSignUnserved
}

// errNodeRewrapUnserved is what the node answers for Rewrap.
var errNodeRewrapUnserved = status.Error(codes.Unimplemented,
	"Rewrap is not served on the node: the crypto service does not report the key versions it used")

// Rewrap is not served on the node: the crypto service does not report the
// key versions it decrypted and re-encrypted with, so the response could not
// name them.
func (nodeCryptoServer) Rewrap(context.Context, *pb.RewrapRequest) (*pb.RewrapResponse, error) {
	return nil, errNodeRewrapUnserved
}
