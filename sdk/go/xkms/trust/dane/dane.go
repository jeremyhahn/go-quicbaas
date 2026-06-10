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

// Package dane re-exports go-xkms's canonical DANE TLSA surface so
// downstream callers (go-trusted-platform's DNS-backed DANE publisher)
// derive and model TLSA records through the SDK path rather than
// reaching into go-xkms or go-truststrap directly.
//
// All symbols are pure aliases of `xkms/sdk/go/dane`. xkms provides NO
// DNS implementation — publishing TLSA records is owned by the
// embedding application.
package dane

import xkmsdane "github.com/jeremyhahn/go-xkms/sdk/go/dane"

// Re-exported types.
type (
	// Record is a fully-formed DANE TLSA resource record.
	Record = xkmsdane.Record

	// DANEPublisher publishes DANE TLSA records to a DNS provider.
	DANEPublisher = xkmsdane.DANEPublisher
)

// DefaultTTL is the TTL (seconds) applied to records produced by
// BuildRecords.
const DefaultTTL = xkmsdane.DefaultTTL

// RFC 6698 TLSA field constants.
const (
	// UsageDANETA is Certificate Usage 2 (DANE-TA): trust anchor assertion.
	UsageDANETA = xkmsdane.UsageDANETA

	// UsageDANEEE is Certificate Usage 3 (DANE-EE): end-entity assertion.
	UsageDANEEE = xkmsdane.UsageDANEEE

	// SelectorFullCert is Selector 0: match the full certificate.
	SelectorFullCert = xkmsdane.SelectorFullCert

	// SelectorSPKI is Selector 1: match the SubjectPublicKeyInfo.
	SelectorSPKI = xkmsdane.SelectorSPKI

	// MatchingSHA256 is Matching Type 1: SHA-256 of the selected content.
	MatchingSHA256 = xkmsdane.MatchingSHA256

	// MatchingSHA512 is Matching Type 2: SHA-512 of the selected content.
	MatchingSHA512 = xkmsdane.MatchingSHA512
)

// Re-exported errors.
var (
	// ErrNoDANEPublisher is returned when a DANE publication is requested
	// but no DANEPublisher has been registered.
	ErrNoDANEPublisher = xkmsdane.ErrNoDANEPublisher

	// ErrInvalidCertificate is returned when a nil or malformed certificate
	// is supplied to a TLSA derivation function.
	ErrInvalidCertificate = xkmsdane.ErrInvalidCertificate
)

// Re-exported derivation functions.
var (
	// BuildRecords derives the common set of DANE TLSA records for cert at
	// the given hostname/port.
	BuildRecords = xkmsdane.BuildRecords

	// ComputeTLSAData computes the RFC 6698 Certificate Association Data
	// for cert using the given selector and matching type.
	ComputeTLSAData = xkmsdane.ComputeTLSAData
)
