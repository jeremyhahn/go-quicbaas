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

package embedded

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Crypto Handlers
// =============================================================================

func handleCryptoEncrypt(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	crypto := svc.Crypto()
	if crypto == nil {
		return nil, NewServiceNotConfiguredError("Crypto")
	}
	r, ok := req.(*service.EncryptRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Crypto", "Encrypt", "*service.EncryptRequest", typeNameOf(req))
	}
	return crypto.Encrypt(ctx, r)
}

func handleCryptoDecrypt(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	crypto := svc.Crypto()
	if crypto == nil {
		return nil, NewServiceNotConfiguredError("Crypto")
	}
	r, ok := req.(*service.DecryptRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Crypto", "Decrypt", "*service.DecryptRequest", typeNameOf(req))
	}
	return crypto.Decrypt(ctx, r)
}

func handleCryptoRewrap(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	crypto := svc.Crypto()
	if crypto == nil {
		return nil, NewServiceNotConfiguredError("Crypto")
	}
	r, ok := req.(*service.RewrapRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Crypto", "Rewrap", "*service.RewrapRequest", typeNameOf(req))
	}
	return crypto.Rewrap(ctx, r)
}

func handleCryptoEncryptBatch(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	crypto := svc.Crypto()
	if crypto == nil {
		return nil, NewServiceNotConfiguredError("Crypto")
	}
	r, ok := req.(*service.EncryptBatchRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Crypto", "EncryptBatch", "*service.EncryptBatchRequest", typeNameOf(req))
	}
	return crypto.EncryptBatch(ctx, r)
}

func handleCryptoDecryptBatch(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	crypto := svc.Crypto()
	if crypto == nil {
		return nil, NewServiceNotConfiguredError("Crypto")
	}
	r, ok := req.(*service.DecryptBatchRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Crypto", "DecryptBatch", "*service.DecryptBatchRequest", typeNameOf(req))
	}
	return crypto.DecryptBatch(ctx, r)
}

func handleCryptoGenerateDataKey(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	crypto := svc.Crypto()
	if crypto == nil {
		return nil, NewServiceNotConfiguredError("Crypto")
	}
	r, ok := req.(*service.GenerateDataKeyRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Crypto", "GenerateDataKey", "*service.GenerateDataKeyRequest", typeNameOf(req))
	}
	return crypto.GenerateDataKey(ctx, r)
}

func handleCryptoGenerateWrappedDataKey(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	crypto := svc.Crypto()
	if crypto == nil {
		return nil, NewServiceNotConfiguredError("Crypto")
	}
	r, ok := req.(*service.GenerateWrappedDataKeyRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Crypto", "GenerateWrappedDataKey", "*service.GenerateWrappedDataKeyRequest", typeNameOf(req))
	}
	return crypto.GenerateWrappedDataKey(ctx, r)
}

func handleCryptoSign(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	crypto := svc.Crypto()
	if crypto == nil {
		return nil, NewServiceNotConfiguredError("Crypto")
	}
	r, ok := req.(*service.SignRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Crypto", "Sign", "*service.SignRequest", typeNameOf(req))
	}
	return crypto.Sign(ctx, r)
}

func handleCryptoVerify(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	crypto := svc.Crypto()
	if crypto == nil {
		return nil, NewServiceNotConfiguredError("Crypto")
	}
	r, ok := req.(*service.VerifyRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Crypto", "Verify", "*service.VerifyRequest", typeNameOf(req))
	}
	return crypto.Verify(ctx, r)
}

func handleCryptoGenerateHMAC(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	crypto := svc.Crypto()
	if crypto == nil {
		return nil, NewServiceNotConfiguredError("Crypto")
	}
	r, ok := req.(*service.HMACRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Crypto", "GenerateHMAC", "*service.HMACRequest", typeNameOf(req))
	}
	return crypto.GenerateHMAC(ctx, r)
}

func handleCryptoVerifyHMAC(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	crypto := svc.Crypto()
	if crypto == nil {
		return nil, NewServiceNotConfiguredError("Crypto")
	}
	r, ok := req.(*service.VerifyHMACRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Crypto", "VerifyHMAC", "*service.VerifyHMACRequest", typeNameOf(req))
	}
	return crypto.VerifyHMAC(ctx, r)
}

func handleCryptoGenerateRandom(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	crypto := svc.Crypto()
	if crypto == nil {
		return nil, NewServiceNotConfiguredError("Crypto")
	}
	r, ok := req.(*service.GenerateRandomRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Crypto", "GenerateRandom", "*service.GenerateRandomRequest", typeNameOf(req))
	}
	return crypto.GenerateRandom(ctx, r)
}

func handleCryptoHash(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	crypto := svc.Crypto()
	if crypto == nil {
		return nil, NewServiceNotConfiguredError("Crypto")
	}
	r, ok := req.(*service.HashRequest)
	if !ok {
		return nil, NewInvalidRequestTypeError("quicbaas.Crypto", "Hash", "*service.HashRequest", typeNameOf(req))
	}
	return crypto.Hash(ctx, r)
}
