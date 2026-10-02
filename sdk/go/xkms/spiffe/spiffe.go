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

// Package spiffe re-exports go-xkms's sdk/go/spiffe: the SPIFFE ID builder and
// validator (any trust domain and path, per the SPIFFE-ID specification) and
// the tenant-scoped identity shape spiffe://<td>/tenant/<t>/<kind>/<name> the
// stack's CA and mTLS login share.
//
// Every name here is an alias, never a wrapper. Consumers import this package
// rather than go-xkms, so quicbaas's SDK is the only upstream path they know.
package spiffe

import xkmsspiffe "github.com/jeremyhahn/go-xkms/sdk/go/spiffe"

// General SPIFFE IDs.
type (
	// TrustDomain is a validated SPIFFE trust-domain name.
	TrustDomain = xkmsspiffe.TrustDomain

	// WorkloadID is a validated SPIFFE ID: a trust domain plus a path.
	WorkloadID = xkmsspiffe.WorkloadID

	// InvalidIDError reports input that breaks the SPIFFE ID grammar.
	InvalidIDError = xkmsspiffe.InvalidIDError
)

// Tenant-scoped identities.
type (
	// ID is the tenant-scoped identity spiffe://<td>/tenant/<t>/<kind>/<name>.
	ID = xkmsspiffe.ID
)

// Constructors and validators.
var (
	// ParseTrustDomain validates a bare trust-domain name.
	ParseTrustDomain = xkmsspiffe.ParseTrustDomain

	// ParseWorkloadID parses and validates a SPIFFE ID string.
	ParseWorkloadID = xkmsspiffe.ParseWorkloadID

	// WorkloadIDFromURL validates a URI SAN as a SPIFFE ID.
	WorkloadIDFromURL = xkmsspiffe.WorkloadIDFromURL

	// ValidatePath validates a SPIFFE ID path.
	ValidatePath = xkmsspiffe.ValidatePath

	// ValidatePathPrefix validates a path prefix used to scope issuance.
	ValidatePathPrefix = xkmsspiffe.ValidatePathPrefix

	// BuildURI builds the tenant-scoped SPIFFE URI.
	BuildURI = xkmsspiffe.BuildURI

	// ParseID extracts a tenant-scoped identity from a URI.
	ParseID = xkmsspiffe.ParseID

	// TenantFromURIs finds the tenant-scoped identity among URI SANs.
	TenantFromURIs = xkmsspiffe.TenantFromURIs
)

// Tenant identity kinds.
const (
	// KindUser identifies a human user.
	KindUser = xkmsspiffe.KindUser

	// KindService identifies a service identity.
	KindService = xkmsspiffe.KindService
)

// Sentinel errors, matched with errors.Is.
var (
	ErrTrustDomainEmpty      = xkmsspiffe.ErrTrustDomainEmpty
	ErrTrustDomainCharacters = xkmsspiffe.ErrTrustDomainCharacters
	ErrTrustDomainTooLong    = xkmsspiffe.ErrTrustDomainTooLong
	ErrTrustDomainMismatch   = xkmsspiffe.ErrTrustDomainMismatch
	ErrSchemeInvalid         = xkmsspiffe.ErrSchemeInvalid
	ErrSegmentEmpty          = xkmsspiffe.ErrSegmentEmpty
	ErrSegmentDotted         = xkmsspiffe.ErrSegmentDotted
	ErrSegmentCharacters     = xkmsspiffe.ErrSegmentCharacters
	ErrIDTooLong             = xkmsspiffe.ErrIDTooLong
	ErrIDComponents          = xkmsspiffe.ErrIDComponents
	ErrPathPrefixInvalid     = xkmsspiffe.ErrPathPrefixInvalid
	ErrTenantEmpty           = xkmsspiffe.ErrTenantEmpty
	ErrKindInvalid           = xkmsspiffe.ErrKindInvalid
	ErrNameEmpty             = xkmsspiffe.ErrNameEmpty
	ErrNoTenantSPIFFEID      = xkmsspiffe.ErrNoTenantSPIFFEID
)
