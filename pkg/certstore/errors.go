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

// Package certstore provides certificate storage backed by QuicBaaS's KVStore.
package certstore

import (
	dragonerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// ErrNilKVStore indicates the KVStore is nil.
var ErrNilKVStore = dragonerrors.New(
	dragonerrors.ErrInvalidInput, "certstore.New", "kvstore cannot be nil")

// ErrEmptyCertID indicates the certificate ID is empty.
var ErrEmptyCertID = dragonerrors.New(
	dragonerrors.ErrInvalidInput, "certstore", "certificate id cannot be empty")

// ErrNilCertificate indicates the certificate is nil.
var ErrNilCertificate = dragonerrors.New(
	dragonerrors.ErrInvalidInput, "certstore", "certificate cannot be nil")

// ErrEmptyCertChain indicates the certificate chain is empty.
var ErrEmptyCertChain = dragonerrors.New(
	dragonerrors.ErrInvalidInput, "certstore", "certificate chain cannot be empty")

// ErrNilCertInChain indicates a nil certificate was found in the chain.
var ErrNilCertInChain = dragonerrors.New(
	dragonerrors.ErrInvalidInput, "certstore", "certificate chain contains nil certificate")

// ErrCertNotFound indicates the certificate was not found.
var ErrCertNotFound = dragonerrors.New(
	dragonerrors.ErrNotFound, "certstore", "certificate not found")

// ErrCertChainNotFound indicates the certificate chain was not found.
var ErrCertChainNotFound = dragonerrors.New(
	dragonerrors.ErrNotFound, "certstore", "certificate chain not found")

// ErrCertCorrupted indicates the stored certificate data is corrupted.
var ErrCertCorrupted = dragonerrors.New(
	dragonerrors.ErrDataLoss, "certstore", "stored certificate data is corrupted")

// ErrChainCorrupted indicates the stored certificate chain data is corrupted.
var ErrChainCorrupted = dragonerrors.New(
	dragonerrors.ErrDataLoss, "certstore", "stored certificate chain data is corrupted")

// ErrStoragePut indicates a failure writing to the KVStore.
var ErrStoragePut = dragonerrors.New(
	dragonerrors.ErrStorage, "certstore", "failed to write certificate to storage")

// ErrStorageDelete indicates a failure deleting from the KVStore.
var ErrStorageDelete = dragonerrors.New(
	dragonerrors.ErrStorage, "certstore", "failed to delete certificate from storage")

// ErrStorageList indicates a failure listing from the KVStore.
var ErrStorageList = dragonerrors.New(
	dragonerrors.ErrStorage, "certstore", "failed to list certificates from storage")
