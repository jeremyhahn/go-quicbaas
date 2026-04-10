//go:build quic

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

// Package main provides tests for QUIC KMS client not-implemented methods.
package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// TestQUICCryptoClient_KMSSign_NotImplemented verifies KMSSign returns not implemented.
func TestQUICCryptoClient_KMSSign_NotImplemented(t *testing.T) {
	client := &QUICCryptoClient{client: nil}
	resp, err := client.KMSSign(context.Background(), &service.SignRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not yet support KMSSign")
}

// TestQUICCryptoClient_KMSVerify_NotImplemented verifies KMSVerify returns not implemented.
func TestQUICCryptoClient_KMSVerify_NotImplemented(t *testing.T) {
	client := &QUICCryptoClient{client: nil}
	resp, err := client.KMSVerify(context.Background(), &service.VerifyRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not yet support KMSVerify")
}

// TestQUICCryptoClient_KMSHash_NotImplemented verifies KMSHash returns not implemented.
func TestQUICCryptoClient_KMSHash_NotImplemented(t *testing.T) {
	client := &QUICCryptoClient{client: nil}
	resp, err := client.KMSHash(context.Background(), &service.HashRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not yet support KMSHash")
}

// TestQUICCryptoClient_KMSGenerateHMAC_NotImplemented verifies KMSGenerateHMAC returns not implemented.
func TestQUICCryptoClient_KMSGenerateHMAC_NotImplemented(t *testing.T) {
	client := &QUICCryptoClient{client: nil}
	resp, err := client.KMSGenerateHMAC(context.Background(), &service.HMACRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not yet support KMSGenerateHMAC")
}

// TestQUICCryptoClient_KMSVerifyHMAC_NotImplemented verifies KMSVerifyHMAC returns not implemented.
func TestQUICCryptoClient_KMSVerifyHMAC_NotImplemented(t *testing.T) {
	client := &QUICCryptoClient{client: nil}
	resp, err := client.KMSVerifyHMAC(context.Background(), &service.VerifyHMACRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not yet support KMSVerifyHMAC")
}

// TestQUICCryptoClient_KMSRewrap_NotImplemented verifies KMSRewrap returns not implemented.
func TestQUICCryptoClient_KMSRewrap_NotImplemented(t *testing.T) {
	client := &QUICCryptoClient{client: nil}
	resp, err := client.KMSRewrap(context.Background(), &service.RewrapRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not yet support KMSRewrap")
}

// TestQUICCryptoClient_KMSGenerateRandom_NotImplemented verifies KMSGenerateRandom returns not implemented.
func TestQUICCryptoClient_KMSGenerateRandom_NotImplemented(t *testing.T) {
	client := &QUICCryptoClient{client: nil}
	resp, err := client.KMSGenerateRandom(context.Background(), &service.GenerateRandomRequest{})
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not yet support KMSGenerateRandom")
}
