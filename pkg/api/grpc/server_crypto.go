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

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	kctypes "github.com/jeremyhahn/go-xkms/sdk/go"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// CryptoServer wraps the Crypto service for gRPC endpoints.
type CryptoServer struct {
	pb.UnimplementedCryptoServer
	cryptoSvc service.CryptoService
	logger    *logger.Logger
}

// NewCryptoServer creates a new gRPC server for Crypto operations.
func NewCryptoServer(cryptoSvc service.CryptoService, log *logger.Logger) *CryptoServer {
	return &CryptoServer{
		cryptoSvc: cryptoSvc,
		logger:    log.WithField("component", "crypto-grpc-server"),
	}
}

// Encrypt encrypts plaintext data using a named encryption key.
func (s *CryptoServer) Encrypt(ctx context.Context, req *pb.CryptoEncryptRequest) (*pb.CryptoEncryptResponse, error) {
	s.logger.WithField("key_id", req.KeyId).Debug("Processing Crypto Encrypt request")

	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}

	if req.Plaintext == nil {
		return nil, status.Error(codes.InvalidArgument, "plaintext is required")
	}

	svcReq := &service.EncryptRequest{
		KeyID:     req.KeyId,
		Plaintext: req.Plaintext,
		Context:   req.Context,
		Nonce:     req.Nonce,
	}

	resp, err := s.cryptoSvc.Encrypt(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	return &pb.CryptoEncryptResponse{
		KeyId:      resp.KeyID,
		KeyVersion: resp.KeyVersion,
		Ciphertext: resp.Ciphertext,
		Nonce:      resp.Nonce,
		Timestamp:  resp.Timestamp.Unix(),
	}, nil
}

// Decrypt decrypts ciphertext that was encrypted with a named key.
func (s *CryptoServer) Decrypt(ctx context.Context, req *pb.CryptoDecryptRequest) (*pb.CryptoDecryptResponse, error) {
	s.logger.WithField("key_id", req.KeyId).Debug("Processing Crypto Decrypt request")

	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}

	if req.Ciphertext == nil {
		return nil, status.Error(codes.InvalidArgument, "ciphertext is required")
	}

	svcReq := &service.DecryptRequest{
		KeyID:      req.KeyId,
		Ciphertext: req.Ciphertext,
		Context:    req.Context,
		Nonce:      req.Nonce,
		KeyVersion: req.KeyVersion,
	}

	resp, err := s.cryptoSvc.Decrypt(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	return &pb.CryptoDecryptResponse{
		KeyId:      resp.KeyID,
		KeyVersion: resp.KeyVersion,
		Plaintext:  resp.Plaintext,
		Timestamp:  resp.Timestamp.Unix(),
	}, nil
}

// Rewrap re-encrypts ciphertext with the latest version of the key.
func (s *CryptoServer) Rewrap(ctx context.Context, req *pb.RewrapRequest) (*pb.RewrapResponse, error) {
	s.logger.WithField("key_id", req.KeyId).Debug("Processing Crypto Rewrap request")

	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}

	if req.Ciphertext == nil {
		return nil, status.Error(codes.InvalidArgument, "ciphertext is required")
	}

	svcReq := &service.RewrapRequest{
		KeyID:      req.KeyId,
		Ciphertext: req.Ciphertext,
		Context:    req.Context,
		Nonce:      req.Nonce,
	}

	resp, err := s.cryptoSvc.Rewrap(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	return &pb.RewrapResponse{
		KeyId:         resp.KeyID,
		OldKeyVersion: resp.OldKeyVersion,
		NewKeyVersion: resp.NewKeyVersion,
		Ciphertext:    resp.Ciphertext,
		Nonce:         resp.Nonce,
		Timestamp:     resp.Timestamp.Unix(),
	}, nil
}

// EncryptBatch performs multiple encrypt operations in a single request.
func (s *CryptoServer) EncryptBatch(ctx context.Context, req *pb.EncryptBatchRequest) (*pb.EncryptBatchResponse, error) {
	s.logger.WithFields(map[string]interface{}{"key_id": req.KeyId, "items": len(req.Items)}).Debug("Processing Crypto EncryptBatch request")

	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}

	if len(req.Items) == 0 {
		return nil, status.Error(codes.InvalidArgument, "items is required")
	}

	items := make([]service.BatchEncryptItem, len(req.Items))
	for i, item := range req.Items {
		items[i] = service.BatchEncryptItem{
			Plaintext: item.Plaintext,
			Context:   item.Context,
		}
	}

	svcReq := &service.EncryptBatchRequest{
		KeyID: req.KeyId,
		Items: items,
	}

	resp, err := s.cryptoSvc.EncryptBatch(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	results := make([]*pb.BatchEncryptResult, len(resp.Results))
	for i, result := range resp.Results {
		results[i] = &pb.BatchEncryptResult{
			Ciphertext: result.Ciphertext,
			Nonce:      result.Nonce,
			Error:      result.Error,
		}
	}

	return &pb.EncryptBatchResponse{
		KeyId:      resp.KeyID,
		KeyVersion: resp.KeyVersion,
		Results:    results,
		Timestamp:  resp.Timestamp.Unix(),
	}, nil
}

// DecryptBatch performs multiple decrypt operations in a single request.
func (s *CryptoServer) DecryptBatch(ctx context.Context, req *pb.DecryptBatchRequest) (*pb.DecryptBatchResponse, error) {
	s.logger.WithFields(map[string]interface{}{"key_id": req.KeyId, "items": len(req.Items)}).Debug("Processing Crypto DecryptBatch request")

	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}

	if len(req.Items) == 0 {
		return nil, status.Error(codes.InvalidArgument, "items is required")
	}

	items := make([]service.BatchDecryptItem, len(req.Items))
	for i, item := range req.Items {
		items[i] = service.BatchDecryptItem{
			Ciphertext: item.Ciphertext,
			Context:    item.Context,
			Nonce:      item.Nonce,
		}
	}

	svcReq := &service.DecryptBatchRequest{
		KeyID: req.KeyId,
		Items: items,
	}

	resp, err := s.cryptoSvc.DecryptBatch(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	results := make([]*pb.BatchDecryptResult, len(resp.Results))
	for i, result := range resp.Results {
		results[i] = &pb.BatchDecryptResult{
			Plaintext: result.Plaintext,
			Error:     result.Error,
		}
	}

	return &pb.DecryptBatchResponse{
		KeyId:     resp.KeyID,
		Results:   results,
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// GenerateDataKey generates a data encryption key for envelope encryption.
func (s *CryptoServer) GenerateDataKey(ctx context.Context, req *pb.GenerateDataKeyRequest) (*pb.GenerateDataKeyResponse, error) {
	s.logger.WithField("key_id", req.KeyId).Debug("Processing Crypto GenerateDataKey request")

	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}

	if req.KeySpec == nil {
		return nil, status.Error(codes.InvalidArgument, "key_spec is required")
	}

	// Convert proto KeySpec to KeyAttributes
	attrs := protoKeySpecToKeyAttributes(req.KeySpec)

	svcReq := &service.GenerateDataKeyRequest{
		KeyID:         req.KeyId,
		KeyAttributes: attrs,
		Context:       req.Context,
	}

	resp, err := s.cryptoSvc.GenerateDataKey(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	return &pb.GenerateDataKeyResponse{
		KeyId:          resp.KeyID,
		Plaintext:      resp.Plaintext,
		CiphertextBlob: resp.CiphertextBlob,
		Timestamp:      resp.Timestamp.Unix(),
	}, nil
}

// GenerateWrappedDataKey generates an encrypted data key without plaintext.
func (s *CryptoServer) GenerateWrappedDataKey(ctx context.Context, req *pb.GenerateWrappedDataKeyRequest) (*pb.GenerateWrappedDataKeyResponse, error) {
	s.logger.WithField("key_id", req.KeyId).Debug("Processing Crypto GenerateWrappedDataKey request")

	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}

	if req.KeySpec == nil {
		return nil, status.Error(codes.InvalidArgument, "key_spec is required")
	}

	// Convert proto KeySpec to KeyAttributes
	attrs := protoKeySpecToKeyAttributes(req.KeySpec)

	svcReq := &service.GenerateWrappedDataKeyRequest{
		KeyID:         req.KeyId,
		KeyAttributes: attrs,
		Context:       req.Context,
	}

	resp, err := s.cryptoSvc.GenerateWrappedDataKey(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	return &pb.GenerateWrappedDataKeyResponse{
		KeyId:          resp.KeyID,
		CiphertextBlob: resp.CiphertextBlob,
		Timestamp:      resp.Timestamp.Unix(),
	}, nil
}

// Sign creates a digital signature of data using a named signing key.
func (s *CryptoServer) Sign(ctx context.Context, req *pb.SignRequest) (*pb.SignResponse, error) {
	s.logger.WithField("key_id", req.KeyId).Debug("Processing Crypto Sign request")

	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}

	if req.Message == nil {
		return nil, status.Error(codes.InvalidArgument, "message is required")
	}

	svcReq := &service.SignRequest{
		KeyID:         req.KeyId,
		Message:       req.Message,
		HashAlgorithm: convertProtoHashAlgorithm(req.HashAlgorithm),
		PreHashed:     req.PreHashed,
	}

	resp, err := s.cryptoSvc.Sign(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	return &pb.SignResponse{
		KeyId:         resp.KeyID,
		KeyVersion:    resp.KeyVersion,
		Signature:     resp.Signature,
		HashAlgorithm: convertServiceHashAlgorithm(resp.HashAlgorithm),
		Timestamp:     resp.Timestamp.Unix(),
	}, nil
}

// Verify verifies a digital signature against data using a named key.
func (s *CryptoServer) Verify(ctx context.Context, req *pb.VerifyRequest) (*pb.VerifyResponse, error) {
	s.logger.WithField("key_id", req.KeyId).Debug("Processing Crypto Verify request")

	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}

	if req.Message == nil {
		return nil, status.Error(codes.InvalidArgument, "message is required")
	}

	if req.Signature == nil {
		return nil, status.Error(codes.InvalidArgument, "signature is required")
	}

	svcReq := &service.VerifyRequest{
		KeyID:         req.KeyId,
		Message:       req.Message,
		Signature:     req.Signature,
		HashAlgorithm: convertProtoHashAlgorithm(req.HashAlgorithm),
		KeyVersion:    req.KeyVersion,
	}

	resp, err := s.cryptoSvc.Verify(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	return &pb.VerifyResponse{
		KeyId:      resp.KeyID,
		KeyVersion: resp.KeyVersion,
		Valid:      resp.Valid,
		Timestamp:  resp.Timestamp.Unix(),
	}, nil
}

// GenerateHMAC generates an HMAC of data using a named key.
func (s *CryptoServer) GenerateHMAC(ctx context.Context, req *pb.HMACRequest) (*pb.HMACResponse, error) {
	s.logger.WithField("key_id", req.KeyId).Debug("Processing Crypto GenerateHMAC request")

	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}

	if req.Message == nil {
		return nil, status.Error(codes.InvalidArgument, "message is required")
	}

	svcReq := &service.HMACRequest{
		KeyID:         req.KeyId,
		Message:       req.Message,
		HashAlgorithm: convertProtoHashAlgorithm(req.HashAlgorithm),
	}

	resp, err := s.cryptoSvc.GenerateHMAC(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	return &pb.HMACResponse{
		KeyId:         resp.KeyID,
		KeyVersion:    resp.KeyVersion,
		Hmac:          resp.HMAC,
		HashAlgorithm: convertServiceHashAlgorithm(resp.HashAlgorithm),
		Timestamp:     resp.Timestamp.Unix(),
	}, nil
}

// VerifyHMAC verifies an HMAC against data using a named key.
func (s *CryptoServer) VerifyHMAC(ctx context.Context, req *pb.VerifyHMACRequest) (*pb.VerifyHMACResponse, error) {
	s.logger.WithField("key_id", req.KeyId).Debug("Processing Crypto VerifyHMAC request")

	if req.KeyId == "" {
		return nil, status.Error(codes.InvalidArgument, "key_id is required")
	}

	if req.Message == nil {
		return nil, status.Error(codes.InvalidArgument, "message is required")
	}

	if req.Hmac == nil {
		return nil, status.Error(codes.InvalidArgument, "hmac is required")
	}

	svcReq := &service.VerifyHMACRequest{
		KeyID:         req.KeyId,
		Message:       req.Message,
		HMAC:          req.Hmac,
		HashAlgorithm: convertProtoHashAlgorithm(req.HashAlgorithm),
		KeyVersion:    req.KeyVersion,
	}

	resp, err := s.cryptoSvc.VerifyHMAC(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	return &pb.VerifyHMACResponse{
		KeyId:      resp.KeyID,
		KeyVersion: resp.KeyVersion,
		Valid:      resp.Valid,
		Timestamp:  resp.Timestamp.Unix(),
	}, nil
}

// GenerateRandom generates high-quality random bytes.
func (s *CryptoServer) GenerateRandom(ctx context.Context, req *pb.GenerateRandomRequest) (*pb.GenerateRandomResponse, error) {
	s.logger.WithField("num_bytes", req.NumBytes).Debug("Processing Crypto GenerateRandom request")

	if req.NumBytes <= 0 {
		return nil, status.Error(codes.InvalidArgument, "num_bytes must be greater than 0")
	}

	svcReq := &service.GenerateRandomRequest{
		NumBytes: int(req.NumBytes),
	}

	resp, err := s.cryptoSvc.GenerateRandom(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	return &pb.GenerateRandomResponse{
		RandomBytes: resp.RandomBytes,
		Timestamp:   resp.Timestamp.Unix(),
	}, nil
}

// Hash computes a cryptographic hash of data.
func (s *CryptoServer) Hash(ctx context.Context, req *pb.HashRequest) (*pb.HashResponse, error) {
	s.logger.WithField("algorithm", req.Algorithm).Debug("Processing Crypto Hash request")

	if req.Data == nil {
		return nil, status.Error(codes.InvalidArgument, "data is required")
	}

	svcReq := &service.HashRequest{
		Data:      req.Data,
		Algorithm: convertProtoHashAlgorithm(req.Algorithm),
	}

	resp, err := s.cryptoSvc.Hash(ctx, svcReq)
	if err != nil {
		return nil, ToGRPCError(err)
	}

	return &pb.HashResponse{
		Algorithm: convertServiceHashAlgorithm(resp.Algorithm),
		Hash:      resp.Hash,
		Timestamp: resp.Timestamp.Unix(),
	}, nil
}

// =============================================================================
// Conversion Helpers
// =============================================================================

// protoKeySpecToKeyAttributes converts a proto KeySpec to KeyAttributes for data key generation.
// For data key generation, we primarily need the key size to determine the DEK size.
func protoKeySpecToKeyAttributes(spec *pb.KeySpec) *kctypes.KeyAttributes {
	attrs := &kctypes.KeyAttributes{
		Exportable: spec.Exportable,
	}

	// Map proto algorithm to go-xkms types
	// For data key generation, we focus on symmetric algorithms
	switch spec.Algorithm {
	case pb.KeyAlgorithm_KEY_ALG_AES_128_GCM:
		attrs.SymmetricAlgorithm = kctypes.SymmetricAES128GCM
		attrs.KeyType = kctypes.KeyTypeEncryption
	case pb.KeyAlgorithm_KEY_ALG_AES_192_GCM:
		attrs.SymmetricAlgorithm = kctypes.SymmetricAES192GCM
		attrs.KeyType = kctypes.KeyTypeEncryption
	case pb.KeyAlgorithm_KEY_ALG_AES_256_GCM:
		attrs.SymmetricAlgorithm = kctypes.SymmetricAES256GCM
		attrs.KeyType = kctypes.KeyTypeEncryption
	case pb.KeyAlgorithm_KEY_ALG_CHACHA20_POLY1305:
		attrs.SymmetricAlgorithm = kctypes.SymmetricChaCha20Poly1305
		attrs.KeyType = kctypes.KeyTypeEncryption
	case pb.KeyAlgorithm_KEY_ALG_XCHACHA20_POLY1305:
		attrs.SymmetricAlgorithm = kctypes.SymmetricXChaCha20Poly1305
		attrs.KeyType = kctypes.KeyTypeEncryption
	default:
		// Default to AES-256-GCM for data key generation
		attrs.SymmetricAlgorithm = kctypes.SymmetricAES256GCM
		attrs.KeyType = kctypes.KeyTypeEncryption
	}

	return attrs
}

func convertProtoHashAlgorithm(alg pb.HashAlgorithm) service.HashAlgorithm {
	switch alg {
	case pb.HashAlgorithm_HASH_ALG_SHA256:
		return service.HashAlgSHA256
	case pb.HashAlgorithm_HASH_ALG_SHA384:
		return service.HashAlgSHA384
	case pb.HashAlgorithm_HASH_ALG_SHA512:
		return service.HashAlgSHA512
	case pb.HashAlgorithm_HASH_ALG_SHA3_256:
		return service.HashAlgSHA3_256
	case pb.HashAlgorithm_HASH_ALG_SHA3_384:
		return service.HashAlgSHA3_384
	case pb.HashAlgorithm_HASH_ALG_SHA3_512:
		return service.HashAlgSHA3_512
	case pb.HashAlgorithm_HASH_ALG_BLAKE2B_256:
		return service.HashAlgBLAKE2b256
	case pb.HashAlgorithm_HASH_ALG_BLAKE2B_512:
		return service.HashAlgBLAKE2b512
	default:
		return service.HashAlgSHA256
	}
}

func convertServiceHashAlgorithm(alg service.HashAlgorithm) pb.HashAlgorithm {
	switch alg {
	case service.HashAlgSHA256:
		return pb.HashAlgorithm_HASH_ALG_SHA256
	case service.HashAlgSHA384:
		return pb.HashAlgorithm_HASH_ALG_SHA384
	case service.HashAlgSHA512:
		return pb.HashAlgorithm_HASH_ALG_SHA512
	case service.HashAlgSHA3_256:
		return pb.HashAlgorithm_HASH_ALG_SHA3_256
	case service.HashAlgSHA3_384:
		return pb.HashAlgorithm_HASH_ALG_SHA3_384
	case service.HashAlgSHA3_512:
		return pb.HashAlgorithm_HASH_ALG_SHA3_512
	case service.HashAlgBLAKE2b256:
		return pb.HashAlgorithm_HASH_ALG_BLAKE2B_256
	case service.HashAlgBLAKE2b512:
		return pb.HashAlgorithm_HASH_ALG_BLAKE2B_512
	default:
		return pb.HashAlgorithm_HASH_ALG_SHA256
	}
}
