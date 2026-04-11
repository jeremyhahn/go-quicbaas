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
	"time"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// KMSSign creates a digital signature of data using a named signing key.
func (c *Client) KMSSign(ctx context.Context, req *service.SignRequest) (*service.SignResponse, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.KMSSign",
			Err:  ErrRequestNil,
		}
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	cryptoClient := pb.NewCryptoClient(c.conn)

	pbReq := &pb.SignRequest{
		KeyId:         req.KeyID,
		Message:       req.Message,
		HashAlgorithm: convertServiceHashAlgorithmToProto(req.HashAlgorithm),
	}

	pbResp, err := cryptoClient.Sign(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.SignResponse{
		KeyID:         pbResp.KeyId,
		KeyVersion:    pbResp.KeyVersion,
		Signature:     pbResp.Signature,
		HashAlgorithm: convertProtoToServiceHashAlgorithm(pbResp.HashAlgorithm),
		Timestamp:     time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSVerify verifies a digital signature against data using a named key.
func (c *Client) KMSVerify(ctx context.Context, req *service.VerifyRequest) (*service.VerifyResponse, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.KMSVerify",
			Err:  ErrRequestNil,
		}
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	cryptoClient := pb.NewCryptoClient(c.conn)

	pbReq := &pb.VerifyRequest{
		KeyId:         req.KeyID,
		Message:       req.Message,
		Signature:     req.Signature,
		HashAlgorithm: convertServiceHashAlgorithmToProto(req.HashAlgorithm),
		KeyVersion:    req.KeyVersion,
	}

	pbResp, err := cryptoClient.Verify(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.VerifyResponse{
		KeyID:      pbResp.KeyId,
		KeyVersion: pbResp.KeyVersion,
		Valid:      pbResp.Valid,
		Timestamp:  time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSHash computes a cryptographic hash of data.
func (c *Client) KMSHash(ctx context.Context, req *service.HashRequest) (*service.HashResponse, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.KMSHash",
			Err:  ErrRequestNil,
		}
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	cryptoClient := pb.NewCryptoClient(c.conn)

	pbReq := &pb.HashRequest{
		Data:      req.Data,
		Algorithm: convertServiceHashAlgorithmToProto(req.Algorithm),
	}

	pbResp, err := cryptoClient.Hash(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.HashResponse{
		Algorithm: convertProtoToServiceHashAlgorithm(pbResp.Algorithm),
		Hash:      pbResp.Hash,
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSGenerateHMAC generates an HMAC of data using a named key.
func (c *Client) KMSGenerateHMAC(ctx context.Context, req *service.HMACRequest) (*service.HMACResponse, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.KMSGenerateHMAC",
			Err:  ErrRequestNil,
		}
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	cryptoClient := pb.NewCryptoClient(c.conn)

	pbReq := &pb.HMACRequest{
		KeyId:         req.KeyID,
		Message:       req.Message,
		HashAlgorithm: convertServiceHashAlgorithmToProto(req.HashAlgorithm),
	}

	pbResp, err := cryptoClient.GenerateHMAC(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.HMACResponse{
		KeyID:         pbResp.KeyId,
		KeyVersion:    pbResp.KeyVersion,
		HMAC:          pbResp.Hmac,
		HashAlgorithm: convertProtoToServiceHashAlgorithm(pbResp.HashAlgorithm),
		Timestamp:     time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSVerifyHMAC verifies an HMAC against data using a named key.
func (c *Client) KMSVerifyHMAC(ctx context.Context, req *service.VerifyHMACRequest) (*service.VerifyHMACResponse, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.KMSVerifyHMAC",
			Err:  ErrRequestNil,
		}
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	cryptoClient := pb.NewCryptoClient(c.conn)

	pbReq := &pb.VerifyHMACRequest{
		KeyId:         req.KeyID,
		Message:       req.Message,
		Hmac:          req.HMAC,
		HashAlgorithm: convertServiceHashAlgorithmToProto(req.HashAlgorithm),
		KeyVersion:    req.KeyVersion,
	}

	pbResp, err := cryptoClient.VerifyHMAC(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.VerifyHMACResponse{
		KeyID:      pbResp.KeyId,
		KeyVersion: pbResp.KeyVersion,
		Valid:      pbResp.Valid,
		Timestamp:  time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSRewrap re-encrypts ciphertext with the latest version of the key.
func (c *Client) KMSRewrap(ctx context.Context, req *service.RewrapRequest) (*service.RewrapResponse, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.KMSRewrap",
			Err:  ErrRequestNil,
		}
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	cryptoClient := pb.NewCryptoClient(c.conn)

	pbReq := &pb.RewrapRequest{
		KeyId:      req.KeyID,
		Ciphertext: req.Ciphertext,
		Context:    req.Context,
		Nonce:      req.Nonce,
	}

	pbResp, err := cryptoClient.Rewrap(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.RewrapResponse{
		KeyID:         pbResp.KeyId,
		OldKeyVersion: pbResp.OldKeyVersion,
		NewKeyVersion: pbResp.NewKeyVersion,
		Ciphertext:    pbResp.Ciphertext,
		Nonce:         pbResp.Nonce,
		Timestamp:     time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSGenerateRandom generates high-quality random bytes.
func (c *Client) KMSGenerateRandom(ctx context.Context, req *service.GenerateRandomRequest) (*service.GenerateRandomResponse, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.KMSGenerateRandom",
			Err:  ErrRequestNil,
		}
	}

	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}

	cryptoClient := pb.NewCryptoClient(c.conn)

	pbReq := &pb.GenerateRandomRequest{
		NumBytes: int32(req.NumBytes), //nolint:gosec // G115: bounded by input validation (max 1MB)
	}

	pbResp, err := cryptoClient.GenerateRandom(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	return &service.GenerateRandomResponse{
		RandomBytes: pbResp.RandomBytes,
		Timestamp:   time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// convertServiceHashAlgorithmToProto converts service.HashAlgorithm to pb.HashAlgorithm.
func convertServiceHashAlgorithmToProto(alg service.HashAlgorithm) pb.HashAlgorithm {
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

// convertProtoToServiceHashAlgorithm converts pb.HashAlgorithm to service.HashAlgorithm.
func convertProtoToServiceHashAlgorithm(alg pb.HashAlgorithm) service.HashAlgorithm {
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
