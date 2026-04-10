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
	"reflect"

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
		return nil, NewInvalidRequestTypeError("quicbaas.Crypto", "Encrypt", "*service.EncryptRequest", reflect.TypeOf(req).String())
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
		return nil, NewInvalidRequestTypeError("quicbaas.Crypto", "Decrypt", "*service.DecryptRequest", reflect.TypeOf(req).String())
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
		return nil, NewInvalidRequestTypeError("quicbaas.Crypto", "Rewrap", "*service.RewrapRequest", reflect.TypeOf(req).String())
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
		return nil, NewInvalidRequestTypeError("quicbaas.Crypto", "EncryptBatch", "*service.EncryptBatchRequest", reflect.TypeOf(req).String())
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
		return nil, NewInvalidRequestTypeError("quicbaas.Crypto", "DecryptBatch", "*service.DecryptBatchRequest", reflect.TypeOf(req).String())
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
		return nil, NewInvalidRequestTypeError("quicbaas.Crypto", "GenerateDataKey", "*service.GenerateDataKeyRequest", reflect.TypeOf(req).String())
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
		return nil, NewInvalidRequestTypeError("quicbaas.Crypto", "GenerateWrappedDataKey", "*service.GenerateWrappedDataKeyRequest", reflect.TypeOf(req).String())
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
		return nil, NewInvalidRequestTypeError("quicbaas.Crypto", "Sign", "*service.SignRequest", reflect.TypeOf(req).String())
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
		return nil, NewInvalidRequestTypeError("quicbaas.Crypto", "Verify", "*service.VerifyRequest", reflect.TypeOf(req).String())
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
		return nil, NewInvalidRequestTypeError("quicbaas.Crypto", "GenerateHMAC", "*service.HMACRequest", reflect.TypeOf(req).String())
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
		return nil, NewInvalidRequestTypeError("quicbaas.Crypto", "VerifyHMAC", "*service.VerifyHMACRequest", reflect.TypeOf(req).String())
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
		return nil, NewInvalidRequestTypeError("quicbaas.Crypto", "GenerateRandom", "*service.GenerateRandomRequest", reflect.TypeOf(req).String())
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
		return nil, NewInvalidRequestTypeError("quicbaas.Crypto", "Hash", "*service.HashRequest", reflect.TypeOf(req).String())
	}
	return crypto.Hash(ctx, r)
}
