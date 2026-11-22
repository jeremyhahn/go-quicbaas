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

package ociregistry

import (
	"context"
	"fmt"
	"strings"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// SDKKeyClient is the part of the SDK KMS client the registry's layer
// encryption needs: look a key up and create one.
type SDKKeyClient interface {
	GetKey(ctx context.Context, keyID string) (*kms.KeyInfo, error)
	GenerateKey(ctx context.Context, req *kms.GenerateKeyRequest) (*kms.GenerateKeyResponse, error)
}

// SDKCryptoClient is the part of the SDK crypto client the registry's layer
// encryption needs: wrap and unwrap a layer DEK under a KMS key.
type SDKCryptoClient interface {
	Encrypt(ctx context.Context, req *quicbaas.EncryptRequest) (*quicbaas.EncryptResponse, error)
	Decrypt(ctx context.Context, req *quicbaas.DecryptRequest) (*quicbaas.DecryptResponse, error)
}

// SDKKMSClient is the registry's KMSClient over the platform KMS. Each layer
// is sealed under its own DEK, and the DEK is wrapped by a platform KMS key
// through the SDK, with the layer digest as additional authenticated data.
// The key material never leaves the KMS.
type SDKKMSClient struct {
	keys   SDKKeyClient
	crypto SDKCryptoClient
}

var _ KMSClient = (*SDKKMSClient)(nil)

// NewSDKKMSClient returns a KMSClient over the SDK's KMS and crypto clients,
// usually client.Security().KMS() and client.Security().Crypto(). Both are
// required.
func NewSDKKMSClient(keys SDKKeyClient, crypto SDKCryptoClient) (*SDKKMSClient, error) {
	if keys == nil {
		return nil, NilClientError{Client: "kms"}
	}
	if crypto == nil {
		return nil, NilClientError{Client: "crypto"}
	}
	return &SDKKMSClient{keys: keys, crypto: crypto}, nil
}

// sdkKeyUsages maps the registry's usage names to the SDK's key usages.
var sdkKeyUsages = map[string]kms.KeyUsage{
	"encrypt": kms.KeyUsageEncrypt,
	"decrypt": kms.KeyUsageDecrypt,
	"wrap":    kms.KeyUsageWrap,
	"unwrap":  kms.KeyUsageUnwrap,
}

// GenerateKey creates an AES-256-GCM key in the platform KMS and returns its
// ID.
func (c *SDKKMSClient) GenerateKey(ctx context.Context, name, algorithm string, usage []string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if algorithm != DefaultKeyAlgorithm {
		return "", InvalidConfigError{
			Field:  "algorithm",
			Reason: fmt.Sprintf("unsupported key algorithm %q (want %s)", algorithm, DefaultKeyAlgorithm),
		}
	}
	usages := make([]kms.KeyUsage, 0, len(usage))
	for _, u := range usage {
		mapped, ok := sdkKeyUsages[strings.ToLower(u)]
		if !ok {
			return "", InvalidConfigError{Field: "usage", Reason: fmt.Sprintf("unsupported key usage %q", u)}
		}
		usages = append(usages, mapped)
	}
	resp, err := c.keys.GenerateKey(ctx, &kms.GenerateKeyRequest{
		Name:      name,
		Algorithm: kms.AlgorithmAES256GCM,
		Usage:     usages,
	})
	if err != nil {
		return "", &WrappedError{Msg: fmt.Sprintf("registry: generate key %s", name), Err: err}
	}
	if resp == nil || resp.KeyID == "" {
		return "", &MessageError{Msg: fmt.Sprintf("registry: generate key %s: KMS returned no key ID", name)}
	}
	return resp.KeyID, nil
}

// GetKey looks a key up in the platform KMS. A missing key is
// KeyNotFoundError; any other failure is wrapped, so a caller can tell a
// missing key from a lookup that did not complete.
func (c *SDKKMSClient) GetKey(ctx context.Context, keyID string) (*KeyInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := c.keys.GetKey(ctx, keyID)
	if err != nil {
		if sdkerrors.IsNotFound(err) {
			return nil, KeyNotFoundError{KeyID: keyID}
		}
		return nil, &WrappedError{Msg: fmt.Sprintf("registry: get key %s", keyID), Err: err}
	}
	if info == nil {
		return nil, KeyNotFoundError{KeyID: keyID}
	}
	usage := make([]string, 0, len(info.Usage))
	for _, u := range info.Usage {
		usage = append(usage, strings.ToLower(u.String()))
	}
	return &KeyInfo{
		KeyID:     info.KeyID,
		Name:      info.Name,
		Algorithm: info.Algorithm.String(),
		Version:   info.Version,
		Usage:     usage,
		CreatedAt: info.CreatedAt,
	}, nil
}

// Encrypt wraps plaintext (a layer DEK) under keyID, binding aad to the
// ciphertext. It returns the ciphertext and the key version used.
func (c *SDKKMSClient) Encrypt(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	resp, err := c.crypto.Encrypt(ctx, &quicbaas.EncryptRequest{KeyID: keyID, Plaintext: plaintext, Context: aad})
	if err != nil {
		return nil, 0, &WrappedError{Msg: fmt.Sprintf("registry: encrypt under key %s", keyID), Err: err}
	}
	if resp == nil || len(resp.Ciphertext) == 0 {
		return nil, 0, &MessageError{Msg: fmt.Sprintf("registry: encrypt under key %s: KMS returned no ciphertext", keyID)}
	}
	return resp.Ciphertext, resp.KeyVersion, nil
}

// Decrypt unwraps ciphertext under keyID; aad must match the value bound at
// Encrypt.
func (c *SDKKMSClient) Decrypt(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resp, err := c.crypto.Decrypt(ctx, &quicbaas.DecryptRequest{KeyID: keyID, Ciphertext: ciphertext, Context: aad})
	if err != nil {
		return nil, &WrappedError{Msg: fmt.Sprintf("registry: decrypt under key %s", keyID), Err: err}
	}
	if resp == nil || len(resp.Plaintext) == 0 {
		return nil, &MessageError{Msg: fmt.Sprintf("registry: decrypt under key %s: KMS returned no plaintext", keyID)}
	}
	return resp.Plaintext, nil
}
